package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/henryyu333/mss/internal/digest"
	"github.com/henryyu333/mss/internal/index"
	"github.com/henryyu333/mss/internal/model"
	"github.com/henryyu333/mss/internal/nfcfold"
	"github.com/henryyu333/mss/internal/policy"
	"github.com/henryyu333/mss/internal/query"
	"github.com/henryyu333/mss/internal/redact"
	"github.com/henryyu333/mss/internal/search"
	"github.com/henryyu333/mss/internal/sources"
)

// version is stamped by the release build (-ldflags "-X main.version=…").
// `go install …@vX.Y.Z` cannot pass ldflags, so an unstamped binary falls back
// to the module version Go recorded — the tag, or a pseudo-version for a
// checkout build — and says dev only when Go recorded nothing.
var version = "dev"

func buildVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

// errAlreadySaid exits non-zero for a command that has already told the reader
// what went wrong. Without it the choice was between a silent success and the
// same sentence twice: `mss unforget x` printed "that is not a command, here
// is the one that is" and exited 0, so a script that checked the code was told
// the work had been done.
var errAlreadySaid = errors.New("already said")

func main() {
	// A command that lands mid-rebuild waits for the whole of it, and silence
	// there reads as a hang rather than as a queue (#994).
	index.LockWaitNotice = func() {
		fmt.Fprintln(os.Stderr, "mss: another mss is building the index — waiting for it to finish")
	}
	if err := run(os.Args[1:]); err != nil {
		// Already said, on stderr, in the words that fit what happened: a
		// second sentence here would repeat it. The exit code is the point —
		// a command nobody typed correctly must not look like one that worked.
		if !errors.Is(err, errAlreadySaid) {
			fmt.Fprintln(os.Stderr, "mss:", rebuildWindowError(err))
		}
		os.Exit(1)
	}
}

// rebuildInProgress is a seam: a test can put the process in the window
// without racing a real rebuild.
var rebuildInProgress = index.RebuildInProgress

// rebuildWindowError names the one state that looks like a broken store and is
// not: a rebuild recreates the index directory, and a reader that lands in that
// window gets `open …/manifest.gob: no such file or directory` (#822).
func rebuildWindowError(err error) error {
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if !rebuildInProgress(index.DefaultDir()) {
		// Not a rebuild, so the manifest is missing for some other reason —
		// and on a machine where the cache cannot be created that reached the
		// reader as `open …/manifest.gob: no such file or directory`, which is
		// the shape #798 replaced everywhere else (#2267).
		if d := index.DefaultDir(); !dirExists(d) {
			if a := nearestExistingDir(filepath.Dir(d)); a != "" && !dirWritable(a) {
				return fmt.Errorf("cannot create the index directory (%s) — %s is not writable; check its permissions, or point MSS_INDEX_DIR somewhere writable", filepath.Dir(d), a)
			}
		}
		return err
	}
	return fmt.Errorf("the index is being rebuilt right now — run this again in a moment")
}

// loadFileSources reads the file-based stores whole, for the paths that run
// without a usable index. opencode's database goes through its own
// prefix/recent loaders at the call sites instead.
func loadFileSources() []model.Session {
	var ss []model.Session
	ss = append(ss, sources.LoadClaude()...)
	ss = append(ss, sources.LoadCodex()...)
	ss = append(ss, sources.LoadCursor()...)
	ss = append(ss, sources.LoadGrok()...)
	ss = append(ss, sources.LoadGrokDB()...)
	ss = append(ss, sources.LoadPi()...)
	ss = append(ss, sources.LoadOmp()...)
	ss = append(ss, sources.LoadDeepSeek()...)
	return ss
}

// worksWithNoHome names the commands that never write under a home directory
// they cannot find: they print, or they diagnose the very state the guard is
// about, or they carry their own refusal.
//
// `version` and `help` write nothing; `doctor` and `sources` diagnose the
// missing home. Skill installation handles its own destination before this guard.
var worksWithNoHome = map[string]bool{
	"version": true, "--version": true, "-version": true,
	"help": true, "--help": true, "-h": true,
	"doctor": true, "sources": true,
}

// dispatchKnows reports whether a word is a command mss runs, from either
// half of the dispatcher: the map, and the switch above it that holds the five
// commands taking their own argument parsing.
func dispatchKnows(name string) bool {
	if _, ok := commands[name]; ok {
		return true
	}
	switch name {
	case "show", "last", "search":
		return true
	}
	return false
}

// homelessRefusal answers what to do when mss cannot find a home directory
// and nothing else says where its index goes.
// A command somebody typed says why it cannot run. Nothing is written, which
// is the part that matters — the index is a database, and it was landing in
// whatever repository the agent was working in.
func homelessRefusal(name string) (bool, error) {
	// Both roots: the index answers to MSS_INDEX_DIR, and the config
	// directory to XDG_CONFIG_HOME, so one of them being absolute says
	// nothing about the other.
	if filepath.IsAbs(index.DefaultDir()) && filepath.IsAbs(xdgConfigHome()) {
		return false, nil
	}
	if worksWithNoHome[name] {
		// Only a name the dispatcher will actually take: `-v` is not a
		// command, so allowlisting it let it through the guard, miss the map
		// and land in the bare-query path — which builds an index (#1692).
		if _, known := commands[name]; known {
			return false, nil
		}
	}
	// The command's own name, or mss's: for a bare query the first word is
	// the reader's, and "retry cannot find your home directory" reads as if
	// `retry` were a command.
	if !dispatchKnows(name) {
		name = "mss"
	}
	return true, fmt.Errorf("%s cannot find your home directory — set HOME, or MSS_INDEX_DIR and XDG_CONFIG_HOME to absolute paths", name)
}

type command func(dir string, rest []string) error

var commands = map[string]command{
	"version":   cmdVersion,
	"help":      func(_ string, _ []string) error { printUsage(); return nil },
	"--help":    func(_ string, _ []string) error { printUsage(); return nil },
	"-h":        func(_ string, _ []string) error { printUsage(); return nil },
	"--version": cmdVersion,
	"-version":  cmdVersion,
	"sources": func(dir string, rest []string) error {
		// The command takes nothing, and --json exists on seven of its
		// neighbours — a script that reaches for it here got the
		// tab-separated table back and parsed it as JSON (#747).
		for _, a := range rest {
			return fmt.Errorf("sources takes no arguments — got %q", a)
		}
		printSources(dir)
		return nil
	},
	"doctor":        func(dir string, rest []string) error { return runDoctor(os.Stdout, rest, dir) },
	"index":         cmdIndex,
	"ctx":           cmdCtx,
	"install-skill": cmdInstallSkill,
}

func run(args []string) error {
	// Skill installation has no reason to inspect an index, store selection or
	// recall policy. Its own guard resolves the one user-selected destination.
	if len(args) > 0 && args[0] == "install-skill" {
		return cmdInstallSkill("", args[1:])
	}
	dir := index.DefaultDir()
	// Before anything reads or writes: every path mss writes hangs off the
	// home directory, and it answers "" when there is none, so
	// `filepath.Join("", ".cache", "mss")` is `.cache/mss` — mss built its
	// index in whatever directory it happened to be run from, on the commands
	// an agent runs unattended as much as on the ones a person types. #1690
	// fixed install; this is the rest (#1692).
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	if stop, err := homelessRefusal(name); stop {
		return err
	}
	// A typo in MSS_STORES silences every store rather than narrowing to one,
	// and what follows reads like a machine with no history instead of like a
	// mistake. Checked once, before any command runs.
	if err := sources.StoresSelectionError(); err != nil {
		return err
	}
	if len(args) == 0 {
		printUsage()
		return nil
	}
	sourceInstance := os.Getenv("MSS_SOURCE_INSTANCE")
	if wantsHelp(args[1:]) {
		if h := helpForCommand(args[0]); h != "" {
			// The same width the page itself respects: `mss last --help`
			// quotes the widest line in it, so a single command's help was
			// the one most likely to wrap (#1661).
			fmt.Print(wrapUsage(h, printableWidth(os.Stdout)))
			return nil
		}
	}
	if _, registered := commands[args[0]]; !registered || args[0] == "ctx" {
		warnPolicyDiagnostic(os.Stderr)
	}
	switch args[0] {
	case "show":
		return cmdShow(dir, args[1:], sourceInstance)
	case "last":
		return cmdLast(dir, args[1:], sourceInstance)
	case "search":
		return cmdSearch(dir, args[1:], sourceInstance)
	}
	if cmd, ok := commands[args[0]]; ok {
		return cmd(dir, args[1:])
	}
	return runBareSearch(dir, args, sourceInstance)
}

func cmdVersion(_ string, rest []string) error {
	if len(rest) > 0 {
		return fmt.Errorf("version takes no arguments — got %q", rest[0])
	}
	fmt.Fprintf(os.Stdout, "mss %s\n", buildVersion())
	return nil
}

// countingWriter passes writes through and remembers whether there were any.
type countingWriter struct {
	w io.Writer
	n int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += n
	return n, err
}

// indexQuietOutcome is the line an index run ends with when the build itself
// said nothing — another mss had already done the work. It reports what is
// actually on disk: a store can change again in the moment between the build
// finishing and this line, and claiming "up to date" there would be a guess.
func indexQuietOutcome(fresh bool, sessions int) string {
	if !fresh {
		return fmt.Sprintf("mss: another mss finished the build (%d session%s indexed); newer sessions are not in it yet", sessions, pluralS(sessions))
	}
	return fmt.Sprintf("mss: index is up to date (%d session%s)", sessions, pluralS(sessions))
}

func cmdIndex(dir string, rest []string) error {
	force := false
	quiet := false
	for _, a := range rest {
		switch a {
		case "--rebuild", "-rebuild":
			force = true
		case "--quiet", "-quiet":
			quiet = true
		default:
			return unknownFlag("index", a, indexFlags)
		}
	}
	// Where the command reports that it worked. Nobody is watching a run out
	// of a shell profile or a hook, and a line printed over the prompt on
	// every new shell is the kind of thing that gets a tool uninstalled
	// rather than reported; `mss index >/dev/null` also swallows the errors
	// you would want to see (#1827).
	//
	// Only the success reporting goes here. A run that cannot read a store or
	// cannot write the index still says so on stderr, or a scheduled job
	// stops working and nothing tells anyone. The counts and the outcome line
	// are what quiet is about; a warning that the exclude list is not applied,
	// a path that could not be read, and an index that came out empty for a
	// reason are not.
	said := io.Writer(os.Stderr)
	if quiet {
		said = io.Discard
	}
	// Silence reads as "it did not run". `update` on the newest release and
	// `doctor` on a fresh index both say so; this one returned to the prompt
	// with nothing (#824). Only here: on a search the same line would be noise.
	// The freshness check walks every store, which on a slow volume is the
	// longest part of the whole command, and it ran before the progress sink
	// existed (#1021).
	index.SweepStaleTmp(dir)
	fresh, n := index.UpToDate(dir, "")
	if fresh && !force {
		fmt.Fprintf(said, "mss: index is up to date (%d session%s)\n", n, pluralS(n))
		// "Up to date" is the most misleading place to stay quiet about it:
		// nothing changed on disk, so this is exactly where an exclusion set
		// after the build looks applied and is not.
		if index.ExclusionsChanged(dir) {
			fmt.Fprintln(os.Stderr, "mss: the exclude list changed since this index was built — `mss index --rebuild` applies it to sessions already indexed")
		}
		return nil
	}
	// Counted, because a run that waited for another build prints nothing of
	// its own: Ensure finds the index current under the lock and returns. The
	// command then owed a closing line and had none, leaving "waiting for it
	// to finish" as the last thing on screen (#1751).
	progress := &countingWriter{w: said}
	build := func() error { return index.Ensure(dir, "", force, progress) }
	draw := func() error { return withBuildProgress(build) }
	if quiet {
		// The live display paints the same progress the sink above is
		// discarding, and it paints it to stdout.
		draw = build
	}
	if err := draw(); err != nil {
		// The command whose whole job is building the index used to pass the
		// syscall through — `mkdir /…/index.db.tmp: permission denied` names
		// an internal temp path and no fix, while every reading command has
		// said what to change since ensureError was written (#798).
		return ensureError(dir, err)
	}
	// Inside the branch, not in its initializer: Go runs an `if` initializer
	// before it tests the condition, so the freshness walk — every registered
	// store, every candidate statted, the longest part of this command on a slow
	// volume — ran on every `mss index` and its answer was thrown away whenever
	// progress had already said what happened (#3500).
	if progress.n == 0 {
		fresh, n := index.UpToDate(dir, "")
		fmt.Fprintln(said, indexQuietOutcome(fresh, n))
	}
	// Two transcripts can carry the same harness:id — two files with the same
	// name in different projects. Both stay searchable, but one manifest row
	// holds them, so one project name covers both. Silence was the worst part
	// of it: the indexer counted every session on disk while the manifest held
	// fewer, and nothing connected the two numbers (#698).
	// A store mss could not read loses its sessions from recall, and the index
	// run is where someone is looking at the counts — doctor knowing about it
	// is not the same as saying it here (#818).
	for _, h := range sortedHarnesses(index.IngestHealth(dir)) {
		e := index.IngestHealth(dir)[h]
		if e.FailedFiles == 0 {
			continue
		}
		fmt.Fprintf(os.Stderr, "mss: %s: %d path%s could not be read — `mss doctor` names %s\n",
			h, e.FailedFiles, pluralS(e.FailedFiles), pluralWhich(e.FailedFiles))
	}
	// An exclusion applies at ingest, so it covers nothing already indexed —
	// and the sequence someone actually performs is to set the pattern, run
	// this command, and sync. That used to return silently having applied
	// nothing, and the export that followed still carried the project (#1307).
	if index.ExclusionsChanged(dir) {
		fmt.Fprintln(os.Stderr, "mss: the exclude list changed since this index was built — `mss index --rebuild` applies it to sessions already indexed")
	}
	// The parse count and the indexed count differ by exactly these, and this
	// is where both are on screen (#868).
	if n := index.ReportEmptySessions(); n > 0 {
		fmt.Fprintf(said, "mss: %d transcript%s held no message mss could index — not counted as %s\n",
			n, pluralS(n), pluralSessionWord(n))
	}
	if n := index.ReportCollisions(); n > 0 {
		fmt.Fprintf(os.Stderr, "mss: %d session%s %s an id with another transcript — each pair is filed under one project, the one whose file sorts first\n", n, pluralS(n), verbShare(n))
	}
	// The per-harness lines above count transcripts, so once any of them
	// merged, those lines add up to more than the index holds — and the
	// reconciling line below is TTY-only, which is not where anyone is
	// counting. Say the real totals whenever the sums have parted (#1091).
	//
	// Every merge, not only the ones mss warns about: a goose session that
	// came back from both of that harness's stores is one conversation and
	// gets no warning, but it is still two transcripts against one row (#2066).
	if b := index.LastBuild; index.ReportMerged() > 0 && b.Messages > 0 {
		fmt.Fprintf(said, "mss: indexed %d session%s, %d message%s — the per-harness lines above count transcripts, not rows\n",
			b.Sessions, pluralS(b.Sessions), b.Messages, pluralS(b.Messages))
	}
	// A machine with no agent history built an empty index and said nothing:
	// the step whose whole job is filling memory returned to the prompt after
	// a bare "indexing ..." line, and the state (no history anywhere, or a
	// store behind a permission wall) only surfaced on the next command.
	// Only when the index is empty too: a pass that found no transcript left
	// on disk still holds the sessions it keeps searchable, and the line
	// above has just said so (#4221).
	//
	// A store behind a permission wall is still named when the index holds
	// other sessions, without the "nothing to index" half: that line is the
	// only pointer to the store this pass could not read.
	if b := index.LastBuild; b.Sessions == 0 && b.Messages == 0 {
		denied := deniedStoreCount()
		switch {
		case indexIsEmpty(dir) && (denied > 0 || noAgentHistoryFound()):
			fmt.Fprintln(os.Stderr, emptyIndexReason(b, index.ReportEvictedFiles()))
		case denied > 0:
			fmt.Fprintln(os.Stderr, deniedStoresLine(denied, index.ReportEvictedFiles()))
		}
	}
	return nil
}

