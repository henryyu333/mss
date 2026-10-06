package index

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henryyu333/mss/internal/query"
	"github.com/henryyu333/mss/internal/sources"
)

// matched_indices is the bridge between the candidate list and `show
// --around`: the positions MatchedRecordPositions returns must be the
// positions the transcript numbering gives the same messages, so a caller
// can jump straight to a hit.
func TestMatchedRecordPositionsNumberLikeShow(t *testing.T) {
	c, tmp := newCFStore(t)
	dir := filepath.Join(tmp, "idx")
	c.write("app", 0, "opening words")
	c.appendTurn("app", 0, 3, "needle here", "an answer")
	c.appendTurn("app", 0, 8, "another needle", "more text")
	if err := EnsureForSearch(dir, query.Options{Query: "needle"}, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	s, ok, err := FindByIdentity(dir, "claude", c.sid(0))
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	var want []int
	for i, m := range s.Messages {
		if m.Text == "needle here" || m.Text == "another needle" {
			want = append(want, i)
		}
	}
	o := query.Options{Query: "needle", All: true}
	got, err := MatchedRecordPositions(dir, map[string]bool{s.Harness + ":" + s.ID: true}, o, QueryRecordMatcher(o, nil))
	if err != nil {
		t.Fatal(err)
	}
	pos := got[s.Harness+":"+s.ID]
	if len(pos) != len(want) {
		t.Fatalf("positions %v, want %v", pos, want)
	}
	for i := range want {
		if pos[i] != want[i] {
			t.Fatalf("positions %v, want %v", pos, want)
		}
	}
}

// --exclude takes the session's lineage with it: a sidechain the excluded
// parent spawned leaves with it, and so does the parent of an excluded
// sidechain.
func TestExpandExcludeTakesTheLineage(t *testing.T) {
	tmp := hermeticIndexEnv(t)
	root := filepath.Join(tmp, "claude")
	t.Setenv("MSS_CLAUDE_ROOT", root)
	t.Setenv("MSS_INCLUDE_SUBAGENTS", "1")
	project := filepath.Join(root, "-tmp-proj")
	write(t, filepath.Join(project, "q-1.jsonl"),
		`{"type":"user","sessionId":"q-1","timestamp":"2026-01-02T03:04:05Z","cwd":"/tmp/proj","message":{"role":"user","content":"parent question"}}`+"\n")
	write(t, filepath.Join(project, "subagents", "q-1", "agent-a1.jsonl"),
		`{"type":"user","sessionId":"q-1","isSidechain":true,"agentId":"agent-a1","timestamp":"2026-01-02T03:05:05Z","cwd":"/tmp/proj","message":{"role":"user","content":"trace the backoff jitter"}}`+"\n")
	write(t, filepath.Join(project, "other.jsonl"),
		`{"type":"user","sessionId":"other","timestamp":"2026-01-02T03:06:05Z","cwd":"/tmp/proj","message":{"role":"user","content":"unrelated session"}}`+"\n")
	dir := filepath.Join(tmp, "idx")
	if err := Ensure(dir, "", true, nil); err != nil {
		t.Fatal(err)
	}
	out, found, err := ExpandExclude(dir, []string{"q-1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("selfFound went false without a nonce")
	}
	if !out["q-1"] || !out["agent-a1"] {
		t.Fatalf("excluding the parent left its subagent in: %v", out)
	}
	if out["other"] {
		t.Fatalf("an unrelated session was excluded: %v", out)
	}
	// The other direction too: the child names its parent.
	out, _, err = ExpandExclude(dir, []string{"agent-a1"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !out["agent-a1"] || !out["q-1"] {
		t.Fatalf("excluding the subagent left its parent in: %v", out)
	}
}

// --exclude-self finds the session whose transcript carries the nonce the
// caller embedded in its own command line — the way a skill asks mss to
// leave out the conversation it is answering inside.
func TestExpandExcludeSelfFindsTheNonceSession(t *testing.T) {
	c, tmp := newCFStore(t)
	dir := filepath.Join(tmp, "idx")
	c.write("app", 0, "an older talk")
	c.appendTurn("app", 0, 4, "mss search --exclude-self noncezzz123 needle", "hits")
	c.write("app", 1, "a different talk")
	if err := EnsureForSearch(dir, query.Options{All: true}, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	out, found, err := ExpandExclude(dir, nil, "noncezzz123")
	if err != nil {
		t.Fatal(err)
	}
	if !found || !out[c.sid(0)] {
		t.Fatalf("the nonce session was not excluded: found=%v out=%v", found, out)
	}
	if out[c.sid(1)] {
		t.Fatalf("an unrelated session was excluded: %v", out)
	}
	// A nonce nobody carries is a coverage gap, not silence.
	_, found, err = ExpandExclude(dir, nil, "nonce-missing")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("a nonce that appears nowhere reported found")
	}
}

// A harness whose files exist but cannot be read — here a zstd-framed dsh
// log without the zstd CLI — belongs in coverage, or "no match" reads as
// "not in your history". And the converse: a harness with nothing on disk
// is absent, not unread, or `complete` could never be true.
func TestSearchCoverageReportsAnUnreadHarness(t *testing.T) {
	tmp := hermeticIndexEnv(t)
	dsh := filepath.Join(tmp, "dsh")
	t.Setenv("DSH_HOME", dsh)
	// Files() walks MSS_DEEPSEEK_ROOT while SkipReason follows DSH_HOME;
	// point both at the same store.
	t.Setenv("MSS_DEEPSEEK_ROOT", filepath.Join(dsh, "sessions"))
	// The name alone marks it framed; without the CLI nothing reads further.
	write(t, filepath.Join(dsh, "sessions", "session-aaaa", "session.v4.jsonl.zstd"), "framed bytes the test cannot make\n")
	dir := filepath.Join(tmp, "idx")
	if err := EnsureForSearch(dir, query.Options{All: true}, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	cov := SearchCoverage(dir)
	if cov == nil {
		t.Fatal("no coverage")
	}
	if sources.ZstdAvailable() {
		if cov.Complete {
			return // everything read; nothing to report
		}
		for _, u := range cov.Unread {
			if u == "deepseek: zstd CLI not found" {
				t.Fatalf("deepseek was reported unread with zstd installed: %v", cov.Unread)
			}
		}
		return
	}
	want := "deepseek: zstd CLI not found"
	for _, u := range cov.Unread {
		if u == want {
			if cov.Complete {
				t.Fatal("coverage claimed complete with an unread harness")
			}
			return
		}
	}
	t.Fatalf("unread harness missing from coverage: %v", cov.Unread)
}

// A harness with no files on disk is absent, not unread: on a machine that
// only runs one harness, `complete` must be reachable.
func TestSearchCoverageIgnoresAbsentHarnesses(t *testing.T) {
	root, dir := allHarnessEnv(t)
	writeLines(t, filepath.Join(root, "claude", "project", "s.jsonl"), claudeLine("s1", "2026-01-01T00:01:00Z", "absentmarker"))
	if err := EnsureForSearch(dir, query.Options{All: true}, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	cov := SearchCoverage(dir)
	if cov == nil {
		t.Fatal("no coverage")
	}
	for _, u := range cov.Unread {
		if strings.HasSuffix(u, ": not read") {
			t.Fatalf("an absent harness counted as unread: %v", cov.Unread)
		}
	}
	if !cov.Complete {
		t.Fatalf("complete is unreachable on a single-harness store: %+v", cov)
	}
}
