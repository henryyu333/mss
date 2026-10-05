package sources

import (
	"os"
	"path/filepath"
	"testing"
)

// A pass that lands while the client is appending sees the last line cut
// short with no newline yet. That is the client still writing, not a broken
// line, and the next pass reads it whole; counting it put "1 line skipped,
// mss could not read it" on an ordinary update (#4276). A line that did end
// is still counted, so the note keeps its meaning.
func TestALineTheClientIsStillWritingIsNotCalledUnreadable(t *testing.T) {
	dir := t.TempDir()
	whole := `{"type":"user","sessionId":"s1","uuid":"u1","timestamp":"2026-10-01T10:00:00Z","message":{"role":"user","content":"tune the backoff jitter"}}` + "\n"
	cut := `{"type":"assistant","sessionId":"s1","uuid":"u2","timestamp":"2026-10-01T10:00:05Z","message":{"role":"assi`
	for _, tc := range []struct {
		name  string
		parse func(string) error
	}{
		{"generic", func(p string) error { return scanJSONLFromOffset(p, 0, func(map[string]any) {}) }},
		{"claude", func(p string) error { _, err := ParseClaudeFile(p); return err }},
	} {
		growing := filepath.Join(dir, tc.name+"-growing.jsonl")
		broken := filepath.Join(dir, tc.name+"-broken.jsonl")
		if err := os.WriteFile(growing, []byte(whole+cut), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(broken, []byte(whole+cut+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, p := range []string{growing, broken} {
			if err := tc.parse(p); err != nil {
				t.Fatal(err)
			}
		}
		got := DiagMalformedCounts()
		if got[growing] != 0 {
			t.Errorf("%s: the half-written last line was counted unreadable %d time(s)", tc.name, got[growing])
		}
		if got[broken] != 1 {
			t.Errorf("%s: a cut line that ended in a newline counted %d time(s), want 1", tc.name, got[broken])
		}
	}
}
