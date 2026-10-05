package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henryyu333/mss/internal/sources"
)

// The run narrates a store and doctor filed the same fact under the file kind —
// "codex-history" where the screen said "codex" — while the documentation calls
// the key a harness. A reader carrying a number from one to the other had to
// know a mapping mss never showed them (#2234).
func TestIngestHealthIsKeyedByTheStoreTheRunNames(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	claude := filepath.Join(tmp, "claude")
	t.Setenv("MSS_CLAUDE_ROOT", claude)
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "codex"))
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(tmp, "none.db"))
	codex := filepath.Join(tmp, "codex")
	t.Setenv("MSS_CODEX_ROOT", codex)

	proj := filepath.Join(claude, "-work-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	good := fmt.Sprintf(`{"type":"user","sessionId":"s1","timestamp":%q,"cwd":"/work/app",`+
		`"message":{"role":"user","content":"the pool timed out during the migration"}}`, stamp)
	if err := os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(good+"\nnot json at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(codex, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codex, "history.jsonl"), []byte("not json at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(tmp, "index.db")
	var out strings.Builder
	if err := Ensure(dir, "", true, &out); err != nil {
		t.Fatal(err)
	}
	health := IngestHealth(dir)
	// The premise: both stores are in there, or the names below prove nothing.
	if len(health) < 2 {
		t.Fatalf("only %d store reported anything:\n%s\n%v", len(health), out.String(), health)
	}
	for name := range health {
		if sources.HarnessForKind(name) != name {
			t.Errorf("doctor files this under %q, which is a file kind; the run says %q",
				name, sources.HarnessForKind(name))
		}
	}
	// And the store the run named is findable by that name.
	if _, ok := health["codex"]; !ok {
		t.Errorf("nothing is filed under the name the run printed: %v", health)
	}
}
