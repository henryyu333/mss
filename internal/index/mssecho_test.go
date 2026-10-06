package index

import (
	"io"
	"path/filepath"
	"testing"

	"github.com/henryyu333/mss/internal/model"
	"github.com/henryyu333/mss/internal/query"
)

// The mss echo is the tool result a transcript holds for the mss call it
// just ran. It reads as evidence — a search for what the recall said lands
// on the recall's own copy — so the postings are dropped at ingest while the
// record stays for `show` to read.

// A session whose only needle lives inside mss's own output must not be
// found by that needle.
func TestMSSOutputDoesNotAnswerItself(t *testing.T) {
	c, tmp := newCFStore(t)
	dir := filepath.Join(tmp, "idx")
	c.write("app", 0, "ask about needle")
	c.appendRaw("app", 0,
		c.turn("app", 0, 5, "mss search needle", `{"produced_by":"mss","hits":[{"text":"needle output copy"}]}`, false))
	if err := EnsureForSearch(dir, query.Options{Query: "needle"}, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	// The mss-quoted needle is invisible; the session's own needle is not.
	ss, err := Search(dir, query.Options{Query: "output copy", All: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ss {
		if s.ID == c.sid(0) {
			t.Fatal("a session was found by the mss output it holds")
		}
	}
	ss, err = Search(dir, query.Options{Query: "needle", All: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 1 || ss[0].ID != c.sid(0) {
		t.Fatalf("the session's own needle went missing: %v", ss)
	}
	// And the record is still there for show: suppression loses postings,
	// not the transcript.
	s, ok, err := FindByIdentity(dir, "claude", c.sid(0))
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	found := false
	for _, m := range s.Messages {
		if m.Text == `{"produced_by":"mss","hits":[{"text":"needle output copy"}]}` {
			found = true
		}
	}
	if !found {
		t.Fatal("the suppressed record vanished from the transcript")
	}
}

// The pairing is positional: a tool result right after an `mss` command line
// is mss's even without the marker, and one after anything else is not.
func TestMSSGateFollowsTheCommand(t *testing.T) {
	var g mssEchoGate
	// The command primes it.
	if g.suppress(model.Message{Role: "command", Text: "mss search needle"}) {
		t.Fatal("the command itself was suppressed")
	}
	if !g.suppress(model.Message{Role: "tool-output", Text: "some needle answer"}) {
		t.Fatal("the result right after an mss command was not suppressed")
	}
	// And it is one shot: the next result is real output again.
	if g.suppress(model.Message{Role: "tool-output", Text: "ordinary needle output"}) {
		t.Fatal("the pairing leaked past the first result")
	}
	// A prose mention of mss is not an invocation.
	g = mssEchoGate{}
	if g.suppress(model.Message{Role: "user", Text: "the mss notes say needle"}) {
		t.Fatal("prose mentioning mss primed the gate")
	}
	if g.suppress(model.Message{Role: "tool-output", Text: "ordinary needle output"}) {
		t.Fatal("prose mentioning mss suppressed the next result")
	}
}