func cmdShow(dir string, rest []string, sourceInstance string) error {
	o, err := parseShow(rest)
	if err != nil {
		// The missing id is the one refusal that depends on the store: on a
		// machine with nothing indexed the honest answer is that there is
		// nothing to show, not that an argument is missing. parseShow has no
		// dir to ask, so the store-aware phrasing is applied here (#1063).
		if err.Error() == showNeedsID {
			return idPrefixNeeded(dir, "show needs an id-prefix", showNeedsID)
		}
		return err
	}
	if o.id == "" {
		return idPrefixNeeded(dir, "show needs an id-prefix", showNeedsID)
	}
	// A harness that does not exist matches nothing, and the refusal then
	// blamed the id the reader typed correctly (#2251). search, last, blame
	// and the MCP tools have always named the value they did not recognise.
	if err := checkHarness(&o.harness); err != nil {
		return err
	}
	// One refresh per show, and it happens before either lookup. The
	// --harness branch used to Ensure here and then fall into
	// findByPrefixHarness, which Ensured a second time whenever the exact
	// lookup missed — the ordinary `show <id-prefix> --harness name` call —
	// so one answer ran the build twice and printed the update line twice.
	//
	// The refresh is the same pass the prefix form runs. Without it the
	// exact-identity path read whatever was on disk, and a store below the
	// redaction floor is fresh — so `show --harness` printed text this build
	// would not write while `show <prefix>` re-read the sources first (#3617).
	// A store that cannot be rebuilt — read-only, no space — falls through to
	// the loader, which refuses and says why.
	//
	// --no-refresh skips the pass and reads the index as it was: the flag the
	// manual-recall flow uses after its one `mss index`, so parallel shows
	// queue on no write lock. The lookups then read the index only — walking
	// every store instead would be the refresh the caller declined.
	fresh := false
	var refresh *query.Refresh
	if o.noRefresh {
		if !index.HasManifest(dir) {
			return errNoIndexToRead
		}
		refresh = refreshStanza(dir)
		printNoRefreshNote(os.Stderr, dir)
	} else if err := ensureIndex(dir, "", false, os.Stderr); err == nil {
		fresh = true
	}
	var s model.Session
	var ok bool
	if o.harness != "" {
		// Exact identity first — that is what --harness is for, and what
		// --json requires. But the usage line documents an id *prefix*, and
		// routing --harness straight to the exact lookup made every
		// prefix+harness call fail: "mss show 019fa282 --harness codex" said
		// no session matches while the same prefix without --harness worked.
		s, ok, err = index.FindByIdentity(dir, o.harness, o.id)
		if err == nil && !ok {
			s, ok, err = findByPrefixHarness(dir, o.id, o.harness, fresh, o.noRefresh)
		}
	} else {
		s, ok, err = findByPrefixAfterEnsure(dir, o.id, fresh, o.noRefresh)
	}
	if err != nil {
		return err
	}
	if !ok {
		if n, cerr := index.SessionCount(dir); cerr == nil && n == 0 {
			return errors.New(strings.TrimPrefix(emptyIndexHint(fmt.Sprintf("no session matches %q", o.id)), "mss: "))
		}
		return fmt.Errorf("no session matches %q", o.id)
	}
	if err := denyPolicyHidden(o.id, s, os.Stderr); err != nil {
		return err
	}
	if o.json {
		return printSessionJSON(os.Stdout, dir, s, o.offset, o.limit, sourceInstance, refresh)
	}
	if o.brief {
		// The brief is a window by construction: a 2,000-message session one
		// line per message is not the page the flag exists to make, so the
		// JSON default applies and the note says where the window sits.
		total := len(s.Messages)
		s.Messages = sliceMessages(s.Messages, o.offset, o.limit)
		if line := showWindowNote(o.offset, len(s.Messages), total); line != "" {
			fmt.Fprintln(os.Stderr, line)
		}
	} else if o.sliced {
		// Both flags are documented for `show` and only the JSON path honoured
		// them; the text output printed the whole session (#709).
		total := len(s.Messages)
		s.Messages = sliceMessages(s.Messages, o.offset, o.limit)
		// The JSON has reported the window all along; the terminal printed the
		// slice and said nothing, so five turns of two hundred read the same as
		// a session that is five turns long (#2296). search says it in this
		// shape two commands away.
		if line := showWindowNote(o.offset, len(s.Messages), total); line != "" {
			fmt.Fprintln(os.Stderr, line)
		}
	} else if n := len(s.Messages); n > showLargeSession {
		fmt.Fprintf(os.Stderr, "mss: %d messages — `--offset n --limit n` reads a slice\n", n)
	}
	if o.harness == "" {
		noteAmbiguousPrefix(dir, o.id, "showing")
	}
	printSpawnEdges(os.Stderr, dir, s)
	// A pasted log is one message, and the index keeps the head of it. The
	// window note above says when messages were left out; nothing said when a
	// message was, so a reader searching a log for the line that explains a
	// failure searched one they believed was whole (#2467).
	if line := clippedMessageNote(dir, s); line != "" {
		fmt.Fprintln(os.Stderr, line)
	}
	if o.brief {
		if briefMatchCount(s.Messages, o.roles) == 0 && len(s.Messages) > 0 {
			fmt.Fprintf(os.Stderr, "mss: no %s messages in this window — `--offset`/`--limit` move it over every role\n", strings.Join(o.roles, " or "))
		}
		printSessionBrief(os.Stdout, s, o)
		return nil
	}
	search.PrintSession(os.Stdout, s)
	return nil
}

// clippedMessageNote says that a message in this session was stored short of
// what the transcript holds. mss records the count per file at ingest, and a
// store like Zed's threads.db keeps every session in one file, so the file's
// count put the note on every session in it (#4340). The file says a clip
// happened; the per-session split says which of its sessions holds it.
func clippedMessageNote(dir string, s model.Session) string {
	if s.Path == "" {
		return ""
	}
	files := index.IngestFilesReport(dir)
	e, ok := files[s.Path]
	if !ok || e.Clipped == 0 {
		return ""
	}
	n := e.ClippedSessions[s.ID]
	if e.ClippedSessions == nil {
		// A store built before the split: the file's count is all there is.
		n = e.Clipped
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("mss: %s stored short of what the transcript holds — the rest of %s is in the file itself",
		pluralMessages(n), pluralThem(n))
}

func pluralMessages(n int) string {
	if n == 1 {
		return "one message was"
	}
	return fmt.Sprintf("%d messages were", n)
}

func pluralThem(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

// showWindowNote is what the terminal says about a slice: which messages it
// printed and how many there are. Empty when the slice is the whole session,
// because a reader who asked for everything needs no arithmetic.
func showWindowNote(offset, returned, total int) string {
	if total == 0 {
		// Not the offset's fault, and asked before the full-window case below:
		// a session mss holds with nothing readable in it answers every
		// window the same way, and printing a bare header for it is the silent
		// empty this note exists to end.
		return "mss: this session has no messages to show"
	}
	if offset <= 0 && returned == total {
		return ""
	}
	if returned == 0 {
		return fmt.Sprintf("mss: --offset %d is past the end — the session has %d message%s", offset, total, pluralS(total))
	}
	first := offset + 1
	last := offset + returned
	if last >= total {
		// The last slice has no next one, and pointing at the offset after it
		// sent the reader to "--offset 16 is past the end — the session has 16
		// messages", which is mss answering its own advice (#3578).
		return fmt.Sprintf("mss: showing message%s %d-%d of %d — the end of the session",
			pluralS(returned), first, last, total)
	}
	return fmt.Sprintf("mss: showing message%s %d-%d of %d — `--offset %d` reads the next slice",
		pluralS(returned), first, last, total, last)
}

// printSpawnEdges names the sessions around this one when an agent spawned it
// or spawned others from it. A subagent's work lives in its own session, so a
// reader who found the parent could not get to it and a reader who found the
// child could not say what asked for it (#1385).
func printSpawnEdges(w io.Writer, dir string, s model.Session) {
	if s.Parent != "" && s.Kind == "fork" {
		// A Codex fork names the thread it branched from; nothing spawned it.
		fmt.Fprintf(w, "mss: forked from %s — `mss show %s`\n", digest.Short(s.Parent), pasteSafe(digest.Short(s.Parent)))
	} else if s.Parent != "" {
		by := ""
		if s.Agent != "" {
			by = " as " + s.Agent
		}
		fmt.Fprintf(w, "mss: spawned from %s%s — `mss show %s`\n", digest.Short(s.Parent), by, pasteSafe(digest.Short(s.Parent)))
	} else if s.Kind != "" {
		// A kind with no parent is all the harness recorded; saying which
		// session asked for it would be a guess.
		fmt.Fprintf(w, "mss: %s session — the harness records no parent for it\n", s.Kind)
	}
	children, err := ChildrenOfSession(dir, s.ID)
	if err != nil || len(children) == 0 {
		return
	}
	// A long session spawns a hundred agents; naming them all buries the line
	// that says how many there were.
	const named = 3
	ids := make([]string, 0, named)
	for _, c := range children[:min(named, len(children))] {
		ids = append(ids, digest.Short(c.ID))
	}
	rest := ""
	if len(children) > len(ids) {
		rest = fmt.Sprintf(" and %d more", len(children)-len(ids))
	}
	fmt.Fprintf(w, "mss: spawned %d session%s: %s%s\n", len(children), pluralS(len(children)), strings.Join(ids, ", "), rest)
}

// ChildrenOfSession is a thin seam so the surface can be tested without an
// index on disk.
var ChildrenOfSession = index.ChildrenOf

// ensureIndex is index.Ensure as this package calls it on a read path: a seam
// a test can use to count refreshes — `show` refreshed twice for one answer
// and nothing else could see it happen.
var ensureIndex = index.Ensure

// ensureSearch is ensureIndex for the search read path, so a --no-refresh
// answer can be pinned to zero refreshes.
var ensureSearch = index.EnsureForSearch

type showOptions struct {
	id, harness   string
	json          bool
	offset, limit int
	// around is the record index a window was asked to centre on; it folds
	// into offset before slicing so the note and the JSON agree.
	around    int
	aroundSet bool
	// sliced records that the reader asked for a slice, as opposed to the
	// JSON default: applying 50 to the text output would silently truncate
	// `mss show` for everyone who reads a session whole (#709).
	sliced bool
	// offsetSet says --offset was given, because around+offset is a
	// contradiction the parse refuses rather than resolves.
	offsetSet bool
	// noRefresh answers from the index as it was: the refresh pass is
	// skipped and so is the write lock it takes.
	noRefresh bool
	// brief prints the compact scanning form instead of the transcript:
	// briefLen is how many characters of each body survive, and roles (empty
	// means all) says whose messages are printed.
	brief    bool
	briefLen int
	roles    []string
}

// showLargeSession is where `mss show` mentions the flags that read a slice.
// A 200k-message transcript prints 600 001 lines.
const showLargeSession = 1000

func parseShow(args []string) (showOptions, error) {
	o := showOptions{limit: 50}
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--json":
			o.json = true
		case "--no-refresh":
			o.noRefresh = true
		case "--brief":
			o.brief = true
			o.briefLen = showBriefDefault
			// An optional length: `--brief 300` cuts at 300 characters, and a
			// bare --brief stops at the default. The id is never numeric, so
			// nothing else can be mistaken for it.
			if i+1 < len(args) {
				if n, e := strconv.Atoi(args[i+1]); e == nil {
					if n < 1 {
						return o, fmt.Errorf("--brief needs a positive number of characters")
					}
					o.briefLen = n
					i++
				}
			}
		case "--harness", "--offset", "--limit", "--around", "--role":
			if i+1 >= len(args) {
				return o, fmt.Errorf("%s needs value", a)
			}
			i++
			if strings.TrimSpace(args[i]) == "" {
				// Empty is how "no filter" is spelled inside mss (#1612).
				return o, fmt.Errorf("%s needs value", a)
			}
			if a == "--harness" {
				o.harness = args[i]
				continue
			}
			if a == "--role" {
				if err := checkRole(args[i]); err != nil {
					return o, err
				}
				o.roles = append(o.roles, args[i])
				continue
			}
			n, e := strconv.Atoi(args[i])
			if e != nil || n < 0 || (a == "--limit" && n == 0) {
				return o, fmt.Errorf("%s needs a positive integer", a)
			}
			switch a {
			case "--offset":
				o.offset = n
				o.offsetSet = true
				o.sliced = true
			case "--around":
				o.around = n
				o.aroundSet = true
				o.sliced = true
			default:
				o.limit = n
				o.sliced = true
			}
		default:
			if strings.HasPrefix(a, "-") {
				return o, unknownFlag("show", a, showFlags)
			}
			if o.id != "" {
				return o, fmt.Errorf("show accepts one session id")
			}
			o.id = a
		}
	}
	// The id first: it is what the command is for, and checking the flag ahead
	// of it cost two runs to learn two missing things (#820).
	if o.id == "" {
		return o, errors.New(showNeedsID)
	}
	if o.json && o.harness == "" {
		return o, fmt.Errorf("show --json requires --harness for exact identity")
	}
	if o.brief && o.json {
		return o, fmt.Errorf("show takes --brief or --json, not both — --brief is the compact text form of a window")
	}
	if len(o.roles) > 0 && !o.brief {
		return o, fmt.Errorf("--role needs --brief — it keeps only the messages the brief prints")
	}
	if o.offsetSet && o.aroundSet {
		return o, fmt.Errorf("show takes --around or --offset, not both")
	}
	if o.aroundSet {
		// The window centres on the record: `--around 12 --limit 5` shows
		// indices 10-14, and --limit is the whole width, not "messages each
		// side".
		o.offset = o.around - o.limit/2
		if o.offset < 0 {
			o.offset = 0
		}
	}
	if o.limit > 200 {
		return o, fmt.Errorf("show --limit must not exceed 200")
	}
	return o, nil
}

// ctxFromIDPrefix answers from the session an id-prefix names, and reports
// whether it did. Six characters is where cmdCtx starts reading an argument as
// an id at all — below that a token is far likelier to be a word — so this also
// runs after the text search comes up empty, where there is no answer left to
// shadow (#1614).
func ctxFromIDPrefix(dir, q string) (bool, error) {
	s, ok, err := findByPrefix(dir, q)
	if err != nil || !ok {
		return false, err
	}
	// Every other reading surface stops here when a rule denies the session's
	// origin; ctx handed the whole session over, and it is the command the hook
	// tells an agent to call (#1026).
	if kept, hidden := policyFilterSessionsCounted(policy.ActivationSearch, []model.Session{s}); len(kept) == 0 {
		fmt.Fprint(os.Stderr, policyHiddenNote(policy.ActivationSearch, hidden))
		return false, fmt.Errorf("no session matches %q", q)
	}
	// The one command an agent is told to call — the hook's lead line names
	// recall_context — answered from one of several sessions behind an elided
	// id without saying it was a choice, while show, share, resume, promote,
	// forget and handoff all said so (#923).
	noteAmbiguousPrefix(dir, q, "answering from")
	search.PrintContext(os.Stdout, s, "")
	return true, nil
}

func cmdCtx(dir string, rest []string) error {
	if len(rest) < 1 {
		return idPrefixNeeded(dir, "ctx needs a query or an id-prefix",
			"ctx needs query or id-prefix (see `mss last`)")
	}
	// ctx takes no flags, and --json/--harness/--project/--since all exist on
	// neighbouring commands — so reaching for one here is the obvious mistake.
	// Folding it into the query answered "no session matches", which is a
	// false statement about the store (#721).
	// Except behind a leading `--`, which is how search, fix and how already
	// spell "the rest is the query". Without it no caller can ask about a
	// query that starts with a dash, and the refusal reads to a plugin exactly
	// like an empty store.
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	} else {
		for _, a := range rest {
			if strings.HasPrefix(a, "--") {
				return fmt.Errorf("ctx takes no flags, only a query or id-prefix — got %q; a question that starts with a dash goes after `--`", a)
			}
		}
	}
	q := strings.Join(rest, " ")
	// Blank is not a query: it matched nothing and the search handed back the
	// first session in the store, so `mss ctx "$TOPIC"` with the variable
	// unset printed a transcript nobody asked for (#2259).
	if strings.TrimSpace(q) == "" {
		return idPrefixNeeded(dir, "ctx needs a query or an id-prefix",
			"ctx needs query or id-prefix (see `mss last`)")
	}
	if !strings.Contains(q, " ") && len(q) >= 6 {
		done, err := ctxFromIDPrefix(dir, q)
		if err != nil || done {
			return err
		}
	}
	o := search.Options{Query: nfcfold.Compose(q), All: true}
	if err := ensureForCLISearch(dir, o, false, os.Stderr); err != nil {
		if !staleUnwritableIndex(dir, err) {
			return err
		}
		fmt.Fprintf(os.Stderr, "mss: answering from the index as it was — %v\n", ensureError(dir, err))
	}
	// Detailed, not the plain form: retrieval answers a sentence by dropping
	// the terms nothing carries and returning the session under the close or
	// relevance tier. Handing those sessions to Run without the tier and its
	// variant map made scoring hunt for the whole literal query, score zero,
	// and report "no session matches" — about a store `mss search` answered
	// on the same words. ctx is the command the hook names to an agent, and
	// agents ask in sentences (#R8).
	result, err := index.SearchWithRecoveryDetailed(dir, o, os.Stderr)
	if err != nil {
		return err
	}
	ss := result.Sessions
	o.Tier = result.Tier
	// Which rung answered, said out loud on stderr as the search screen says
	// it — stdout stays the context block an agent parses. ctx served an
	// answer to a word the caller never typed and said nothing about it.
	if result.Neighbour {
		printNeighbour(os.Stderr, result.Variants)
		o.Stemmed = true
		o.FuzzyVariants = result.Variants
	} else if result.Stemmed {
		printStemmed(os.Stderr, result.Variants)
		o.Stemmed = true
		o.FuzzyVariants = result.Variants
	} else if result.Fuzzy {
		printSpellings(os.Stderr, result.Variants)
		o.Fuzzy = true
		o.FuzzyVariants = result.Variants
	}
	if result.Tier == search.TierClose && o.FuzzyVariants == nil {
		o.FuzzyVariants = result.Variants
	}
	var hits []search.Hit
	if result.Tier == search.TierError {
		fmt.Fprintln(os.Stderr, "mss: matched by error signature; showing the sessions that hit it")
		// Both tiers below build their own hits and never went through the cap
		// in RunDetailed, so `--limit 3` printed the whole window — fifty
		// sessions — and so did a query with no flag (#3345).
		hits, _ = capTierHits(search.ErrorHits(ss), o)
	} else if result.Tier == search.TierRelevance {
		o.Strict = result.Strict
		fmt.Fprintln(os.Stderr, relevanceLead(result.Strict))
		hits, _ = capTierHits(markStrictHits(search.RelevanceHitsWeighted(ss, index.RelevanceMatchTerms(o.Query), result.TermIDF), result), o)
	} else if hits, err = search.Run(ss, o); err != nil {
		return err
	}
	hits, policyHidden := policyFilterHitsCounted(policy.ActivationSearch, hits)
	if len(hits) == 0 && policyHidden > 0 {
		fmt.Fprint(os.Stderr, policyHiddenNote(policy.ActivationSearch, policyHidden))
		return fmt.Errorf("no session matches %q", q)
	}
	if len(hits) == 0 {
		// A prefix shorter than six characters never reached the id branch
		// above, so a session `mss show` opens on the same argument was
		// reported as no session at all (#1614). The query has already failed
		// here, so there is no answer for the id to shadow.
		if !strings.Contains(q, " ") && len(q) < 6 {
			done, ferr := ctxFromIDPrefix(dir, q)
			if ferr != nil {
				return ferr
			}
			if done {
				return nil
			}
		}
		// Same as #834 in files and restore: an empty store is not a miss on
		// the query.
		if n, cerr := index.SessionCount(dir); cerr == nil && n == 0 {
			return errors.New(strings.TrimPrefix(emptyIndexHint(fmt.Sprintf("no session matches %q", q)), "mss: "))
		}
		return fmt.Errorf("no session matches %q", q)
	}
	// The hit carries the matching snippets, not the session: for a promoted
	// note that meant the correction — which rarely repeats the words of the
	// decision — was missing, and the command whose whole job is packaging
	// context handed an agent a decision that had been withdrawn (#1011).
	whole := hits[0].Session
	if full, ok, ferr := findByPrefix(dir, whole.ID); ferr == nil && ok {
		whole = full
	}
	search.PrintContext(os.Stdout, whole, q)
	return nil
}

