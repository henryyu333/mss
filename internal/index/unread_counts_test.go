package index

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// A transcript the index has no state for at all. Audited on a real store: 1,533
// files tracked and ten never tracked, five of them written eight weeks
// earlier, with no surface saying so — a store's session count reads as
// "nothing written yet" rather than "files never opened" (#3747).
func TestUnreadCountsNameTheTranscriptsNeverRead(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	setHome(t, home)
	t.Setenv("USERPROFILE", home)
	claude := filepath.Join(tmp, "claude")
	t.Setenv("MSS_CLAUDE_ROOT", claude)
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "codex"))
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(tmp, "opencode.db"))
	dir := filepath.Join(tmp, "index.db")

	writeLines(t, filepath.Join(claude, "-p-app", "one.jsonl"),
		claudeLine("aaa", "2026-09-01T10:00:00Z", "the pool ran dry under load"))
	if err := Ensure(dir, "", true, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := HarnessUnreadCounts(dir); len(got) != 0 {
		t.Fatalf("a freshly built index has unread transcripts: %v", got)
	}

	// A second transcript arrives and nothing indexes it yet.
	writeLines(t, filepath.Join(claude, "-p-app", "two.jsonl"),
		claudeLine("bbb", "2026-09-02T10:00:00Z", "the cache eviction was wrong"))
	if got := HarnessUnreadCounts(dir)["claude"]; got != 1 {
		t.Fatalf("claude unread = %d, want 1", got)
	}

	// And the store this came from: one whose files all arrived after the
	// index was built, so it holds no session at all. Nothing in the manifest
	// speaks for it.
	other := filepath.Join(tmp, "pi")
	t.Setenv("MSS_PI_ROOT", other)
	writeLines(t, filepath.Join(other, "s-q1.jsonl"),
		`{"type":"session","version":3,"id":"q1","timestamp":"2026-09-03T10:00:00Z","cwd":"/tmp/app"}`+"\n",
		`{"type":"message","id":"m1","timestamp":"2026-09-03T10:00:01Z","message":{"role":"user","content":[{"type":"text","text":"the retry budget was wrong"}]}}`+"\n")
	got := HarnessUnreadCounts(dir)
	if got["pi"] == 0 {
		t.Fatalf("a store with nothing indexed reported no unread transcripts: %v", got)
	}

	// After a pass the count goes to nothing, which is what makes it a
	// prompt rather than a permanent complaint.
	if err := Ensure(dir, "", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := HarnessUnreadCounts(dir); len(got) != 0 {
		t.Fatalf("after an index pass, unread = %v", got)
	}
}

// A directory or a symlink in a store is not a transcript the index means to
// hold, and counting it would make the number unactionable.
func TestUnreadCountsIgnoreWhatTheWalkDeclines(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	setHome(t, home)
	t.Setenv("USERPROFILE", home)
	claude := filepath.Join(tmp, "claude")
	t.Setenv("MSS_CLAUDE_ROOT", claude)
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "codex"))
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(tmp, "opencode.db"))
	dir := filepath.Join(tmp, "index.db")
	writeLines(t, filepath.Join(claude, "-p-app", "one.jsonl"),
		claudeLine("aaa", "2026-09-01T10:00:00Z", "the pool ran dry under load"))
	if err := Ensure(dir, "", true, io.Discard); err != nil {
		t.Fatal(err)
	}
	// A symlink to the transcript already indexed: the walk declines it, so it
	// is not unread work.
	link := filepath.Join(claude, "-p-app", "copy.jsonl")
	if err := os.Symlink(filepath.Join(claude, "-p-app", "one.jsonl"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got := HarnessUnreadCounts(dir); len(got) != 0 {
		t.Fatalf("a symlink counted as an unread transcript: %v", got)
	}
}

// A store mss cannot read at all is a different report, and the count must not
// speak for it: an index that was never built has no manifest to compare
// against.
func TestUnreadCountsSayNothingWithoutAnIndex(t *testing.T) {
	tmp := t.TempDir()
	if got := HarnessUnreadCounts(filepath.Join(tmp, "index.db")); got != nil {
		t.Fatalf("unread counts without an index = %v, want nothing", got)
	}
}
