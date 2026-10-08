package index

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A store can be half-read: some sessions arrive, one file is refused. The line
// carried the refused *lines* and the missing-tool reason and said nothing
// about a refused file, so a store that gave up ten sessions and lost three
// tasks read exactly like a store with nothing wrong (#2236).
func TestAStoreNarratesTheFilesItRefusedBesideTheSessionsItRead(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("file permissions do not stop a read here")
	}
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

	assertHalfReadStore(t, dir, "claude", said)
}

// A corrupt SQLite store fails on every platform without relying on chmod.
// Cursor's readable transcript must still land beside the failed store.
func TestAStoreNarratesACorruptDatabaseBesideTheSessionsItRead(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	tmp := hermeticIndexEnv(t)
	db := filepath.Join(os.Getenv("MSS_CURSOR_ROOT"), "globalStorage", "state.vscdb")
	write(t, db, "not a sqlite database\n")
	transcript := filepath.Join(os.Getenv("MSS_CURSOR_CLI_ROOT"), "projects",
		"work-app", "agent-transcripts", "s1.jsonl")
	write(t, transcript, `{"role":"user","message":{"content":[{"type":"text","text":"the pool timed out during the migration"}]}}`+"\n")

	dir := filepath.Join(tmp, "index.db")
	var out strings.Builder
	if err := Ensure(dir, "cursor", true, &out); err != nil {
		t.Fatal(err)
	}
	assertHalfReadStore(t, dir, "cursor", out.String())
}

func assertHalfReadStore(t *testing.T, dir, harness, said string) {
	t.Helper()
	// Without a readable session this would only exercise the empty-store
	// report, not the half-read store's narration.
	if !strings.Contains(said, harness+": 1 session") {
		t.Fatalf("the readable session did not land, so this measures nothing:\n%s", said)
	}
	health := IngestHealth(dir)
	if health[harness].FailedFiles != 1 {
		t.Fatalf("the refused file was not recorded, so this measures nothing: %v", health)
	}
	if health[harness].LastError == "" {
		t.Errorf("the refused file has no error detail: %v", health)
	}
	if !strings.Contains(said, "1 path could not be read at all") {
		t.Errorf("the line says nothing about the file it could not read:\n%s", said)
	}
}
