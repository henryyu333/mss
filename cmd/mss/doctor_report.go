package main

import (
	"bufio"
	"bytes"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/henryyu333/mss/internal/index"
	"github.com/henryyu333/mss/internal/jsonout"
	"github.com/henryyu333/mss/internal/model"
	"github.com/henryyu333/mss/internal/policy"
	"github.com/henryyu333/mss/internal/sources"
)

type doctorStore struct {
	Name  string   `json:"name"`
	State string   `json:"state"`
	Paths []string `json:"paths"`
	Files int      `json:"files"`
	// IndexedSessions is the other half of the pair the human line has printed
	// since #861: files against sessions is how collapsing shows, and a reader
	// of --json could see only the files (#1088). Imported is counted apart for
	// the same reason the printed line separates it (#894).
	IndexedSessions int `json:"indexed_sessions"`
	Imported        int `json:"indexed_from_elsewhere,omitempty"`
	// Denied names the path that refused to be read, so the warning can point
	// at the directory to fix rather than at the harness (#802). Partial says
	// the rest of the store was readable: sessions are missing from recall
	// rather than the whole harness (#816).
	// Unchecked says the permission walk stopped at its budget, so no
	// permission problem past that point was looked for (#1025).
	// Skipped is why part of the store could not be read at all — a missing
	// sqlite3 or zstd CLI. The text row has carried it in its detail; a reader
	// of --json saw a state and no reason (#1758).
	// Error is what the parser said when it refused the store. Without it the
	// report told a person their format may have changed and asked them to
	// report it, and the report they could send carried no more than the word
	// "unreadable" — neither they nor we could tell a renamed column from a
	// locked database from a missing CLI (#1642).
	// NeverRead is how many of the store's transcripts the index has no state
	// for at all. The printed row carries it for the same reason it carries
	// the files-against-sessions pair, and a reader of --json could see
	// neither: a store's session count of zero reads as "nothing written yet"
	// rather than "five files never opened" (#3747).
	// Note is why a missing store may hold no files on a machine that runs
	// the harness: current Amp keeps its threads on ampcode.com (#4355).
	NeverRead int    `json:"never_read,omitempty"`
	Note      string `json:"note,omitempty"`
	Error     string `json:"error,omitempty"`
	Denied    string `json:"denied,omitempty"`
	Skipped   string `json:"skipped,omitempty"`
	Partial   bool   `json:"partial,omitempty"`
	Unchecked bool   `json:"unchecked,omitempty"`
}

type doctorComponent struct {
	State string `json:"state"`
	Path  string `json:"path,omitempty"`
	Error string `json:"error,omitempty"`
}

// doctorIndexReport is the index component. It carries stale_stores, which
// docs/json-output.md documents and which the sqlite3 component has no meaning
// for — sharing one struct put the field in both. omitempty is deliberately
// absent: the document names the three keys that may be missing and this is
// not one of them, so with omitempty the zero the example shows was the one
// value never written, and a script reading it raised on every machine whose
// index was fresh (#1710).
type doctorIndexReport struct {
	State       string `json:"state"`
	Path        string `json:"path,omitempty"`
	StaleStores int    `json:"stale_stores"`
	// SessionsAhead counts sessions stamped later than this machine's clock.
	// One of those leads `mss last` and the digest's recent block until the
	// data is edited, and it arrives from an ordinary place: a hand-written
	// note's ts (#2063), or a store whose stamps were read in the wrong unit
	// (#2102). doctor has named the same fact for a peer since #1855.
	SessionsAhead int `json:"sessions_stamped_ahead"`
	// SourcesReadAt is when mss last walked this machine's stores, which is
	// not when the index was last written: an import rewrites the manifest
	// without opening a transcript, and a machine that syncs on a timer looked
	// freshly indexed on every surface that compared against the build time
	// (#3747). Empty when the store predates the field, and the word "never"
	// when an import built the index and nothing local has been read at all.
	SourcesReadAt string `json:"sources_read_at,omitempty"`
	// Format is how the store on disk relates to this build, when it is not
	// what this build writes. The text screen has carried a `format` row for
	// this since #877 and the JSON carried nothing, so a script watching index
	// health read "ok" on a store whose recall was off — the same miss #2292
	// closed for damage (#3600). Omitted when the store is current, which is
	// every ordinary run.
	Format string `json:"format,omitempty"`
}

