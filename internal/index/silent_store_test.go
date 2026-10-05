package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A store mss read and could not use is the one case where nothing else will
// mention it: the skip line covers a store it could not open, and the count is
// computed and then thrown away for a store that yielded no session because its
// file would not parse (#2229). That is #794's shape, one branch over.
func TestAStoreThatYieldedNothingButRefusedLinesIsNarrated(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	claude := filepath.Join(tmp, "claude")
	t.Setenv("MSS_CLAUDE_ROOT", claude)
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "codex"))
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(tmp, "none.db"))
	codex := filepath.Join(tmp, "codex")
	t.Setenv("MSS_CODEX_ROOT", codex)

	// One healthy store, so the run has a line to print either way and the
	// silence below is about the other one.
	proj := filepath.Join(claude, "-work-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	line := fmt.Sprintf(`{"type":"user","sessionId":"s1","timestamp":%q,"cwd":"/work/app",`+
		`"message":{"role":"user","content":"the pool timed out during the migration"}}`, stamp)
	if err := os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// And one store whose only session will not parse.
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
	said := out.String()

	// The premise: mss did notice, and filed it where doctor reads. Either
	// bucket counts — a task that will not parse is a path mss could not read
	// (#2232), a line inside a JSONL transcript is a line.
	health := IngestHealth(dir)
	var counted int
	for _, e := range health {
		counted += e.MalformedLines + e.FailedFiles
	}
	if counted == 0 {
		t.Fatalf("nothing was recorded as unreadable, so this measures nothing:\n%s\n%v", said, health)
	}
	// The healthy store is narrated, or the run said nothing at all.
	if !strings.Contains(said, "claude: 1 session") {
		t.Fatalf("the run did not narrate the store it read:\n%s", said)
	}
	// And the one it could not use is named too.
	if !strings.Contains(said, "codex") {
		t.Errorf("the run says nothing about the store whose session it refused:\n%s", said)
	}
}