func cmdLast(dir string, rest []string, sourceInstance string) error {
	n, o, sinceRaw, err := parseLast(rest)
	if err != nil {
		return err
	}
	harnessRaw := o.Harness
	if err := checkHarness(&o.Harness); err != nil {
		return err
	}
	if err := checkRole(o.Role); err != nil {
		return err
	}
	// Uncut, because the count the reader is told about has to be the count
	// they would get by asking again — and the trust policy filters below,
	// after the cut, so a total taken before it promised rows the policy would
	// not hand over (#2638). RecentMatching builds the whole matching set
	// before cutting anyway, so nothing is read twice.
	ss, _, err := recentMatchingCounted(dir, 0, o)
	if err != nil {
		return err
	}
	// The listing is titles and project names, which is exactly what the trust
	// policy exists to keep off the screen — and it was the one path that never
	// consulted it, while `search` under the same rule refused and said so
	// (#937). Listing counts as browsing: the search activation governs it.
	ss, policyHidden := policyFilterSessionsCounted(policy.ActivationSearch, ss)
	total := len(ss)
	if n > 0 && len(ss) > n {
		ss = ss[:n]
	}
	// Printing nothing at all and exiting 0 leaves no way to tell whether the
	// command worked, found nothing, or failed silently — which is what a
	// fresh install sees. blame already answers this shape of question.
	if note := policyHiddenNote(policy.ActivationSearch, policyHidden); note != "" {
		fmt.Fprintln(os.Stderr, note)
	}
	// And the other rule that withholds rows here. It filters inside the index
	// (#2541), so the count comes from the manifest rather than from what came
	// back — the same walk the listing has just done, over metas already read.
	if note := ignoredHiddenNote(index.IgnoredMatching(dir, o)); note != "" {
		fmt.Fprint(os.Stderr, note)
	}
	if len(ss) == 0 {
		if o.JSON {
			return printRecentJSONWithheld(os.Stdout, nil, sourceInstance, policyHidden)
		}
		// The rule that emptied the list was named a line above; "no sessions
		// indexed yet — run `mss index`" is advice for a state mss is not in,
		// and the backside of teaching the listing to filter at all (#937,
		// #949).
		if policyHidden > 0 {
			return nil
		}
		// "Run mss index" is advice for an empty store. With a filter set it
		// is advice for a state the tool is not in: indexing changes nothing
		// and doctor reports the stores as found. Name what emptied the result
		// instead (#637).
		if where := activeFilters(o, sinceRaw, harnessRaw); where != "" {
			fmt.Fprintf(os.Stderr, "mss: no sessions match %s\n", where)
			fmt.Fprint(os.Stderr, olderThanWindow(dir, o.Since))
			return nil
		}
		// "run `mss index`" cannot bring back what the reader forgot, and on a
		// store emptied by their own settings it is the wrong instruction —
		// search has said so since #844; the listing did not (#1007).
		if note := hiddenByOwnSettings(); note != "" {
			fmt.Fprint(os.Stderr, "mss: no sessions indexed yet\n"+note)
			return nil
		}
		fmt.Fprintln(os.Stderr, emptyIndexHint("no sessions indexed yet"))
		return nil
	}
	if o.JSON {
		return printRecentJSONWithheld(os.Stdout, ss, sourceInstance, policyHidden)
	}
	// The cut is silent everywhere else on this screen: ten rows read as the
	// whole answer, and the two rules that withhold rows here already have a
	// line above the list. `last` takes its count as a bare argument, so that
	// is the form the way out takes (#2638).
	if total > len(ss) {
		fmt.Fprintf(os.Stderr, "mss: showing %d of %d — `mss last %d` shows the rest\n", len(ss), total, total)
	}
	for _, s := range ss {
		// A session whose timestamp was missing or unparseable carries the Go
		// zero time, and "0001-01-01" reads as corrupted data rather than as a
		// missing field. Search prints a dash here and the first screen leaves
		// such sessions out of its range; this was the one place that did not
		// follow the convention (#765).
		when := "-"
		if !s.Updated.IsZero() {
			// The reader's zone, like the brief and stats: a session stamped
			// 22:00 UTC is 01:00 tomorrow for its author, and this line put it
			// on the day before the other two screens did (#849).
			when = s.Updated.Local().Format("2006-01-02")
		}
		// The id's own day is not used here, unlike search: this line prints
		// the id whole, so nothing has to be rebuilt from the date (#883),
		// while borrowing the id's day made the column run 06, 07, 04 down
		// the screen for a reader far enough east of the writer (#1038).

		// Project, id and title are text a harness wrote, and this is one
		// line: an escape byte in any of them recolours the rest of the
		// listing and a carriage return rewinds it (#1090).
		fmt.Printf("[%s · %s · %s · %s]", s.Harness, redact.SafeForDisplay(displayProject(s)), when, redact.SafeForDisplay(s.ID))
		title := s.Title
		if title == "" {
			title = firstUserTitle(s)
		}
		// The title is transcript text going straight to a terminal: an escape
		// in it repaints the screen and a carriage return rewinds the line.
		// SafeForDisplay keeps a newline on purpose — the reading surfaces are
		// the session's own layout — but this is one row of a listing, and a
		// note title carries whatever a person wrote by hand (#2058).
		if title = search.SafeNoteTitle(redact.SafeForDisplay(title)); title != "" {
			// A session with no user turn borrows the assistant's opening line
			// (#692), and unmarked it read like the reader's own question
			// (#1100).
			if s.AgentTitle {
				fmt.Printf(" agent: %s", title)
			} else {
				fmt.Printf(" %s", title)
			}
		}
		fmt.Println()
	}
	// The listing is ordered by a date, so one that has not happened leads it
	// and nothing else on the screen says why. The first screen carries the
	// same sentence beside the same list, because leaving it unexplained makes
	// the counters and the list disagree for no visible reason (#696, #2104).
	if n := stampedAheadCount(ss, time.Now()); n > 0 {
		fmt.Fprintf(os.Stderr, "mss: %d session%s stamped later than this machine's clock — %s at the top of this list\n",
			n, pluralS(n), pluralThatThose(n))
	}
	return nil
}

// stampedAheadCount counts the listed sessions whose stamp is after now, by the
// same rule the first screen counts them with.
func stampedAheadCount(ss []model.Session, now time.Time) int {
	n := 0
	for _, s := range ss {
		if index.StampedAhead(s.Updated, now) {
			n++
		}
	}
	return n
}

// stampedAheadHits counts the same thing over a result set rather than a
// listing. Search ranks on recency among other things, so the sessions whose
// stamp cannot place them are the ones it places first.
func stampedAheadHits(hits []search.Hit, now time.Time) int {
	n := 0
	for _, h := range hits {
		if index.StampedAhead(h.Session.Updated, now) {
			n++
		}
	}
	return n
}

// pluralThatThose keeps the sentence above readable for one session and for
// several, the way pluralThem does for the ingest lines.
func pluralThatThose(n int) string {
	if n == 1 {
		return "that one is"
	}
	return "those are"
}

// cmdSearch is the explicit form. Bare `mss <words>` also searches, but a
// single word that happens to name a subcommand runs that instead, which is
// how `/mss uninstall` inside a plugin came back with "nothing matches".
// Anything shelling out to mss with user text should use this.
func cmdSearch(dir string, rest []string, sourceInstance string) error {
	if len(rest) == 0 {
		return fmt.Errorf("search needs a query")
	}
	return runSearch(dir, rest, sourceInstance)
}

// ensureForCLISearch refreshes the index before the search answers. An
// explicit invocation is willing to wait: the alternative — answering from
// what the index used to hold — reported the same question as unasked every
// time a harness wrote between `mss index` and the search that followed it.
// A long refresh narrates itself on stderr through `progress`.
//
// `--rebuild` means the same thing it always has: force, then wait.
func ensureForCLISearch(dir string, o search.Options, force bool, progress io.Writer) error {
	return ensureSearch(dir, o, force, progress)
}

// searchFromIndex reads an answer. A corrupt index normally heals through one
// rebuild-and-retry; a --no-refresh answer asked not to touch the index, so
// the damage is reported to the caller instead.
func searchFromIndex(dir string, o search.Options, noRefresh bool) (index.SearchResult, error) {
	if noRefresh {
		return index.SearchDetailed(dir, o)
	}
	return index.SearchWithRecoveryDetailed(dir, o, os.Stderr)
}

// errNoIndexToRead is what --no-refresh answers with when there is no index
// yet: the mode reads the index as it was, and there is no it.
var errNoIndexToRead = errors.New("no index to answer from yet — `mss index` builds one (--no-refresh reads the index as it was)")

// refreshStanza is the JSON envelope's record for an answer served from the
// index as it was: nothing walked the stores, and this is when something last
// did.
func refreshStanza(dir string) *query.Refresh {
	return &query.Refresh{Refreshed: false, LastRefresh: index.ManifestSourcesReadAt(dir)}
}

// printNoRefreshNote says on stderr what the refresh stanza says in JSON.
// Silence here would make a stale answer indistinguishable from a fresh one
// for a reader watching the terminal.
func printNoRefreshNote(w io.Writer, dir string) {
	if t := index.ManifestSourcesReadAt(dir); !t.IsZero() {
		fmt.Fprintf(w, "mss: answering from the index as it was — no refresh ran; it was last refreshed %s (run `mss index` to refresh it)\n", t.Local().Format("2006-01-02 15:04"))
		return
	}
	fmt.Fprintf(w, "mss: answering from the index as it was — no refresh ran, and the index records no refresh time (run `mss index` to refresh it)\n")
}

func runBareSearch(dir string, args []string, sourceInstance string) error {
	return searchWithOptions(dir, args, sourceInstance, true)
}

func runSearch(dir string, args []string, sourceInstance string) error {
	return searchWithOptions(dir, args, sourceInstance, false)
}