type doctorReport struct {
	SchemaVersion int               `json:"schema_version"`
	Stores        []doctorStore     `json:"stores"`
	Index         doctorIndexReport `json:"index"`
	SQLite3       doctorComponent   `json:"sqlite3"`
	// Git is the other tool the text report names, and what it is needed for
	// degrades in silence: changed-file notes, worktree names, the task signal.
	// A machine checking this install could see a missing sqlite3 and not a
	// missing git (#2411).
	Git    doctorComponent                `json:"git"`
	Policy doctorPolicyReport             `json:"policy"`
	Ingest map[string]index.HarnessIngest `json:"ingest_health,omitempty"`
	// IngestFiles is where those counts came from. Without it the pointer at
	// the end of doctor's ingest line led back to the numbers it had just
	// printed, and the file to fix was never named (#2189).
	IngestFiles map[string]index.FileIngest `json:"ingest_files,omitempty"`
	Deep        *index.DeepReport           `json:"deep,omitempty"`
}

// doctorPolicyReport is the trust policy in the machine form. The text report
// has had a block for it since #661 while `--json` had no key at all, so a
// script could not see that recall is switched off on a machine (#1027).
type doctorPolicyReport struct {
	// State is one of default, active, unreadable — what is in force, not what
	// the file says, which is the distinction the text block draws too.
	State       string                      `json:"state"`
	Path        string                      `json:"path"`
	Error       string                      `json:"error,omitempty"`
	Total       int                         `json:"indexed_sessions"`
	Activations map[string]doctorPolicyRule `json:"activations"`
	Ignored     []string                    `json:"ignored,omitempty"`
	Inert       []string                    `json:"inert,omitempty"`
}

type doctorPolicyRule struct {
	Rule     string `json:"rule"`
	Withheld int    `json:"withheld"`
}

func collectDoctorPolicy(dir string) doctorPolicyReport {
	r := doctorPolicyReport{State: "active", Path: policy.Path(), Activations: map[string]doctorPolicyRule{}}
	exists, unknown, err := policy.Diagnose()
	switch {
	case !exists:
		r.State = "default"
	case err != nil:
		r.State = "unreadable"
		r.Error = err.Error()
	}
	pol := policy.Load()
	withheld, total := policyWithheldCounts(dir)
	r.Total = total
	for _, a := range []string{policy.ActivationSearch, policy.ActivationMCP, policy.ActivationAuto} {
		r.Activations[a] = doctorPolicyRule{Rule: pol.Describe(a), Withheld: withheld[a]}
	}
	r.Ignored = unknown
	r.Inert = unmatchedImportGroups(dir)
	return r
}

type doctorStoreCheck struct {
	name  string
	paths []string
	files []string
	parse func(string) ([]model.Session, error)
}

func collectDoctorReport(dir string) doctorReport {
	stores := doctorStoreChecks()
	report := doctorReport{SchemaVersion: jsonout.Version, Stores: make([]doctorStore, 0, len(stores))}
	storeMods := make([]time.Time, 0, len(stores))
	indexed := index.HarnessSessionCounts(dir)
	neverRead := index.HarnessUnreadCounts(dir)
	for _, check := range stores {
		store, mod := inspectDoctorStore(check)
		store.IndexedSessions = indexed[check.name]
		store.NeverRead = neverRead[check.name]
		report.Stores = append(report.Stores, store)
		storeMods = append(storeMods, mod)
	}
	report.Index = inspectDoctorIndex(dir, storeMods)
	report.Ingest = index.IngestHealth(dir)
	report.IngestFiles = index.IngestFilesReport(dir)
	report.SQLite3 = doctorSQLite3()
	report.Git.State = "missing"
	if _, err := exec.LookPath("git"); err == nil {
		report.Git.State = "ok"
	}
	report.Policy = collectDoctorPolicy(dir)
	return report
}

