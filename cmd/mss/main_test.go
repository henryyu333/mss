package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/henryyu333/mss/internal/index"
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
