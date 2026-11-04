package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henryyu333/mss/internal/model"
)

func TestRecordQueryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	RecordQuery(dir, "connection pool exhausted", []string{"claude:s1", "codex:s2"})
	RecordQuery(dir, "pool", []string{"claude:s1"})

	counts := RecallCounts(dir)
	if counts["s1"] != 2 || counts["s2"] != 1 {
		t.Fatalf("counts = %v, want s1=2 s2=1", counts)
	}
	// The raw query text is not what the log keeps: the terms are, so a
	// later ask worded differently can still meet it.
	data, err := os.ReadFile(filepath.Join(dir, queriesLogFile))
	if err != nil {
		t.Fatal(err)
	}
	var e queryLogEntry
	if json.Unmarshal([]byte(strings.Split(string(data), "\n")[0]), &e) != nil {
		t.Fatal("first line is not the entry RecordQuery wrote")
	}
	if len(e.Terms) == 0 || e.Terms[0] != "connection" {
		t.Fatalf("terms = %v, want the relevance expansion of the query", e.Terms)
	}
	if len(e.Keys) != 2 || e.Keys[0] != "claude:s1" {
		t.Fatalf("keys = %v", e.Keys)
	}
}

func TestRecallCountsSkipsCorruptLines(t *testing.T) {
	dir := t.TempDir()
	good, _ := json.Marshal(queryLogEntry{TS: "2026-01-01T00:00:00Z", Keys: []string{"pi:p1"}})
	bad := []byte("{this is not json\n")
	if err := os.WriteFile(filepath.Join(dir, queriesLogFile),
		append(append(bad, good...), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if counts := RecallCounts(dir); counts["p1"] != 1 {
		t.Fatalf("corrupt line must not lose the good one: %v", counts)
	}
}

func TestRecordQueryWithoutIndexIsSilent(t *testing.T) {
	// No index directory at all: the note skips itself rather than create
	// one or fail. The search that served the answer already proved the
	// index exists; a write racing a swap must not invent it back.
	RecordQuery(filepath.Join(t.TempDir(), "missing"), "x", []string{"a:b"})
}

func TestCompactQueriesLogKeepsTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, queriesLogFile)
	var lines []byte
	for i := range 4000 {
		b, _ := json.Marshal(queryLogEntry{TS: "t", Keys: []string{fmt.Sprintf("h:s%d", i)}})
		lines = append(lines, b...)
		lines = append(lines, '\n')
	}
	if err := os.WriteFile(path, lines, 0o600); err != nil {
		t.Fatal(err)
	}
	compactQueriesLog(path)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() > queriesLogMaxBytes {
		t.Fatalf("compacted log still %d bytes", st.Size())
	}
	data, _ := os.ReadFile(path)
	first := strings.SplitN(string(data), "\n", 2)[0]
	var e queryLogEntry
	if json.Unmarshal([]byte(first), &e) != nil || len(e.Keys) == 0 {
		t.Fatalf("tail must start on a line boundary, got %q", first[:80])
	}
	counts := RecallCounts(dir)
	if counts["s3999"] != 1 {
		t.Fatalf("newest entry must survive the cut: %v", counts)
	}
	if counts["s0"] != 0 {
		t.Fatalf("oldest entries must be dropped, got %v", counts)
	}
}

func TestQueriesLogSurvivesRebuild(t *testing.T) {
	tmp := hermeticIndexEnv(t)
	dir := filepath.Join(tmp, "index.db")
	writeTinyIndex(t, dir)
	RecordQuery(dir, "alpha", []string{"claude:s1"})

	// A cold rebuild swaps the whole directory; without the carry the log
	// would be the one file whose history cannot be re-mined.
	if err := rebuild(dir, "", "", map[string]FileState{
		"source-a": {Path: "source-a", Size: 1, MTime: 2},
		"source-b": {Path: "source-b", Size: 3, MTime: 4},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if counts := RecallCounts(dir); counts["s1"] != 1 {
		t.Fatalf("rebuild dropped the recall log: %v", counts)
	}
}

func TestRecallCountsMergesHarnessCopies(t *testing.T) {
	// Two harnesses can index the same native id; the boost looks sessions
	// up by id alone, so the counts merge the way the lookup does.
	dir := t.TempDir()
	RecordQuery(dir, "q", []string{"claude:dup", "codex:dup"})
	if counts := RecallCounts(dir); counts[model.LoggedID("dup")] != 2 {
		t.Fatalf("counts = %v, want the two copies folded into one id", counts)
	}
}
