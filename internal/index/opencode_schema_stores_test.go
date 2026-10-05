package index

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// addOpencodeTurn adds one more message to a session already in an
// opencode-schema store.
func addOpencodeTurn(t *testing.T, db, session, id, text string, createdMillis int64) {
	t.Helper()
	stmts := fmt.Sprintf(`
insert into message values ('%[2]s','%[1]s','{"role":"assistant","time":{"created":%[4]d}}',%[4]d);
insert into part values ('p-%[2]s','%[2]s',json_object('type','text','text','%[3]s','time',json_object('start',%[4]d)));
update session set time_updated=%[4]d where id='%[1]s';
`, session, id, text, createdMillis)
	if out, err := exec.Command("sqlite3", db, stmts).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3 seed: %v %s", err, out)
	}
}

// A write to opencode's database sent every session in it back through the
// pass, which appended them to the records already held: a session doubled on
// each pass after an unrelated write (#4396).
func TestOpencodeSchemaStoresKeepOneCopyOfEachSession(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 CLI not available")
	}
	for _, c := range []struct{ harness, env string }{
		{"opencode", "MSS_OPENCODE_DB"},
	} {
		t.Run(c.harness, func(t *testing.T) {
			tmp := t.TempDir()
			setHome(t, tmp)
			t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "config"))
			t.Setenv("MSS_CLAUDE_ROOT", filepath.Join(tmp, "claude"))
			t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "codex"))
			for _, env := range []string{"MSS_OPENCODE_DB"} {
				t.Setenv(env, filepath.Join(tmp, "none-"+env+".db"))
			}
			db := filepath.Join(tmp, c.harness+".db")
			t.Setenv(c.env, db)

			seedOpencodeSession(t, db, "s1", "fix the retry loop", 1767322800000)
			dir := filepath.Join(tmp, "index.db")
			if err := Ensure(dir, "", true, nil); err != nil {
				t.Fatal(err)
			}
			count := func(id string) int {
				t.Helper()
				s, ok, err := FindByIdentity(dir, c.harness, id)
				if err != nil || !ok {
					t.Fatalf("%s:%s is not in the index (%v)", c.harness, id, err)
				}
				return len(s.Messages)
			}
			if got := count("s1"); got != 1 {
				t.Fatalf("the build holds %d messages for s1, so this measures nothing", got)
			}

			// The client writes the database without touching this session.
			later := time.Now().Add(time.Minute)
			if err := os.Chtimes(db, later, later); err != nil {
				t.Fatal(err)
			}
			var out strings.Builder
			if err := Ensure(dir, "", false, &out); err != nil {
				t.Fatal(err)
			}
			if got := count("s1"); got != 1 {
				t.Fatalf("after a pass over an untouched session it holds %d messages, want 1", got)
			}

			// A new session, and a turn on the old one: each held once, and
			// the old session keeps the turn it had.
			seedOpencodeSession(t, db, "s2", "a second session", 1767326400000)
			addOpencodeTurn(t, db, "s1", "m-s1-2", "the pool was too small", 1767330000000)
			if err := Ensure(dir, "", false, &out); err != nil {
				t.Fatal(err)
			}
			if got := count("s1"); got != 2 {
				t.Errorf("s1 holds %d messages after one more turn, want 2", got)
			}
			if got := count("s2"); got != 1 {
				t.Errorf("s2 holds %d messages, want 1", got)
			}
			if !strings.Contains(out.String(), replacementPassMarker) {
				t.Fatalf("this was not the merge path, so it does not measure what it is about: %q", out.String())
			}
		})
	}
}
