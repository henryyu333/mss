package sources

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/henryyu333/mss/internal/model"
)

// Harness is one AI tool mss ingests. Everything the index needs to discover,
// load, match and parse a tool's sessions lives in a single registry entry, so
// adding a harness is one entry here instead of edits scattered across the index
// dispatch (load, path-match, full-parse, incremental-parse). Signatures use
// primitives only (no index types) to keep sources a dependency-free leaf.
type Harness struct {
	Name  string                 // coarse name: claude, codex, cursor, ...
	Load  func() []model.Session // full cold load of every session
	Files func() []string        // current on-disk files to consider for indexing
	Kinds []FileKind             // one or more on-disk file shapes to match+parse
}

// FileKind is one on-disk file shape belonging to a harness. A harness can have
// several (codex has rollout logs and a history file; cursor has an IDE sqlite
// db and CLI transcripts), each matched and parsed differently.
type FileKind struct {
	// Name is the fine-grained kind reported for a path (e.g. "codex-history").
	Name  string
	Match func(path string) bool
	// Parse does a full parse. sinceNano>0 asks db-backed kinds to return only
	// sessions newer than that instant; file kinds ignore it.
	Parse func(path string, sinceNano int64) ([]model.Session, error)
	// ParseFrom resumes an incremental parse: offset for append-only text logs,
	// sinceNano for db-backed kinds. nil means the kind is not incremental.
	ParseFrom func(path string, offset, sinceNano int64) ([]model.Session, error)
	// Resumes reports whether the bytes from offset can be read on their own
	// and added to what is stored. nil means always. A kind whose new lines can
	// rewrite a record already stored says no, and the file is read whole.
	Resumes func(path string, offset int64) bool
	// Sidecar fingerprints the files beside a transcript that the reader takes
	// the session's title, workspace or clock from. The agent writes them
	// without touching the transcript, late or on a rename, so the fingerprint
	// is part of the file state and a change re-reads the session (#4319,
	// #4446). nil when the transcript holds it all.
	Sidecar func(path string) (size, stamp int64)
}

func sinceTime(nano int64) time.Time { return time.Unix(0, nano) }

// fullParse/offsetParse adapt parsers that take no time cursor to the FileKind
// signatures without a wrapper at every call site.
func fullParse(f func(string) ([]model.Session, error)) func(string, int64) ([]model.Session, error) {
	return func(p string, _ int64) ([]model.Session, error) { return f(p) }
}

func offsetParse(f func(string, int64) ([]model.Session, error)) func(string, int64, int64) ([]model.Session, error) {
	return func(p string, off, _ int64) ([]model.Session, error) { return f(p, off) }
}

// dbParse/dbParseFrom handle the db-backed kinds — opencode, cursor, goose,
// grok, hermes and zed — which filter by time rather than byte offset.
func dbParse(full func(string) ([]model.Session, error), since func(string, time.Time) ([]model.Session, error)) func(string, int64) ([]model.Session, error) {
	return func(p string, nano int64) ([]model.Session, error) {
		if nano > 0 {
			return since(p, sinceTime(nano))
		}
		return full(p)
	}
}

func dbParseFrom(full func(string) ([]model.Session, error), since func(string, time.Time) ([]model.Session, error)) func(string, int64, int64) ([]model.Session, error) {
	return func(p string, _ int64, nano int64) ([]model.Session, error) {
		if nano > 0 {
			return since(p, sinceTime(nano))
		}
		return full(p)
	}
}

// resumesUnlessAnswering is the Resumes of a format that files a tool call and
// its result as two lines joined by an id. A tail that answers a call made
// before it cannot be read on its own: the call is stored already, without the
// exit status or the refusal its result carries, and only a read that holds
// both marks it (#4443). hint is a substring every call and result line holds,
// so the rest of the tail is not decoded; answers gives the ids of the calls a
// line makes and the id of the call it answers, when the answer changes what
// the call recorded.
func resumesUnlessAnswering(hint string, answers func(m map[string]any) (calls []string, answered string)) func(string, int64) bool {
	return func(path string, offset int64) bool {
		if offset <= 0 {
			return true
		}
		made := map[string]bool{}
		ok := true
		_ = scanJSONLBytes(path, offset, func(line []byte) {
			if !ok || !bytes.Contains(line, []byte(hint)) {
				return
			}
			var m map[string]any
			d := json.NewDecoder(bytes.NewReader(line))
			d.UseNumber()
			if d.Decode(&m) != nil {
				return
			}
			calls, answered := answers(m)
			for _, id := range calls {
				made[id] = true
			}
			if answered != "" && !made[answered] {
				ok = false
			}
		})
		return ok
	}
}

