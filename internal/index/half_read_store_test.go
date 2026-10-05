package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A store can be half-read: some sessions arrive, one file is refused. The line
// carried the refused *lines* and the missing-tool reason and said nothing
// about a refused file, so a store that gave up ten sessions and lost three
// tasks read exactly like a store with nothing wrong (#2236).
func TestAStoreNarratesTheFilesItRefusedBesideTheSessionsItRead(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	t.Setenv("MSS_CLAUDE_ROOT", filepath.Join(tmp, "claude"))
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "codex"))
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(tmp, "none.db"))
	claude := filepath.Join(tmp, "claude")
	t.Setenv("MSS_CLAUDE_ROOT", claude)
	proj := filepath.Join(claude, "-work-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}

	stamp := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	good := fmt.Sprintf(`{"type":"user","sessionId":"s1","timestamp":%q,"cwd":"/work/app",`+
		`"message":{"role":"user","content":"the pool timed out during the migration"}}`, stamp)
	if err := os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(good+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A transcript that cannot be opened at all: the walk sees the name, the
	// pass refuses the file.
	bad := filepath.Join(proj, "s2.jsonl")
	if err := os.WriteFile(bad, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o600) })

	dir := filepath.Join(tmp, "index.db")
	var out strings.Builder
	if err := Ensure(dir, "", true, &out); err != nil {
		t.Fatal(err)
	}
	said := out.String()

	// The premise: the store did produce a session, so the line exists and the
	// silence below is about the other file.
	if !strings.Contains(said, "claude: 1 session") {
		t.Fatalf("the readable session did not land, so this measures nothing:\n%s", said)
	}
	health := IngestHealth(dir)
	if health["claude"].FailedFiles != 1 {
		t.Fatalf("the refused file was not recorded, so this measures nothing: %v", health)
	}
	if !strings.Contains(said, "path") {
		t.Errorf("the line says nothing about the file it could not read:\n%s", said)
	}
}