// firstDeniedDir walks the store roots until something refuses to be read and
// returns that path. The walk is bounded: doctor is a diagnostic command, but
// a harness root can hold tens of thousands of transcripts. The second result
// is false when the budget cut the walk short, so the caller can avoid calling
// a half-checked store whole (#1025).
func firstDeniedDir(paths []string) (string, bool) {
	// Directories, not entries: a store of 50k transcripts sits in a few
	// hundred of them, and counting files spent the budget in the first
	// project — a locked directory later in the walk was never reached, and
	// doctor reported the store whole (#864). A machine with a few thousand
	// projects hit the same wall at the directory bound, so it is high enough
	// that only a pathological tree reaches it (#1025).
	const budget = 200_000
	visited := 0
	whole := true
	home := sources.Home()
	for _, root := range paths {
		// aider's root is the home directory itself: any locked directory
		// anywhere under $HOME would be blamed on aider, and the walk would
		// cost the whole tree.
		if root == "" || root == home {
			continue
		}
		denied := ""
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsPermission(err) {
					denied = p
					return filepath.SkipAll
				}
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if visited++; visited > budget {
				whole = false
				return filepath.SkipAll
			}
			return nil
		})
		if denied != "" {
			return denied, true
		}
	}
	return "", whole
}

// doctorSQLite3 is `ok`, `missing`, or `broken`: on PATH and not answering a
// query, which reads every database store as empty.
func doctorSQLite3() doctorComponent {
	switch problem := sources.SQLite3Problem(); {
	case problem == "":
		return doctorComponent{State: "ok"}
	case sources.SQLite3Broken():
		path, _ := exec.LookPath("sqlite3")
		return doctorComponent{State: "broken", Path: path, Error: problem}
	}
	return doctorComponent{State: "missing"}
}

// storeNeedsSQLite3 names the harnesses mss reads through the sqlite3 CLI.
func storeNeedsSQLite3(name string) bool {
	switch name {
	case "opencode", "cursor", "grok":
		return true
	}
	return false
}