func searchWithOptions(dir string, args []string, sourceInstance string, bare bool) error {
	force := false
	noRefresh := false
	sessionsMode := false
	var excludeIDs []string
	var excludeSelf string
	var filtered []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			// Everything after -- is the query verbatim, even words spelled
			// like these flags: `mss search -- --exclude` asks for the text.
			filtered = append(filtered, args[i:]...)
			break
		}
		if a == "--rebuild" || a == "-rebuild" {
			force = true
			continue
		}
		if a == "--sessions" {
			sessionsMode = true
			continue
		}
		// Peeled here, like --exclude: parseSearch would otherwise fold it
		// into the query or refuse it as a near-miss of another flag.
		if a == "--no-refresh" || a == "-no-refresh" {
			noRefresh = true
			continue
		}
		if strings.HasPrefix(a, "--exclude=") {
			v := strings.TrimPrefix(a, "--exclude=")
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("--exclude needs value")
			}
			excludeIDs = append(excludeIDs, v)
			continue
		}
		if strings.HasPrefix(a, "--exclude-self=") {
			v := strings.TrimPrefix(a, "--exclude-self=")
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("--exclude-self needs value")
			}
			excludeSelf = v
			continue
		}
		if a == "--exclude" || a == "--exclude-self" {
			if i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
				return fmt.Errorf("%s needs value", a)
			}
			i++
			if a == "--exclude" {
				excludeIDs = append(excludeIDs, args[i])
			} else {
				excludeSelf = args[i]
			}
			continue
		}
		filtered = append(filtered, a)
	}
	o, err := parseSearch(filtered)
	if err != nil {
		return err
	}
	if o.Sort != "" && !sessionsMode {
		return fmt.Errorf("--sort needs --sessions — the candidate list is what it orders")
	}
	o.ExcludeIDs = excludeIDs
	o.ExcludeSelf = excludeSelf
	o.Sessions = sessionsMode
	if sessionsMode {
		// The candidate list is a machine answer by design: the point is a
		// set a reader walks, and text columns truncate what a session id
		// needs whole. --json does not change the envelope, which is already
		// JSON-shaped.
		o.JSON = true
	}
	harnessRaw := o.Harness
	if err := checkHarness(&o.Harness); err != nil {
		return err
	}
	if err := checkRole(o.Role); err != nil {
		return err
	}
	sinceRaw := sinceRawArg(filtered)
	o.SourceInstance = sourceInstance
	if noRefresh && force {
		return fmt.Errorf("search takes --no-refresh or --rebuild, not both — one answers without touching the index and the other rebuilds it")
	}
	if noRefresh {
		// The one thing --no-refresh cannot do is build an index that is not
		// there; say so rather than letting the read fail on a missing file.
		if !index.HasManifest(dir) {
			return errNoIndexToRead
		}
		o.Refresh = refreshStanza(dir)
		printNoRefreshNote(os.Stderr, dir)
	} else if err := withBuildProgress(func() error { return ensureForCLISearch(dir, o, force, os.Stderr) }); err != nil {
		// A store that cannot be written still has an index that can be read:
		// answering from it beats answering nothing (#904).
		if !staleUnwritableIndex(dir, err) {
			return ensureError(dir, err)
		}
		fmt.Fprintf(os.Stderr, "mss: answering from the index as it was — %v\n", ensureError(dir, err))
	}
	// Exclusions resolve after the refresh: an id found a moment ago by the
	// session asking is the id the answer must not contain, and resolving it
	// first would miss the session that is writing it.
	selfFound := true
	if len(o.ExcludeIDs) > 0 || o.ExcludeSelf != "" {
		var expanded map[string]bool
		expanded, selfFound, err = resolveSearchExcludes(dir, o.ExcludeIDs, o.ExcludeSelf, noRefresh)
		if err != nil {
			return err
		}
		if o.ExcludeSessions == nil {
			o.ExcludeSessions = map[string]bool{}
		}
		for id := range expanded {
			o.ExcludeSessions[id] = true
		}
	}
	o.Coverage = index.SearchCoverage(dir)
	if o.ExcludeSelf != "" {
		o.Coverage.SelfRequested = true
		o.Coverage.SelfExcluded = selfFound
		if !selfFound {
			o.Coverage.Complete = false
		}
	}
	result, err := searchFromIndex(dir, o, noRefresh)
	if err != nil {
		if noRefresh && index.IsCorrupt(err) {
			// The repair that normally runs here is a build, and --no-refresh
			// was asked not to touch the index.
			return fmt.Errorf("the index is damaged — `mss index` rebuilds it (--no-refresh never writes)")
		}
		return fmt.Errorf("search: %w", err)
	}
	// Before ranking, not after: the cap is applied while ranking, so a rule
	// that runs later still lets denied sessions occupy the result slots and
	// the allowed ones never reach the page (#1060).
	ss, policyHidden := policyFilterSessionsCounted(policy.ActivationSearch, search.WithoutIgnored(result.Sessions))
	o.PolicyWithheld = policyHidden
	o.Tier = result.Tier
	if result.Neighbour {
		printNeighbour(os.Stderr, result.Variants)
		o.Stemmed = true
		o.FuzzyVariants = result.Variants
	} else if result.Stemmed {
		printStemmed(os.Stderr, result.Variants)
		o.Stemmed = true
		o.FuzzyVariants = result.Variants
	} else if result.Fuzzy {
		printFuzzy(os.Stderr, result.Variants)
		o.Fuzzy = true
		o.FuzzyVariants = result.Variants
	}
	if result.Tier == search.TierClose && o.FuzzyVariants == nil {
		o.FuzzyVariants = result.Variants
	}
	var hits []search.Hit
	switch result.Tier {
	case search.TierError:
		// A pasted error IS a match; ErrorHits keeps the ranking and the error
		// neighbourhood the tier found. Re-scoring it as an ordinary run would
		// zero it out — the word ladder already failed, which is why this tier
		// fired at all.
		fmt.Fprintln(os.Stderr, "mss: matched by error signature; showing the sessions that hit it")
		hits = search.ErrorHits(ss)
		o.Total, o.Capped = result.Total, result.Capped
		if o.Total < len(hits) {
			o.Total = len(hits)
		}
		var capped bool
		hits, capped = capTierHits(hits, o)
		o.Capped = o.Capped || capped
	case search.TierRelevance:
		o.Strict = result.Strict
		fmt.Fprintln(os.Stderr, relevanceLead(result.Strict))
		hits = markStrictHits(search.RelevanceHitsWeighted(ss, index.RelevanceMatchTerms(o.Query), result.TermIDF), result)
		// This tier ranks and truncates inside retrieval, so counting the
		// sessions it handed back measures its window, not the match: every
		// query deeper than the window reported the window's own size and
		// capped: false, which told a consumer to stop checking exactly when
		// there was something to check. Take the tier's figures when it has
		// them; the paths that report none (a quoted query retried without
		// its quotes, served here under the relevance label) never truncated,
		// so what arrived is the whole of it.
		o.Total, o.Capped = result.Total, result.Capped
		if o.Total < len(hits) {
			o.Total = len(hits)
		}
		var relCapped bool
		hits, relCapped = capTierHits(hits, o)
		o.Capped = o.Capped || relCapped
	default:
		// RunDetailed rather than Run: the JSON envelope reports how many
		// sessions matched before the cap, and that is not recoverable from a
		// list the cap has already trimmed.
		detailed, rerr := search.RunDetailed(ss, o)
		if rerr != nil {
			// "run:" is the name of a function, and the reader of this line is
			// someone who mistyped a pattern (#2286). The only error that
			// reaches here in practice is the regexp compile, so it says which
			// input to look at, in the words the other bad flags use (#1602).
			if o.Regex {
				return rePatternError(o.Query, rerr)
			}
			return rerr
		}
		hits, o.Total, o.Capped = detailed.Hits, detailed.Total, detailed.Capped
	}
	// Policy scoping runs after the cap, so the pre-cap count can no longer
	// describe what is being returned. When nothing was capped the honest total
	// is simply what survived; when it was, there is no way to know how the
	// filters would have treated the hidden ones, so the pre-cap figure stands
	// and `capped` says to distrust it.
	attachMoved(hits)
	if !o.Capped {
		o.Total = len(hits)
	}
	if note := otherWordFormsNote(dir, o, hits); note != "" {
		fmt.Fprint(os.Stderr, note)
	}
	mistyped := false
	if len(hits) == 0 {
		// The policy is named before the generic advice: "try fewer words" is
		// wrong counsel for someone whose words were fine (#680). A filter the
		// caller set is the same kind of fact, and `mss last` has named it all
		// along (#715).
		switch note := policyHiddenNote(policy.ActivationSearch, policyHidden); {
		case note != "":
			fmt.Fprint(os.Stderr, note)
		case activeFilters(o, sinceRaw, harnessRaw) != "":
			fmt.Fprintf(os.Stderr, "mss: %q matched nothing under %s\n", o.Query, activeFilters(o, sinceRaw, harnessRaw))
			fmt.Fprint(os.Stderr, emptyRoleNote(dir, o.Role))
			fmt.Fprint(os.Stderr, olderThanWindow(dir, o.Since))
			// This branch does not go through printNoMatches, which is where
			// the line lives for the other miss, so it says it itself.
			fmt.Fprint(os.Stderr, ignoredHiddenNoteFor("answer", index.IgnoredWithAllTerms(dir, query.Tokens(o.Query))))
		default:
			mistyped = printNoMatches(os.Stderr, dir, o.Query, o.Regex)
		}
	}
	if o.Capped && len(hits) > 0 {
		// The cap is silent everywhere else: 15 results look like the whole
		// answer, and nothing says another N are waiting behind --all. mss
		// narrates every other place the ladder hides a session, so it says
		// this one too.
		//
		// Except to a reader who already typed --all: there the list was cut by
		// an explicit --limit, or by the relevance tier's own retrieval window,
		// and neither of those lifts with the flag they are being sent to
		// (#1608). Say what is shown and leave the advice out.
		if o.All {
			fmt.Fprintf(os.Stderr, "mss: showing %d of %d\n", len(hits), o.Total)
		} else {
			fmt.Fprintf(os.Stderr, "mss: showing %d of %d — add --all to see the rest\n", len(hits), o.Total)
		}
	}
	// The answer can also be short for a reason no flag lifts. Counted over the
	// sessions holding every term, so an ordinary search in a store with a big
	// ignored tree stays quiet (#2562).
	//
	// Only where there is an answer to be short: printNoMatches says the same
	// thing on the way to "try fewer words", and a miss printed it twice
	// (#2632).
	if len(hits) > 0 {
		if note := ignoredHiddenNoteFor("answer", index.IgnoredWithAllTerms(dir, query.Tokens(o.Query))); note != "" {
			fmt.Fprint(os.Stderr, note)
		}
	}
	if sessionsMode {
		// The candidate list answers a different question from the hits:
		// which sessions matched, and where — for a reader that will open
		// the transcript itself. The same filtering and policy applied to
		// the hits applies to the list, so nothing excluded or denied leaks
		// back in by taking the other door.
		listed, listCapped, listTotal := orderSessionsForList(dir, ss, hits, o, result)
		matchIdx, err := sessionsMatchIndices(dir, listed, o, result)
		if err != nil {
			return err
		}
		if err := printSessionsJSON(os.Stdout, listed, matchIdx, o, listCapped, listTotal); err != nil {
			return err
		}
		if mistyped {
			return errAlreadySaid
		}
		return nil
	}
	// The window this is being printed into, so the lines can be budgeted
	// rather than assumed 80 wide. Only for a terminal: a pipe gets the whole
	// line, since a script reading mss wants the text and not the layout
	// (#604).
	o.Width = printableWidth(os.Stdout)
	// JSON hit messages carry their record index — the numbering
	// `show --around` reads — so a consumer can walk from the answer to the
	// passage without re-searching the session. Text keeps what it always
	// printed.
	if o.JSON {
		annotateHitIndices(dir, hits)
	}
	search.Print(os.Stdout, hits, o)
	// A stamp that has not happened is a date the reader cannot use, and recency
	// is half of what puts a hit where it is. The listing has said so since
	// #2104, the brief and doctor count them, recall marks the one it quotes —
	// search was the surface that printed "Jan 1 2099" beside an answer and left
	// the reader to work out what it meant (#3595).
	if n := stampedAheadHits(hits, time.Now()); n > 0 {
		fmt.Fprintf(os.Stderr, "mss: %d session%s here stamped later than this machine's clock — a stamp that has not happened cannot be placed against the others\n",
			n, pluralS(n))
	}
	// A wrong guess at a command name falls through to search, and the hint
	// that names it ran only on an empty result — so a typo whose word happens
	// to be in the history got a conversation back and nothing about the
	// command it meant (#2197). After the results, on stderr, and only for the
	// bare form: `mss search doctro` is someone searching for the word.
	//
	// One word only, which is narrower than the empty-result hint on purpose.
	// There, a hint costs nothing over a failed search; here it lands on an
	// answer the reader may well have wanted, and "doctors pool" is a search
	// however close its first word sits to a command name.
	if bare && len(hits) > 0 && len(strings.Fields(o.Query)) == 1 {
		fmt.Fprint(os.Stderr, commandHint(o.Query))
	}
	// A search that found nothing is not a failure — that is the ordinary
	// answer to a question the history cannot settle. A command name that does
	// not exist is: `mss unforget x` did nothing, said so, and exited 0, so a
	// script that ran it and checked the code was told otherwise. The hint is
	// narrow enough to carry the distinction — it stays quiet unless a real
	// command is within one edit of what was typed.
	if mistyped {
		return errAlreadySaid
	}
	return nil
}

// printNeighbour narrates a co-occurrence swap. It is not a word form: the
// corpus says these two words keep company, which is a different claim and
// reads as a typo correction if it borrows the other sentence (#1786).
func printNeighbour(w io.Writer, variants map[string][]string) {
	keys := make([]string, 0, len(variants))
	for token := range variants {
		keys = append(keys, token)
	}
	sort.Strings(keys)
	for _, token := range keys {
		for _, variant := range variants[token] {
			if variant != "" && variant != token {
				fmt.Fprintf(w, "mss: no session has those words together, so mss tried one this corpus keeps beside %q: %s\n", token, variant)
			}
		}
	}
}

// relevanceLead is the sentence above a relevance answer.
//
// It said "no exact match" whatever the answer held. The relevance tier is
// also where a strict answer of fewer than thinAND sessions is published once
// the ranking has been hung underneath it, and there the sentence is false:
// over 93 two-word queries on a 2,422-session store, every one of the 20
// answers labelled relevance carried a strict head of 1 to 9 sessions, so the
// line disowned a real match every time it fired (#3815).
func relevanceLead(strict int) string {
	if strict > 0 {
		return fmt.Sprintf("mss: %s %s every word of the query; the rest below are ranked by relevance", pluralSessions(strict), holdOrHolds(strict))
	}
	return "mss: no exact match; showing sessions ranked by relevance to the whole query"
}

func holdOrHolds(n int) string {
	if n == 1 {
		return "holds"
	}
	return "hold"
}

// markStrictHits flags the hits of a relevance answer that hold every word of
// the query, so a caller reading one hit is told what that hit is. The count
// on the envelope says how many there are; the order does not, because the
// merged ranking can put a ranked session above a strict one.
func markStrictHits(hits []search.Hit, result index.SearchResult) []search.Hit {
	if result.Strict == 0 {
		return hits
	}
	for i := range hits {
		hits[i].Strict = result.IsStrict(hits[i].Session)
	}
	return hits
}

func printStemmed(w io.Writer, variants map[string][]string) {
	keys := make([]string, 0, len(variants))
	for token := range variants {
		keys = append(keys, token)
	}
	sort.Strings(keys)
	for _, token := range keys {
		for _, variant := range variants[token] {
			if variant == "" {
				fmt.Fprintf(w, "mss: ignoring %q — no session matches it together with the rest\n", token)
				continue
			}
			if variant != token {
				fmt.Fprintf(w, "mss: no exact match, trying word forms: %s -> %s\n", token, variant)
			}
		}
	}
}

