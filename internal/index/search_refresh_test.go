package index

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henryyu333/mss/internal/query"
)

// EnsureForSearch is the contract every reading surface leans on: the answer
// is computed over what is on disk now, not over a snapshot that predates the
// question. The cases that used to defer the work — a large tail, a deleted
// file, a version bump — refresh in place and the search that follows sees
// the new state.

// A tail bigger than the old 8 MiB inline cap used to split the refresh:
// search answered from the pre-append index and a detached process was
// supposed to finish the job — a process nothing started, so the stale flag
// persisted across runs. Now the refresh happens before the answer.
func TestSearchSeesLargeTailAppend(t *testing.T) {
	root, dir := allHarnessEnv(t)
	path := filepath.Join(root, "claude", "project", "s.jsonl")
	writeLines(t, path, claudeLine("s1", "2026-01-01T00:01:00Z", "before"))
	o := query.Options{Query: "before", All: true}
	if err := EnsureForSearch(dir, o, false, io.Discard); err != nil {
		t.Fatal(err)
	}

	var tail strings.Builder
	for i := 0; i < 2000; i++ {
		tail.WriteString(claudeLine("s1", "2026-01-01T00:02:00Z", strings.Repeat("x", 5000)))
	}
	// The tail carries the thing being searched for, at the end of ~10 MB of
	// filler: past the old cap, this append is what proved the refresh ran.
	tail.WriteString(claudeLine("s1", "2026-01-01T00:03:00Z", "aftermarker"))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(tail.String()); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if err := EnsureForSearch(dir, o, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	o.Query = "aftermarker"
	ss, err := Search(dir, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 1 {
		t.Fatalf("the appended turn was not searchable: %d sessions", len(ss))
	}
}

// A removed transcript tree cannot be absorbed by appending; the refresh
// rewrites and the answer loses the sessions that went away. One file
// missing inside a live store is kept back on purpose — that is the client
// deleting the conversations it chose to, which mss reports as still
// searchable — so the rewrite case is the whole store directory going.
func TestSearchAfterStoreTreeRemoved(t *testing.T) {
	root, dir := allHarnessEnv(t)
	writeLines(t, filepath.Join(root, "claude", "projA", "s1.jsonl"), claudeLine("s1", "2026-01-01T00:01:00Z", "gonemarker"))
	writeLines(t, filepath.Join(root, "claude", "projB", "s2.jsonl"), claudeLine("s2", "2026-01-01T00:02:00Z", "keptmarker"))
	o := query.Options{All: true}
	if err := EnsureForSearch(dir, o, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "claude", "projA")); err != nil {
		t.Fatal(err)
	}
	if err := EnsureForSearch(dir, o, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	o.Query = "gonemarker"
	ss, err := Search(dir, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 0 {
		t.Fatalf("a removed store still answered: %d sessions", len(ss))
	}
	o.Query = "keptmarker"
	ss, err = Search(dir, o)
	if err != nil || len(ss) != 1 {
		t.Fatalf("the surviving file did not: %d %v", len(ss), err)
	}
}

// A manifest written under an older content version is rebuilt before the
// answer — the version bump that used to send the caller away to wait now
// happens inside the call.
func TestSearchRebuildsAcrossVersionBump(t *testing.T) {
	root, dir := allHarnessEnv(t)
	writeLines(t, filepath.Join(root, "claude", "project", "s.jsonl"), claudeLine("s1", "2026-01-01T00:01:00Z", "vermarker"))
	o := query.Options{Query: "vermarker", All: true}
	if err := EnsureForSearch(dir, o, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	m, err := readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.Version = version - 1
	if err := writeManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	// The same call shape a search makes: it must come back having rebuilt,
	// not having deferred.
	if err := EnsureForSearch(dir, o, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	m2, err := readManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Version != version {
		t.Fatalf("the bumped store was not rebuilt in place: version %d", m2.Version)
	}
	ss, err := Search(dir, o)
	if err != nil || len(ss) != 1 {
		t.Fatalf("answer after rebuild: %d %v", len(ss), err)
	}
}

// A lock held by another process does not hide the answer: the snapshot
// read answers from what is on disk without taking it, while the refresh
// waits its turn. This pins the read half — Search never blocks on the lock.
func TestSearchUnderHeldLockReadsSnapshot(t *testing.T) {
	root, dir := allHarnessEnv(t)
	writeLines(t, filepath.Join(root, "claude", "project", "s.jsonl"), claudeLine("s1", "2026-01-01T00:01:00Z", "lockmarker"))
	o := query.Options{Query: "lockmarker", All: true}
	if err := EnsureForSearch(dir, o, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	unlock, ok, err := tryLockDir(dir)
	if err != nil || !ok {
		t.Fatalf("taking the lock: %v %v", ok, err)
	}
	defer unlock()
	// The refresh path refuses to wait on a held lock and returns busy=false
	// with the read still possible — the same guarantee the snapshot read
	// itself makes.
	ss, err := Search(dir, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 1 {
		t.Fatalf("a held lock hid the answer: %d sessions", len(ss))
	}
}
