package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/henryyu333/mss/internal/index"
	"github.com/henryyu333/mss/internal/model"
	"github.com/henryyu333/mss/internal/query"
	"github.com/henryyu333/mss/internal/search"
)

// TestMain keeps the suite off the developer's real stores and index: a
// machine with session history must run the same tests as CI, and every test
// below pins the same variables again to a temp dir of its own.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "mss-cmd-test-")
	if err != nil {
		panic(err)
	}
	pinned := map[string]string{
		"HOME":        root,
		"USERPROFILE": root,
		// The trust policy and the exclude list live under XDG_CONFIG_HOME.
		"XDG_CONFIG_HOME":       filepath.Join(root, "config"),
		"XDG_DATA_HOME":         "",
		"MSS_INDEX_DIR":         filepath.Join(root, "index.db"),
		"MSS_POLICY_FILE":       filepath.Join(root, "no-policy.json"),
		"MSS_CLAUDE_ROOT":       filepath.Join(root, "claude"),
		"MSS_CODEX_ROOT":        filepath.Join(root, "codex"),
		"MSS_OPENCODE_DB":       filepath.Join(root, "opencode.db"),
		"MSS_CURSOR_ROOT":       filepath.Join(root, "cursor"),
		"MSS_CURSOR_CLI_ROOT":   filepath.Join(root, "cursor-cli"),
		"MSS_GROK_ROOT":         filepath.Join(root, "grok"),
		"MSS_PI_ROOT":           filepath.Join(root, "pi"),
		"MSS_OMP_ROOT":          filepath.Join(root, "omp"),
		"MSS_DEEPSEEK_ROOT":     filepath.Join(root, "deepseek"),
		"MSS_INCLUDE_SUBAGENTS": "",
	}
	scrubEnv(pinned)
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

// scrubEnv unsets every variable that names a place unless pinned names it,
// the same guard internal/index uses.
var envLocation = regexp.MustCompile(`^[A-Z0-9]+(_[A-Z0-9]+)*_(HOME|DIR|DIRS|DIR_NAME|ROOT|ROOTS|DB|FILE|PATH|CONFIG)$`)

func scrubEnv(pinned map[string]string) {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if _, ok := pinned[name]; ok {
			continue
		}
		if strings.HasPrefix(name, "MSS_") || envLocation.MatchString(name) {
			if err := os.Unsetenv(name); err != nil {
				panic(err)
			}
		}
	}
	for k, v := range pinned {
		if err := os.Setenv(k, v); err != nil {
			panic(err)
		}
	}
}

// cmdEnv points every store and the index at a temp dir of the test's own and
// returns the store root and the index dir.
func cmdEnv(t *testing.T) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("MSS_INDEX_DIR", filepath.Join(root, "index.db"))
	t.Setenv("MSS_POLICY_FILE", filepath.Join(root, "no-policy.json"))
	t.Setenv("MSS_CLAUDE_ROOT", filepath.Join(root, "claude"))
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(root, "codex"))
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(root, "opencode.db"))
	t.Setenv("MSS_CURSOR_ROOT", filepath.Join(root, "cursor-user"))
	t.Setenv("MSS_CURSOR_CLI_ROOT", filepath.Join(root, "cursor-cli"))
	t.Setenv("MSS_GROK_ROOT", filepath.Join(root, "grok"))
	t.Setenv("MSS_PI_ROOT", filepath.Join(root, "pi"))
	t.Setenv("MSS_OMP_ROOT", filepath.Join(root, "omp"))
	t.Setenv("MSS_DEEPSEEK_ROOT", filepath.Join(root, "deepseek"))
	t.Setenv("MSS_INCLUDE_SUBAGENTS", "")
	return root, filepath.Join(root, "index.db")
}

// claudeUser is one Claude Code user line, the shape the parser reads.
func claudeUser(sid, ts, text string) string {
	return fmt.Sprintf(`{"type":"user","sessionId":%q,"timestamp":%q,"cwd":"/tmp/proj","message":{"role":"user","content":%q}}`+"\n", sid, ts, text)
}

func claudeAssistant(sid, ts, text string) string {
	return fmt.Sprintf(`{"type":"assistant","sessionId":%q,"timestamp":%q,"cwd":"/tmp/proj","message":{"role":"assistant","content":%q}}`+"\n", sid, ts, text)
}

