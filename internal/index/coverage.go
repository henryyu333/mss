package index

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/henryyu333/mss/internal/model"
	"github.com/henryyu333/mss/internal/query"
	"github.com/henryyu333/mss/internal/sources"
)

// SearchCoverage reports what a query could not see. The caller collects it
// right after EnsureForSearch, while the manifest still describes the pass
// that just ran: a store that read nothing, a file that failed, a line that
// did not parse, a message that was cut. An answer printed beside these is
// read as partial rather than as negative — 855 hits is a different claim
// when one of the stores was skipped.
//
// The manifest's IngestFiles is what the last pass left: for a file the pass
// read, the count that pass produced; for one it carried, whatever the file
// still owes.
func SearchCoverage(dir string) *query.Coverage {
	if dir == "" {
		dir = DefaultDir()
	}
	cov := &query.Coverage{Complete: true}
	m, err := readManifestCached(dir)
	if err != nil {
		return cov
	}
	read := map[string]bool{}
	for p := range m.Files {
		if h := sources.HarnessForKind(harnessForPath(p)); h != "" {
			read[h] = true
		}
	}
	order := map[string]int{}
	for i, h := range sources.Registry() {
		order[h.Name] = i
		if read[h.Name] {
			continue
		}
		reason := sources.SkipReason(h.Name)
		if reason == "" {
			reason = "not read"
		}
		cov.Unread = append(cov.Unread, h.Name+": "+reason)
	}
	sort.Strings(cov.Unread)
	health := healthFromFiles(m.IngestFiles)
	for _, name := range sortedHarnessNames(health, order) {
		h := health[name]
		if h.MalformedLines == 0 && h.FailedFiles == 0 {
			continue
		}
		if cov.Skipped == nil {
			cov.Skipped = map[string]query.SkippedIngest{}
		}
		cov.Skipped[name] = query.SkippedIngest{Records: h.MalformedLines, Files: h.FailedFiles}
	}
	cov.Clipped = clippedTotal(m)
	if len(cov.Unread) > 0 || len(cov.Skipped) > 0 || cov.Clipped > 0 {
		cov.Complete = false
	}
	return cov
}

// sortedHarnessNames walks health in registry order, then any extra names —
// a store the registry no longer lists still has its failures reported.
func sortedHarnessNames(health map[string]HarnessIngest, order map[string]int) []string {
	names := make([]string, 0, len(health))
	for name := range health {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		oi, okI := order[names[i]]
		oj, okJ := order[names[j]]
		if okI && okJ {
			return oi < oj
		}
		if okI != okJ {
			return okI
		}
		return names[i] < names[j]
	})
	return names
}

func clippedTotal(m Manifest) int {
	n := 0
	for _, e := range m.IngestFiles {
		n += e.Clipped
	}
	return n
}

// ClippedMessagePositions reports which record positions in a session hold a
// message the index stored cut, and whether the session is known to hold any
// clipped message at all. A window answers "what I show was truncated" from
// the positions when they exist and from the session-level count when they
// do not — an older index or a store that reports only counts still flags
// the window, it just cannot say where.
func ClippedMessagePositions(dir string, s model.Session) (positions map[int]bool, sessionHas bool) {
	if dir == "" {
		dir = DefaultDir()
	}
	m, err := readManifestCached(dir)
	if err != nil {
		return nil, false
	}
	for _, e := range m.IngestFiles {
		if idx := e.ClippedMsgIdx[s.ID]; len(idx) > 0 {
			if positions == nil {
				positions = map[int]bool{}
			}
			for _, i := range idx {
				positions[i] = true
			}
		}
		if e.ClippedSessions[s.ID] > 0 {
			sessionHas = true
		}
	}
	return positions, sessionHas
}