func hasBase(p, base string) bool { return filepath.Base(p) == base }

// Registry returns the harnesses mss reads, in load order. Flattening the
// kinds preserves the original path-match precedence (matches are on disjoint
// roots/basenames, so order only needs to stay deterministic).
//
// MSS_STORES narrows it; without that variable this is every harness mss
// knows, which is what it has always been.
func Registry() []Harness {
	all := allHarnesses()
	want, ok := storesSelection()
	if !ok {
		return all
	}
	out := make([]Harness, 0, len(want))
	for _, h := range all {
		if want[h.Name] {
			out = append(out, h)
		}
	}
	return out
}

// AllHarnesses is every harness mss knows how to read, whatever MSS_STORES
// says. The set `--harness` accepts, and the number the documentation counts:
// silencing a store for one run does not make mss a tool that reads fewer
// harnesses.
func AllHarnesses() []Harness { return allHarnesses() }

func allHarnesses() []Harness {
	return []Harness{
		{
			Name: "claude", Load: LoadClaude, Files: ClaudeFiles,
			Kinds: []FileKind{{
				Name:      "claude",
				Match:     func(p string) bool { return strings.HasSuffix(p, ".jsonl") && UnderClaudeRoot(p) },
				Parse:     fullParse(ParseClaudeFile),
				ParseFrom: offsetParse(ParseClaudeFileFromOffset),
				Resumes:   claudeExitResumes,
			}},
		},
		{
			Name: "codex", Load: LoadCodex, Files: CodexFiles,
			Kinds: []FileKind{
				{
					Name:      "codex-history",
					Match:     func(p string) bool { return hasBase(p, "history.jsonl") && underCodexRoot(p, CodexRoot()) },
					Parse:     fullParse(ParseCodexHistory),
					ParseFrom: offsetParse(ParseCodexHistoryFromOffset),
				},
				{
					Name: "codex",
					Match: func(p string) bool {
						return codexRolloutWanted(p) && underAnyCodexRoot(p) && underAnyCodexSessionsRoot(p)
					},
					Parse:     fullParse(ParseCodexRollout),
					ParseFrom: offsetParse(ParseCodexRolloutFromOffset),
					Resumes:   codexResumes,
				},
			},
		},
		{
			Name: "opencode", Load: LoadOpencode,
			Files: func() []string {
				return append([]string{OpencodeDB()}, OpencodeDiffFiles()...)
			},
			Kinds: []FileKind{
				{
					Name:      "opencode",
					Match:     func(p string) bool { return p == OpencodeDB() },
					Parse:     dbParse(parseOpencodeStore, parseOpencodeStoreSince),
					ParseFrom: dbParseFrom(parseOpencodeStore, parseOpencodeStoreSince),
				},
				{
					// The per-session diff store beside the database: for most
					// sessions it is the only record of what they changed
					// (#3791). Keyed on the same session ids: a changed diff is
					// read as its session, whole, the way a full build holds it.
					Name: "opencode-diff",
					Match: func(p string) bool {
						return strings.HasPrefix(p, OpencodeDiffDir()+string(filepath.Separator)) &&
							strings.HasSuffix(p, ".json")
					},
					Parse: fullParse(ParseOpencodeDiffSession),
				},
			},
		},
		{
			Name: "cursor", Load: LoadCursor,
			Files: func() []string { return append(append([]string{}, CursorDBs()...), CursorTranscripts()...) },
			Kinds: []FileKind{
				{
					Name:      "cursor-db",
					Match:     func(p string) bool { return hasBase(p, "state.vscdb") && strings.HasPrefix(p, CursorUserRoot()) },
					Parse:     dbParse(ParseCursorDB, ParseCursorDBSince),
					ParseFrom: dbParseFrom(ParseCursorDB, ParseCursorDBSince),
				},
				{
					Name: "cursor",
					Match: func(p string) bool {
						return strings.HasSuffix(p, ".jsonl") && strings.HasPrefix(p, filepath.Join(CursorCLIRoot(), "projects"))
					},
					Parse: fullParse(ParseCursorTranscript),
				},
			},
		},
		{
			Name: "grok",
			Load: func() []model.Session { return append(LoadGrok(), LoadGrokDB()...) },
			Files: func() []string {
				files := GrokSessionFiles()
				if db := GrokDB(); fileExists(db) {
					files = append(files, db)
				}
				return files
			},
			Kinds: []FileKind{{
				Name: "grok",
				Match: func(p string) bool {
					return hasBase(p, "updates.jsonl") && strings.HasPrefix(p, filepath.Join(GrokRoot(), "sessions"))
				},
				Parse:     fullParse(ParseGrokFile),
				ParseFrom: offsetParse(ParseGrokFileFromOffset),
				Resumes:   GrokResumes,
				Sidecar:   besideSidecar("summary.json"),
			}, {
				// The maintained CLI writes no session files at all: one
				// SQLite store beside the config, like opencode's.
				Name:      "grok",
				Match:     func(p string) bool { return p == GrokDB() },
				Parse:     dbParse(func(p string) ([]model.Session, error) { return ParseGrokDBSince(p, time.Time{}) }, ParseGrokDBSince),
				ParseFrom: dbParseFrom(func(p string) ([]model.Session, error) { return ParseGrokDBSince(p, time.Time{}) }, ParseGrokDBSince),
			}},
		},
		{
			Name: "pi", Load: LoadPi, Files: PiSessionFiles,
			Kinds: []FileKind{{
				Name:      "pi",
				Match:     func(p string) bool { return strings.HasSuffix(p, ".jsonl") && strings.HasPrefix(p, PiRoot()) },
				Parse:     fullParse(ParsePiFile),
				ParseFrom: offsetParse(ParsePiFileFromOffset),
				Resumes:   piResumes,
			}},
		},
		{
			Name: "omp", Load: LoadOmp, Files: OmpSessionFiles,
			Kinds: []FileKind{{
				Name:      "omp",
				Match:     func(p string) bool { return strings.HasSuffix(p, ".jsonl") && underOmpRoot(p) },
				Parse:     fullParse(ParseOmpFile),
				ParseFrom: offsetParse(ParseOmpFileFromOffset),
				Resumes:   piResumes,
			}},
		},
		{
			// DeepSeek Harness writes one log per session, zstd-framed by
			// default, so a machine without the zstd CLI sees the files and
			// reads nothing out of them (SkipReason says so).
			Name: "deepseek", Load: LoadDeepSeek, Files: DeepSeekSessionFiles,
			Kinds: []FileKind{{
				Name:  "deepseek",
				Match: isDeepSeekLog,
				Parse: fullParse(ParseDeepSeekFile),
			}},
		},
	}
}

