package index

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func grokChunk(ts int, update, meta string) string {
	return fmt.Sprintf(`{"timestamp":%d,"method":"session/update","params":{"update":%s,"_meta":%s}}`, 1784300000+ts, update, meta) + "\n"
}

func grokUser(ts, idx int, text string) string {
	return grokChunk(ts, fmt.Sprintf(`{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":%q},"_meta":{"promptIndex":%d}}`, text, idx), "{}")
}

func grokAgent(ts int, prompt, text string) string {
	return grokChunk(ts, fmt.Sprintf(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":%q}}`, text), fmt.Sprintf(`{"promptId":%q}`, prompt))
}

// Grok joins agent chunks that share a promptId, across the tool records
// between them. A pass mid-reply stored two replies (#4445).
func TestGrokReplyStreamedAcrossAPassIsOneMessage(t *testing.T) {
	tmp := t.TempDir()
	isolateStores(t, tmp)
	root := filepath.Join(tmp, "grok")
	t.Setenv("MSS_GROK_ROOT", root)
	t.Setenv("MSS_GROK_DB", filepath.Join(tmp, "absent.db"))
	updates := filepath.Join(root, "sessions", "%2Ftmp%2Fproj", "s-retry", "updates.jsonl")
	writeAt(t, updates, grokUser(30, 2, "and the backoff?")+grokAgent(31, "p2", "the backoff "), time.Now())
	dir := filepath.Join(tmp, "index.db")
	indexPass(t, dir)
	appendTo(t, updates, grokChunk(32, `{"sessionUpdate":"tool_call","toolCallId":"call-3","title":"read_file","rawInput":{"target_file":"/tmp/proj/retry.go"},"_meta":{"x.ai/tool":{"name":"read_file"}}}`, `{"promptId":"p2"}`)+
		grokAgent(33, "p2", "doubles each attempt"))
	indexPass(t, dir)
	matchesRebuild(t, dir, "grok", "s-retry")

	// The next prompt's reply is its own.
	appendTo(t, updates, grokUser(40, 3, "and the cap?")+grokAgent(41, "p3", "three attempts"))
	indexPass(t, dir)
	matchesRebuild(t, dir, "grok", "s-retry")
}
