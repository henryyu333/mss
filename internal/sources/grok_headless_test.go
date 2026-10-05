package sources

import (
	"os"
	"path/filepath"
	"testing"
)

// Grok Build 1.0.41 writes "session_kind": "headless" for every `grok -p` run:
// a top-level session started from a shell, not a spawned one. Read as a
// kind, every scripted session showed up as a subagent whose parent was lost
// (#4585). A spawned kind is still kept.
func TestGrokHeadlessSessionIsNotASpawn(t *testing.T) {
	for _, tc := range []struct{ kind, want string }{
		{"headless", ""},
		{"subagent_fork", "subagent_fork"},
	} {
		_, updates := grokTree(t)
		summary := `{"info":{"id":"019f-grok-session","cwd":"/work/cool-app"},"session_kind":"` + tc.kind + `","created_at":"2026-07-01T10:00:00Z"}`
		if err := os.WriteFile(filepath.Join(filepath.Dir(updates), "summary.json"), []byte(summary), 0o644); err != nil {
			t.Fatal(err)
		}
		line := `{"timestamp":1782900001,"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"run the tests"}}}}` + "\n"
		if err := os.WriteFile(updates, []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
		ss, err := ParseGrokFile(updates)
		if err != nil || len(ss) != 1 {
			t.Fatalf("%s: %v %#v", tc.kind, err, ss)
		}
		if ss[0].Kind != tc.want {
			t.Errorf("session_kind %q read as kind %q, want %q", tc.kind, ss[0].Kind, tc.want)
		}
	}
}
