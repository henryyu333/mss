package index

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henryyu333/mss/internal/search"
)

// runStoreSQL feeds a script to the sqlite3 CLI on stdin: a fixture that opens
// with a `--` comment would otherwise be read as an option.
func runStoreSQL(t *testing.T, db, sql string) {
	t.Helper()
	cmd := exec.Command("sqlite3", db)
	cmd.Stdin = strings.NewReader(sql)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v: %s", err, out)
	}
}

// stampedStoreEnv is hermeticIndexEnv plus the grok store it does not pin, so
// a contributor with real grok history runs the same test as CI and the case
// sees only the store it wrote.
func stampedStoreEnv(t *testing.T) string {
	t.Helper()
	tmp := hermeticIndexEnv(t)
	t.Setenv("MSS_GROK_DB", filepath.Join(tmp, "none-grok.db"))
	t.Setenv("MSS_GROK_ROOT", filepath.Join(tmp, "none-grok"))
	return tmp
}

// Every store mss reads through SQLite is stamped with a watermark and parsed
// from it (#2075), so the second pass over one of them has three things to get
// right at once: the session that arrived is indexed, the sessions that did not
// move are still there, and a session that gained a turn holds the turns it had
// as well as the new one — once each.
//
// The stamped-late store (grok) is the case here. The turn below the watermark
// is what a partial parse cannot hand back, and the record count is what says
// whether the pass replaced the session or added to it.
func TestEveryStampedStoreSurvivesTheSecondPass(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	cases := []struct {
		harness string
		// seed writes the store and returns its path: two sessions, the older
		// of which nothing touches again, and an opening turn in the newer.
		seed func(t *testing.T, tmp string) string
		// grow adds a session and a turn to the one that already existed.
		grow func(t *testing.T, db string)
		// key is the session that gains a turn, and turns the count it should
		// hold afterwards.
		key   string
		turns int
	}{{
		harness: "grok",
		seed: func(t *testing.T, tmp string) string {
			db := filepath.Join(tmp, "grok.db")
			t.Setenv("MSS_GROK_DB", db)
			runStoreSQL(t, db, `CREATE TABLE workspaces (id TEXT PRIMARY KEY, canonical_path TEXT);
CREATE TABLE sessions (id TEXT PRIMARY KEY, workspace_id TEXT, title TEXT, cwd_last TEXT, created_at TEXT);
CREATE TABLE messages (session_id TEXT, seq INTEGER, role TEXT, message_json TEXT, created_at TEXT);
INSERT INTO sessions VALUES ('gq','w','Quiet one','/work/api','2026-07-26T10:00:00.000Z');
INSERT INTO messages VALUES ('gq',0,'user','{"content":"needlequiet a session nobody touches again"}','2026-07-26T10:00:00.000Z');
INSERT INTO sessions VALUES ('gl','w','Pool timeouts','/work/api','2026-07-27T10:00:00.000Z');
INSERT INTO messages VALUES ('gl',0,'user','{"content":"needleopening the opening question"}','2026-07-27T10:00:00.000Z');
INSERT INTO messages VALUES ('gl',1,'assistant','{"content":[{"type":"text","text":"needlemiddle the first answer"}]}','2026-07-27T11:00:00.000Z');`)
			return db
		},
		grow: func(t *testing.T, db string) {
			runStoreSQL(t, db, `INSERT INTO messages VALUES ('gl',2,'user','{"content":"needlelatest the follow up"}','2026-07-27T13:00:00.000Z');
INSERT INTO sessions VALUES ('gn','w','Brand new','/work/api','2026-07-27T14:00:00.000Z');
INSERT INTO messages VALUES ('gn',0,'user','{"content":"needlearrival a session that did not exist"}','2026-07-27T14:00:00.000Z');`)
		},
		key: "grok:gl", turns: 3,
	}}

	for _, c := range cases {
		t.Run(c.harness, func(t *testing.T) {
			tmp := stampedStoreEnv(t)
			db := c.seed(t, tmp)

			dir := filepath.Join(tmp, "index.db")
			if err := Ensure(dir, "", true, nil); err != nil {
				t.Fatal(err)
			}
			hits := func(needle string) int {
				t.Helper()
				ss, err := Search(dir, search.Options{Query: needle, All: true})
				if err != nil {
					t.Fatal(err)
				}
				return len(ss)
			}
			// The premise: without this the assertions below hold for a store
			// mss never read.
			for _, needle := range []string{"needlequiet", "needleopening", "needlemiddle"} {
				if n := hits(needle); n != 1 {
					t.Fatalf("%s: %d hits before the second pass, so the store was not indexed at all", needle, n)
				}
			}
			m, err := readManifest(dir)
			if err != nil {
				t.Fatal(err)
			}
			if m.Files[db].LastUpdated == 0 {
				t.Fatalf("%s was not stamped, so its since-the-watermark parser never runs", db)
			}

			c.grow(t, db)
			if err := Ensure(dir, "", false, nil); err != nil {
				t.Fatal(err)
			}
			for _, needle := range []string{
				"needlearrival", // the session that arrived
				"needlequiet",   // the session nothing touched
				"needleopening", // the turn below the watermark
				"needlemiddle",  // the turn at the watermark
				"needlelatest",  // the turn that arrived
			} {
				if n := hits(needle); n != 1 {
					t.Errorf("%s: %d hits after the second pass, want 1", needle, n)
				}
			}
			// Once each: a store parsed from its watermark that hands a session
			// back whole has to replace what the index holds for it, and one
			// that hands back the new turns alone has to add to it. Either way
			// the session ends with the turns the store has.
			m, err = readManifest(dir)
			if err != nil {
				t.Fatal(err)
			}
			rs, err := recordsForKey(filepath.Join(dir, "records.bin"), tablesFromManifest(m), c.key)
			if err != nil {
				t.Fatal(err)
			}
			if len(rs) != c.turns {
				t.Errorf("%s holds %d records, want %d", c.key, len(rs), c.turns)
			}
		})
	}
}