// HarnessNames lists every coarse harness name mss knows, in registry order.
// It is the set `--harness` accepts — independent of what is installed, so a
// known-but-empty harness stays valid and only a typo is rejected.
func HarnessNames() []string {
	reg := allHarnesses()
	out := make([]string, 0, len(reg))
	for _, h := range reg {
		out = append(out, h.Name)
	}
	return out
}

// IsKnownHarness reports whether name is a harness mss can read.
func IsKnownHarness(name string) bool {
	for _, h := range allHarnesses() {
		if h.Name == name {
			return true
		}
	}
	return false
}

// HarnessForKind names the store a fine-grained kind belongs to: "cline-sdk"
// and "cline-vscode" are both cline. A caller holding a kind and speaking to a
// person wants this one — the index run narrates per store, and looking a kind
// up under the harness's own name found nothing (#2229).
func HarnessForKind(kind string) string {
	for _, h := range allHarnesses() {
		if h.Name == kind {
			return h.Name
		}
		for _, k := range h.Kinds {
			if k.Name == kind {
				return h.Name
			}
		}
	}
	return ""
}

// KindForPath returns the fine-grained kind whose Match accepts p, or "".
func KindForPath(p string) string {
	for _, h := range Registry() {
		for _, k := range h.Kinds {
			if k.Match(p) {
				return k.Name
			}
		}
	}
	return ""
}

// KindsWithOffsetParsers names every kind that can resume a parse where the
// last pass stopped. The index gates its append path on this rather than on a
// list of harness names it has to remember to grow (#2870).
func KindsWithOffsetParsers() []string {
	var out []string
	for _, h := range Registry() {
		for _, k := range h.Kinds {
			if k.ParseFrom != nil {
				out = append(out, k.Name)
			}
		}
	}
	return out
}

// KindForPathKind returns the full FileKind whose Match accepts p, for
// callers that need to parse, not just classify.
func KindForPathKind(p string) (FileKind, bool) {
	for _, h := range Registry() {
		for _, k := range h.Kinds {
			if k.Match(p) {
				return k, true
			}
		}
	}
	return FileKind{}, false
}
