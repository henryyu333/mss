package sources

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henryyu333/mss/internal/model"
)

// TestMain gives the package a home of its own before any test runs. Only
// hermeticEnv moved the XDG_* roots, so a test that set HOME alone and saved a
// note still wrote mss/notes.jsonl into the XDG_DATA_HOME the shell exported
// (#4405). cmd/mss's TestMain does the same.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "mss-sources-test-")
	if err != nil {
		panic(err)
	}
	for key, value := range map[string]string{
		"HOME":            root,
		"USERPROFILE":     root,
		"APPDATA":         filepath.Join(root, "AppData", "Roaming"),
		"LOCALAPPDATA":    filepath.Join(root, "AppData", "Local"),
		"XDG_DATA_HOME":   "",
		"XDG_CONFIG_HOME": "",
		"XDG_CACHE_HOME":  "",
		"MSS_NOTES_FILE":  "",
	} {
		if err := os.Setenv(key, value); err != nil {
			panic(err)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

// parseKindForTest runs one registry kind's parser, for tests that exercise a
// file shape through the registry rather than the function name.
func parseKindForTest(t *testing.T, kind, p string) []model.Session {
	t.Helper()
	for _, h := range Registry() {
		for _, k := range h.Kinds {
			if k.Name == kind {
				ss, err := k.Parse(p, 0)
				if err != nil {
					t.Fatalf("parse %s: %v", p, err)
				}
				return ss
			}
		}
	}
	t.Fatalf("no kind %s", kind)
	return nil
}

// commandsOf collects the command records a parse produced, in order.
func commandsOf(ss []model.Session) []string {
	var out []string
	for _, s := range ss {
		for _, m := range s.Messages {
			if m.Role == RoleCommand {
				out = append(out, m.Text)
			}
		}
	}
	return out
}

// hermeticSourcesEnv points every kept harness's root and home under a temp
// dir, so a test reads only what it plants.
func hermeticSourcesEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	t.Setenv("USERPROFILE", filepath.Join(tmp, "home"))
	t.Setenv("MSS_CLAUDE_ROOT", filepath.Join(tmp, "claude"))
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "codex"))
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(tmp, "opencode.db"))
	t.Setenv("MSS_CURSOR_ROOT", filepath.Join(tmp, "cursor-user"))
	t.Setenv("MSS_CURSOR_CLI_ROOT", filepath.Join(tmp, "cursor-cli"))
	t.Setenv("MSS_GROK_ROOT", filepath.Join(tmp, "grok"))
	t.Setenv("MSS_PI_ROOT", filepath.Join(tmp, "pi"))
	t.Setenv("MSS_OMP_ROOT", filepath.Join(tmp, "omp"))
	t.Setenv("MSS_DEEPSEEK_ROOT", filepath.Join(tmp, "deepseek"))
	t.Setenv("MSS_INCLUDE_SUBAGENTS", "")
	return tmp
}

// tailFile writes head and tail as one transcript and returns the offset
// between them, where the stored part ends.
func tailFile(t *testing.T, name, head, tail string) (string, int64) {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(head+tail), 0o644); err != nil {
		t.Fatal(err)
	}
	return p, int64(len(head))
}

// pathToProjectKey converts an absolute workspace path to the dash-encoded
// key claudeProjectName expects (#4458). A Windows path folds the same way
// (#3217).
func pathToProjectKey(p string) string {
	return strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(p)
}

// writeStore builds a SQLite fixture through the sqlite3 CLI.
func writeStore(t *testing.T, db, script string) {
	t.Helper()
	if out, err := exec.Command("sqlite3", db, script).CombinedOutput(); err != nil {
		t.Fatalf("sqlite setup: %v %s", err, out)
	}
}

// vocabJSON marshals a tool-call payload for the vocabulary fixtures.
func vocabJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