// claudeSession writes one transcript under the pinned Claude store.
func claudeSession(t *testing.T, root, sid string, lines ...string) {
	t.Helper()
	path := filepath.Join(root, "claude", "-tmp-proj", sid+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustEnsure(t *testing.T, dir string) {
	t.Helper()
	if err := index.Ensure(dir, "", false, io.Discard); err != nil {
		t.Fatal(err)
	}
}

// countRefreshes records every call to the package's index seam and returns
// how many happened.
func countRefreshes(t *testing.T) *int {
	t.Helper()
	n := new(int)
	old := ensureIndex
	ensureIndex = func(dir, harness string, force bool, progress io.Writer) error {
		*n++
		return old(dir, harness, force, progress)
	}
	t.Cleanup(func() { ensureIndex = old })
	return n
}

// countSearchRefreshes is countRefreshes for the search read path's seam.
func countSearchRefreshes(t *testing.T) *int {
	t.Helper()
	n := new(int)
	old := ensureSearch
	ensureSearch = func(dir string, o query.Options, force bool, progress io.Writer) error {
		*n++
		return old(dir, o, force, progress)
	}
	t.Cleanup(func() { ensureSearch = old })
	return n
}

// runCaptured runs f with stdout and stderr redirected, so the test can read
// what a command printed without it reaching the test log.
func runCaptured(t *testing.T, f func() error) (stdout, stderr string, err error) {
	t.Helper()
	outR, outW, perr := os.Pipe()
	if perr != nil {
		t.Fatal(perr)
	}
	errR, errW, perr := os.Pipe()
	if perr != nil {
		t.Fatal(perr)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	var wg sync.WaitGroup
	var out, er []byte
	wg.Add(2)
	go func() { defer wg.Done(); out, _ = io.ReadAll(outR) }()
	go func() { defer wg.Done(); er, _ = io.ReadAll(errR) }()
	err = f()
	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	wg.Wait()
	return string(out), string(er), err
}

// envelopeRefresh is the stanza an answer served without a refresh carries.
type envelopeRefresh struct {
	Refreshed   bool      `json:"refreshed"`
	LastRefresh time.Time `json:"last_refresh"`
}

// TestSessionsListSortAndIdentity pins the two things the candidate list was
// asked for: --sort updated puts the newest first over the whole list, and
// the list holds exactly one row per session whatever order the retrieval
// set hands it in.
func TestSessionsListSortAndIdentity(t *testing.T) {
	day := func(n int) time.Time { return time.Date(2026, 1, n, 0, 0, 0, 0, time.UTC) }
	ss := []model.Session{
		{Harness: "omp", ID: "old", Updated: day(1)},
		{Harness: "omp", ID: "new", Updated: day(3)},
		{Harness: "omp", ID: "middle", Updated: day(2)},
		// The same session delivered twice: the invariant is one row.
		{Harness: "omp", ID: "new", Updated: day(3)},
	}
	hits := []search.Hit{{Session: model.Session{Harness: "omp", ID: "old"}}}

	o := search.Options{Sessions: true}
	listed, _, total := orderSessionsForList("", ss, hits, o, index.SearchResult{})
	if total != 3 || len(listed) != 3 {
		t.Fatalf("list = %d rows, total %d, want 3 rows for 3 sessions", len(listed), total)
	}
	if listed[0].ID != "old" {
		t.Fatalf("default order = %q first, want the hit (old)", listed[0].ID)
	}

	o.Sort = sortUpdated
	listed, _, _ = orderSessionsForList("", ss, hits, o, index.SearchResult{})
	var got []string
	for _, s := range listed {
		got = append(got, s.ID)
	}
	if strings.Join(got, ",") != "new,middle,old" {
		t.Fatalf("--sort updated order = %v, want [new middle old]", got)
	}
}

// TestSortFlagRefusals keeps --sort honest: it orders the candidate list, so
// it needs one, and it knows one order.
func TestSortFlagRefusals(t *testing.T) {
	_, _, err := runCaptured(t, func() error {
		return searchWithOptions("", []string{"--sort", "updated", "q"}, "", true)
	})
	if err == nil || !strings.Contains(err.Error(), "--sort needs --sessions") {
		t.Fatalf("--sort without --sessions = %v, want the refusal", err)
	}
	_, _, err = runCaptured(t, func() error {
		return searchWithOptions("", []string{"--sessions", "--sort", "sideways", "q"}, "", true)
	})
	if err == nil || !strings.Contains(err.Error(), "--sort takes updated") {
		t.Fatalf("--sort sideways = %v, want the refusal", err)
	}
}

// TestNoRefreshMarksTheAnswer pins the --no-refresh contract: no refresh ran
// — so parallel commands queue on no write lock — and the answer says so, with
// the stamp of the last refresh, instead of reading like a fresh one.
func TestNoRefreshMarksTheAnswer(t *testing.T) {
	root, dir := cmdEnv(t)
	claudeSession(t, root, "s1-aaaa",
		claudeUser("s1-aaaa", "2026-01-02T03:04:05Z", "envelopeneedle alpha"),
		claudeAssistant("s1-aaaa", "2026-01-02T03:04:06Z", "envelopeneedle beta"),
	)
	mustEnsure(t, dir)
	wantStamp := index.ManifestSourcesReadAt(dir)
	if wantStamp.IsZero() {
		t.Fatal("the fixture index records no refresh time")
	}

	checkStanza := func(t *testing.T, raw, what string) {
		t.Helper()
		var env struct {
			Refresh *envelopeRefresh `json:"refresh"`
		}
		if err := json.Unmarshal([]byte(raw), &env); err != nil {
			t.Fatalf("%s: %v\n%s", what, err, raw)
		}
		if env.Refresh == nil {
			t.Fatalf("%s carries no refresh stanza", what)
		}
		if env.Refresh.Refreshed {
			t.Fatalf("%s says refreshed: true, want false", what)
		}
		if !env.Refresh.LastRefresh.Equal(wantStamp) {
			t.Fatalf("%s last_refresh = %v, want %v", what, env.Refresh.LastRefresh, wantStamp)
		}
	}

	n := countSearchRefreshes(t)
	out, stderr, err := runCaptured(t, func() error {
		return searchWithOptions(dir, []string{"--sessions", "--no-refresh", "envelopeneedle"}, "", true)
	})
	if err != nil {
		t.Fatal(err)
	}
	if *n != 0 {
		t.Fatalf("--no-refresh search refreshed %d times, want 0", *n)
	}
	checkStanza(t, out, "search --sessions --no-refresh")
	if !strings.Contains(stderr, "answering from the index as it was") {
		t.Fatalf("no stderr note that the index was not walked: %q", stderr)
	}

	// The hit envelope carries the same stanza.
	out, _, err = runCaptured(t, func() error {
		return searchWithOptions(dir, []string{"--no-refresh", "--json", "envelopeneedle"}, "", true)
	})
	if err != nil {
		t.Fatal(err)
	}
	checkStanza(t, out, "search --no-refresh")

	// And `show`'s window envelope does, with no refresh of its own.
	nShow := countRefreshes(t)
	out, _, err = runCaptured(t, func() error {
		return cmdShow(dir, []string{"s1-aaaa", "--harness", "claude", "--json", "--no-refresh", "--limit", "5"}, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	if *nShow != 0 {
		t.Fatalf("--no-refresh show refreshed %d times, want 0", *nShow)
	}
	checkStanza(t, out, "show --no-refresh")

	// Without the flag nothing changed: one refresh, no stanza.
	n2 := countSearchRefreshes(t)
	out, _, err = runCaptured(t, func() error {
		return searchWithOptions(dir, []string{"--sessions", "envelopeneedle"}, "", true)
	})
	if err != nil {
		t.Fatal(err)
	}
	if *n2 != 1 {
		t.Fatalf("default search refreshed %d times, want 1", *n2)
	}
	var plain map[string]any
	if err := json.Unmarshal([]byte(out), &plain); err != nil {
		t.Fatal(err)
	}
	if _, ok := plain["refresh"]; ok {
		t.Fatal("a refreshed answer carries a refresh stanza")
	}
}

// TestShowBriefTrimsAndFilters pins the compact form: one header per message
// (index, role, time), bodies cut at the asked-for length with the command
// that reads the whole one, and --role keeping only the messages asked for.
func TestShowBriefTrimsAndFilters(t *testing.T) {
	root, dir := cmdEnv(t)
	long := strings.Repeat("x", 300)
	claudeSession(t, root, "s1-aaaa",
		claudeUser("s1-aaaa", "2026-01-02T03:04:05Z", "briefneedle first question "+long),
		claudeAssistant("s1-aaaa", "2026-01-02T03:04:06Z", "briefneedle answer"),
		claudeUser("s1-aaaa", "2026-01-02T03:04:07Z", "briefneedle second question"),
	)
	mustEnsure(t, dir)

	out, _, err := runCaptured(t, func() error {
		return cmdShow(dir, []string{"s1-aaaa", "--harness", "claude", "--brief", "20", "--limit", "10"}, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 6 {
		t.Fatalf("brief printed %d lines, want 6 (three headers, three bodies):\n%s", len(lines), out)
	}
	fields := strings.Fields(lines[0])
	if len(fields) != 3 || fields[0] != "[0]" || fields[1] != "user" {
		t.Fatalf("header = %q, want [0] user <time>", lines[0])
	}
	if _, err := time.Parse(time.RFC3339, fields[2]); err != nil {
		t.Fatalf("header time %q is not RFC3339: %v", fields[2], err)
	}
	if !strings.HasPrefix(lines[1], "briefneedle first qu") {
		t.Fatalf("cut body = %q", lines[1])
	}
	if !strings.Contains(lines[1], "cut at 20 characters") ||
		!strings.Contains(lines[1], "`mss show s1-aaaa --harness claude --around 0 --limit 1`") {
		t.Fatalf("the cut body does not name the way to read it whole: %q", lines[1])
	}
	if lines[3] != "briefneedle answer" {
		t.Fatalf("an uncut body was altered: %q", lines[3])
	}

	// --role keeps only the messages asked for.
	out, _, err = runCaptured(t, func() error {
		return cmdShow(dir, []string{"s1-aaaa", "--harness", "claude", "--brief", "--role", "user", "--limit", "10"}, "")
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out, "briefneedle"); n != 2 {
		t.Fatalf("--role user printed %d bodies, want 2:\n%s", n, out)
	}
	if strings.Contains(out, "assistant") {
		t.Fatalf("--role user leaked an assistant message:\n%s", out)
	}
}

// TestBriefFlagRefusals pins the combinations that cannot mean anything.
func TestBriefFlagRefusals(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"s1", "--brief", "--json", "--harness", "claude"}, "--brief or --json"},
		{[]string{"s1", "--role", "user"}, "--role needs --brief"},
		{[]string{"s1", "--brief", "0"}, "positive number"},
	} {
		_, err := parseShow(tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("parseShow(%v) = %v, want %q", tc.args, err, tc.want)
		}
	}
}

// TestNoRefreshRefusals pins what --no-refresh will not do: build an index
// that is not there, and mean --rebuild.
func TestNoRefreshRefusals(t *testing.T) {
	_, dir := cmdEnv(t)
	_, _, err := runCaptured(t, func() error {
		return searchWithOptions(dir, []string{"--no-refresh", "anything"}, "", true)
	})
	if err == nil || !strings.Contains(err.Error(), "no index to answer from") {
		t.Fatalf("search --no-refresh without an index = %v, want the no-index refusal", err)
	}
	_, _, err = runCaptured(t, func() error {
		return searchWithOptions(dir, []string{"--no-refresh", "--rebuild", "anything"}, "", true)
	})
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("search --no-refresh --rebuild = %v, want the contradiction", err)
	}
}

// TestShowRefreshesOnce pins one `show`, one refresh: the --harness exact
// lookup missing an id prefix used to fall into the prefix path, which ran the
// whole build again — the reader saw the update line twice for one answer.
func TestShowRefreshesOnce(t *testing.T) {
	root, dir := cmdEnv(t)
	claudeSession(t, root, "s1-aaaa",
		claudeUser("s1-aaaa", "2026-01-02T03:04:05Z", "prefixneedle alpha"),
		claudeAssistant("s1-aaaa", "2026-01-02T03:04:06Z", "prefixneedle beta"),
	)
	mustEnsure(t, dir)

	cases := [][]string{
		// The double-refresh path: exact lookup misses, prefix fallback runs.
		{"s1", "--harness", "claude", "--limit", "5"},
		// Exact identity.
		{"s1-aaaa", "--harness", "claude", "--limit", "5"},
		// Prefix without --harness.
		{"s1", "--limit", "5"},
	}
	for _, args := range cases {
		n := countRefreshes(t)
		if err := cmdShow(dir, args, ""); err != nil {
			t.Fatalf("show %v: %v", args, err)
		}
		if *n != 1 {
			t.Fatalf("show %v refreshed %d times, want 1", args, *n)
		}
	}
}
