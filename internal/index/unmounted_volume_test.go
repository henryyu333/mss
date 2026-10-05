package index

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/henryyu333/mss/internal/query"
)

// A store that was indexed off a removable disk left the index the moment the
// disk was unmounted: the first search said the volume was gone, evicted the
// records, and every search after that reported "no matches" with no line at
// all — the transcripts were already indexed and had no business disappearing.
func TestUnmountedVolumeKeepsWhatItAlreadyIndexed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mount points are drive letters on windows")
	}
	tmp := hermeticIndexEnv(t)
	parent := filepath.Join(tmp, "Volumes")
	vol := filepath.Join(parent, "MssVol")
	store := filepath.Join(vol, "claude")
	t.Setenv("MSS_CLAUDE_ROOT", store)
	old := mountParents
	mountParents = []string{parent}
	t.Cleanup(func() { mountParents = old })

	proj := filepath.Join(store, "-tmp-proj")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","sessionId":"vol1","timestamp":"2026-07-01T10:00:00Z","cwd":"/tmp/proj","message":{"role":"user","content":"the ledger checksum must be recomputed after compaction"}}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, "s.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(tmp, "idx")
	if err := Ensure(dir, "", false, nil); err != nil {
		t.Fatal(err)
	}
	q := query.Options{Query: "checksum", Limit: 5}
	if ss, err := Search(dir, q); err != nil || len(ss) != 1 {
		t.Fatalf("mounted: %d sessions err=%v, want the transcript", len(ss), err)
	}

	// Unmount: the mount point stays, the volume under it does not.
	if err := os.RemoveAll(vol); err != nil {
		t.Fatal(err)
	}
	for run := 1; run <= 2; run++ {
		var buf bytes.Buffer
		if err := Ensure(dir, "", false, &buf); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if !strings.Contains(buf.String(), vol+" is not mounted") {
			t.Errorf("run %d said %q, want the volume named as not mounted", run, buf.String())
		}
		ss, err := Search(dir, q)
		if err != nil || len(ss) != 1 {
			t.Fatalf("run %d: %d sessions err=%v, want the transcript still searchable", run, len(ss), err)
		}
	}

	// A store the user deleted is still a deletion: those records go.
	t.Setenv("MSS_CLAUDE_ROOT", filepath.Join(tmp, "claude"))
	store2 := filepath.Join(tmp, "claude", "projects", "-tmp-proj")
	if err := os.MkdirAll(store2, 0o700); err != nil {
		t.Fatal(err)
	}
	sess := `{"type":"user","sessionId":"gone1","timestamp":"2026-07-02T10:00:00Z","cwd":"/tmp/proj","message":{"role":"user","content":"backoff ladder in the uploader"}}` + "\n"
	if err := os.WriteFile(filepath.Join(store2, "s.jsonl"), []byte(sess), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir, "", false, nil); err != nil {
		t.Fatal(err)
	}
	if ss, err := Search(dir, query.Options{Query: "backoff", Limit: 5}); err != nil || len(ss) != 1 {
		t.Fatalf("deleted-store setup: %d sessions err=%v", len(ss), err)
	}
	if err := os.RemoveAll(filepath.Join(tmp, "claude")); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir, "", false, nil); err != nil {
		t.Fatal(err)
	}
	if ss, err := Search(dir, query.Options{Query: "backoff", Limit: 5}); err != nil || len(ss) != 0 {
		t.Fatalf("deleted store: %d sessions err=%v, want none", len(ss), err)
	}
}

// macOS mounts a disk as "/Volumes/Name 1" when something already sits on
// "/Volumes/Name". mss evicted what it had indexed from that volume and told
// the user to reconnect a disk that was already connected.
func TestRenamedMountIsNamedInsteadOfEvicted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mount points are drive letters on windows")
	}
	tmp := hermeticIndexEnv(t)
	parent := filepath.Join(tmp, "Volumes")
	store := filepath.Join(parent, "MssIdx", "claude")
	t.Setenv("MSS_CLAUDE_ROOT", store)
	old := mountParents
	mountParents = []string{parent}
	t.Cleanup(func() { mountParents = old })

	proj := filepath.Join(store, "-tmp-proj")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","sessionId":"ren1","timestamp":"2026-07-01T10:00:00Z","cwd":"/tmp/proj","message":{"role":"user","content":"the compaction window is 15 minutes"}}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, "s.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(tmp, "idx")
	if err := Ensure(dir, "", false, nil); err != nil {
		t.Fatal(err)
	}
	q := query.Options{Query: "compaction", Limit: 5}
	if ss, err := Search(dir, q); err != nil || len(ss) != 1 {
		t.Fatalf("before the remount: %d sessions err=%v", len(ss), err)
	}

	// The volume comes back one name over; an empty one takes its old place.
	renamed := filepath.Join(parent, "MssIdx 1")
	if err := os.Rename(filepath.Join(parent, "MssIdx"), renamed); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(parent, "MssIdx"), 0o700); err != nil {
		t.Fatal(err)
	}
	for run := 1; run <= 2; run++ {
		var buf bytes.Buffer
		if err := Ensure(dir, "", false, &buf); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if !strings.Contains(buf.String(), "is mounted as "+filepath.Join(renamed, "claude")) {
			t.Errorf("run %d said %q, want the path the volume moved to", run, buf.String())
		}
		ss, err := Search(dir, q)
		if err != nil || len(ss) != 1 {
			t.Fatalf("run %d: %d sessions err=%v, want the transcript still searchable", run, len(ss), err)
		}
	}
	if got := renamedMount(filepath.Join(parent, "NoSuchVolume", "mss")); got != "" {
		t.Errorf("renamedMount invented %q", got)
	}
}
