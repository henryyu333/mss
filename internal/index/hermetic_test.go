package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/henryyu333/mss/internal/model"
)

// hermeticIndexEnv points the index and every store root at a temp dir of the
// test's own, so a contributor with real session history runs the same test as
// CI and nothing pins a file outside it.
func hermeticIndexEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	setHome(t, filepath.Join(tmp, "home"))
	t.Setenv("USERPROFILE", filepath.Join(tmp, "home"))
	t.Setenv("MSS_INDEX_DIR", filepath.Join(tmp, "default-index"))
	t.Setenv("MSS_CLAUDE_ROOT", filepath.Join(tmp, "claude"))
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "codex"))
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(tmp, "opencode.db"))
	t.Setenv("MSS_CURSOR_ROOT", filepath.Join(tmp, "cursor-user"))
	t.Setenv("MSS_CURSOR_CLI_ROOT", filepath.Join(tmp, "cursor-cli"))
	t.Setenv("MSS_INCLUDE_SUBAGENTS", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("MSS_GROK_ROOT", filepath.Join(tmp, "grok"))
	t.Setenv("MSS_PI_ROOT", filepath.Join(tmp, "pi"))
	t.Setenv("MSS_OMP_ROOT", filepath.Join(tmp, "omp"))
	t.Setenv("MSS_DEEPSEEK_ROOT", filepath.Join(tmp, "deepseek"))
	return tmp
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// codexRolloutHead is the session_meta opener a rollout starts with.
const codexRolloutHead = `{"timestamp":"2026-07-31T00:00:00Z","type":"session_meta","payload":{"id":"cx-1","session_id":"cx-1","cwd":"/w/app"}}
`

func writeTinyIndex(t *testing.T, dir string) {
	t.Helper()
	tmp := dir + ".tmp"
	if err := os.MkdirAll(filepath.Join(tmp, "buckets"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	ss := []model.Session{
		{Harness: "claude", ID: "s1", Project: "proj-a", Path: "source-a", Started: base, Updated: base.Add(time.Minute), Messages: []model.Message{{Role: "user", Text: "alpha needle AKIAIOSFODNN7EXAMPLE", Time: base}, {Role: "assistant", Text: "opencode answer", Time: base.Add(time.Minute)}}},
		{Harness: "codex", ID: "s2", Project: "proj-b", Path: "source-b", Started: base.Add(time.Hour), Updated: base.Add(time.Hour), Messages: []model.Message{{Role: "user", Text: "regex only zzz", Time: base.Add(time.Hour)}}},
	}
	files := map[string]FileState{"source-a": {Path: "source-a", Size: 1, MTime: 2}, "source-b": {Path: "source-b", Size: 3, MTime: 4}}
	if err := writeSessions(tmp, dir, ss, files, "scope-a"); err != nil {
		t.Fatal(err)
	}
}

// allHarnessEnv points every kept store at a sandbox subdir and returns the
// index dir. Missing stores are simply empty.
func allHarnessEnv(t *testing.T) (root, dir string) {
	t.Helper()
	root = t.TempDir()
	setHome(t, filepath.Join(root, "home"))
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	t.Setenv("MSS_CLAUDE_ROOT", filepath.Join(root, "claude"))
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(root, "codex"))
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(root, "opencode.db"))
	t.Setenv("MSS_CURSOR_ROOT", filepath.Join(root, "cursor-user"))
	t.Setenv("MSS_CURSOR_CLI_ROOT", filepath.Join(root, "cursor-cli"))
	t.Setenv("MSS_GROK_ROOT", filepath.Join(root, "grok"))
	t.Setenv("MSS_PI_ROOT", filepath.Join(root, "pi"))
	t.Setenv("MSS_OMP_ROOT", filepath.Join(root, "omp"))
	t.Setenv("MSS_DEEPSEEK_ROOT", filepath.Join(root, "deepseek"))
	if err := os.MkdirAll(filepath.Join(root, "home"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(root, "index.db")
}
