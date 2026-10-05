package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/henryyu333/mss/internal/search"
)

// The same claude conversation can be left in more than one transcript of a
// project — a fork writes a second file for the same session id. Full rebuilds
// and — the harder case — incremental updates that touch both files in one
// pass must not write the shared messages twice. Distinct messages under one
// session key must still merge.
func TestDuplicateFormatSessionsDoNotDuplicateMessages(t *testing.T) {
	tmp := t.TempDir()
	claude := filepath.Join(tmp, "claude")
	proj := filepath.Join(claude, "-tmp-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	head := `{"type":"user","sessionId":"dup1","timestamp":"2026-07-15T10:00:01Z","message":{"role":"user","content":"dupneedle question"}}` + "\n"
	one := filepath.Join(proj, "dup1.jsonl")
	two := filepath.Join(proj, "dup1-fork.jsonl")
	writeTwo := func() {
		if err := os.WriteFile(two, []byte(head), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(one, []byte(head), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTwo()
	setHome(t, t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("MSS_CLAUDE_ROOT", claude)
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "nx"))
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(tmp, "n.db"))
	dir := filepath.Join(tmp, "index.db")
	o := search.Options{Query: "dupneedle", All: true}
	if err := EnsureForSearch(dir, o, false, nil); err != nil {
		t.Fatal(err)
	}
	count := func(what string) int {
		t.Helper()
		ss, err := Search(dir, search.Options{Query: what, All: true})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, s := range ss {
			n += len(s.Messages)
		}
		return n
	}
	if got := count("dupneedle question"); got != 1 {
		t.Fatalf("after rebuild: %d copies, want 1", got)
	}

	// Touch both transcripts in one incremental pass.
	f, err := os.OpenFile(one, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"assistant","sessionId":"dup1","timestamp":"2026-07-15T10:06:00Z","message":{"role":"assistant","content":"dupneedle again"}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	writeTwo() // rewrite -> mtime change on the twin
	if err := EnsureForSearch(dir, o, false, nil); err != nil {
		t.Fatal(err)
	}
	if got := count("dupneedle question"); got != 1 {
		t.Fatalf("after dual-file incremental: %d copies, want 1", got)
	}
	if got := count("dupneedle again"); got != 1 {
		t.Fatalf("new message: %d copies, want 1", got)
	}
}
