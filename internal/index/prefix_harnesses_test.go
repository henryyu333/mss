package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two harnesses can carry the same id, and there the advice "use a longer
// prefix" cannot be followed — the ids are the same string (#719).
func TestPrefixHarnesses(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, filepath.Join(tmp, "home"))
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("MSS_CLAUDE_ROOT", filepath.Join(tmp, "claude"))
	t.Setenv("MSS_PI_ROOT", filepath.Join(tmp, "pi"))
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "codex"))
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(tmp, "opencode.db"))
	claude := filepath.Join(tmp, "claude", "proj-p")
	pi := filepath.Join(tmp, "pi", "proj-z")
	for _, d := range []string{claude, pi} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(claude, "abc12345.jsonl"),
		`{"type":"user","sessionId":"abc12345","timestamp":"2026-07-20T10:00:00Z","message":{"role":"user","content":"claude side"}}`)
	write(filepath.Join(claude, "abc99999.jsonl"),
		`{"type":"user","sessionId":"abc99999","timestamp":"2026-07-21T10:00:00Z","message":{"role":"user","content":"other claude session"}}`)
	write(filepath.Join(pi, "abc12345.jsonl"),
		`{"type":"session","version":3,"id":"abc12345","timestamp":"2026-07-25T10:00:00Z","cwd":"/tmp/proj-z"}`+"\n"+
			`{"type":"message","id":"m1","timestamp":"2026-07-25T10:00:01Z","message":{"role":"user","content":[{"type":"text","text":"pi side"}]}}`)
	dir := filepath.Join(tmp, "index.db")
	if err := Ensure(dir, "", true, nil); err != nil {
		t.Fatal(err)
	}

	if got := PrefixHarnesses(dir, "abc12345"); strings.Join(got, "|") != "claude|pi" {
		t.Errorf("shared id → %v", got)
	}
	// A prefix is not an id: "abc" matches three sessions but names none.
	if got := PrefixHarnesses(dir, "abc"); len(got) != 0 {
		t.Errorf("prefix → %v", got)
	}
	if got := PrefixHarnesses(dir, "abc99999"); strings.Join(got, "|") != "claude" {
		t.Errorf("unique id → %v", got)
	}
	if got := PrefixHarnesses(dir, ""); got != nil {
		t.Errorf("empty → %v", got)
	}
	if got := PrefixHarnesses(dir, "nothing-like-this"); len(got) != 0 {
		t.Errorf("missing → %v", got)
	}
}