// MatchedRecordPositions walks the record log once and returns, for each
// session in keys, the 0-based record positions where match holds — the
// order and numbering `mss show` slices by. Positions count every stored
// record (servable and not), while match runs only on records the query may
// be served and the ingest gate did not silence: a record mss wrote into
// the transcript itself has no postings and cannot be a match here either.
// The gate replays per key — the stream is transcript order, which is what
// it mirrors.
func MatchedRecordPositions(dir string, keys map[string]bool, o query.Options, match func(Record) bool) (map[string][]int, error) {
	if dir == "" {
		dir = DefaultDir()
	}
	m, err := readManifestCached(dir)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	gates := map[string]*mssEchoGate{}
	out := map[string][]int{}
	err = eachRecordForKeys(filepath.Join(dir, "records.bin"), tablesFromManifest(m), keys, func(r Record) {
		pos := counts[r.Key]
		counts[r.Key] = pos + 1
		g := gates[r.Key]
		if g == nil {
			g = &mssEchoGate{}
			gates[r.Key] = g
		}
		if g.suppress(model.Message{Role: r.Role, Text: r.Text, Time: r.Time}) {
			return
		}
		if !recordServable(r.Role, o) {
			return
		}
		if match(r) {
			out[r.Key] = append(out[r.Key], pos)
		}
	})
	return out, err
}

// QueryRecordMatcher adapts the record check a query applies inside
// MatchedRecordPositions: exact/term/fuzzy all share the one matcher shape.
func QueryRecordMatcher(o query.Options, variants map[string][]string) func(Record) bool {
	m := newRecordMatcher(o, variants)
	return m.matches
}

// ErrorSigRecordMatcher matches the error-signature check — the records an
// error-tier hit counted on, which is lines whose friction hash is in the
// query's own signature set.
func ErrorSigRecordMatcher(o query.Options) func(Record) bool {
	sigs := querySigs(o.Query)
	return func(r Record) bool {
		for _, raw := range strings.Split(r.Text, "\n") {
			if line, ok := FrictionLine(raw); ok && sigs[frictionHash(line)] {
				return true
			}
		}
		return false
	}
}

// SessionIDsByPrefix expands each id-or-prefix into the sessions the index
// holds. An empty list for a token means "not found"; more than one means
// the prefix is ambiguous, which the caller reports rather than guessing.
func SessionIDsByPrefix(dir string, prefixes []string) map[string][]string {
	if dir == "" {
		dir = DefaultDir()
	}
	m, err := readManifestCached(dir)
	out := map[string][]string{}
	if err != nil {
		return out
	}
	for _, p := range prefixes {
		var hits []string
		for _, meta := range m.Sessions {
			if meta.ID == p || len(meta.ID) > len(p) && strings.HasPrefix(meta.ID, p) {
				hits = append(hits, meta.ID)
			}
		}
		sort.Strings(hits)
		out[p] = hits
	}
	return out
}

// ExpandExclude resolves what an exclusion reaches: every session named by
// ids or the nonce, plus the lineage each carries — the subagents it spawned
// and the forks it continues. Excluding one without its copies leaves the
// answer holding the same conversation under another id, which is the
// pollution the flag exists to remove.
//
// selfFound reports whether the nonce was found at all: a requested
// self-exclusion that matched nothing is a coverage gap, not silence. The
// check is at posting granularity — the nonce lives in one record of one
// session, and sessions hold records, so a session carrying the token is the
// session that asked.
func ExpandExclude(dir string, ids []string, nonce string) (out map[string]bool, selfFound bool, err error) {
	if dir == "" {
		dir = DefaultDir()
	}
	seeds := map[string]bool{}
	for _, id := range ids {
		seeds[id] = true
	}
	selfFound = true
	if nonce != "" {
		posts, perr := intersectPostings(dir, retrievalKeys(queryKeys(nonce)))
		if perr != nil {
			return nil, false, perr
		}
		m, merr := readManifestCached(dir)
		if merr != nil {
			return nil, false, merr
		}
		metaByOrd := sessionMetaByOrd(m)
		selfFound = false
		for _, p := range posts {
			if meta, ok := metaByOrd[p.Sid]; ok {
				if !seeds[meta.ID] {
					seeds[meta.ID] = true
				}
				selfFound = true
			}
		}
	}
	return Lineage(dir, seeds), selfFound, nil
}
