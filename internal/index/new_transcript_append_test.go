package index

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henryyu333/mss/internal/search"
)

// A new conversation is a new file, so the commonest update there is was the
// one that rewrote the whole store: `canAppendIncremental` refused any path it
// had not seen, and the replacement path reads every surviving record back
// through the tokenizer. Measured on a synthetic Codex store, one new
// one-message transcript cost 1.19s at 42.9 MB of records and 4.76s at
// 171.4 MB, against 0.11s and 0.30s for a message appended to a file already
// indexed — the cost followed the store, not the new file (#3500).
//
// The progress line is the assertion because it names the path taken: the
// append path says what it updated, the replacement path prints its
// changed/removed counts.
func TestANewTranscriptIsAppendedNotRewritten(t *testing.T) {
	tmp := hermeticIndexEnv(t)
	claude := os.Getenv("MSS_CLAUDE_ROOT")
	first := filepath.Join(claude, "p", "first.jsonl")
	write(t, first, claudeLine("s-first", "2026-01-02T03:04:05Z", "the exporter retries without a pause"))
	dir := filepath.Join(tmp, "idx")
	if err := Ensure(dir, "", false, nil); err != nil {
		t.Fatal(err)
	}

	// The bytes already written, so the next pass can be held to not rewriting
	// them. This is the invariant the progress line only describes: whatever the
	// pass says it did, the records that were there have to still be there,
	// unchanged, at the same offsets — that is what makes the cost follow the new
	// file instead of the store.
	recordsBefore, err := os.ReadFile(filepath.Join(dir, "records.bin"))
	if err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(claude, "p", "second.jsonl"),
		claudeLine("s-second", "2026-01-02T05:00:00Z", "the billing webhook retries twice"))
	var progress bytes.Buffer
	if err := Ensure(dir, "", false, &progress); err != nil {
		t.Fatal(err)
	}
	if got := progress.String(); !strings.Contains(got, "updated 1 file") {
		t.Errorf("a new transcript did not take the append path: %q", got)
	}
	recordsAfter, err := os.ReadFile(filepath.Join(dir, "records.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recordsAfter) <= len(recordsBefore) {
		t.Fatalf("records.bin is %d bytes after the append and was %d — the new session went somewhere else",
			len(recordsAfter), len(recordsBefore))
	}
	if !bytes.Equal(recordsAfter[:len(recordsBefore)], recordsBefore) {
		t.Error("the records already on file were rewritten, so the pass paid for the whole store")
	}

	for query, want := range map[string]string{
		"billing webhook retries": "s-second",
		"exporter retries":        "s-first",
	} {
		ss, err := Search(dir, search.Options{Query: query, All: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(ss) != 1 || ss[0].ID != want {
			t.Fatalf("%q found %#v, want the session %s", query, ss, want)
		}
	}
}

// Resuming a parse is what a kind without an offset parser cannot do, and a
// file mss has never read needs no resuming — so it takes the append path
// too, read whole from its first byte.
//
// This used to be refused, and the refusal was not free: it applied to the
// whole batch, so one new file of such a kind sent every other changed file
// down the replacement path as well. The only surfaces that take that path are
// a CLI search and `mss index`; a machine driven by hooks takes neither, and
// new dsh logs sat unread there for weeks while every pass saw them (#3747).
func TestANewFileOfAKindThatCannotResumeIsAppendedWhole(t *testing.T) {
	tmp := hermeticIndexEnv(t)
	claude := os.Getenv("MSS_CLAUDE_ROOT")
	write(t, filepath.Join(claude, "p", "first.jsonl"),
		claudeLine("s-first", "2026-01-02T03:04:05Z", "the exporter retries without a pause"))
	dir := filepath.Join(tmp, "idx")
	if err := Ensure(dir, "", false, nil); err != nil {
		t.Fatal(err)
	}
	recordsBefore, err := os.ReadFile(filepath.Join(dir, "records.bin"))
	if err != nil {
		t.Fatal(err)
	}

	ds := filepath.Join(os.Getenv("MSS_DEEPSEEK_ROOT"), "--work-app--", "session-ds-new")
	write(t, filepath.Join(ds, "session.jsonl"),
		`{"type":"session","version":0,"id":"ds-new","createdAt":1790506922540,"cwd":"/work/app"}`+"\n"+
			`{"type":"user/message","seq":1,"time":1790506922600,"data":{"content":[{"type":"text","text":"the invoice job hammers the billing API"}],"source":{"kind":"user"},"role":"user"}}`+"\n")
	var progress bytes.Buffer
	if err := Ensure(dir, "", false, &progress); err != nil {
		t.Fatal(err)
	}
	if got := progress.String(); !strings.Contains(got, "updated 1 file") {
		t.Errorf("a new file of a kind that cannot resume took the replacement path: %q", got)
	}
	recordsAfter, err := os.ReadFile(filepath.Join(dir, "records.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recordsAfter[:len(recordsBefore)], recordsBefore) {
		t.Error("the records already on file were rewritten, so the pass paid for the whole store")
	}
	// The point of the old refusal: a file marked read whose sessions were
	// never indexed. Both halves are checked — the session answers a query,
	// and the one already there still does.
	for query, want := range map[string]string{
		"invoice job hammers": "ds-new",
		"exporter retries":    "s-first",
	} {
		ss, err := Search(dir, search.Options{Query: query, All: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(ss) != 1 || ss[0].ID != want {
			t.Fatalf("%q found %#v, want the session %s", query, ss, want)
		}
	}
}
