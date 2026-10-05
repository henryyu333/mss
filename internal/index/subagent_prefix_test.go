package index

import (
	"path/filepath"
	"testing"
)

// A sub-agent runs its own transcript and records the launch under the
// parent's id. The whole id opens the session it names — never the newer
// child that would win a plain newest-first prefix — and the child stays
// reachable by the id its own lines carry (#4483).
func TestAWholeIDOpensThatSessionNotANewerOneItPrefixes(t *testing.T) {
	tmp := hermeticIndexEnv(t)
	root := filepath.Join(tmp, "claude")
	t.Setenv("MSS_CLAUDE_ROOT", root)
	t.Setenv("MSS_INCLUDE_SUBAGENTS", "1")
	project := filepath.Join(root, "-tmp-proj")
	write(t, filepath.Join(project, "q-1.jsonl"),
		`{"type":"user","sessionId":"q-1","timestamp":"2026-01-02T03:04:05Z","cwd":"/tmp/proj","message":{"role":"user","content":"parent question"}}`+"\n")
	write(t, filepath.Join(project, "subagents", "q-1", "agent-a1.jsonl"),
		`{"type":"user","sessionId":"q-1","isSidechain":true,"agentId":"agent-a1","timestamp":"2026-01-02T03:05:05Z","cwd":"/tmp/proj","message":{"role":"user","content":"trace the backoff jitter"}}`+"\n")
	dir := filepath.Join(tmp, "idx")
	if err := Ensure(dir, "", true, nil); err != nil {
		t.Fatal(err)
	}
	s, ok, err := FindByPrefix(dir, "q-1")
	if err != nil || !ok {
		t.Fatalf("q-1 resolved to nothing: %v", err)
	}
	if s.ID != "q-1" {
		t.Errorf("the whole id q-1 opened %s", s.ID)
	}
	if n := PrefixMatches(dir, "q-1"); n != 1 {
		t.Errorf("q-1 counts %d matches, want the one it names", n)
	}
	// Control: the child is still reached by its own id.
	if s, ok, _ := FindByPrefix(dir, "agent-a1"); !ok || s.ID != "agent-a1" || s.Parent != "q-1" {
		t.Errorf("agent-a1 opened %+v", s)
	}
}
