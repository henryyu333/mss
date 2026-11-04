package index

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/henryyu333/mss/internal/model"
)

// queriesLogFile records which sessions each answered search served. The
// recall boost — the "reused" count a hit can carry and the small lift an
// often-served session earns — reads nothing else, so the file is the whole
// of what the index remembers about its own use. It is derived state, not
// source of truth: lose it and the only thing lost is the boost's history.
const queriesLogFile = "queries.log"

// queriesLogMaxBytes bounds the file. Past it the tail is kept whole — old
// recalls matter least, and a log rewritten in place still serves.
const queriesLogMaxBytes = 1 << 17

// queryLogKeys bounds how many served sessions one answer records. A search
// that serves everything it has teaches the boost nothing.
const queryLogKeys = 25

// queryLogEntry is one served answer. Terms are the relevance expansion of
// the query, not the raw text: the log exists to let a later differently-
// worded ask find the same sessions, and terms are the vocabulary it will
// ask in.
type queryLogEntry struct {
	TS    string   `json:"ts"`
	Terms []string `json:"terms,omitempty"`
	Keys  []string `json:"keys"`
}

// RecordQuery appends one served-answer entry to the recall log. It is
// best-effort by design: a search that just answered must not fail — or even
// wait — because the note about it could not be written. The lock is the
// non-blocking index lock; a build in progress simply skips the note.
func RecordQuery(dir, text string, keys []string) {
	if dir == "" {
		dir = DefaultDir()
	}
	if len(keys) == 0 {
		return
	}
	unlock, ok, err := tryLockDir(dir)
	if err != nil || !ok {
		return
	}
	defer unlock()
	recordQueryNotesLocked(dir, text, keys)
}

func recordQueryNotesLocked(dir, text string, keys []string) {
	if len(keys) > queryLogKeys {
		keys = keys[:queryLogKeys]
	}
	entry := queryLogEntry{TS: time.Now().UTC().Format(time.RFC3339), Keys: keys}
	// Terms cost a few bytes each and are the only bridge between a query's
	// intent and its words; a regex search has no meaningful ones.
	entry.Terms = RelevanceTerms(text)
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	path := filepath.Join(dir, queriesLogFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	if _, err := f.Write(append(line, '\n')); err == nil {
		if st, statErr := f.Stat(); statErr == nil && st.Size() > queriesLogMaxBytes {
			_ = f.Close()
			compactQueriesLog(path)
			return
		}
	}
	_ = f.Close()
}

// compactQueriesLog keeps the tail of an overgrown log. Lines survive whole,
// so a reader never has to skip a half entry at the head.
func compactQueriesLog(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	// Keep the newest half's worth of lines. The cut is made at a line
	// boundary, not at the byte budget, so no half entry survives.
	var kept [][]byte
	size := 0
	lines := bytes.Split(data, []byte{'\n'})
	for i := len(lines) - 1; i >= 0 && size <= queriesLogMaxBytes/2; i-- {
		if len(lines[i]) == 0 {
			continue
		}
		kept = append([][]byte{lines[i]}, kept...)
		size += len(lines[i]) + 1
	}
	if len(kept) == 0 {
		return
	}
	tmp := path + ".qtmp"
	if err := os.WriteFile(tmp, append(bytes.Join(kept, []byte{'\n'}), '\n'), 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// RecallCounts reads the log back into the session-id -> served-count map
// the ranking boost consumes. A missing or unreadable log is empty history,
// not an error: the boost is an extra, never a reason a search fails. Keys
// that no longer resolve to a session are harmless — the lookup just misses.
func RecallCounts(dir string) map[string]int {
	if dir == "" {
		dir = DefaultDir()
	}
	counts := map[string]int{}
	f, err := os.Open(filepath.Join(dir, queriesLogFile))
	if err != nil {
		return counts
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e queryLogEntry
		if json.Unmarshal(line, &e) != nil {
			// A torn tail or a foreign line skips itself; the rest of the
			// log still counts.
			continue
		}
		for _, k := range e.Keys {
			if i := strings.IndexByte(k, ':'); i >= 0 && i+1 < len(k) {
				counts[model.LoggedID(k[i+1:])]++
			}
		}
	}
	return counts
}

// carryQueriesLog copies the recall log into an index being swapped in.
// Derived files are re-mined by a cold build, but the log's history cannot
// be rebuilt from transcripts — it exists only because searches ran.
func carryQueriesLog(dir, tmp string) {
	b, err := os.ReadFile(filepath.Join(dir, queriesLogFile))
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(tmp, queriesLogFile), b, 0o600)
}
