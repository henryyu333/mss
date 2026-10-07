package index

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henryyu333/mss/internal/search"
)

// seedOpencodeSession adds one session with one text part to an opencode store,
// creating the tables if the file is new.
func seedOpencodeSession(t *testing.T, db, session, text string, createdMillis int64) {
	t.Helper()
	stmts := fmt.Sprintf(`
create table if not exists session (id text primary key, directory text, time_created integer, time_updated integer);
create table if not exists message (id text primary key, session_id text, data text, time_created integer);
create table if not exists part (id text primary key, message_id text, data text);
insert into session values ('%[1]s','/tmp/app',%[2]d,%[2]d);
insert into message values ('m-%[1]s','%[1]s','{"role":"user","time":{"created":%[2]d}}',%[2]d);
insert into part values ('p-%[1]s','m-%[1]s',json_object('type','text','text','%[3]s','time',json_object('start',%[2]d)));
`, session, createdMillis, text)
	runStoreSQL(t, db, stmts)
}

// A database-backed store reads only what is new — the since cursor the index
// stamps as LastUpdated — but it changes like any other file, so the merge
// branch treated it as re-read whole and started its ingest counts over. The
// database growing by one message threw away what the rest of it holds (#2025).
//
// grok, because its reader selects messages. opencode was the case here until
// it began handing back touched sessions whole (#4207); a store that does that
// starts its counts over, the goose trade below.
func TestADatabaseThatGrowsKeepsItsIngestCounts(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 CLI not available")
	}
	tmp := t.TempDir()
	setHome(t, tmp)
	t.Setenv("MSS_CLAUDE_ROOT", filepath.Join(tmp, "claude"))
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "codex"))
	t.Setenv("MSS_GOOSE_DB", filepath.Join(tmp, "none-goose.db"))
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(tmp, "none-opencode.db"))
	t.Setenv("MSS_NOTES_FILE", filepath.Join(tmp, "notes.jsonl"))
	db := filepath.Join(tmp, "grok.db")
	t.Setenv("MSS_GROK_DB", db)
	t.Setenv("MSS_GROK_ROOT", filepath.Join(tmp, "grok"))

	long := strings.Repeat("pgbouncer pool timed out and the retry took a second ", 6400)
	if len(long) < maxIndexedText {
		t.Fatalf("the fixture message is %d bytes, under the %d that gets it clipped", len(long), maxIndexedText)
	}
	seedGrokDB(t, db, [][2]string{{long, "2026-01-01T10:00:00.000Z"}})

	dir := filepath.Join(tmp, "index.db")
	if err := Ensure(dir, "", true, nil); err != nil {
		t.Fatal(err)
	}
	clipped := func() int {
		t.Helper()
		m, err := readManifest(dir)
		if err != nil {
			t.Fatal(err)
		}
		return m.IngestHealth["grok"].ClippedMessages
	}
	if got := clipped(); got != 1 {
		t.Fatalf("the build clipped %d messages, so this measures nothing", got)
	}

	// The store grows by a message that has nothing wrong with it. The pass
	// reads only that message, so it cannot speak for the rest of the store.
	seedGrokDB(t, db, [][2]string{{"a short second message", "2026-01-01T11:00:00.000Z"}})
	var out strings.Builder
	if err := Ensure(dir, "", false, &out); err != nil {
		t.Fatal(err)
	}
	if said := out.String(); !strings.Contains(said, replacementPassMarker) {
		t.Fatalf("this was not the merge path, so it does not measure what it is about: %q", said)
	}
	if hits, err := Search(dir, search.Options{Query: "short second message", All: true}); err != nil || len(hits) == 0 {
		t.Fatalf("the new message was not read (%d hits, %v), so the store did not grow the way this expects", len(hits), err)
	}
	if got := clipped(); got != 1 {
		t.Errorf("the clipped message is still in the database and the count is %d", got)
	}
}

// opencode asks for everything in a session that was touched, not only the new
// messages, so a continued session hands back turns already counted. With
// nothing resetting a database's counts (#2025) those turns were counted again
// on every pass — a store in daily use would report clips in the thousands.
func TestADatabaseSessionThatContinuesIsNotCountedTwice(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 CLI not available")
	}
	tmp := t.TempDir()
	setHome(t, tmp)
	t.Setenv("MSS_CLAUDE_ROOT", filepath.Join(tmp, "claude"))
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "codex"))
	db := filepath.Join(tmp, "opencode.db")
	t.Setenv("MSS_OPENCODE_DB", db)

	long := strings.Repeat("pgbouncer pool timed out and the retry took a second ", 6400)
	seedOpencodeSession(t, db, "s1", long, 1767322800000)
	dir := filepath.Join(tmp, "index.db")
	if err := Ensure(dir, "", true, nil); err != nil {
		t.Fatal(err)
	}
	clipped := func() int {
		t.Helper()
		m, err := readManifest(dir)
		if err != nil {
			t.Fatal(err)
		}
		return m.IngestHealth["opencode"].ClippedMessages
	}
	if got := clipped(); got != 1 {
		t.Fatalf("the build clipped %d messages, so this measures nothing", got)
	}

	// The session carries on. Each pass hands the long message back again.
	for i, ts := range []int64{1767326400000, 1767330000000} {
		addOpencodeTurn(t, db, "s1", fmt.Sprintf("m-%d", ts), "a short answer", ts)
		if err := Ensure(dir, "", false, nil); err != nil {
			t.Fatal(err)
		}
		if got := clipped(); got != 1 {
			t.Fatalf("one long message in the store, pass %d says %d clipped", i+1, got)
		}
	}
}

// The other side of handing back whole sessions: what the index already holds
// has to survive the return. A partial replacement would drop every turn it did
// not include (#2032).
func TestADatabaseSessionKeepsItsEarlierTurns(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 CLI not available")
	}
	tmp := t.TempDir()
	setHome(t, tmp)
	t.Setenv("MSS_CLAUDE_ROOT", filepath.Join(tmp, "claude"))
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "codex"))
	db := filepath.Join(tmp, "opencode.db")
	t.Setenv("MSS_OPENCODE_DB", db)

	seedOpencodeSession(t, db, "s1", "why does pgbouncer time out", 1785166187000)
	dir := filepath.Join(tmp, "index.db")
	if err := Ensure(dir, "", true, nil); err != nil {
		t.Fatal(err)
	}
	if hits, err := Search(dir, search.Options{Query: "pgbouncer", All: true}); err != nil || len(hits) == 0 {
		t.Fatalf("the first turn is not in the index (%d hits, %v), so this measures nothing", len(hits), err)
	}

	addOpencodeTurn(t, db, "s1", "m2", "the pool was too small", 1785166787000)
	var out strings.Builder
	if err := Ensure(dir, "", false, &out); err != nil {
		t.Fatal(err)
	}
	if said := out.String(); !strings.Contains(said, replacementPassMarker) {
		t.Fatalf("this was not the merge path, so it does not measure what it is about: %q", said)
	}
	s2, ok, err := FindByIdentity(dir, "opencode", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || len(s2.Messages) != 2 {
		t.Fatalf("the session came back with %d messages (found=%v): the new turn replaced what was there", len(s2.Messages), ok)
	}
	if hits, err := Search(dir, search.Options{Query: "pgbouncer", All: true}); err != nil || len(hits) == 0 {
		t.Errorf("the first turn is gone from the index after the session continued (%d hits, %v)", len(hits), err)
	}
}
