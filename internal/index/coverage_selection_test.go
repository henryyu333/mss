package index

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/henryyu333/mss/internal/query"
)

// A store the selection silences is absent from coverage entirely: its files
// are not read, but they are not reported unread either — that line belongs
// to a store mss is trying to read. The converse runs on the same index: a
// selected store whose files arrived after the build is unread, because mss
// meant to read it and could not.
func TestSearchCoverageFollowsTheStoreSelection(t *testing.T) {
	root, dir := allHarnessEnv(t)
	writeLines(t, filepath.Join(root, "claude", "proj", "s.jsonl"),
		claudeLine("s1", "2026-01-01T00:01:00Z", "selectmarker"))
	if err := EnsureForSearch(dir, query.Options{All: true}, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	// The codex store grew after the build: present on disk, absent from the
	// manifest — the one selected store that is unread.
	writeLines(t, filepath.Join(root, "codex", "history.jsonl"), "{}\n")

	t.Setenv("MSS_STORES", "claude,codex")
	cov := SearchCoverage(dir)
	if len(cov.Unread) != 1 || cov.Unread[0] != "codex: not read" {
		t.Fatalf("a selected store with files on disk was not reported unread: %v", cov.Unread)
	}
	if cov.Complete {
		t.Fatal("coverage claimed complete with an unread store")
	}

	// Silencing codex takes its row with it — the files are still there, but
	// nothing mss runs is meant to read them.
	t.Setenv("MSS_STORES", "claude")
	cov = SearchCoverage(dir)
	if len(cov.Unread) != 0 {
		t.Fatalf("a silenced store was reported unread: %v", cov.Unread)
	}
	if !cov.Complete {
		t.Fatalf("coverage incomplete over the stores mss was asked to read: %+v", cov)
	}
}

// The selection reads the environment on every call, and so do the roots:
// moving MSS_PI_ROOT between two reports changes what "unread" names, because
// the report is about the store as it is now, not as the last call saw it.
func TestSearchCoverageSeesRootChangesBetweenCalls(t *testing.T) {
	root, dir := allHarnessEnv(t)
	writeLines(t, filepath.Join(root, "claude", "proj", "s.jsonl"),
		claudeLine("s1", "2026-01-01T00:01:00Z", "rootmarker"))
	if err := EnsureForSearch(dir, query.Options{All: true}, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if cov := SearchCoverage(dir); !cov.Complete {
		t.Fatalf("an untouched pi root was already unread: %+v", cov)
	}
	pi2 := filepath.Join(root, "pi-elsewhere")
	writeLines(t, filepath.Join(pi2, "s.jsonl"),
		`{"type":"session","version":3,"id":"q2","timestamp":"2026-09-04T10:00:00Z","cwd":"/tmp/app"}`+"\n")
	t.Setenv("MSS_PI_ROOT", pi2)
	cov := SearchCoverage(dir)
	for _, u := range cov.Unread {
		if u == "pi: not read" {
			return
		}
	}
	t.Fatalf("the moved pi root was not reported unread: %v", cov.Unread)
}

// The health fold classifies on the same selection as the path scan: a
// silenced store keeps neither its read marks nor its failures, and lifting
// the variable between calls puts them back. A lookup frozen at build time
// would answer the second call the first call's way.
func TestHealthFromFilesFollowsTheStoreSelection(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	pi := filepath.Join(tmp, "pi")
	t.Setenv("MSS_PI_ROOT", pi)
	p := filepath.Join(pi, "s.jsonl")
	files := map[string]FileIngest{
		p: {Error: "open s.jsonl: permission denied", Malformed: 2},
	}
	t.Setenv("MSS_STORES", "claude")
	if got := healthFromFiles(files); got != nil {
		t.Fatalf("a silenced store kept its ingest health: %v", got)
	}
	t.Setenv("MSS_STORES", "")
	got := healthFromFiles(files)
	h, ok := got["pi"]
	if !ok {
		t.Fatalf("lifting the selection lost the pi entry: %v", got)
	}
	if h.FailedFiles != 1 || h.MalformedLines != 2 {
		t.Fatalf("health folded to %+v, want one failed file and two bad lines", h)
	}
	if h.LastError != "open s.jsonl: permission denied" {
		t.Fatalf("last error %q, want the file's own", h.LastError)
	}
}
