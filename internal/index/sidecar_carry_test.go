package index

import (
	"os"
	"path/filepath"
	"testing"
)

// An incremental update replaces the whole index directory, and the path that
// builds it never mines the sidecars — it deliberately never holds the whole
// corpus. So the swap used to delete them: one new session anywhere, and the
// command table was gone until the next full rebuild. Ordinary use, silently
// degraded.
//
// This asserts the file survives and that the table is recomputed from the
// records the update holds, not merely carried stale.
func TestIncrementalUpdateKeepsTheCommandTable(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, filepath.Join(tmp, "home"))
	claude := filepath.Join(tmp, "claude")
	proj := filepath.Join(claude, "-work")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MSS_CLAUDE_ROOT", claude)

	session := func(name, ts string) {
		body := `{"type":"user","sessionId":"` + name + `","cwd":"/w","timestamp":"` + ts + `","message":{"role":"user","content":"restart the database"}}
{"type":"assistant","sessionId":"` + name + `","cwd":"/w","timestamp":"` + ts + `","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"docker compose restart flimbardb"}}]}}
`
		if err := os.WriteFile(filepath.Join(proj, name+".jsonl"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	session("a", "2026-01-02T03:04:05Z")
	session("b", "2026-01-03T03:04:05Z")

	dir := filepath.Join(tmp, "index.db")
	if err := Ensure(dir, "", false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, commandsFile)); err != nil {
		t.Fatalf("full build did not write %s: %v", commandsFile, err)
	}
	use, ok := CommandHistory(dir, "docker compose restart flimbardb")
	if !ok || use.Sessions != 2 {
		t.Fatalf("full build command table = %+v, %v; want 2 sessions", use, ok)
	}

	// One more session is all it takes: this is the incremental path.
	session("c", "2026-01-04T03:04:05Z")
	if err := Ensure(dir, "", false, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, commandsFile)); err != nil {
		t.Errorf("an incremental update dropped %s: %v", commandsFile, err)
	}
	use, ok = CommandHistory(dir, "docker compose restart flimbardb")
	if !ok || use.Sessions != 3 {
		t.Errorf("the command table counted %+v, %v; want 3 sessions", use, ok)
	}

	// Keeping the old row is half of it. A new session is a new file, and a new
	// file never takes the append path, so sessions arriving after the first
	// build are the only source of new rows and all of them come through here.
	session("d", "2026-02-01T03:04:05Z")
	session("e", "2026-02-02T03:04:05Z")
	if err := Ensure(dir, "", false, nil); err != nil {
		t.Fatal(err)
	}
	use, ok = CommandHistory(dir, "docker compose restart flimbardb")
	if !ok || use.Sessions != 5 {
		t.Errorf("a command that became a habit after the full build is missing: %+v, %v", use, ok)
	}
}
