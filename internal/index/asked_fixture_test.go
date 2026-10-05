package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func askedFixture(t *testing.T, sessions map[string][]string, when map[string]string) string {
	t.Helper()
	tmp := t.TempDir()
	proj := filepath.Join(tmp, "claude", "-tmp-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	for id, msgs := range sessions {
		var b strings.Builder
		for _, m := range msgs {
			fmt.Fprintf(&b, `{"type":"user","sessionId":%q,"cwd":"/tmp/app","timestamp":%q,"message":{"role":"user","content":%q}}`+"\n",
				id, when[id], m)
		}
		if err := os.WriteFile(filepath.Join(proj, id+".jsonl"), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	setHome(t, tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("MSS_CLAUDE_ROOT", filepath.Join(tmp, "claude"))
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "codex"))
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(tmp, "none.db"))
	dir := filepath.Join(tmp, "index.db")
	if err := Ensure(dir, "", true, nil); err != nil {
		t.Fatal(err)
	}
	return dir
}
