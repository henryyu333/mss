package index

import (
	"io"
	"os"
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

// The pairing is positional: a tool result while an `mss` command is still
// unpaired is mss's even without the marker, and one with nothing pending is
// not.
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
	// Prose naming the tool is not an invocation.
	g = mssEchoGate{}
	if g.suppress(model.Message{Role: "user", Text: "the mss notes say needle"}) {
		t.Fatal("prose mentioning mss primed the gate")
	}
	if g.suppress(model.Message{Role: "tool-output", Text: "ordinary needle output"}) {
		t.Fatal("prose mentioning mss suppressed the next result")
	}
	// A word sitting in an argument slot is not a run: `go build ./cmd/mss`
	// must not prime, or the next tool record loses postings it owns.
	g = mssEchoGate{}
	if g.suppress(model.Message{Role: "command", Text: "go build ./cmd/mss"}) {
		t.Fatal("the build command itself was suppressed")
	}
	if g.suppress(model.Message{Role: "tool-output", Text: "ordinary needle output"}) {
		t.Fatal("a path ending in mss primed the gate")
	}
	// Bare queries count: `mss needle` routes to search.
	g = mssEchoGate{}
	g.suppress(model.Message{Role: "command", Text: "mss needle"})
	if !g.suppress(model.Message{Role: "tool-output", Text: "some needle answer"}) {
		t.Fatal("a bare mss invocation did not prime the gate")
	}
}

// A harness can batch a turn's command lines and join their results: pending
// counts up, and a sibling command arriving between does not cancel the mss
// run still waiting for its output.
func TestMSSGateSurvivesBatchedCommands(t *testing.T) {
	var g mssEchoGate
	g.suppress(model.Message{Role: "command", Text: "mss search needle"})
	if g.suppress(model.Message{Role: "command", Text: "go test ./..."}) {
		t.Fatal("a sibling command was suppressed")
	}
	if !g.suppress(model.Message{Role: "tool-output", Text: "joined needle answer"}) {
		t.Fatal("the joined result after a batch was not suppressed")
	}
	// Each unpaired command consumes one result.
	if g.suppress(model.Message{Role: "tool-output", Text: "ordinary needle output"}) {
		t.Fatal("one command suppressed two results")
	}
}

// Suppression is durable: a rewrite-grade pass that carries untouched records
// forward must not re-post what the first pass suppressed, or the echo
// returns the first time any unrelated store changes shape.
func TestMSSSuppressionSurvivesACarryPass(t *testing.T) {
	c, tmp := newCFStore(t)
	dir := filepath.Join(tmp, "idx")
	c.write("app", 0, "ask about needle")
	c.appendRaw("app", 0,
		c.turn("app", 0, 5, "mss search needle", `{"produced_by":"mss","hits":[{"text":"needle output copy"}]}`, false))
	c.write("app", 1, "an unrelated talk")
	if err := EnsureForSearch(dir, query.Options{Query: "needle"}, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if ss, err := Search(dir, query.Options{Query: "output copy", All: true}); err != nil || len(ss) != 0 {
		t.Fatalf("suppressed before the carry: %d %v", len(ss), err)
	}
	// Removing a whole other store forces the carry walk — the remaining
	// sessions are re-emitted rather than re-parsed.
	if err := os.RemoveAll(filepath.Join(tmp, "claude", "-tmp-app", c.sid(1)+".jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := EnsureForSearch(dir, query.Options{Query: "needle"}, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	ss, err := Search(dir, query.Options{Query: "output copy", All: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ss {
		if s.ID == c.sid(0) {
			t.Fatal("the carry pass re-posted a suppressed mss echo")
		}
	}
}

// The pairing spans append boundaries: a command indexed in one pass whose
// marker-less result lands in the next pass's tail is the same run, and its
// output is still mss's.
func TestMSSSuppressionSpansAppends(t *testing.T) {
	c, tmp := newCFStore(t)
	dir := filepath.Join(tmp, "idx")
	c.write("app", 0, "ask about needle")
	c.appendRaw("app", 0, c.turnOnly("app", 0, 5, "mss search needle"))
	if err := EnsureForSearch(dir, query.Options{Query: "needle"}, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	c.appendRaw("app", 0, c.resultOnly("app", 0, 5, "splitpass needle answer"))
	if err := EnsureForSearch(dir, query.Options{Query: "needle"}, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	ss, err := Search(dir, query.Options{Query: "splitpass needle answer", All: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ss {
		if s.ID == c.sid(0) {
			t.Fatal("a result split across appends escaped the pairing")
		}
	}
}
