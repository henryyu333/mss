package index

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// These tests hold an index built a pass at a time to what a rebuild of the
// same stores gives. Each one found a case where the two disagreed.

// parityStores points every store mss reads at a directory that does not
// exist, so a test reads only the store it sets up.
var parityStores = []string{
	"MSS_CC_MIRROR_ROOT", "MSS_CLAUDE_ROOT", "MSS_CODEX_ROOT", "MSS_CURSOR_CLI_ROOT", "MSS_CURSOR_ROOT",
	"MSS_DEEPSEEK_ROOT", "MSS_GROK_DB", "MSS_GROK_ROOT", "MSS_OMP_ROOT", "MSS_OPENCODE_DB",
	"MSS_OPENCODE_DIFFS", "MSS_PI_ROOT", "MSS_XCODE_CLAUDE_ROOT", "MSS_XCODE_CODEX_ROOT",
}

// parityEnv isolates a test to the stores it names (env var to a path under
// the returned root) and returns that root.
func parityEnv(t *testing.T, stores map[string]string) string {
	t.Helper()
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	setHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("MSS_NOTES_FILE", filepath.Join(home, "notes.jsonl"))
	t.Setenv("MSS_INCLUDE_SUBAGENTS", "")
	for _, e := range parityStores {
		t.Setenv(e, filepath.Join(tmp, "absent", strings.ToLower(e)))
	}
	for k, v := range stores {
		t.Setenv(k, filepath.Join(tmp, v))
	}
	return tmp
}

func parityPass(t *testing.T, dir string, force bool) {
	t.Helper()
	if err := Ensure(dir, "", force, io.Discard); err != nil {
		t.Fatalf("Ensure(%s, force=%v): %v", dir, force, err)
	}
}

