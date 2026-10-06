package main

import (
	"encoding/json"
	"io"

	"github.com/henryyu333/mss/internal/index"
	"github.com/henryyu333/mss/internal/jsonout"
	"github.com/henryyu333/mss/internal/model"
	"github.com/henryyu333/mss/internal/query"
	"github.com/henryyu333/mss/internal/search"
)

type recentJSON struct {
	SchemaVersion int             `json:"schema_version"`
	ProducedBy    string          `json:"produced_by"`
	Sessions      []model.Session `json:"sessions"`
	// Withheld is how many rows the trust policy kept out of this listing —
	// the same fact the text form prints on stderr (#990).
	Withheld int `json:"policy_withheld,omitempty"`
}

type sessionWindow struct {
	Offset   int `json:"offset"`
	Limit    int `json:"limit"`
	Total    int `json:"total"`
	Returned int `json:"returned"`
	// Clipped says the window contains a message the index stored short of
	// what the transcript holds — searchable to the cut, silent past it.
	Clipped bool `json:"clipped,omitempty"`
}

type sessionJSON struct {
	SchemaVersion int           `json:"schema_version"`
	ProducedBy    string        `json:"produced_by"`
	Session       model.Session `json:"session"`
	Window        sessionWindow `json:"window"`
}

// sessionCandidate is one row of `mss search --sessions`: the session's
// metadata and where the query landed in it, without excerpts. The shape is
// the listing's answer, not a hit's — a consumer deciding which sessions to
// read wants positions, not passages.
type sessionCandidate struct {
	Session        model.Session `json:"session"`
	HitCount       int           `json:"hit_count"`
	MatchedIndices []int         `json:"matched_indices,omitempty"`
}

// sessionsEnvelope is the candidate-list answer `mss search --sessions`
// writes. It exists because the ranked list answers a different question —
// which eight sessions to show — and a reader who wants the whole matching
// set gets a page instead of the set. Total and capped describe the list
// itself, not the hits: past sessionsListCap the list stops and says so.
type sessionsEnvelope struct {
	SchemaVersion int    `json:"schema_version"`
	ProducedBy    string `json:"produced_by"`
	// Match is the same word the hit envelope uses: "found" means these
	// sessions matched, "candidates" means nothing did and the list is the
	// relevance ranking's whole take.
	Match    string             `json:"match"`
	Query    string             `json:"query"`
	Total    int                `json:"total"`
	Capped   bool               `json:"capped,omitempty"`
	Withheld int                `json:"policy_withheld,omitempty"`
	Strict   int                `json:"strict,omitempty"`
	Sessions []sessionCandidate `json:"sessions"`
	Coverage *query.Coverage    `json:"coverage,omitempty"`
}

func printRecentJSONWithheld(w io.Writer, sessions []model.Session, sourceInstance string, withheld int) error {
	for i := range sessions {
		sessions[i].Messages = nil
		sessions[i].SetSource(sourceInstance)
		// The listing's own printer filters what a transcript supplied; this
		// path did not, and a title is free text (#3616).
		sessions[i] = search.SafeSession(sessions[i])
	}
	if sessions == nil {
		sessions = []model.Session{}
	}
	return json.NewEncoder(w).Encode(recentJSON{SchemaVersion: jsonout.Version, ProducedBy: "mss", Sessions: sessions, Withheld: withheld})
}

// sliceMessages applies --offset and --limit. Both are documented for `mss
// show` and, until #709, only the JSON path honoured them: the human-readable
// output printed the whole session, which on a 200k-message transcript is
// 600 001 lines and exactly what --limit exists to avoid.
func sliceMessages(ms []model.Message, offset, limit int) []model.Message {
	total := len(ms)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return ms[offset:end]
}

// printSessionJSON answers `mss show <id> --json`. Every message carries its
// index — the position `show --offset` and `--around` count by — so a reader
// can go from a hit's number to this window without re-counting the session.
// The window's clipped flag says a message in it was stored short of the
// transcript: the rest is in the file, and saying nothing made a searched
// line look absent from what mss holds (#2467).
func printSessionJSON(w io.Writer, dir string, session model.Session, offset, limit int, sourceInstance string) error {
	clipIdx, sessionClipped := index.ClippedMessagePositions(dir, session)
	clipped := sessionClipped
	if clipIdx != nil {
		clipped = false
		for i := offset; i < offset+limit && i < len(session.Messages); i++ {
			if clipIdx[i] {
				clipped = true
				break
			}
		}
	}
	total := len(session.Messages)
	session.Messages = sliceMessages(session.Messages, offset, limit)
	for i := range session.Messages {
		idx := offset + i
		session.Messages[i].Index = &idx
	}
	session.SetSource(sourceInstance)
	session = search.SafeSession(session)
	return json.NewEncoder(w).Encode(sessionJSON{
		SchemaVersion: jsonout.Version,
		ProducedBy:    "mss",
		Session:       session,
		Window: sessionWindow{
			Offset: offset, Limit: limit, Total: total, Returned: len(session.Messages),
			Clipped: clipped,
		},
	})
}

// printSessionsJSON writes the candidate-list answer `mss search --sessions`
// produces: the whole matching set as session metadata plus where the query
// landed, not excerpts. A reader walks it to choose what to open with
// `mss show --around`, which is the only reason the indices exist.
func printSessionsJSON(w io.Writer, sessions []model.Session, matchIndices map[string][]int, o search.Options, listCapped bool, listTotal int) error {
	cands := make([]sessionCandidate, 0, len(sessions))
	for _, s := range sessions {
		s.Messages = nil
		s.SetSource(o.SourceInstance)
		s = search.SafeSession(s)
		key := s.Harness + ":" + s.ID
		cands = append(cands, sessionCandidate{
			Session:        s,
			HitCount:       len(matchIndices[key]),
			MatchedIndices: matchIndices[key],
		})
	}
	if len(cands) == 0 {
		cands = []sessionCandidate{}
	}
	return json.NewEncoder(w).Encode(sessionsEnvelope{
		SchemaVersion: jsonout.Version,
		ProducedBy:    "mss",
		Match:         matchStringFor(o, len(cands)),
		Query:         o.Query,
		Total:         listTotal,
		Capped:        listCapped,
		Withheld:      o.PolicyWithheld,
		Strict:        o.Strict,
		Sessions:      cands,
		Coverage:      o.Coverage,
	})
}

// matchStringFor is the candidate list's form of the hit envelope's match
// rule: strict hits on a relevance answer still mean found.
func matchStringFor(o search.Options, n int) string {
	return search.MatchLabel(o, n)
}
