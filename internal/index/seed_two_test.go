package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/henryyu333/mss/internal/search"
)

func seedTwoTranscripts(t *testing.T) (dir, claudeRoot, s1, s2 string) {
	t.Helper()
	tmp := t.TempDir()
	claudeRoot = filepath.Join(tmp, "claude")
	proj := filepath.Join(claudeRoot, "-Users-me-mss")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	s1 = filepath.Join(proj, "s1.jsonl")
	s2 = filepath.Join(proj, "s2.jsonl")
	if err := os.WriteFile(s1, []byte(`{"type":"user","sessionId":"s1","timestamp":"2026-06-02T03:04:05Z","message":{"role":"user","content":"the zorblax pool deadlocked"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s2, []byte(`{"type":"user","sessionId":"s2","timestamp":"2026-08-02T03:04:05Z","message":{"role":"user","content":"the quuxbar worker stalled"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	setHome(t, filepath.Join(tmp, "home"))
	t.Setenv("USERPROFILE", filepath.Join(tmp, "home"))
	t.Setenv("MSS_CLAUDE_ROOT", claudeRoot)
	dir = filepath.Join(tmp, "index.db")
	if err := Ensure(dir, "claude", false, nil); err != nil {
		t.Fatal(err)
	}
	return dir, claudeRoot, s1, s2
}

func sessionsFor(t *testing.T, dir, q string) []string {
	t.Helper()
	ss, err := Search(dir, search.Options{Query: q})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.ID)
	}
	return out
}
