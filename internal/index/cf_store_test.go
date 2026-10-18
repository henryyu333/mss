package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// cfStore is a claude store of sessions that run commands. It was extracted
// from the command-failure tests when their table was removed, because other
// traversal and retrieval tests still build their fixtures through it.
type cfStore struct {
	t    *testing.T
	root string
}

func newCFStore(t *testing.T) (*cfStore, string) {
	t.Helper()
	tmp := t.TempDir()
	setHome(t, tmp)
	t.Setenv("MSS_CLAUDE_ROOT", filepath.Join(tmp, "claude"))
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "codex"))
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(tmp, "none.db"))
	t.Setenv("MSS_NOTES_FILE", filepath.Join(tmp, "notes.jsonl"))
	return &cfStore{t: t, root: filepath.Join(tmp, "claude")}, tmp
}

func (c *cfStore) path(project string, i int) string {
	return filepath.Join(c.root, "-tmp-"+project, fmt.Sprintf("%08d-0000-4000-8000-000000000000.jsonl", i))
}

func (c *cfStore) sid(i int) string { return fmt.Sprintf("%08d-0000-4000-8000-000000000000", i) }

// turn is one Bash call and what it printed.
func (c *cfStore) turn(project string, i, k int, cmd, out string, failed bool) string {
	return c.turnOnly(project, i, k, cmd) + c.resultOnly(project, i, k, out)
}

// turnOnly is the command half of a Bash call, for tests that split a run
// across appends: the parser joins them back, but each lands in its own pass.
func (c *cfStore) turnOnly(project string, i, k int, cmd string) string {
	sid, id := c.sid(i), fmt.Sprintf("call_%d_%d", i, k)
	ts := fmt.Sprintf("2026-09-%02dT10:%02d:00Z", 1+i%28, k%60)
	a := map[string]any{"type": "assistant", "sessionId": sid, "timestamp": ts, "cwd": "/tmp/" + project, "message": map[string]any{
		"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": id, "name": "Bash", "input": map[string]any{"command": cmd}}}}}
	ab, _ := json.Marshal(a)
	return string(ab) + "\n"
}

// resultOnly is the output half of a Bash call.
func (c *cfStore) resultOnly(project string, i, k int, out string) string {
	sid, id := c.sid(i), fmt.Sprintf("call_%d_%d", i, k)
	ts := fmt.Sprintf("2026-09-%02dT10:%02d:00Z", 1+i%28, k%60)
	r := map[string]any{"type": "user", "sessionId": sid, "timestamp": ts, "cwd": "/tmp/" + project, "message": map[string]any{
		"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": out}}}}
	rb, _ := json.Marshal(r)
	return string(rb) + "\n"
}

func (c *cfStore) prompt(project string, i int, text string) string {
	b, _ := json.Marshal(map[string]any{"type": "user", "sessionId": c.sid(i), "timestamp": fmt.Sprintf("2026-09-%02dT09:59:00Z", 1+i%28),
		"cwd": "/tmp/" + project, "message": map[string]any{"role": "user", "content": text}})
	return string(b) + "\n"
}

// write gives session i a prompt and three runs: a failing test, a passing
// build and, every third session, a failing lint.
func (c *cfStore) write(project string, i int, title string) {
	c.t.Helper()
	body := c.prompt(project, i, title) +
		c.turn(project, i, 0, "go test ./...", "--- FAIL: TestStoreRejectsStaleWrites (0.01s)\nFAIL", true) +
		c.turn(project, i, 1, "go build ./...", "ok", false)
	if i%3 == 0 {
		body += c.turn(project, i, 2, "npm run lint", "npm ERR! missing script: lint", true)
	}
	if err := os.MkdirAll(filepath.Dir(c.path(project, i)), 0o755); err != nil {
		c.t.Fatal(err)
	}
	if err := os.WriteFile(c.path(project, i), []byte(body), 0o644); err != nil {
		c.t.Fatal(err)
	}
}

func (c *cfStore) appendTurn(project string, i, k int, cmd, out string) {
	c.t.Helper()
	f, err := os.OpenFile(c.path(project, i), os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(c.turn(project, i, k, cmd, out, true)); err != nil {
		c.t.Fatal(err)
	}
}

func (c *cfStore) appendRaw(project string, i int, text string) {
	c.t.Helper()
	f, err := os.OpenFile(c.path(project, i), os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(text); err != nil {
		c.t.Fatal(err)
	}
}
