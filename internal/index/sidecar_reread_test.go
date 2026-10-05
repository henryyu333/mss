package index

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// onlySession names the one session the index holds.
func onlySession(t *testing.T, dir string) (harness, id string) {
	t.Helper()
	metas, err := AllMeta(dir)
	if err != nil || len(metas) != 1 {
		t.Fatalf("want one session in the index, have %d (%v)", len(metas), err)
	}
	return metas[0].Harness, metas[0].ID
}

// Grok takes a session's title, workspace or start from a file beside the
// transcript, which the agent writes on its own: late, after the transcript,
// and again on a rename. A change to that file alone never reached the
// incremental index, only a rebuild (#4446, after #4319 for cline-sdk).
func TestASidecarChangeAloneIsReread(t *testing.T) {
	tmp := t.TempDir()
	isolateStores(t, tmp)
	root := filepath.Join(tmp, "grok")
	t.Setenv("MSS_GROK_ROOT", root)
	session := filepath.Join(root, "sessions", "%2Ftmp%2Fproj", "gr-sidecar")
	transcript := filepath.Join(session, "updates.jsonl")
	sidecar := filepath.Join(session, "summary.json")
	meta := func(title string) string {
		return fmt.Sprintf(`{"info":{"id":"gr-sidecar","cwd":"/tmp/proj"},"generated_title":%q,"created_at":"2026-09-30T10:00:00Z","updated_at":"2026-09-30T10:00:20Z"}`, title)
	}
	body := grokTurn("user", "p0", "fix the retry loop", 1790762400000)
	at := time.Now().Add(-time.Hour)
	writeAt(t, transcript, body, at)
	dir := filepath.Join(tmp, "index.db")
	indexPass(t, dir)
	harness, id := onlySession(t, dir)

	// The sidecar lands after the transcript.
	writeAt(t, sidecar, meta("fix the retry loop"), at.Add(time.Minute))
	indexPass(t, dir)
	matchesRebuild(t, dir, harness, id)

	// A rename rewrites it alone, same size.
	writeAt(t, sidecar, meta("cap the retry at three"), at.Add(2*time.Minute))
	indexPass(t, dir)
	matchesRebuild(t, dir, harness, id)
}

// A rename that lands with an appended turn reaches the append path, which
// sees only the tail. Renamed to a title too thin to keep, the row kept the
// long title it had before the rename, where a rebuild widens the new one
// from the session's first turn, and it stayed until a rebuild (#4592).
func TestRenameToAThinTitleWithAnAppendedTurn(t *testing.T) {
	tmp := t.TempDir()
	isolateStores(t, tmp)
	root := filepath.Join(tmp, "grok")
	t.Setenv("MSS_GROK_ROOT", root)
	session := filepath.Join(root, "sessions", "%2Ftmp%2Fproj", "gr-rename")
	updates := filepath.Join(session, "updates.jsonl")
	summary := filepath.Join(session, "summary.json")
	meta := func(title string) string {
		return fmt.Sprintf(`{"info":{"id":"gr-rename","cwd":"/tmp/proj"},"generated_title":%q,"created_at":"2026-09-30T10:00:00Z","updated_at":"2026-09-30T10:00:20Z"}`, title)
	}
	body := grokTurn("user", "p0", "fix the retry loop", 1790762400000)
	at := time.Now().Add(-time.Hour)
	writeAt(t, updates, body, at)
	writeAt(t, summary, meta("investigate the flaky retry loop in the http client"), at)
	dir := filepath.Join(tmp, "index.db")
	indexPass(t, dir)

	writeAt(t, summary, meta("retry loop"), at.Add(time.Minute))
	body += grokTurn("user", "p1", "now run the tests", 1790762460000)
	writeAt(t, updates, body, at.Add(time.Minute))
	indexPass(t, dir)
	harness, id := onlySession(t, dir)
	matchesRebuild(t, dir, harness, id)

	// The next turn alone is a tail too; the row has to stay right after it.
	writeAt(t, updates, body+grokTurn("user", "p2", "and the lint", 1790762520000), at.Add(2*time.Minute))
	indexPass(t, dir)
	matchesRebuild(t, dir, harness, id)
}

// grokTurn is one user turn in a grok session's updates.jsonl.
func grokTurn(role, promptID, text string, ms int64) string {
	kind := "user_message_chunk"
	if role == "assistant" {
		kind = "agent_message_chunk"
	}
	return fmt.Sprintf(`{"timestamp":%d,"params":{"update":{"sessionUpdate":%q,"content":{"type":"text","text":%q},"_meta":{"promptIndex":0}},"_meta":{"promptId":%q}}}`+"\n", ms, kind, text, promptID)
}