func doctorStoreChecks() []doctorStoreCheck {
	cursorFiles := append(sources.CursorTranscripts(), sources.CursorDBs()...)
	checks := []doctorStoreCheck{
		{"claude", sources.ClaudeRoots(), sources.ClaudeFiles(), sources.ParseClaudeFile},
		{"codex", sources.CodexRoots(), sources.CodexFiles(), parseDoctorCodex},
		{"opencode", []string{sources.OpencodeDB()}, presentDoctorFile(sources.OpencodeDB()), doctorProbeOpencode},
		{"cursor", []string{sources.CursorUserRoot(), sources.CursorCLIRoot()}, cursorFiles, parseDoctorCursor},
		{"grok", []string{filepath.Join(sources.GrokRoot(), "sessions")}, sources.GrokSessionFiles(), sources.ParseGrokFile},
		{"pi", []string{sources.PiRoot()}, sources.PiSessionFiles(), sources.ParsePiFile},
		{"omp", []string{sources.OmpRoot()}, sources.OmpSessionFiles(), sources.ParseOmpFile},
		{"deepseek", []string{sources.DeepSeekRoot()}, sources.DeepSeekSessionFiles(), sources.ParseDeepSeekFile},
	}
	// A store MSS_STORES silences has no row: it is not missing, not empty and
	// not unreadable, and saying any of those about a store nobody asked mss to
	// read is the noise this variable exists to remove.
	out := make([]doctorStoreCheck, 0, len(checks))
	for _, c := range checks {
		if sources.StoreSilenced(c.name) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func anotherFileOpens(files []string, except string) bool {
	const probe = 64
	tried := 0
	for _, p := range files {
		if p == except {
			continue
		}
		if tried >= probe {
			return false
		}
		tried++
		if f, err := os.Open(p); err == nil {
			_ = f.Close()
			return true
		}
	}
	return false
}

// stateForMissingTool names the state by the tool that is missing. zed and
// deepseek need zstd rather than sqlite3, and calling that "needs-sqlite3"
// sends the reader to install the wrong package (#1758).
func stateForMissingTool(reason string) string {
	if strings.Contains(reason, "sqlite3") {
		return "needs-sqlite3"
	}
	if strings.Contains(reason, "zstd") {
		return "needs-zstd"
	}
	return "unreadable"
}

func presentDoctorFile(path string) []string {
	// By content: an empty notes file is not a store with something in it, and
	// counting it made the two forms of this command disagree about the same
	// row — `missing` in the text, `ok` in JSON (#999).
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() && fi.Size() > 0 {
		return []string{path}
	}
	return nil
}

func parseDoctorCodex(path string) ([]model.Session, error) {
	if filepath.Base(path) == "history.jsonl" {
		return sources.ParseCodexHistory(path)
	}
	return sources.ParseCodexRollout(path)
}

func parseDoctorCursor(path string) ([]model.Session, error) {
	if filepath.Base(path) == "state.vscdb" {
		return sources.ParseCursorDB(path)
	}
	return sources.ParseCursorTranscript(path)
}

func inspectDoctorStore(check doctorStoreCheck) (doctorStore, time.Time) {
	store, mod, newest := probeDoctorStore(check)
	if newest == "" {
		return store, mod
	}
	return parseDoctorStore(check, store, mod, newest)
}

// probeDoctorStore is the half of the inspection that needs no parser: every
// root statted and listed, and the newest file opened. Every "denied" answer
// comes from here, so a caller asking only that stops here — the parse that
// follows read a whole SQLite store again on every `mss index` (#4272).
// newest is "" when the answer is already final.
func probeDoctorStore(check doctorStoreCheck) (store doctorStore, mod time.Time, newest string) {
	store = doctorStore{Name: check.name, State: "missing", Paths: check.paths, Files: len(check.files)}
	// A store the reader has excluded is answered before anything is read. The
	// row said `needs-sqlite3` or `needs-zstd` for a harness they may never
	// want read, which names a package to install for a problem they do not
	// have, and the exclude file had no way to say so (#3499).
	if sources.HarnessExcluded(check.name) {
		store.State = "excluded"
		return store, time.Time{}, ""
	}
	// A store with more than one root can be half-readable, and this loop
	// returns on the first refusal — the walk below has said so since #816
	// while this said the whole harness was denied (#3407).
	denyRoot := func(path string) (doctorStore, time.Time, string) {
		store.State = "denied"
		store.Denied = path
		store.Partial = len(check.files) > 0
		return store, time.Time{}, ""
	}
	for _, path := range check.paths {
		if path == "" {
			continue
		}
		fi, err := os.Stat(path)
		if err != nil {
			if os.IsPermission(err) {
				return denyRoot(path)
			}
			continue
		}
		if fi.IsDir() {
			f, err := os.Open(path)
			if err != nil {
				if os.IsPermission(err) {
					return denyRoot(path)
				}
				continue
			}
			_, err = f.Readdirnames(1)
			_ = f.Close()
			if err != nil && err != io.EOF && os.IsPermission(err) {
				return denyRoot(path)
			}
		}
	}
	// The file collectors swallow EACCES, so a locked directory silently takes
	// its sessions out of recall. With no files at all that read as a harness
	// nobody has used (#802); with some files it read as a complete store
	// missing a few, which is quieter still (#816).
	denied, whole := firstDeniedDir(check.paths)
	if denied != "" {
		store.State = "denied"
		store.Denied = denied
		store.Partial = len(check.files) > 0
		return store, time.Time{}, ""
	}
	// Nothing refused to be read *of what was walked*. Saying "found" for a
	// store whose walk stopped early is the same silence #864 closed, one
	// order of magnitude up (#1025).
	store.Unchecked = !whole
	if len(check.files) == 0 {
		// The text rows have separated a store whose disk went away from one
		// that was deleted since #933; a script reading this could not (#999).
		for _, p := range check.paths {
			if p != "" && storeDiskGone(p) {
				store.State = "unplugged"
				break
			}
		}
		return store, time.Time{}, ""
	}
	newest, mod = newestDoctorFile(check.files)
	f, err := os.Open(newest)
	if err != nil {
		if os.IsPermission(err) {
			// A file mss may not open is the same fault as a directory it may
			// not list, and has the same answer: name it and say it is
			// permissions. Calling it "unreadable" told the user their harness
			// had changed its format and asked them to report it (#1747).
			store.State = "denied"
			store.Denied = newest
			// "Partly readable" has to mean something did read: a store whose
			// files are all locked is not partly anything, and saying "some
			// sessions are missing" there understates it.
			store.Partial = anotherFileOpens(check.files, newest)
			return store, mod, ""
		}
		store.State = "parsed-zero"
		return store, mod, ""
	}
	_ = f.Close()
	return store, mod, newest
}

func parseDoctorStore(check doctorStoreCheck, store doctorStore, mod time.Time, newest string) (doctorStore, time.Time) {
	sessions, parseErr := check.parse(newest)
	store.State = "ok"
	// A store can be half-readable: cursor keeps CLI transcripts as JSONL and
	// its IDE sessions in SQLite, so the newest file can parse while the other
	// half cannot be read at all. The text row has said so in its detail all
	// along; this form called the store ok (#1758).
	if reason := sources.SkipReason(check.name); reason != "" {
		store.State = stateForMissingTool(reason)
		store.Skipped = reason
		store.Partial = len(check.files) > 1
		return store, mod
	}
	// A parser that could not run is not a store that could not be understood.
	// Without this, removing the sqlite3 CLI told the user their harness had
	// changed its format and asked them to report it — two lines above mss
	// naming the missing CLI itself (#792).
	if parseErr != nil && storeNeedsSQLite3(check.name) && !sources.SQLite3Available() {
		store.State = "needs-sqlite3"
		store.Skipped = sources.SQLite3Problem()
		return store, mod
	}
	// A parser that refuses to read the store is the loudest thing doctor can
	// learn, and it used to be discarded: a harness that changed its schema
	// showed up here as a healthy store while its recall was empty.
	if parseErr != nil {
		store.State = "unreadable"
		// Bounded: a sqlite3 error can quote the query, and the query is a
		// screenful. The head of it names the cause — a missing column, a
		// locked database, a file that is not a database at all.
		store.Error = boundedStoreError(parseErr.Error())
		return store, mod
	}
	// A session someone opened and closed without typing parses to nothing,
	// correctly. Calling that a parse failure sends people looking for a bug
	// in mss when the file simply holds no conversation — so only a file
	// with something to parse counts.
	if len(sessions) == 0 && !fileHasConversation(newest) {
		return store, mod
	}
	if len(sessions) == 0 {
		store.State = "parsed-zero"
	}
	return store, mod
}

func newestDoctorFile(files []string) (string, time.Time) {
	files = append([]string(nil), files...)
	sort.Strings(files)
	newest := files[0]
	var newestMod time.Time
	for _, path := range files {
		if fi, err := os.Stat(path); err == nil && fi.ModTime().After(newestMod) {
			newest, newestMod = path, fi.ModTime()
		}
	}
	return newest, newestMod
}

func inspectDoctorIndex(dir string, storeMods []time.Time) doctorIndexReport {
	result := doctorIndexReport{State: "missing", Path: dir}
	if !index.HasManifest(dir) {
		// A file where the directory belongs reads as "never built" on both
		// surfaces, and the fix is not the one `missing` implies: a build
		// refuses to run here rather than deleting what is there (#3610).
		if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
			result.State = "path-is-a-file"
		}
		return result
	}
	result.State = "ok"
	if ov, err := index.Overview(dir); err == nil {
		result.SessionsAhead = ov.Future
	}
	// When mss last read the stores, not when the manifest was last written:
	// a sync import writes the manifest and never looks at a local
	// transcript, so comparing against the build time reported zero stale
	// stores on a machine that syncs on a timer (#3747).
	readAt := index.ManifestSourcesReadAt(dir)
	if readAt.IsZero() {
		// An import built this index and nothing local has been read; a
		// machine reading the JSON should not have to infer that from an
		// absent field.
		result.SourcesReadAt = "never"
	} else {
		result.SourcesReadAt = readAt.UTC().Format(time.RFC3339)
	}
	for _, mod := range storeMods {
		if !mod.IsZero() && mod.After(readAt) {
			result.StaleStores++
		}
	}
	// Damage outranks staleness: a store that cannot answer is not merely
	// behind. The human report has named this since #735 while the JSON called
	// the same store "ok", so a script watching it for index health was told
	// everything was fine (#2292).
	if index.Damaged(dir) {
		result.State = "damaged"
		return result
	}
	// And the format, on the same terms. Two of the four states answer nothing
	// until the sources are re-read, which is not staleness either: `mss
	// index` is the fix for both, but a stale store still recalls while these
	// do not, and that is the distinction a health check is looking for.
	switch indexReadState(dir) {
	case index.ReadStateUnreadable:
		result.Format, result.State = "unreadable", "rereading"
		return result
	case index.ReadStateWithheld:
		result.Format, result.State = "withheld", "rereading"
		return result
	case index.ReadStateOlderRules:
		result.Format = "older-rules"
	case index.ReadStateNewer:
		result.Format = "newer"
	}
	if result.StaleStores > 0 {
		result.State = "stale"
		// "run `mss index`" is the advice attached to `stale`, and it cannot
		// be followed when the index cannot be written: the build fails with
		// the same permission error every time. search says so on this exact
		// state; doctor called it ordinary staleness (#1004).
		if !indexDirWritable(dir) {
			result.State = "stale-readonly"
		}
	}
	return result
}

func doctorParsedZeroWarning() string {
	var names []string
	for _, check := range doctorStoreChecks() {
		store, _ := inspectDoctorStore(check)
		if store.State == "parsed-zero" {
			names = append(names, store.Name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "warning: " + strings.Join(names, ", ") + " files found but newest parsed to zero"
}

// fileHasConversation reports whether a store file holds anything that should
// have produced a message. Harness files begin with setup records — protocol
// metadata, model config, tool lists — and a session that was opened and never
// used contains only those.
func fileHasConversation(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true // unreadable is a real problem; let the caller flag it
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(io.LimitReader(f, 1<<20))
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		for _, marker := range [][]byte{
			[]byte(`"role":"user"`), []byte(`"role": "user"`),
			[]byte(`"role":"assistant"`), []byte(`"role": "assistant"`),
			[]byte(`"type":"user"`), []byte(`"type":"assistant"`),
			[]byte("#### "),
		} {
			if bytes.Contains(line, marker) {
				return true
			}
		}
	}
	return false
}

// doctorProbeOpencode answers the only question the store check asks — does
// this store parse into sessions — without reading all of it. Parsing the
// whole database took 6.5 seconds of doctor's 8 on a 2.8 GB store, and every
// row after the first few adds nothing to the answer.
func doctorProbeOpencode(db string) ([]model.Session, error) {
	// A plain limit does not help: the query orders by session and message
	// time, so sqlite sorts the whole join before it can take the first row.
	// Narrowing to the newest session first is what makes this cheap.
	return sources.ParseOpencodeNewest(db)
}

// storeErrorMax is how much of a parser's complaint the report carries. Long
// enough for the sentence sqlite3 leads with, short enough that a quoted query
// does not become the report.
const storeErrorMax = 300

func boundedStoreError(msg string) string {
	msg = strings.TrimSpace(msg)
	if i := strings.IndexAny(msg, "\r\n"); i >= 0 {
		msg = strings.TrimSpace(msg[:i])
	}
	if len(msg) > storeErrorMax {
		msg = strings.TrimSpace(msg[:storeErrorMax]) + "…"
	}
	return msg
}