func parityWrite(t *testing.T, path, body string, appendTo bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appendTo {
		flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(path, flag, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(body); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func paritySQL(t *testing.T, db, stmts string) {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available")
	}
	if err := os.MkdirAll(filepath.Dir(db), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sqlite3", db)
	cmd.Stdin = strings.NewReader(stmts)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite3 %s: %v\n%s", db, err, out)
	}
}

// paritySnapshot is what a reader can see of an index: each session's row
// and records, and the two command tables.
func paritySnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	metas, err := AllMeta(dir)
	if err != nil {
		t.Fatalf("AllMeta %s: %v", dir, err)
	}
	out := map[string]string{}
	var ids []Identity
	for _, m := range metas {
		out[m.Harness+":"+m.ID] = fmt.Sprintf("title=%q project=%q counted=%d asked=%v words=%d touched=%v",
			m.Title, m.Project, m.Counted, m.Asked, m.Words, m.Touched)
		ids = append(ids, Identity{Harness: m.Harness, ID: m.ID})
	}
	full, err := FindManyByIdentity(dir, ids)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range full {
		var msgs []string
		for _, m := range s.Messages {
			msgs = append(msgs, m.Role+"|"+m.Time.UTC().Format(time.RFC3339)+"|"+m.Text)
		}
		out[s.Harness+":"+s.ID] += "\n    " + strings.Join(msgs, "\n    ")
	}
	out["commands"] = fmt.Sprint(ReadCommands(dir))
	out["fails"] = fmt.Sprint(ReadCommandFails(dir))
	out["facts"] = fmt.Sprint(ReadSessionFacts(dir))
	return out
}

// sameAsRebuild fails the test where the index in inc differs from a fresh
// build of the same stores.
func sameAsRebuild(t *testing.T, inc string) {
	t.Helper()
	fresh := inc + "-fresh"
	parityPass(t, fresh, true)
	a, b := paritySnapshot(t, inc), paritySnapshot(t, fresh)
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	var diffs []string
	for k := range keys {
		if a[k] != b[k] {
			diffs = append(diffs, fmt.Sprintf("%s\n  incremental: %s\n  rebuild:     %s", k, a[k], b[k]))
		}
	}
	sort.Strings(diffs)
	if len(diffs) > 0 {
		t.Errorf("incremental index differs from a rebuild:\n%s", strings.Join(diffs, "\n"))
	}
}

func parityClaudeLine(id, ts, role, content string) string {
	return fmt.Sprintf(`{"type":%q,"sessionId":%q,"timestamp":"2026-07-17T09:%s","cwd":"/tmp/proj","message":{"role":%q,"content":%s}}`, role, id, ts, role, content) + "\n"
}

// A command left in one session after an update rewrote the other is no
// longer a recurring one; the recomputed table was empty and the old one
// stayed (#4441).
func TestUpdateThatEmptiesTheCommandTableDropsIt(t *testing.T) {
	tmp := parityEnv(t, map[string]string{"MSS_CLAUDE_ROOT": "claude"})
	proj := filepath.Join(tmp, "claude", "-tmp-proj")
	session := func(id string, withCommand bool) {
		body := parityClaudeLine(id, "00:00Z", "user", `"fix the retry loop"`)
		if withCommand {
			body += parityClaudeLine(id, "00:01Z", "assistant", `[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"make flakycheck"}}]`) +
				parityClaudeLine(id, "00:02Z", "user", `[{"type":"tool_result","tool_use_id":"t1","content":"ok retry 0.1s"}]`)
		}
		parityWrite(t, filepath.Join(proj, id+".jsonl"), body, false)
	}
	session("s1", true)
	session("s2", true)
	inc := filepath.Join(tmp, "inc")
	parityPass(t, inc, false)
	if len(ReadCommands(inc)) == 0 {
		t.Fatal("control: two sessions running one command make a table")
	}
	session("s2", false)
	later := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(filepath.Join(proj, "s2.jsonl"), later, later)
	parityPass(t, inc, false)
	if got := ReadCommands(inc); len(got) != 0 {
		t.Errorf("command table after the update = %v, want none", got)
	}
	sameAsRebuild(t, inc)
}

// A line the client writes after a pass took its file state is read in that
// pass and again in the next one, unless the pass stops where it recorded
// (#4442). The append is made between the walk and the parse here, which is
// the window a live client writes into.
func TestLineWrittenDuringAPassIsReadOnce(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(fmt.Sprint("first-build=", full), func(t *testing.T) {
			tmp := parityEnv(t, map[string]string{"MSS_PI_ROOT": "pi"})
			f := filepath.Join(tmp, "pi", "--tmp-proj--", "s-retry.jsonl")
			line := func(id, ts, text string) string {
				return fmt.Sprintf(`{"type":"message","id":%q,"timestamp":"2026-09-01T09:%s","message":{"role":"user","content":[{"type":"text","text":%q}]}}`, id, ts, text) + "\n"
			}
			parityWrite(t, f, `{"type":"session","version":3,"id":"s-retry","timestamp":"2026-09-01T09:00:00Z","cwd":"/tmp/proj"}`+"\n"+line("u1", "00:01Z", "fix the retry loop"), false)
			inc := filepath.Join(tmp, "inc")
			if !full {
				parityPass(t, inc, false)
				parityWrite(t, f, line("u2", "01:00Z", "now run the tests"), true)
			}
			want := currentFiles("")
			parityWrite(t, f, line("u3", "02:00Z", "written while the pass ran"), true)
			if full {
				if err := rebuild(inc, "", "", want, io.Discard); err != nil {
					t.Fatal(err)
				}
			} else if err := updateIndex(inc, "", "", want, false, io.Discard); err != nil {
				t.Fatal(err)
			}
			parityPass(t, inc, false)
			sameAsRebuild(t, inc)
		})
	}
}