// otherWordFormsNote says that a word form the query did not use has sessions
// of its own that this answer does not contain.
//
// The exact tier wins and stops, so `retry` returns what wrote "retry" and
// never reaches "retries" — the stem tier below it only runs when exact comes
// up empty. mss narrates every other time the ladder shapes an answer (close
// spellings, dropped terms, the trust policy); this rung was the silent one,
// and silence here reads as "that is all there is". Only the forms that bring
// sessions the answer does not already hold are worth a line.
func otherWordFormsNote(dir string, o query.Options, hits []search.Hit) string {
	if o.Regex || o.Tier != search.TierExact || len(hits) == 0 {
		return ""
	}
	terms := query.Tokens(o.Query)
	if len(terms) == 0 || len(terms) > 4 {
		return ""
	}
	forms := index.OtherWordForms(dir, terms)
	if len(forms) == 0 {
		return ""
	}
	var flat []string
	for _, list := range forms {
		flat = append(flat, list...)
	}
	// Spoken sessions only: mss prints this very note into terminals, the
	// transcripts keep it, and the next index reads the forms mss generated
	// back as forms the store holds (#3820).
	counts := index.TermSessionCountsSpoken(dir, flat)
	var parts []string
	for _, term := range terms {
		for _, form := range forms[term] {
			extra := counts[form] - sessionsHolding(hits, form)
			if extra <= 0 {
				continue
			}
			parts = append(parts, fmt.Sprintf("%q in %d more", form, extra))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("mss: word forms this answer leaves out: %s — search the form to see them\n", strings.Join(parts, ", "))
}

// sessionsHolding counts the returned sessions that already contain a word
// form, so the note reports what is missing rather than what is on the page.
func sessionsHolding(hits []search.Hit, form string) int {
	n := 0
	for _, h := range hits {
		for _, m := range h.Session.Messages {
			if containsWord(strings.ToLower(m.Text), form) {
				n++
				break
			}
		}
	}
	return n
}

// containsWord matches a whole word, not a substring: "retry" inside
// "retrying" is a different form and counting it would hide the one the note
// exists to name.
func containsWord(low, word string) bool {
	for i := 0; ; {
		j := strings.Index(low[i:], word)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(word)
		beforeOK := start == 0 || !isWordByte(low[start-1])
		afterOK := end == len(low) || !isWordByte(low[end])
		if beforeOK && afterOK {
			return true
		}
		i = start + 1
	}
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b >= 0x80
}

// termCountLine names the terms that do match on their own, so "try fewer
// words" says which. It stays quiet when every term is already unknown to the
// store — there is nothing to drop then, and a row of zeroes is noise.
func termCountLine(dir, q string) string {
	terms := query.Tokens(q)
	if len(terms) < 2 || len(terms) > 6 {
		return ""
	}
	counts := index.TermSessionCounts(dir, terms)
	var parts []string
	for _, t := range terms {
		if counts[t] > 0 {
			parts = append(parts, fmt.Sprintf("%q in %d", t, counts[t]))
		}
	}
	if len(parts) == 0 || len(parts) == len(terms) && len(terms) > 3 {
		return ""
	}
	return fmt.Sprintf("mss: on their own: %s — no session has them together\n", strings.Join(parts, ", "))
}

// printNoMatches says how big the store actually is, not how many sessions the
// query happened to load.
//
// On the index-backed path the loaded set is empty by definition when nothing
// matched, so this printed "no matches in 0 indexed sessions" for a perfectly
// healthy index — and internal/index/manifest.go reserves that exact sentence
// as the signature of a corrupt store, on the grounds that a reader told the
// index holds zero sessions concludes the tool is broken rather than that
// their query missed. It fired on every ordinary miss, so the signature could
// not be used to recognise the failure it was written for (#637).
func printNoMatches(w io.Writer, dir, q string, regex bool) (mistypedCommand bool) {
	// An empty store is not a query problem: "fewer words" cannot help when
	// nothing is indexed, and `last`, `blame` and the brief all say what to do
	// instead. Search is the command a new machine reaches for first (#832).
	if n, err := index.SessionCount(dir); err == nil && n == 0 {
		// "Zero indexed sessions" has two very different causes, and the early
		// return named only the first: a machine with no history, and a machine
		// where the reader forgot everything themselves. `mss index` cannot
		// bring back a tombstoned session (#844).
		if note := hiddenByOwnSettings(); note != "" {
			fmt.Fprintf(w, "mss: no matches for %q\n", q)
			fmt.Fprint(w, note)
			return
		}
		fmt.Fprintln(w, emptyIndexHint(fmt.Sprintf("no matches for %q", q)))
		return
	}
	// Nothing to look up: very short tokens are dropped and punctuation is
	// trimmed, so this query never reached the index. "Try fewer words"
	// cannot be followed with one word, or none (#828). The message does not
	// state the rule — the cut is on bytes, so "л" and "舵" are long enough
	// while "p" is not, and a rule that reads false to half the world's
	// alphabets is worse than none.
	// Both sentences below are about the word index, and --re does not use it:
	// a regex is matched against the text itself, so an emoji it did not find
	// is a miss like any other rather than something mss cannot look up.
	if !regex && len(query.Tokens(q)) == 0 {
		// Length is the wrong reason for a query that holds no word at all: an
		// emoji is four bytes, and it was dropped for being a symbol. Sending
		// that reader after a longer word sends them after nothing (#2133).
		if !hasIndexableRune(q) {
			fmt.Fprintf(w, "mss: nothing to search for in %q — mss indexes words, and emoji, symbols and punctuation are not words\n", q)
			return
		}
		fmt.Fprintf(w, "mss: nothing to search for in %q — every word in it is too short to look up\n", q)
		return
	}
	// The size worth naming is the part of the store this path may read. Under
	// a trust rule the two differ, and "no matches in 1 indexed session — try
	// fewer words" sent the reader after a wording problem in a session the
	// rule never let search open (#986).
	if reach, trusted, total, ok := reachableSessionCount(dir); ok && reach == 0 && total > 0 {
		fmt.Fprintf(w, "mss: no matches for %q\n", q)
		// Which rule emptied the path. Counting the ignore rule here (#2707)
		// put a store hidden by a path pattern under a sentence about the
		// trust policy, quoting a trust rule that allows everything.
		if trusted == 0 {
			fmt.Fprintf(w, "mss: the trust policy withholds every indexed session from this path (%s: %s)%s\n",
				policy.ActivationSearch, policy.Load().Describe(policy.ActivationSearch), seePolicyFile())
		} else {
			fmt.Fprintf(w, "mss: the ignore rule withholds every indexed session from this path (%s)%s\n",
				strings.Join(policy.Load().IgnorePatterns(), ", "), seePolicyFile())
		}
		return
	} else if ok {
		fmt.Fprintf(w, "mss: no matches in %d indexed session%s — try fewer words or --re (query %q)\n", reach, pluralS(reach), q)
	} else if n, err := index.SessionCount(dir); err == nil {
		fmt.Fprintf(w, "mss: no matches in %d indexed session%s — try fewer words or --re (query %q)\n", n, pluralS(n), q)
	} else {
		fmt.Fprintf(w, "mss: no matches — try fewer words or --re (query %q)\n", q)
	}
	// Before advising fewer words: the sessions that hold every one of them may
	// exist and be covered by the ignore rule, in which case rewording is the
	// wrong advice and the count is the answer (#2562).
	if note := ignoredHiddenNoteFor("answer", index.IgnoredWithAllTerms(dir, query.Tokens(q))); note != "" {
		fmt.Fprint(w, note)
	}
	// Which word to drop is the reader's next question, and mss read the
	// per-term counts to decide there was no intersection (#826).
	if line := termCountLine(dir, q); line != "" {
		fmt.Fprint(w, line)
	}
	// The first thing anyone types is a guess at a command name, and a wrong
	// guess falls through to search — where the advice is to use fewer words,
	// which cannot help someone who was not searching (#674). Falling through
	// stays the default; an empty result is where the other reading is worth
	// naming.
	if hint := commandHint(q); hint != "" {
		fmt.Fprint(w, hint)
		mistypedCommand = true
	}
	// Last of the reasons a miss is not a miss, and the only one where mss is
	// holding the answer rather than withholding it.
	if hint := roleServedHint(dir, q); hint != "" {
		fmt.Fprint(w, hint)
	}
	if note := hiddenByOwnSettings(); note != "" {
		fmt.Fprint(w, note)
	}
	return mistypedCommand
}

// roleServedHint names the answer mss is holding but ranking will never
// return.
//
// Commands, file lists and edits are 38% of the record log and none of it
// reaches ranking, on purpose: a path that happens to contain the words of a
// question is not an answer to it. `how` and `blame` serve those roles when
// asked for by role — so a word that lives only in a command or a filename
// misses here while both of them answer it, and "try fewer words" is counsel no
// rewording can follow, the same wrong turn #2562 and #686 closed for the other
// reasons a miss is not a miss (#2634).
//
// Neither half costs a record-log scan: the commands table is a prebuilt
// artefact and the touched-file lists are one metadata read.
func roleServedHint(dir, q string) string {
	terms := query.Tokens(q)
	if len(terms) == 0 {
		return ""
	}
	var out strings.Builder
	if n := commandsMatchingWords(dir, terms); n > 0 {
		match := "match"
		if n == 1 {
			match = "matches"
		}
		out.WriteString(howOfferLine(n, match, terms))
	}
	return out.String()
}

// commandsMatchingWords counts the commands in the recurring-command table that
// hold every one of these words.
//
// Commands rather than sessions, because the table cannot give an honest
// session count here: one session that ran two matching commands appears under
// both, ByProject is capped at commandProjectCap, and summing either would
// state a number that is wrong in a direction the reader cannot see. How many
// commands match is exact, and it is the number that decides whether `mss how`
// is worth typing.
//
// The trust policy keys on the project, which is what the table carries, so a
// command is counted only where a project this path may read has it. The ignore
// rule matches on path or project and the table has no path, so a session
// ignored by path alone is not excluded here — the same gap `hook-tool` has.
func commandsMatchingWords(dir string, terms []string) int {
	pol := policy.Load()
	n := 0
	for _, u := range index.ReadCommands(dir) {
		low := strings.ToLower(u.Command)
		matched := true
		for _, t := range terms {
			if !commandMentions(low, strings.ToLower(t)) {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		for proj := range u.ByProject {
			if pol.Allows(policy.ActivationSearch, proj) && !pol.Ignored("", proj) {
				n++
				break
			}
		}
	}
	return n
}

// howOfferLine is the half of the hint that offers a command-role search.
//
// The words go over as words: a quoted phrase becomes one term required
// contiguously, so quoting the whole query would hand over a search that
// misses under a line that had just counted three.
func howOfferLine(n int, match string, terms []string) string {
	// A term can start with a dash — someone asking about `-run` or `--limit`
	// — and the search would read it as a flag. `--` says the rest is the
	// query.
	dashed := ""
	for _, t := range terms {
		if strings.HasPrefix(t, "-") {
			dashed = "-- "
			break
		}
	}
	return fmt.Sprintf("mss: %d command%s this machine ran %s it — `mss search --role command %s%s`\n",
		n, pluralS(n), match, dashed, pasteSafeWords(terms))
}

// hiddenByOwnSettings names the states the reader created themselves and can
// undo. "Excluded by my own pattern" and "never happened" were one answer, and
// "try fewer words" is wrong counsel for the first (#686).
func hiddenByOwnSettings() string {
	var parts []string
	// Counted apart, because they are different rules and the reader acts on
	// them differently: a project pattern hides work mss read, and a
	// `harness:` line means a whole store was never walked (#3499). Calling
	// both "project patterns" told someone who had excluded their only store
	// that a project of theirs was hidden.
	stores := sources.ExcludedHarnesses()
	if n := len(sources.ExclusionPatterns()) - len(stores); n > 0 {
		parts = append(parts, fmt.Sprintf("%d project pattern%s excluded from indexing (%s)", n, pluralS(n), sources.ExcludePath()))
	}
	if n := len(stores); n > 0 {
		parts = append(parts, fmt.Sprintf("%d store%s not read at all (%s)", n, pluralS(n), sources.ExcludePath()))
	}
	if len(parts) == 0 {
		return ""
	}
	return "mss: this machine also has " + strings.Join(parts, ", ") + "\n"
}

// commandHint reads an empty search as a possible mistyped subcommand. It
// stays quiet unless a command name is within one edit of the first word: an
// empty search is usually an empty search, and a hint on every miss is noise
// that teaches people to skip the last line.
func commandHint(q string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(q), " ")
	if first == "" || strings.HasPrefix(first, "-") {
		return ""
	}
	// A hint is about a guess at a command, and a guess is short: a command
	// with an argument or two. "hook up the pool" is a sentence, and a hint
	// there is noise on top of a search that already failed — the same lesson
	// #715 learned about short words (#2115).
	if !looksLikeAnInvocation(q) {
		return ""
	}
	low := strings.ToLower(first)
	// A guess with an exact answer behind it. `mss hook context` is the
	// hyphen-free spelling of a command that exists, and the candidate list
	// below drops every `hook-` name on the grounds that plumbing is not
	// something anyone means to type — which is right about proposing it and
	// wrong about hiding it from the reader who typed its own stem (#2115).
	if hint := hyphenatedCommandHint(q, low); hint != "" {
		return hint
	}
	// A one- or two-letter word is a word, not a mistyped command name. Any
	// prefix counts as "near" — so "a" reached `mss aider` and every sentence
	// starting with a short word got a hint (#715, my own regression from
	// #674).
	if len([]rune(low)) < 4 {
		return ""
	}
	if _, ok := commands[low]; ok {
		return "" // reached search only because it was used as a word
	}
	for _, name := range []string{"search", "show", "last"} {
		if low == name {
			return ""
		}
	}
	// The switch in run() handles these before the map is consulted, so the
	// map alone would miss the commonest words of all — "serch" landed on
	// "bench" while `search` was one letter away.
	names := []string{"search", "show", "last"}
	for name := range commands {
		// Hidden plumbing is not something anyone means to type.
		if strings.HasPrefix(name, "-") || strings.HasPrefix(name, "hook-") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	near := nearestTarget(first, names)
	if near == "" {
		return ""
	}
	return fmt.Sprintf("mss: %q is not a command — did you mean `mss %s`?\n", first, near)
}

// hyphenatedCommandHint answers the guess that spelled a hyphenated command
// with a space: the two words joined name a real one, or the first word is the
// stem of some and there is no second word to join (#2115).
func hyphenatedCommandHint(q, low string) string {
	rest := strings.Fields(strings.TrimSpace(q))
	if len(rest) > 1 {
		joined := low + "-" + strings.ToLower(rest[1])
		if _, ok := commands[joined]; ok {
			return fmt.Sprintf("mss: %q is not a command — did you mean `mss %s`?\n", low+" "+rest[1], joined)
		}
	}
	// Only the stem on its own. With words after it that do not join into a
	// command, the reader is searching — "hook up the pool" is a sentence, and
	// a hint about `hook-context` there is noise on top of a failed search.
	if len(rest) != 1 {
		return ""
	}
	var stems []string
	for name := range commands {
		if strings.HasPrefix(name, low+"-") {
			stems = append(stems, name)
		}
	}
	if len(stems) == 0 {
		return ""
	}
	sort.Strings(stems)
	if len(stems) > 3 {
		stems = stems[:3]
	}
	return fmt.Sprintf("mss: %q is not a command on its own — `mss %s` and the rest of that family are; `mss help` names them\n",
		low, strings.Join(stems, "`, `mss "))
}

// looksLikeAnInvocation separates a guess at a command from prose. A command
// and an argument or two is short; "recall the decision we made about the pool"
// is a sentence, and the hint is about the first kind (#2115).
func looksLikeAnInvocation(q string) bool {
	return len(strings.Fields(q)) <= 3
}

// hasIndexableRune says whether a query holds anything the index could have
// kept. Letters and digits are what the tokenizer keeps; everything else is a
// separator, which is why an emoji-only query reaches the index as nothing.
func hasIndexableRune(q string) bool {
	for _, r := range q {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// sinceRawArg recovers the text the reader typed after --since. parseSearch
// keeps only the parsed duration, and the search path had nothing else to hand
// activeFilters, so it reported "since 720h0m0s" for `--since 30d` (#1059).
// Last wins, as in parseSearch.
func sinceRawArg(args []string) string {
	args = splitEqualsForms(args)
	raw := ""
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--since" {
			raw = args[i+1]
		}
	}
	return raw
}

// olderThanWindow names the store's own range when --since cut off everything
// in it. Without it someone returning to a year-old store is told their query
// matched nothing — advice about the query, when it was the window that
// emptied the result and no query under it could have returned anything.
// `mss stats` has said this since #854; search and `last` had not (#1059).
func olderThanWindow(dir string, since time.Duration) string {
	if since <= 0 {
		return ""
	}
	// Servable: this is advice on a search that came back empty, so the
	// sessions it counts have to be the ones search could have read (#2650).
	ov, err := index.OverviewServable(dir)
	if err != nil || ov.Sessions == 0 || ov.Newest.IsZero() {
		return ""
	}
	if !ov.Newest.Before(time.Now().Add(-since)) {
		return ""
	}
	return fmt.Sprintf("mss: every one of the %d indexed sessions is older than that window — the newest is %s\n",
		ov.Sessions, ov.Newest.Local().Format("2006-01-02"))
}

// checkHarness rejects a --harness value mss does not know. A typo used to be
// indistinguishable from a real harness with no sessions — both said "matched
// nothing under harness X" — so `--harness cluade` read as "you have no claude
// history" instead of "that is not a harness". A known-but-empty harness is
// still valid; only an unknown name is refused (#1113).
func checkHarness(name *string) error {
	if *name == "" || sources.IsKnownHarness(*name) {
		return nil
	}
	return fmt.Errorf("%q is not a harness mss knows — one of: %s", *name, strings.Join(sources.HarnessNames(), ", "))
}

// knownRoles is the set `--role` accepts. It lists the documented spellings the
// help text prints plus "tool-output", the stored form "tool" is an alias for —
// both reach tool records, so both must be accepted.
var knownRoles = []string{
	"user", "assistant", "tool", sources.RoleToolOutput,
	sources.RoleFiles, sources.RoleCommand, sources.RoleEdit, sources.RoleSummary,
}

// checkRole rejects a --role value that is not a known role. Like an unknown
// --harness, a typo used to be indistinguishable from a real role with no
// matches: `--role toool` said "matched nothing under role toool" instead of
// naming the mistake (#1113).
func checkRole(role string) error {
	if role == "" {
		return nil
	}
	for _, r := range knownRoles {
		if r == role {
			return nil
		}
	}
	return fmt.Errorf("%q is not a role mss knows — one of: %s", role, strings.Join(knownRoles, ", "))
}

// emptyRoleNote says when a role found nothing because the store holds none of
// that kind, rather than because the query missed.
//
// mss advertises that it indexes the work and not only the talk, and a harness
// that records no tool calls gives a transcript that looks complete: `--role
// tool` came back the same way a bad query does, and the reader had no way to
// tell "not found" from "not recorded" (#1321). Only asked when a role-filtered
// search already returned nothing.
func emptyRoleNote(dir, role string) string {
	if role == "" {
		return ""
	}
	stored := role
	if role == "tool" {
		stored = sources.RoleToolOutput
	}
	if index.HasRecordOfRole(dir, stored) {
		return ""
	}
	return fmt.Sprintf("mss: this index holds no %s records at all — the harnesses it read do not write them, or wrote none yet\n", role)
}

// activeFilters names the filters a caller set, so an empty result can say
// which of them emptied it rather than blaming the index. sinceRaw carries
// what the reader actually typed: "168h0m0s" is not the flag they passed.
func activeFilters(o search.Options, sinceRaw, harnessRaw string) string {
	var parts []string
	if o.Harness != "" {
		// Same reason as sinceRaw: `--harness notes` is stored as "mss", and
		// telling someone their "mss" filter matched nothing names a flag
		// they did not pass (#2191).
		name := o.Harness
		if harnessRaw != "" {
			name = harnessRaw
		}
		parts = append(parts, fmt.Sprintf("harness %q", name))
	}
	if o.Project != "" {
		parts = append(parts, fmt.Sprintf("project %q", o.Project))
	}
	if o.Role != "" {
		parts = append(parts, fmt.Sprintf("role %q", o.Role))
	}
	if o.Session != "" {
		parts = append(parts, fmt.Sprintf("session %q", o.Session))
	}
	// The one filter this did not know about, and the one most likely to match
	// nothing: --from exists for the multi-machine case, and the name a reader
	// types comes off another machine. Unnamed, it fell through to "no sessions
	// indexed yet — run `mss index`" on a store that was fine (#2642).
	if o.From != "" {
		parts = append(parts, fmt.Sprintf("from %q", o.From))
	}
	// The same predicate filterRecentSources uses. parseDur accepts a negative
	// duration, and a negative Since filters nothing — naming it would report a
	// filter that was never applied and hide the empty-store advice, which is
	// the right answer there.
	if o.Since > 0 {
		since := sinceRaw
		if since == "" {
			since = o.Since.String()
		}
		parts = append(parts, "since "+since)
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
}

// printSpellings names the words the close tier substituted. ctx uses this
// rather than printFuzzy: "did you mean the command" is advice for a person
// at a shell, and ctx is called by an agent that typed nothing.
func printSpellings(w io.Writer, variants map[string][]string) {
	keys := make([]string, 0, len(variants))
	for token := range variants {
		keys = append(keys, token)
	}
	sort.Strings(keys)
	for _, token := range keys {
		for _, variant := range variants[token] {
			if variant != token {
				fmt.Fprintf(w, "mss: no exact match, trying close spellings: %s -> %s\n", token, variant)
			}
		}
	}
}

func printFuzzy(w io.Writer, variants map[string][]string) {
	printSpellings(w, variants)
	keys := make([]string, 0, len(variants))
	for token := range variants {
		keys = append(keys, token)
	}
	sort.Strings(keys)
	for _, token := range keys {
		for _, variant := range variants[token] {
			// A misspelled subcommand is searched for as a word: `mss
			// isntall` corrects to "install" and returns sessions that
			// mention installing, which is not what the typist wanted.
			// Spelled correctly it would have run the command, so the only
			// case this fires on is the one where the hint is wanted.
			if variant != token && isSubcommand(variant) {
				fmt.Fprintf(w, "mss: `%s` is also a command — run `mss %s` if that is what you meant\n", variant, variant)
				return
			}
		}
	}
}

// isSubcommand reports whether a word names something mss can run.
func isSubcommand(word string) bool {
	if _, ok := commands[word]; ok {
		return true
	}
	switch word {
	case "show", "last", "help":
		return true
	}
	return false
}

// noteAmbiguousPrefix says when a selector reached more than one session.
//
// The prefix picks the newest of its matches, which is the right default and
// was a silent one: "2" resolved eleven sessions on a real store. `show`
// learned to say so in #719 and #859; promote, handoff, resume and share
// resolve the same way and still picked in silence — promote records a state
// against whichever session it chose (#872).
func noteAmbiguousPrefix(dir, id, action string) {
	// Counted under the rule the reader is searching by: a session the policy
	// withholds is not one they can reach with a longer prefix (#2401).
	pol := policy.Load()
	n := index.PrefixMatchesAllowed(dir, id, func(project string) bool {
		return pol.Allows(policy.ActivationSearch, project)
	})
	if n <= 1 {
		return
	}
	// When the matches are the same id in different harnesses there is no
	// longer prefix to reach for, and naming the harness is the only thing
	// that separates them (#719). The `harness:id` form is the one every
	// command that resolves a selector accepts — show/last also take
	// --harness, but promote/handoff/resume/share do not, so advising the
	// flag sent those readers into "unknown flag --harness".
	if hs := index.PrefixHarnesses(dir, id); len(hs) > 1 {
		forms := make([]string, len(hs))
		for i, h := range hs {
			forms[i] = h + ":" + id
		}
		fmt.Fprintf(os.Stderr, "mss: %d sessions share the id %q — %s the most recent; name one as %s\n",
			len(hs), id, action, strings.Join(forms, " or "))
		return
	}
	// "A longer prefix" is not available when the reader copied an elided id
	// off a result line: the characters that would disambiguate are the ones
	// the elision replaced (#859).
	advice := "use a longer prefix for another"
	if strings.Contains(id, "…") {
		advice = "the ids differ in the middle the line elides — `mss last` prints them whole"
	}
	fmt.Fprintf(os.Stderr, "mss: %d sessions match %q — %s the most recent; %s\n", n, id, action, advice)
}

func findByPrefix(dir, p string) (model.Session, bool, error) {
	fresh := false
	if err := ensureIndex(dir, "", false, os.Stderr); err == nil {
		fresh = true
	}
	return findByPrefixAfterEnsure(dir, p, fresh, false)
}

// findByPrefixAfterEnsure resolves an id prefix when the caller has already
// refreshed — or decided not to. fresh means the index holds what a refresh
// just wrote; otherwise the stores are read directly, so an index that cannot
// be rebuilt — read-only, no space — still answers. indexOnly is the
// --no-refresh lookup: the index is the only source, because walking every
// store would be the refresh the caller declined.
func findByPrefixAfterEnsure(dir, p string, fresh, indexOnly bool) (model.Session, bool, error) {
	if fresh || indexOnly {
		s, ok, err := index.FindByPrefix(dir, p)
		if err == nil || indexOnly {
			return s, ok, err
		}
	}
	ss := loadFileSources()
	ss = append(ss, sources.LoadOpencodePrefix(p)...)
	s, ok := search.FindByPrefix(ss, p)
	return s, ok, nil
}

// findByPrefixHarness resolves an id prefix within one harness, so the
// documented "mss show <id-prefix> --harness name" form works. fresh is the
// caller's one refresh, already done: doing it here again is how show ran the
// build twice for one answer. indexOnly is findByPrefixAfterEnsure's.
func findByPrefixHarness(dir, p, harness string, fresh, indexOnly bool) (model.Session, bool, error) {
	s, ok, err := findByPrefixAfterEnsure(dir, p, fresh, indexOnly)
	if err != nil || !ok {
		return model.Session{}, false, err
	}
	if s.Harness != harness {
		return model.Session{}, false, nil
	}
	return s, true, nil
}

func recent(dir string, n int) ([]model.Session, error) {
	return recentMatching(dir, n, search.Options{})
}

func recentMatching(dir string, n int, o search.Options) ([]model.Session, error) {
	ss, _, err := recentMatchingCounted(dir, n, o)
	return ss, err
}

// recentMatchingCounted is recentMatching with how many matched before the cut,
// so the listing can say what it left out (#2638).
func recentMatchingCounted(dir string, n int, o search.Options) ([]model.Session, int, error) {
	if err := index.Ensure(dir, "", false, os.Stderr); err == nil {
		if o.Role != "" {
			// The role has to travel into the index query, not just the filter
			// below: a scan with no role set drops file, command and edit
			// records on the way out — they are indexed but served only when
			// asked for — so `last --role files` saw sessions with the very
			// records it was selecting on already removed.
			ss, err := index.SearchWithRecovery(dir, search.Options{All: true, Role: o.Role}, io.Discard)
			if err == nil {
				ss = filterRecentSources(ss, o)
				return search.Recent(ss, n), len(ss), nil
			}
		} else if ss, total, err := index.RecentMatchingCounted(dir, n, o); err == nil {
			return ss, total, nil
		}
	}
	ss := filterRecentSources(loadFileSources(), o)
	if o.Harness == "" || o.Harness == "opencode" {
		ss = append(ss, filterRecentSources(sources.LoadOpencodeRecent(n), o)...)
	}
	return search.Recent(ss, n), len(ss), nil
}

func parseLast(args []string) (int, search.Options, string, error) {
	sinceRaw := ""
	n := 10
	seenN := false
	o := search.Options{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--json":
			o.JSON = true
		case "--harness", "--project", "--since", "--role", "--from":
			if i+1 >= len(args) {
				return n, o, sinceRaw, fmt.Errorf("%s needs value", a)
			}
			i++
			v := args[i]
			// Empty is how "no filter" is spelled inside mss, so an empty
			// argument reached the search as no filter at all and a scripted
			// `--project "$PROJECT"` with the variable unset quietly returned
			// the whole store (#1612). It is the same mistake as leaving the
			// value off, so it gets the same sentence.
			if strings.TrimSpace(v) == "" {
				return n, o, sinceRaw, fmt.Errorf("%s needs value", a)
			}
			switch a {
			case "--harness":
				o.Harness = v
			case "--project":
				o.Project = v
			case "--role":
				o.Role = v
			case "--from":
				o.From = v
			default:
				d, err := parseDur(v)
				if err != nil {
					return n, o, sinceRaw, err
				}
				o.Since = d
				sinceRaw = v
			}
		default:
			if strings.HasPrefix(a, "-") {
				if flagName(a) == "--limit" {
					// The two commands beside this one take --limit, so the
					// reader is carrying it over rather than guessing; the
					// count last takes is a bare argument (#3405).
					return n, o, sinceRaw, fmt.Errorf("last: unknown flag %q — the count is a bare argument, `mss last 3`", a)
				}
				return n, o, sinceRaw, fmt.Errorf("last: unknown flag %q", a)
			}
			// The only bare argument last takes is the count. Dropping anything
			// else in silence answered `mss last api-gateway` — the filter the
			// help spells `--project api-gateway` — with ten sessions from every
			// project and no sign the word did nothing (#1618).
			x, err := strconv.Atoi(a)
			if err != nil || x < 1 {
				return n, o, sinceRaw, fmt.Errorf("last: %q is not a count — use `mss last 5`, or --project/--harness to narrow", a)
			}
			if seenN {
				return n, o, sinceRaw, fmt.Errorf("last takes one count, got %d and %q", n, a)
			}
			n, seenN = x, true
		}
	}
	return n, o, sinceRaw, nil
}

func filterRecentSources(ss []model.Session, o search.Options) []model.Session {
	// These are sessions read straight off this machine's stores, so they are
	// local by definition: asking for another machine's work must not hand
	// back this one's.
	if o.From != "" && !strings.EqualFold(o.From, "local") {
		return nil
	}
	if o.Harness == "" && o.Project == "" && o.Since <= 0 && o.Role == "" {
		return ss
	}
	cut := time.Time{}
	if o.Since > 0 {
		cut = time.Now().Add(-o.Since)
	}
	out := make([]model.Session, 0, len(ss))
	project := strings.ToLower(o.Project)
	for _, s := range ss {
		if o.Harness != "" && s.Harness != o.Harness {
			continue
		}
		if project != "" && !strings.Contains(strings.ToLower(s.Project), project) {
			continue
		}
		if !cut.IsZero() && s.Updated.Before(cut) {
			continue
		}
		if o.Role != "" && !sessionHasRole(s, o.Role) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// sessionHasRole accepts the role names the help text documents. `--role tool`
// is what `mss help` promises and "tool-output" is what is stored, so the
// documented spelling matched nothing here while `mss search --role tool` —
// which grew the alias in #623 — worked (#717).
func sessionHasRole(s model.Session, role string) bool {
	for _, m := range s.Messages {
		if m.Role == role || (role == "tool" && m.Role == "tool-output") {
			return true
		}
	}
	return false
}

func firstUserTitle(s model.Session) string {
	for _, msg := range s.Messages {
		if msg.Role != "user" {
			continue
		}
		t := strings.Join(strings.Fields(msg.Text), " ")
		r := []rune(t)
		if len(r) > 60 {
			return strings.TrimSpace(string(r[:60])) + "…"
		}
		return t
	}
	return ""
}

func parseSearch(args []string) (search.Options, error) {
	o := search.Options{}
	var q []string
	// --limit=100 used to fall through and be searched for as a query term,
	// silently returning results for a different question than the one asked.
	args = splitEqualsForms(args)
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			// End of options: the rest is the query verbatim, even a word that
			// spells a flag. Without this there is no way to search for the
			// literal text of a flag name — `mss -- --json` kept parsing
			// --json as the flag and left "--" stranded in the query.
			q = append(q, args[i+1:]...)
			break
		}
		switch a {
		case "--json":
			o.JSON = true
		case "--re":
			o.Regex = true
		case "--all":
			o.All = true
		case "--harness", "--project", "--since", "--role", "--limit", "--session", "--sort":
			if i+1 >= len(args) {
				return o, fmt.Errorf("%s needs value", a)
			}
			i++
			v := args[i]
			// Empty is how "no filter" is spelled inside mss, so an empty
			// argument reached the search as no filter at all and a scripted
			// `--project "$PROJECT"` with the variable unset quietly returned
			// the whole store (#1612). It is the same mistake as leaving the
			// value off, so it gets the same sentence.
			if strings.TrimSpace(v) == "" {
				return o, fmt.Errorf("%s needs value", a)
			}
			switch a {
			case "--harness":
				o.Harness = v
			case "--project":
				o.Project = v
			case "--role":
				o.Role = v
			case "--session":
				o.Session = v
			case "--limit":
				n, err := strconv.Atoi(v)
				if err != nil || n < 1 || n > 100 {
					return o, fmt.Errorf("--limit needs an integer from 1 to 100")
				}
				o.Limit = n
			case "--sort":
				if v != sortUpdated {
					return o, fmt.Errorf("--sort takes updated — %q is not an order mss knows", v)
				}
				o.Sort = v
			default:
				d, err := parseDur(v)
				if err != nil {
					return o, err
				}
				o.Since = d
			}
		default:
			// A query may legitimately start with a dash — `mss search
			// "--retry budget"` is a real search. A token one edit away from a
			// real flag is not: folding `--limti` into the query turned a
			// working search into "you have no such memory" (#755).
			if nearestKnownFlag(a, searchFlags) != "" {
				return o, unknownFlag("search", a, searchFlags)
			}
			// A flag mss takes elsewhere is not a typo and is nowhere near a
			// search flag by edit distance, so it went into the query and the
			// search that would have found everything reported nothing (#2249).
			if cmd := flagsOfOtherCommands[a]; cmd != "" {
				return o, fmt.Errorf("%s is a flag of `mss %s`, not of search — put it after `--` to search for the text", a, cmd)
			}
			q = append(q, a)
		}
	}
	// Match the NFC canonicalisation ingest applies to stored text, so a query
	// typed in either normalisation reaches the same records (#1098). Trim
	// surrounding space: a leading space left the exact-match tier hunting for
	// " token" and an exact-only term (an error code, a coined name) missed,
	// while a real word was rescued by its word-forms and hid the gap.
	o.Query = nfcfold.Compose(strings.TrimSpace(strings.Join(q, " ")))
	if o.Query == "" {
		return o, fmt.Errorf("query required")
	}
	return o, nil
}

// flagsOfOtherCommands names the command each flag belongs to, for the tokens
// that are real mss flags somewhere but not here. Only exact matches: a query
// may legitimately start with a dash, and `--` still ends option parsing.
var flagsOfOtherCommands = map[string]string{
	"--offset": "show",
	"--deep":   "doctor",
	"--from":   "last",
}

// searchFlags is every flag the bare search form accepts, for the typo check.
// The flags each converted command takes, in the order its own parser lists
// them. A list that drifts from its parser costs a wrong suggestion, not a
// wrong refusal.
var (
	indexFlags  = []string{"--rebuild", "--quiet"}
	showFlags   = []string{"--json", "--harness", "--offset", "--limit", "--around", "--no-refresh", "--brief", "--role"}
	doctorFlags = []string{"--json", "--deep"}
)

var searchFlags = []string{
	"--json", "--re", "--all", "--rebuild", "--sessions", "--no-refresh",
	"--harness", "--project", "--since", "--role", "--limit", "--session", "--sort",
	"--exclude", "--exclude-self",
}

// nearestSearchFlag names the search flag a token was probably meant to be.
// The rule it introduced is now every command's, in nearestKnownFlag; this is
// the search-shaped name for it, kept because search is the one command where
// the answer also decides whether a dashed token is a query term.
func nearestSearchFlag(a string) string {
	return nearestKnownFlag(a, searchFlags)
}

// parseDur is the window a reader asked to look back over, so it has to be one:
// zero or less used to parse and then disappear, because every caller cuts on
// `Since > 0` — `--since -1d` searched the whole store and nothing in the answer
// said the filter had been dropped (#1610). parseDurAny is the raw form, for
// `forget --before`, which has its own word for the same mistake.
func parseDur(s string) (time.Duration, error) {
	d, err := parseDurAny(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("%q is not a window — --since counts back from now, so it needs a positive duration like 30d", s)
	}
	return d, nil
}

// maxDurationDays is how many whole days fit in a time.Duration: past it the
// multiplication wraps, and `--since 365000d` — the way to say "all of it" —
// came out negative and dropped the filter it was asked for.
const maxDurationDays = int(math.MaxInt64 / (24 * int64(time.Hour)))

func parseDurAny(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, durationError(s)
		}
		if n > maxDurationDays {
			return time.Duration(math.MaxInt64), nil
		}
		if n < -maxDurationDays {
			return time.Duration(math.MinInt64), nil
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		// time.ParseDuration's own message names Go's syntax, not mss's, and
		// it does not mention days — which is the unit people reach for here.
		return 0, durationError(s)
	}
	return d, nil
}

func durationError(s string) error {
	return fmt.Errorf("%q is not a duration mss understands — try 30d, 12h, or 90m", s)
}

// presentFiles is one path if it exists, nothing otherwise: a store mss names
// but does not have should weigh nothing rather than the directory around it.
func presentFiles(paths ...string) []string {
	var out []string
	for _, p := range paths {
		if p == "" {
			continue
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

func cursorReadFiles() []string {
	return append(sources.CursorTranscripts(), sources.CursorDBs()...)
}

// grokReadFiles is both of Grok's stores: the session files Grok Build writes,
// and the SQLite store the maintained CLI writes instead. The row listed only
// the first, so the size column read 0 B on a machine whose whole history is in
// the database (#3225).
func grokReadFiles() []string {
	files := sources.GrokSessionFiles()
	if db := sources.GrokDB(); db != "" {
		files = append(files, db)
	}
	return files
}

// filesSize sums what the parser would open, counting each path once: cursor
// and hermes list a file under more than one discovery rule.
func filesSize(paths []string) int64 {
	seen := map[string]bool{}
	var total int64
	for _, p := range paths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			total += fi.Size()
		}
	}
	return total
}

// commonDir is the deepest directory holding every path, which is the answer to
// "where is my history" — the configured root can be a parent of it, or, when a
// harness keeps its store somewhere else entirely, not contain it at all.
func commonDir(paths []string) string {
	dir := ""
	for _, p := range paths {
		if p == "" {
			continue
		}
		d := filepath.Dir(p)
		if dir == "" {
			dir = d
			continue
		}
		for dir != d && !strings.HasPrefix(d+string(filepath.Separator), dir+string(filepath.Separator)) {
			parent := filepath.Dir(dir)
			if parent == dir {
				return dir
			}
			dir = parent
		}
	}
	return dir
}

// projectExcludePatterns is how many lines in the exclude file are project
// patterns. A `harness:` line lives in the same file and is not one, so
// counting them together reported "excluded-patterns=1" against every store on
// a machine whose only rule was "never read this one" (#3499).
func projectExcludePatterns() int {
	return len(sources.ExclusionPatterns()) - len(sources.ExcludedHarnesses())
}

func printSources(dir string) {
	redactions := map[string]int{}
	if red, err := index.Redactions(dir); err == nil {
		redactions = red.Files
	}
	// Transcripts a store holds that the index has never read. doctor has said
	// so since #3747, but `mss sources` is the command people run first, and
	// its session count reads as "nothing written yet" rather than "files never
	// opened" — which is exactly how ten unread transcripts, five of them eight
	// weeks old, went unnoticed on a real store (#3752). Nil before the first
	// index, and then there is nothing to say.
	neverRead := index.HarnessUnreadCounts(dir)
	unreadNote := func(name string) string {
		if u := neverRead[name]; u > 0 {
			return "\t(" + doctorCount(u, "transcript") + " never read — `mss index`)"
		}
		return ""
	}
	// files names what the parser opens. The screen answers "where is my history
	// and how much of it is there", and roots are where mss looks rather than
	// what it reads: a codex root reported 108 MB of plugins, caches and sqlite
	// WALs around 1.3 MB of transcripts (#654).
	items := []struct {
		name, location string
		roots          []string
		files          func() []string
		load           func() []model.Session
	}{
		{"claude", strings.Join(sources.ClaudeRoots(), string(os.PathListSeparator)), sources.ClaudeRoots(), sources.ClaudeFiles, sources.LoadClaude},
		{"codex", strings.Join(sources.CodexRoots(), string(os.PathListSeparator)), sources.CodexRoots(), sources.CodexFiles, sources.LoadCodex},
		{"cursor", strings.Join([]string{sources.CursorUserRoot(), sources.CursorCLIRoot()}, string(os.PathListSeparator)), []string{sources.CursorUserRoot(), sources.CursorCLIRoot()}, cursorReadFiles, sources.LoadCursor},
		// Both halves, the way the registry's grok entry loads them: the
		// maintained CLI writes one SQLite store beside the config and no
		// session files at all, so a machine on that build read as zero
		// sessions here while doctor and search both found them (#3225).
		{"grok", sources.GrokRoot(), []string{sources.GrokRoot()}, grokReadFiles, func() []model.Session {
			return append(sources.LoadGrok(), sources.LoadGrokDB()...)
		}},
		{"pi", sources.PiRoot(), []string{sources.PiRoot()}, sources.PiSessionFiles, sources.LoadPi},
		{"omp", sources.OmpRoot(), []string{sources.OmpRoot()}, sources.OmpSessionFiles, sources.LoadOmp},
		{"deepseek", sources.DeepSeekRoot(), []string{sources.DeepSeekRoot()}, sources.DeepSeekSessionFiles, sources.LoadDeepSeek},
	}
	skipStore := sources.ExcludedHarnesses()
	for _, it := range items {
		// A store the reader excluded is named and not read. Walking it here
		// reported three sessions and eighteen messages for a harness mss had
		// just been told never to open, which is the opposite of what this
		// screen is for (#3499).
		if skipStore[it.name] {
			fmt.Printf("%s\t%s\texcluded — `harness:%s` is in %s\n",
				it.name, it.location, it.name, sources.ExcludePath())
			continue
		}
		redacted := 0
		for _, root := range it.roots {
			redacted += redactionsUnder(redactions, root)
		}
		// Only paths that are files on this machine: a discovery rule can name
		// something that is not one — hermes returns a token for a Postgres
		// store — and neither a size nor a directory can be read off that.
		read := presentFiles(it.files()...)
		size := filesSize(read)
		location := it.location
		if where := commonDir(read); where != "" {
			location = where
		}
		raw := it.load()
		ss := sources.FilterSessions(raw)
		excluded := len(raw) - len(ss)
		msg := 0
		for _, s := range ss {
			msg += len(s.Messages)
		}
		note := ""
		// `mss sources` is where the empty-machine advice sends people, and a
		// store mss is not allowed to read looked exactly like one nobody has
		// used: `sessions=0 messages=0 size=0 B` (#1000).
		if denied, whole := firstDeniedDir(it.roots); denied != "" {
			note = "\t(cannot be read — permission denied on " + denied + ")"
		} else if !whole {
			note = "\t(permissions not fully checked — too many directories to walk)"
		}
		if it.name == "cursor" && len(sources.CursorDBs()) > 0 && !sources.SQLite3Available() {
			note = "\t(" + sources.SQLite3Problem() + " — Cursor IDE sessions unavailable)"
		}
		if n := projectExcludePatterns(); n > 0 {
			note += fmt.Sprintf("\texcluded-patterns=%d", n)
		}
		if excluded > 0 {
			note += fmt.Sprintf("\texcluded-sessions=%d", excluded)
		}
		note += unreadNote(it.name)
		fmt.Printf("%s\t%s\tsessions=%d messages=%d size=%s redacted=%d%s\n", it.name, location, sources.CountSessions(ss), msg, humanBytes(size), redacted, note)
	}
	// The opencode row below is written by hand rather than driven by the table
	// above, and the exclusion has to reach it too (#3499).
	excludedRow := func(name, location string) bool {
		if !skipStore[name] {
			return false
		}
		fmt.Printf("%s\t%s\texcluded — `harness:%s` is in %s\n",
			name, location, name, sources.ExcludePath())
		return true
	}
	if excludedRow("opencode", sources.OpencodeDB()) {
		return
	}
	var size int64
	if fi, err := os.Stat(sources.OpencodeDB()); err == nil {
		size = fi.Size()
	}
	s, m, countErr := sources.OpencodeCounts()
	// The counts come out of sqlite, which knows nothing about the exclude
	// list, so with a pattern in force this row kept reporting sessions that
	// are not indexed, not searchable and not exported while every other row
	// subtracted them (#2247). Only then is the store loaded: counting by SQL
	// is why this row is cheap on a large database.
	opencodeExcluded := 0
	if len(sources.ExclusionPatterns()) > 0 {
		raw := sources.LoadOpencode()
		kept := sources.FilterSessions(raw)
		opencodeExcluded = len(raw) - len(kept)
		// Subtracted from the SQL numbers rather than recounted from what
		// loaded: the loader drops a session holding no text at all, which the
		// row has always counted, so recounting would move the numbers for a
		// reason that has nothing to do with the exclude list.
		dropped := 0
		for _, x := range raw {
			if sources.ExcludedProject(x.Project) {
				dropped += len(x.Messages)
			}
		}
		s, m = max(0, s-opencodeExcluded), max(0, m-dropped)
	}
	note := ""
	if size > 0 && !sources.SQLite3Available() {
		note = "\t(" + sources.SQLite3Problem() + " — opencode sessions unavailable)"
	}
	if n := projectExcludePatterns(); n > 0 {
		note += fmt.Sprintf("\texcluded-patterns=%d", n)
	}
	if opencodeExcluded > 0 {
		note += fmt.Sprintf("\texcluded-sessions=%d", opencodeExcluded)
	}
	// A store sqlite could not open — locked past the timeout, or a file the
	// reader may not open — looked like one nobody had used, the shape #1000
	// fixed for the file stores (#3190).
	if countErr != nil && sources.SQLite3Available() {
		reason := "sqlite3: " + countErr.Error()
		if line := sources.ExitStderrLine(countErr); line != "" {
			reason = "sqlite3: " + line
		}
		if f, err := os.Open(sources.OpencodeDB()); err != nil {
			reason = err.Error()
		} else {
			f.Close()
		}
		note = "\t(cannot be read — " + reason + ")" + note
	}
	note += unreadNote("opencode")
	fmt.Printf("opencode\t%s\tsessions=%d messages=%d size=%s redacted=%d%s\n", sources.OpencodeDB(), s, m, humanBytes(size), redactions[sources.OpencodeDB()], note)
}

func redactionsUnder(files map[string]int, root string) int {
	total := 0
	for p, n := range files {
		if p == root || strings.HasPrefix(p, root+string(os.PathSeparator)) {
			total += n
		}
	}
	return total
}

func pathSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type()&os.ModeSymlink == 0 && !d.IsDir() {
			if fi, e := d.Info(); e == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

func humanBytes(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

func printUsage() {
	fmt.Print(wrapUsage(usageText(), printableWidth(os.Stdout)))
}

// usageText renders the usage block so `--help` on a single command can quote
// the lines that belong to it instead of the whole page.
func usageText() string {
	return `mss - search your coding agents' past sessions

Usage:
  mss [flags] <query>
  mss search [flags] <query>   (same, but a query may start with a dash)
  mss show <id-prefix> [--json --harness name] [--offset n] [--limit n]
           [--around n] [--brief [n] --role name] [--no-refresh]
  mss ctx <query|id-prefix>
  mss last [n] [--json] [--project name] [--harness name] [--since duration] [--role user|assistant|tool|files|command|edit|summary]
  mss sources
  mss doctor [--json] [--deep]
  mss index [--rebuild] [--quiet]
  mss install-skill <claude|codex|pi|omp> [--language en|zh-CN]
  mss version
  mss <command> --help

Search flags ("mss search" or the bare "mss [flags] <query>" form):
  --harness <name>              only sessions from one harness (claude, codex…)
  --project <name>              only sessions from one project
  --since <duration>            only sessions newer than e.g. 30d, 12h
  --role <name>                 only match turns from one role: user, assistant,
                                tool (tool output), files, command, edit
  --session <id>                only one session, by the id a hit prints
  --limit <1-100>               max sessions to return (default 15)
  --all                         return every match, no cap
  --sessions                    the matching set instead of the ranked hits:
                                session metadata, hit count and record positions
  --sort updated                order --sessions by last update, newest first
  --exclude <id-or-prefix>      drop a session, with its subagents and forks
  --exclude-self <nonce>        leave out the session carrying this nonce
  --no-refresh                  answer from the index as it was; "mss index"
                                refreshes it
  --re                          treat the query as a regular expression
  --json                        machine-readable output

Show flags:
  --around <n>                  centre the window on record n (a hit's "index")
  --brief [n]                   one header per message — index, role, time —
                                and the body cut at n characters (default 200)
  --role <name>                 with --brief: only these roles; repeat for more
  --no-refresh                  answer from the index as it was; "mss index"
                                refreshes it

Examples:
  mss "jwt refresh token bug"
  mss '"connection pool exhausted"'
  mss "exhaustd"  # a typo: with no exact hit, close spellings are tried
  mss --harness claude --since 30d "panic in indexer"
  mss --all "connection pool"  # every match, not just the first 15
  mss last 20 --harness codex
  mss last --project api-gateway
  mss last --since 7d --role user
  mss --session 01a00feb --role tool "go build"   (what ran inside one session)
  mss search --sessions --sort updated "worktree cleanup"   # newest first
  mss show 01a0f7c6 --harness pi --brief --around 12 --limit 40
  mss --re "timeout|deadline exceeded"
  mss ctx "schema migration rollback" > mss-context.md
`
}

// helpForCommand answers `mss <cmd> --help`. Every command rejected it as an
// unknown flag, and a couple did worse: `mss statusline --help` printed a
// statusline and `mss mcp --help` started the server and hung the terminal
// (#1111).
func helpForCommand(name string) string {
	var out []string
	usage := usageText()
	if i := strings.Index(usage, "\nUsage:\n"); i >= 0 {
		usage = usage[i+len("\nUsage:\n"):]
	}
	if i := strings.Index(usage, "\nExamples:\n"); i >= 0 {
		usage = usage[:i]
	}
	// Indented continuations belong to the preceding command's usage line.
	matched := false
	for _, line := range strings.Split(usage, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "mss "+name || strings.HasPrefix(t, "mss "+name+" "):
			out = append(out, line)
			matched = true
		case matched && t != "" && strings.HasPrefix(line, "    ") && !strings.HasPrefix(t, "mss "):
			out = append(out, line)
		default:
			matched = false
		}
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\nSee `mss help` for every command and flag.\n"
}

// wantsHelp reports whether a command line asks for help rather than work.
func wantsHelp(rest []string) bool {
	for _, a := range rest {
		if a == "--help" || a == "-h" {
			return true
		}
		if a == "--" {
			return false
		}
	}
	return false
}

// showNeedsID is the refusal show gives with no argument. "id-prefix" names a
// thing the reader has no way to produce on their own; promote has pointed at
// `mss last` all along, and show, share and resume are the three commands
// reached for right after a search result (#1063).
const showNeedsID = "show needs id-prefix (see `mss last`)"

// idPrefixNeeded is the refusal for a command that needs a session named on the
// command line. "see `mss last`" is a step the reader can take and learn
// nothing from when the store is empty: the listing answers with the same
// emptiness mss already knows about here (#992).
func idPrefixNeeded(dir, subject, refusal string) error {
	if n, err := index.SessionCount(dir); err == nil && n == 0 {
		return errors.New(strings.TrimPrefix(emptyIndexHint(subject+", and nothing is indexed yet"), "mss: "))
	}
	return errors.New(refusal)
}

// indexIsEmpty reports whether the index holds no session at all. An index
// that cannot be counted counts as empty on purpose, so the caller falls back
// to the empty-index hint it printed before.
func indexIsEmpty(dir string) bool {
	n, err := index.SessionCount(dir)
	return err != nil || n == 0
}

// emptyIndexReason opens the empty-index sentence. "Nothing to index yet" is
// for a machine mss has never seen history from; a run that has just evicted a
// store says what went away instead, because the line above it has already told
// the reader what was lost and the two must not contradict each other (#1762).
func emptyIndexReason(b index.BuildSummary, evicted int) string {
	if evicted > 0 {
		return emptyIndexHint(fmt.Sprintf("%d indexed file%s went away with the store that held %s, and nothing is left to index",
			evicted, pluralS(evicted), pluralWhich(evicted)))
	}
	return emptyIndexHint("nothing to index yet")
}

// deniedStoresLine names the stores mss could not read on a pass whose index
// still holds sessions, with the files the pass evicted when there were any.
func deniedStoresLine(denied, evicted int) string {
	line := fmt.Sprintf("%d store%s could not be read (permission denied); `mss doctor` names %s",
		denied, pluralS(denied), pluralWhich(denied))
	if evicted > 0 {
		line = fmt.Sprintf("%d indexed file%s went away with the store that held %s — %s",
			evicted, pluralS(evicted), pluralWhich(evicted), line)
	}
	return "mss: " + line
}

// emptyIndexHint phrases the nothing-here answer the same way everywhere, and
// points at the next command rather than leaving the user to guess.
//
// Which command depends on why it is empty. Every path that reaches here has
// already built the index — the build narration prints a line above this one —
// so telling someone to run `mss index` sends them to do again what just
// happened, and they are left where they started. When no agent history was
// found at all, the useful next step is finding out where mss looked.
func emptyIndexHint(what string) string {
	// A store mss is not allowed to open, before anything else. The sessions
	// are there, behind a permission wall doctor and sources both name (#1020)
	// — and the branch used to sit inside the no-history one, so it was
	// unreachable in the case that matters: a store whose files mss can see
	// and cannot read counts as history, so the answer was "run `mss index`",
	// which is what the reader had just done and cannot help (#3585).
	if denied := deniedStoreCount(); denied > 0 {
		return fmt.Sprintf("mss: %s — %d store%s could not be read (permission denied); `mss doctor` names %s",
			what, denied, pluralS(denied), pluralWhich(denied))
	}
	if noAgentHistoryFound() {
		return "mss: " + what + " — no agent history was found on this machine; `mss sources` shows where mss looked"
	}
	return "mss: " + what + " — run `mss index`, or `mss doctor` to see which agent stores were found"
}

// deniedStoreCount reports how many harness stores exist but cannot be opened.
// Opened, not parsed: the probe stops before doctor's parser (#4272).
func deniedStoreCount() int {
	n := 0
	for _, check := range deniedStoreChecks() {
		if store, _, _ := probeDoctorStore(check); store.State == "denied" {
			n++
		}
	}
	return n
}

// deniedStoreChecks is doctorStoreChecks, swappable so a test can count what
// the probe parses.
var deniedStoreChecks = doctorStoreChecks

// noAgentHistoryFound reports whether the stores themselves are empty, as
// opposed to an index that merely has not been built yet.
func noAgentHistoryFound() bool {
	for _, check := range doctorStoreChecks() {
		// The count, not the inspection. `store.Files` is `len(check.files)`
		// and nothing else ever sets it (doctor_report.go:471), so asking
		// `inspectDoctorStore` for it paid a stat per path, a listing per
		// directory, and the newest file of the store opened and run through
		// that store's parser — SQLite for opencode and cursor — to learn a
		// number already in hand. Measured on a real home, 514 ms against
		// 6.6 ms for the same answer (#1991).
		if len(check.files) == 0 {
			continue
		}
		// By content, not by existence: an empty notes file — one `mss
		// remember` later forgotten, or a file someone touched — counted as
		// history and turned the answer into "run `mss index`", which is the
		// one piece of advice this branch exists to avoid (#996).
		for _, f := range check.files {
			if fi, err := os.Stat(f); err == nil && fi.Size() > 0 {
				return false
			}
		}
	}
	return true
}

// pluralNote words the notes in the failure line above.
func pluralNote(n int) string {
	if n == 1 {
		return "the promoted note"
	}
	return "the promoted notes"
}

// forgetTitleFix names the fix by the cause. The first version of this line
// said "permissions" whatever had happened, so a full disk sent the reader to
// chmod a file that was already writable (#808).
func forgetTitleFix(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "fix that file's permissions"
	case errors.Is(err, syscall.ENOSPC):
		return "free some space on that filesystem"
	}
	return "clear the problem above"
}

// sortedHarnesses orders the ingest-health entries so a run reports them the
// same way twice.
func sortedHarnesses(m map[string]index.HarnessIngest) []string {
	out := make([]string, 0, len(m))
	for h := range m {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// pluralSessionWord words the tail of the empty-transcript line.
func pluralSessionWord(n int) string {
	if n == 1 {
		return "a session"
	}
	return "sessions"
}

// pluralWhich matches the pronoun to the count in the ingest warning.
func pluralWhich(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

// staleUnwritableIndex reports that a build could not run because the store
// cannot be written, while an index that can still answer is right there.
// Refusing the whole search then is mss withholding what it has: the reader
// gets nothing instead of slightly old memory and a line saying why (#904).
// A full disk belongs here next to a denied one: it is the commoner of the
// two, and it took every answer with it — empty stdout and exit 1 while a
// complete index sat in the store.
func staleUnwritableIndex(dir string, err error) bool {
	if !errors.Is(err, fs.ErrPermission) && !errors.Is(err, syscall.ENOSPC) {
		return false
	}
	return index.HasManifest(dir)
}

// deniedPath names the file a permission error was actually about, so the fix
// mss suggests points at it rather than at the index directory (#1031).
func deniedPath(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Path
	}
	return ""
}

// existingNonDirAncestor names the first ancestor of p that exists and is not
// a directory. Such a path can never hold an index, and the errno differs by
// platform, so the shape is worth naming rather than the syscall.
func existingNonDirAncestor(p string) string {
	for cur := filepath.Clean(p); ; {
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		if fi, err := os.Stat(parent); err == nil && !fi.IsDir() {
			return parent
		}
		cur = parent
	}
}

// nearestExistingDir walks up until it finds a directory that is there, so a
// refusal can name the thing that actually refused rather than the path that
// could not be created under it.
func nearestExistingDir(p string) string {
	for cur := filepath.Clean(p); ; {
		parent := filepath.Dir(cur)
		if parent == cur {
			if dirExists(cur) {
				return cur
			}
			return ""
		}
		if dirExists(parent) {
			return parent
		}
		cur = parent
	}
}

// dirWritable reports whether this process can create something in dir.
// Permission bits alone answer for the wrong user on the wrong platform, so it
// asks the filesystem.
func dirWritable(dir string) bool {
	f, err := os.CreateTemp(dir, ".mss-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// ensureError turns a failed build into something the reader can act on.
// A denied write surfaced as `ensure: open /…/index.db.lock: permission
// denied` — the path of an internal lock file and a syscall error, which says
// nothing about what to change.
func ensureError(dir string, err error) error {
	if dir == "" {
		dir = index.DefaultDir()
	}
	if errors.Is(err, fs.ErrPermission) {
		// An ejected volume takes its mount point with it, and creating that
		// point back fails with EPERM on macOS — so the errno says permissions
		// while the disk is simply gone. The same trap the notes and export
		// paths hit (#893, #906), and the reader is sent to check the
		// permissions of a directory that no longer exists (#931).
		if parent := filepath.Dir(dir); !dirExists(dir) && !dirExists(parent) {
			// Unless something above it is there and simply refuses: a
			// locked-down ~/.cache cannot be written into either, and reading
			// that as an ejected volume sent the reader to reconnect a disk
			// that never left (#2267).
			if a := nearestExistingDir(parent); a != "" && !dirWritable(a) {
				return fmt.Errorf("cannot create the index directory (%s) — %s is not writable; check its permissions, or point MSS_INDEX_DIR somewhere writable", parent, a)
			}
			return fmt.Errorf("the index directory is not there (%s) — the disk it lives on may have been unmounted; reconnect it, or point MSS_INDEX_DIR somewhere local", parent)
		}
		// The denial is not always about the index: forget writes the
		// tombstone file first, and a read-only ~/.config/mss arrived here
		// as "check the index directory", which was writable — the reader was
		// sent to change a permission that was already right (#1031, #808).
		if p := deniedPath(err); p != "" && !strings.HasPrefix(p, dir) {
			return fmt.Errorf("cannot write %s — check that file's permissions", p)
		}
		return fmt.Errorf("cannot write the index at %s — check the directory's permissions, or point MSS_INDEX_DIR somewhere writable", dir)
	}
	// A full disk arrived as `ensure: write /…/index.db.tmp/records.bin: no
	// space left on device`: an internal path nobody can act on, and the same
	// shape #798 replaced for permissions. The build needs room beside the
	// index, so the directory to free is the one named here (#888).
	// A volume that went away mid-write — an unmounted disk, a network share
	// that dropped — arrives as `write /…/index.db.tmp/records.bin:
	// input/output error`: the same internal path as #888, and a reader who
	// cannot tell that the disk is simply gone (#899).
	if errors.Is(err, syscall.EIO) || errors.Is(err, syscall.ENXIO) || errors.Is(err, syscall.ENODEV) {
		return fmt.Errorf("the index directory is not reachable (%s) — the disk it lives on may have been unmounted or dropped; reconnect it, or point MSS_INDEX_DIR somewhere local", filepath.Dir(dir))
	}
	if errors.Is(err, syscall.ENOSPC) {
		return fmt.Errorf("no space left where the index is built (%s) — free some room there, or point MSS_INDEX_DIR at a disk that has it", filepath.Dir(dir))
	}
	// A volume ejected cleanly mid-build leaves its mount point behind as an
	// empty directory, so the write fails with ENOENT rather than the EIO of a
	// disk yanked out (#899) — an internal `idx.tmp/buckets/…` path and a
	// syscall for what is simply a disconnected disk. The index that was
	// already there is untouched, since the build writes beside it and
	// renames, and saying so is the part that decides whether the reader goes
	// looking for damage (#1068).
	// An index path that points inside a file is not a disconnected disk: on
	// unix the write fails with ENOTDIR and fell through to the raw syscall,
	// on windows it fails with ENOENT and read as an unmounted volume (found
	// by CI on windows after #1068).
	if p := existingNonDirAncestor(dir); p != "" {
		return fmt.Errorf("the index path runs through %s, which is a file — point MSS_INDEX_DIR at a directory", p)
	}
	if errors.Is(err, fs.ErrNotExist) && !dirExists(dir) {
		return fmt.Errorf("the index directory went away mid-build (%s) — the disk it lives on may have been unmounted; the index already there is unharmed, so reconnect it and run `mss index` again, or point MSS_INDEX_DIR somewhere local", dir)
	}
	// Already worded where it was raised — the leftover-swap case names the
	// directory to remove and the command to rerun, the refused index path
	// names the file it will not delete, and "ensure:" in front of either is
	// internal noise (#1009).
	for _, worded := range []string{"an earlier index swap left ", "the index path "} {
		if strings.HasPrefix(err.Error(), worded) {
			return err
		}
	}
	return fmt.Errorf("ensure: %w", err)
}

// searchValueFlags take a value, so "--flag=value" has to become two arguments
// before the parser sees it. Anything else keeps its equals sign: a query may
// legitimately contain one.
var searchValueFlags = map[string]bool{
	"--harness": true, "--project": true, "--since": true,
	"--role": true, "--limit": true, "--sort": true,
}

func splitEqualsForms(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		name, value, found := strings.Cut(a, "=")
		if found && searchValueFlags[name] {
			out = append(out, name, value)
			continue
		}
		out = append(out, a)
	}
	return out
}

// reachableSessionCount reports how many indexed sessions this path may read,
// how many of those the trust policy alone allows, and how many there are.
//
// Both rules withhold, so both are counted (#2707): the trust policy was here
// from the start (#986) and the ignore rule was not counted at all, which had
// the empty answer naming twice the history search would open. The trust-only
// figure is kept because the reader has to be told which of the two emptied
// their search.
func reachableSessionCount(dir string) (reach, trusted, total int, ok bool) {
	metas, err := index.AllMeta(dir)
	if err != nil {
		return 0, 0, 0, false
	}
	pol := policy.Load()
	for _, m := range metas {
		if !pol.Allows(policy.ActivationSearch, m.Project) {
			continue
		}
		trusted++
		if !pol.Ignored(m.Path, m.Project) {
			reach++
		}
	}
	return reach, trusted, len(metas), true
}

// flagName is the flag without its value, so `--limit=3` and `--limit 3` are
// recognised as the same spelling.
func flagName(a string) string {
	if i := strings.IndexByte(a, '='); i >= 0 {
		return a[:i]
	}
	return a
}

// capTierHits bounds the two tiers that build their own hits — the ranked
// relevance list and the error signature — which never went through the cap in
// RunDetailed: `--limit 3` printed the whole retrieval window of fifty
// sessions there while the exact tier printed three (#3345). With no flag they
// keep the window they have always served, which the JSON envelope's own test
// pins; only what the reader asked for binds them.
func capTierHits(hits []search.Hit, o search.Options) ([]search.Hit, bool) {
	if o.Limit == 0 {
		return hits, false
	}
	// A limit the reader typed binds even beside --all, which is how the exact
	// tier has always read the pair: --all lifts the default, and the number
	// asked for is still the number wanted. Letting --all win here made the
	// same two flags mean opposite things depending on which tier answered
	// (review of #3345).
	return search.CapHits(hits, o.Limit, false)
}

// resolveSearchExcludes turns what the flags named into the set of session
// ids the search must not return: each --exclude id-or-prefix, plus whatever
// session the --exclude-self nonce was written into. The set is then widened
// through the lineage — a session's subagents and forks are the same
// conversation under other ids, and excluding one of them alone leaves the
// answer holding the rest.
//
// selfFound says whether the nonce matched anything; a miss is reported as
// coverage, not an error — the session asking may simply not be indexed yet.
//
// noRefresh is the caller's --no-refresh: a torn read normally heals by
// rebuilding here, and this mode does not write.
func resolveSearchExcludes(dir string, ids []string, nonce string, noRefresh bool) (map[string]bool, bool, error) {
	byPrefix, err := index.SessionIDsByPrefix(dir, ids)
	if err != nil {
		// The caller distinguishes corrupt from unmatched: without this the
		// error below would blame the id for what the store broke.
		return nil, false, ensureError(dir, err)
	}
	var resolved []string
	for _, p := range ids {
		hits := byPrefix[p]
		switch len(hits) {
		case 0:
			return nil, false, fmt.Errorf("--exclude %q matches no session", p)
		case 1:
			resolved = append(resolved, hits[0])
		default:
			// Leaving one of several matches in is the pollution the flag
			// exists to remove, so an ambiguous prefix excludes them all and
			// says so rather than guessing one.
			fmt.Fprintf(os.Stderr, "mss: --exclude %q is ambiguous — excluding all %d matches\n", p, len(hits))
			resolved = append(resolved, hits...)
		}
	}
	out, found, err := index.ExpandExclude(dir, resolved, nonce)
	if err != nil {
		if index.IsCorrupt(err) && !noRefresh {
			// The exclusion ran before the search, so it missed the retry
			// the search itself would take on a torn read: refresh once,
			// then resolve again rather than failing what would have healed.
			if rerr := index.EnsureForSearch(dir, search.Options{All: true}, true, nil); rerr == nil {
				return index.ExpandExclude(dir, resolved, nonce)
			}
		}
		return nil, false, ensureError(dir, err)
	}
	return out, found, nil
}

// sessionsListCap bounds the rows `mss search --sessions` prints: a machine
// walks the list by opening sessions, and past this point the answer says so
// rather than printing ten thousand ids. The hit cap stays separate — the
// list answers a different question from the hits, but neither question
// wants an unbounded page.
const sessionsListCap = 500

// sortUpdated is the one order --sort knows: newest last-updated first. A
// reader asking "how did this end" needs the newest sessions on the first
// screen, and the list's own order — hits first — answers the other question.
const sortUpdated = "updated"

// orderSessionsForList turns the retrieval set into the candidate list: what
// the hits already proved matched, in hit order, then whatever else matched
// by identity order. Hits prove the match — a session the scorer saw and
// kept — so their ranking is the list's; the relevance tail, relevance
// window rows beyond the hits, and any session a filter kept out of the
// hits stay sorted but unranked behind them. The --session filter, which the
// scorer honours while building hits, is re-applied: retrieval hands back
// every matching session and the filter only bound the hits.
//
// --sort updated replaces that order with last-updated first, over the whole
// list. It runs before the cap, so the rows a capped list drops are the
// oldest ones.
func orderSessionsForList(dir string, ss []model.Session, hits []search.Hit, o search.Options, result index.SearchResult) (listed []model.Session, capped bool, total int) {
	byKey := make(map[string]model.Session, len(ss))
	for _, s := range ss {
		key := s.Harness + ":" + s.ID
		if _, seen := byKey[key]; !seen {
			byKey[key] = s
		}
	}
	keep := func(s model.Session) bool {
		if o.Session != "" && !strings.HasPrefix(s.ID, o.Session) && !strings.HasPrefix(s.OrigID, o.Session) {
			return false
		}
		return true
	}
	matched := make(map[string]bool, len(hits))
	for _, h := range hits {
		key := h.Session.Harness + ":" + h.Session.ID
		if _, ok := byKey[key]; !ok {
			continue
		}
		if !keep(h.Session) {
			continue
		}
		if !matched[key] {
			matched[key] = true
			listed = append(listed, byKey[key])
		}
	}
	var rest []model.Session
	for key, s := range byKey {
		if matched[key] || !keep(s) {
			continue
		}
		rest = append(rest, s)
	}
	sort.Slice(rest, func(i, j int) bool {
		if rest[i].Harness != rest[j].Harness {
			return rest[i].Harness < rest[j].Harness
		}
		return rest[i].ID < rest[j].ID
	})
	listed = append(listed, rest...)
	if o.Sort == sortUpdated {
		// Identity breaks ties, the same way `last` orders sessions that
		// share a stamp: a map walked for `rest` is not an order.
		sort.Slice(listed, func(i, j int) bool {
			if !listed[i].Updated.Equal(listed[j].Updated) {
				return listed[i].Updated.After(listed[j].Updated)
			}
			if listed[i].Harness != listed[j].Harness {
				return listed[i].Harness < listed[j].Harness
			}
			return listed[i].ID < listed[j].ID
		})
	}
	total = len(listed)
	if len(listed) > sessionsListCap {
		listed = listed[:sessionsListCap]
		capped = true
	}
	return listed, capped, total
}

// sessionsMatchIndices computes, per served session, where the query landed —
// the record positions `mss show --around` can jump straight to. Each tier
// answers "which records counted" its own way: the error signature lines for
// an error hit, the relevance terms for a ranked one, the query's own check
// for the rest. The positions are the transcript's numbering, not the
// filtered list's, so a record the policy keeps out still counts its slot.
func sessionsMatchIndices(dir string, ss []model.Session, o search.Options, result index.SearchResult) (map[string][]int, error) {
	if o.Regex || !index.QueryHasTerms(o.Query) {
		// A regex has no term check — the real pattern runs in the scorer,
		// which this walk never consults — and a query with no terms
		// matches by presence, not by content. Both would report every
		// servable record as a match, so neither gets positions: the row
		// keeps its hit_count, matched_indices stays absent.
		return nil, nil
	}
	keys := make(map[string]bool, len(ss))
	for _, s := range ss {
		keys[s.Harness+":"+s.ID] = true
	}
	var match func(index.Record) bool
	switch {
	case result.Tier == search.TierError:
		match = index.ErrorSigRecordMatcher(o)
	case result.Tier == search.TierRelevance:
		terms := index.RelevanceMatchTerms(o.Query)
		match = func(r index.Record) bool {
			low := strings.ToLower(r.Text)
			for _, t := range terms {
				if strings.Contains(low, t) {
					return true
				}
			}
			return false
		}
	default:
		match = index.QueryRecordMatcher(o, result.Variants)
	}
	return index.MatchedRecordPositions(dir, keys, o, match)
}

// annotateHitIndices marks every served message of every hit with its record
// index — the position `mss show --offset` and `--around` count by. The hits
// arrive holding a subset of the session (match windows, servable records),
// so the numbering is recovered by walking each session's full record list
// and merge-joining: the subset keeps transcript order, and a duplicated
// message lands on the next position the join cursor has not passed.
func annotateHitIndices(dir string, hits []search.Hit) {
	seen := map[string]bool{}
	var ids []index.Identity
	for _, h := range hits {
		id := index.Identity{Harness: h.Session.Harness, ID: h.Session.ID}
		if !seen[id.Harness+":"+id.ID] {
			seen[id.Harness+":"+id.ID] = true
			ids = append(ids, id)
		}
	}
	fulls, err := index.FindManyByIdentity(dir, ids)
	if err != nil {
		return
	}
	byKey := make(map[string][]model.Message, len(fulls))
	for i := range fulls {
		byKey[fulls[i].Harness+":"+fulls[i].ID] = fulls[i].Messages
	}
	for hi := range hits {
		full := byKey[hits[hi].Session.Harness+":"+hits[hi].Session.ID]
		j := 0
		for mi := range hits[hi].Session.Messages {
			m := hits[hi].Session.Messages[mi]
			for j < len(full) && !sameMessageTriple(full[j], m) {
				j++
			}
			if j >= len(full) {
				break
			}
			idx := j
			hits[hi].Session.Messages[mi].Index = &idx
			j++
		}
	}
}

// sameMessageTriple is the join's equality: role, text and stamp. A session
// could in principle carry two records identical on all three — the join
// assigns them in order, which is the only honest answer the data gives.
func sameMessageTriple(a, b model.Message) bool {
	return a.Role == b.Role && a.Text == b.Text && a.Time.Equal(b.Time)
}