// A tool result that lands in the pass after its call still settles the call:
// the exit status on the command, a refused edit taken back (#4443).
func TestToolResultInTheNextPassSettlesItsCall(t *testing.T) {
	cases := []struct {
		name, env, file string
		head, call, res string
	}{
		{
			name: "codex", env: "MSS_CODEX_ROOT",
			file: "sessions/2026/07/17/rollout-2026-07-17T09-00-00-0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001.jsonl",
			head: `{"timestamp":"2026-07-17T09:00:00.000Z","type":"session_meta","payload":{"id":"0199aaaa-bbbb-7ccc-8ddd-eeeeffff0001","cwd":"/tmp/proj"}}` + "\n" +
				`{"timestamp":"2026-07-17T09:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"fix the retry loop"}]}}` + "\n",
			call: `{"timestamp":"2026-07-17T09:02:00.000Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"go vet ./retry\"}","call_id":"call_3"}}` + "\n",
			res:  `{"timestamp":"2026-07-17T09:02:01.000Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call_3","output":"Process exited with code 2\nOutput:\nvet: unreachable"}}` + "\n",
		},
		{
			name: "pi bash", env: "MSS_PI_ROOT",
			file: "--tmp-proj--/s-retry.jsonl",
			head: `{"type":"session","version":3,"id":"s-retry","timestamp":"2026-09-01T09:00:00Z","cwd":"/tmp/proj"}` + "\n" +
				`{"type":"message","id":"u1","timestamp":"2026-09-01T09:00:01Z","message":{"role":"user","content":[{"type":"text","text":"fix the retry loop"}]}}` + "\n",
			call: `{"type":"message","id":"a1","timestamp":"2026-09-01T09:02:00Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"c-vet","name":"bash","arguments":{"command":"go vet ./retry"}}]}}` + "\n",
			res:  `{"type":"message","id":"r1","timestamp":"2026-09-01T09:02:01Z","message":{"role":"toolResult","toolCallId":"c-vet","toolName":"bash","content":[{"type":"text","text":"vet: unreachable"}],"details":{"exitCode":2},"isError":true}}` + "\n",
		},
		{
			// Claude stamps a failure's exit too, so the split matters for it.
			name: "claude", env: "MSS_CLAUDE_ROOT",
			file: "-tmp-proj/c1.jsonl",
			head: `{"type":"user","sessionId":"c1","timestamp":"2026-10-01T10:00:00Z","cwd":"/tmp/proj","message":{"role":"user","content":"fix the retry loop"}}` + "\n",
			call: `{"type":"assistant","sessionId":"c1","timestamp":"2026-10-01T10:00:01Z","cwd":"/tmp/proj","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"go vet ./retry"}}]}}` + "\n",
			res:  `{"type":"user","sessionId":"c1","timestamp":"2026-10-01T10:00:02Z","cwd":"/tmp/proj","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","is_error":true,"content":"Exit code 2\nvet: unreachable"}]}}` + "\n",
		},
		{
			// omp's hashline edit takes its replaced lines from the result
			// since #4525.
			name: "omp hashline", env: "MSS_OMP_ROOT",
			file: "--tmp-proj--/2026-10-01T10-00-00-000Z_o1.jsonl",
			head: `{"type":"session","version":3,"id":"omp-edits","timestamp":"2026-10-01T10:00:00.000Z","cwd":"/tmp/proj"}` + "\n" +
				`{"type":"message","id":"u1","timestamp":"2026-10-01T10:00:00.000Z","message":{"role":"user","content":[{"type":"text","text":"fix the retry loop"}]}}` + "\n",
			call: `{"type":"message","id":"a0","timestamp":"2026-10-01T10:00:01.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"c0","name":"edit","arguments":{"input":"*** Begin Patch\n[retry.go#1a2b]\nPUT 3.=3:\n+\tfor attempt := 0; attempt < maxAttempts; attempt++ {\n*** End Patch"}}]}}` + "\n",
			res:  `{"type":"message","id":"r0","timestamp":"2026-10-01T10:00:02.000Z","message":{"role":"toolResult","toolCallId":"c0","toolName":"edit","content":[{"type":"text","text":"Updated retry.go"}],"details":{"path":"retry.go","diff":"-3|\tfor {\n+3|\tfor attempt := 0; attempt < maxAttempts; attempt++ {"},"isError":false}}` + "\n",
		},
		{
			name: "pi refused edit", env: "MSS_PI_ROOT",
			file: "--tmp-proj--/s-retry.jsonl",
			head: `{"type":"session","version":3,"id":"s-retry","timestamp":"2026-09-01T09:00:00Z","cwd":"/tmp/proj"}` + "\n" +
				`{"type":"message","id":"u1","timestamp":"2026-09-01T09:00:01Z","message":{"role":"user","content":[{"type":"text","text":"fix the retry loop"}]}}` + "\n",
			call: `{"type":"message","id":"a1","timestamp":"2026-09-01T09:02:00Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"c-edit","name":"edit","arguments":{"path":"/tmp/proj/retry.go","edits":[{"oldText":"for {","newText":"for i := 0; i < 3; i++ {"}]}}]}}` + "\n",
			res:  `{"type":"message","id":"r1","timestamp":"2026-09-01T09:02:01Z","message":{"role":"toolResult","toolCallId":"c-edit","toolName":"edit","content":[{"type":"text","text":"oldText not found"}],"isError":true}}` + "\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := parityEnv(t, map[string]string{tc.env: "store"})
			f := filepath.Join(tmp, "store", filepath.FromSlash(tc.file))
			parityWrite(t, f, tc.head, false)
			inc := filepath.Join(tmp, "inc")
			parityPass(t, inc, false)
			parityWrite(t, f, tc.call, true)
			parityPass(t, inc, false)
			parityWrite(t, f, tc.res, true)
			parityPass(t, inc, false)
			sameAsRebuild(t, inc)
		})
	}
}

// A session deleted from an OpenCode-schema database is kept by the pass
// that sees it go (#2970), and a rebuild in the same index kept it only for
// stores whose rows name the database (#4447).
func TestRebuildKeepsSessionDeletedFromOpencodeSchemaDB(t *testing.T) {
	const schema = `create table session(id text primary key, project_id text, parent_id text, directory text, title text, version text, time_created integer, time_updated integer);
create table message(id text primary key, session_id text, time_created integer, time_updated integer, data text);
create table part(id text primary key, message_id text, session_id text, time_created integer, time_updated integer, data text);
`
	const base = int64(1784278800000)
	row := func(sid, dir, text string, at int64) string {
		return fmt.Sprintf("insert into session values('%[1]s','p1',null,'%[2]s','%[3]s','1.0.0',%[4]d,%[4]d);\n"+
			"insert into message values('m_%[1]s','%[1]s',%[4]d,%[4]d,'{\"role\":\"user\",\"time\":{\"created\":%[4]d}}');\n"+
			"insert into part values('p_%[1]s','m_%[1]s','%[1]s',%[4]d,%[4]d,'{\"type\":\"text\",\"text\":\"%[3]s\",\"time\":{\"start\":%[4]d}}');\n", sid, dir, text, at)
	}
	for _, tc := range []struct{ harness, env string }{
		{"opencode", "MSS_OPENCODE_DB"},
	} {
		t.Run(tc.harness, func(t *testing.T) {
			tmp := parityEnv(t, map[string]string{tc.env: "store/store.db"})
			db := filepath.Join(tmp, "store", "store.db")
			paritySQL(t, db, schema+row("ses_retry", "/tmp/proj", "fix the retry loop", base)+row("ses_other", "/tmp/other", "inspect the sqlite fixture", base+500))
			inc := filepath.Join(tmp, "inc")
			parityPass(t, inc, false)
			paritySQL(t, db, "delete from part where session_id='ses_retry'; delete from message where session_id='ses_retry'; delete from session where id='ses_retry';\n")
			parityPass(t, inc, false)
			key := tc.harness + ":ses_retry"
			kept, ok := paritySnapshot(t, inc)[key]
			if !ok {
				t.Fatalf("control: the incremental pass dropped %s", key)
			}
			parityPass(t, inc, true)
			if got, ok := paritySnapshot(t, inc)[key]; !ok {
				t.Errorf("%s gone after a rebuild in the same index", key)
			} else if got != kept {
				t.Errorf("%s after a rebuild:\n  %s\nwant what the incremental pass kept:\n  %s", key, got, kept)
			}
		})
	}
}

// A Codex session known only from history.jsonl is one session however many
// lines it has; the full build kept the derived fields of its last (#4449).
func TestFullBuildReadsCodexHistorySessionWhole(t *testing.T) {
	tmp := parityEnv(t, map[string]string{"MSS_CODEX_ROOT": "codex"})
	h := filepath.Join(tmp, "codex", "history.jsonl")
	parityWrite(t, h, `{"session_id":"s1","ts":1784278000,"text":"why does the retry loop spin"}`+"\n", false)
	inc := filepath.Join(tmp, "inc")
	parityPass(t, inc, false)
	parityWrite(t, h, `{"session_id":"s1","ts":1784278100,"text":"and why does the backoff never reset"}`+"\n", true)
	parityPass(t, inc, false)
	sameAsRebuild(t, inc)
}
