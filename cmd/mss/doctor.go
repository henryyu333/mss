package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/henryyu333/mss/internal/index"
	"github.com/henryyu333/mss/internal/policy"
	"github.com/henryyu333/mss/internal/search"
	"github.com/henryyu333/mss/internal/sources"
)

// countSubagentFiles counts the child transcripts among the files a harness
// offered. They are read as their task and their answer rather than in full, so
// the row says how much of them is searchable.
func countSubagentFiles(seen []string) int {
	n := 0
	for _, p := range seen {
		if sources.IsSubagentPath(p) {
			n++
		}
	}
	return n
}

// runDoctor prints a self-diagnosis report. Diagnosis itself never fails, so
// both human and JSON reports keep exit status 0.
func runDoctor(w io.Writer, args []string, dir string) error {
	jsonOutput := false
	deep := false
	for _, arg := range args {
		switch arg {
		case "--json":
			jsonOutput = true
		case "--deep":
			deep = true
		default:
			return unknownFlag("doctor", arg, doctorFlags)
		}
	}
	report := collectDoctorReport(dir)
	var deepReport *index.DeepReport
	if deep {
		// doctor is what the no-home refusal sends the reader to, so it runs
		// without one — but --deep takes the index lock before it reads, and
		// with a relative index dir that left `.cache/mss/index.db.lock` in
		// whatever directory they were standing in (#1692). There is no
		// database at a path like that to verify anyway.
		if !filepath.IsAbs(dir) {
			return fmt.Errorf("doctor --deep cannot find your index — set HOME, or MSS_INDEX_DIR to an absolute path")
		}
		dr, err := index.DeepVerify(dir)
		if err != nil {
			return fmt.Errorf("doctor: deep verify: %w", err)
		}
		deepReport = &dr
		report.Deep = deepReport
	}
	if jsonOutput {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
		return deepDriftErr(deepReport)
	}
	doctorHarnesses(w, dir)
	printDoctorStoreWarnings(w, report.Stores)
	fmt.Fprintln(w)
	doctorTools(w)
	fmt.Fprintln(w)
	doctorPolicy(w, dir)
	fmt.Fprintln(w)
	doctorIndex(w, report.Index, dir)
	if deepReport != nil {
		fmt.Fprintln(w)
		doctorDeep(w, *deepReport)
	}
	return deepDriftErr(deepReport)
}

// doctorDeep prints the source-vs-index proof. Everything above it is mss
// trusting its own bookkeeping; this section is the recount.
func doctorDeep(w io.Writer, r index.DeepReport) {
	fmt.Fprintln(w, "Deep verification:")
	fmt.Fprintf(w, "  checked  %s, %s, %s re-parsed, %s resolved\n",
		doctorCount(r.FilesChecked, "source file"),
		doctorCount(r.SessionsIndexed, "indexed session"),
		doctorCount(r.SampledFiles, "sampled file"),
		doctorCount(r.SampledPostings, "posting"))
	if len(r.Stale) > 0 {
		fmt.Fprintf(w, "  stale    %s changed since last pass — `mss index` will absorb them\n", doctorCount(len(r.Stale), "source"))
	}
	doctorKept(w, &r)
	if r.Clean() {
		// What this pass compares is message counts per session, plus the
		// structure around them: sizes, magic numbers, postings that resolve.
		// It cannot see a same-length edit inside a record, which leaves the
		// count identical and the session unreachable — so the line says what
		// was checked rather than promising nothing was lost (#1712).
		//
		// And only what was actually checked: nothing is sampled when every
		// source is stale, or when the sampled tokens carry no postings, and a
		// clean report then means "found nothing wrong", not "compared and
		// agreed".
		switch {
		case r.SampledFiles == 0 && r.SampledPostings == 0:
			fmt.Fprintln(w, "  status   nothing to compare — no source was in sync to re-parse and no sampled token carried postings")
		case r.SampledFiles == 0:
			fmt.Fprintln(w, "  status   the sampled postings resolve; no source was in sync to re-parse, so no message count was compared")
		case r.SampledPostings == 0:
			fmt.Fprintln(w, "  status   every sampled session's message count matches its source; no sampled token carried postings")
		default:
			fmt.Fprintln(w, "  status   every sampled session's message count matches its source, and the sampled postings resolve")
		}
		return
	}
	for _, f := range r.Findings {
		fmt.Fprintf(w, "  drift    [%s] %s\n", f.Kind, f.Detail)
	}
	fmt.Fprintf(w, "  status   %s — run `mss index --rebuild`\n", doctorCount(len(r.Findings), "finding"))
}

// doctorKept says which indexed transcripts are no longer on disk and are kept
// on purpose — the client's cleanup, not drift (#2970). Printed before the
// verdict so a reader sees it is not what any finding is about.
func doctorKept(w io.Writer, r *index.DeepReport) {
	if r == nil || len(r.Kept) == 0 {
		return
	}
	fmt.Fprintf(w, "  kept     %d transcript%s no longer on disk, still searchable\n", len(r.Kept), pluralS(len(r.Kept)))
}

func deepDriftErr(r *index.DeepReport) error {
	if r == nil || r.Clean() {
		return nil
	}
	return fmt.Errorf("doctor: index drift detected (%s) — run `mss index --rebuild`", doctorCount(len(r.Findings), "finding"))
}

// inDotDir reports whether the path sits inside a dot-directory below the
// root — a cache, a temp dir, anything a tool keeps for itself.
func inDotDir(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	for _, part := range strings.Split(filepath.Dir(rel), string(filepath.Separator)) {
		if strings.HasPrefix(part, ".") && part != "." && part != ".." {
			return true
		}
	}
	return false
}

// unplacedFiles counts transcripts under root that the harness's own filter did
// not pick up. A harness that changes its layout in a new version presents
// exactly this way: quietly fewer sessions, no error, and a directory size that
// still looks right (#701).
//
// unplacedFiles counts the transcripts under a root that mss did not read,
// split by whether it declined them on purpose. Everything was one number
// before, and on a machine that spawns subagents most of it is the deliberate
// skip: this store reported "1192 not recognised here" of which 596 were
// subagent transcripts mss is written to leave alone (#1384). A number that
// large reads as the tool failing to understand the user's own history, which
// is the one thing doctor exists to rule out.
func unplacedFiles(root string, seen []string, skipped func(string) bool) (unread, byRule int) {
	return unplacedFilesIn(root, seen, skipped, false)
}

// unplacedFilesIn is unplacedFiles with the one decision a caller can make:
// whether a dot directory under this root is the store itself.
func unplacedFilesIn(root string, seen []string, skipped func(string) bool, dotDirsAreTheStore bool) (unread, byRule int) {
	have := make(map[string]bool, len(seen))
	for _, p := range seen {
		have[filepath.Clean(p)] = true
	}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".jsonl", ".json":
		default:
			return nil
		}
		if have[filepath.Clean(p)] {
			return nil
		}
		// A store's own scratch is not a transcript mss failed to read: 452
		// of the 482 this machine reported for codex sat in `.tmp`, and a
		// count that size reads as a parser that cannot cope with the store.
		//
		//
		// Unless the store keeps its transcripts there: Antigravity files
		// everything under `.system_generated`, so the rule silenced its row
		// completely rather than trimming its noise. Codex is the opposite —
		// it writes in-progress rollouts under `.tmp` — which is why this is
		// the caller's decision and not something inferred from the files
		// (#3377).
		if !dotDirsAreTheStore && inDotDir(root, p) {
			return nil
		}
		// Nor is an extension's own state. The pi family keeps it beside the
		// transcripts, at `sessions/<project>/extensions/<name>/<id>.json` —
		// senpi's terminal extension writes one per session — and counting
		// those said "2 not recognised here" about a store whose every
		// transcript had just been indexed, while `doctor --json` for the same
		// store said ok (#3669).
		if inExtensionState(root, p) {
			return nil
		}
		if skipped != nil && skipped(p) {
			byRule++
			return nil
		}
		unread++
		return nil
	})
	return unread, byRule
}

// inExtensionState reports whether a path sits under an `extensions` directory
// inside the store. A transcript never does: the directory belongs to whatever
// extension the harness is running, and what it keeps there is state.
func inExtensionState(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == "extensions" {
			return true
		}
	}
	return false
}

// printDoctorStoreWarnings says what mss could not read and why.
func printDoctorStoreWarnings(w io.Writer, stores []doctorStore) {
	for _, store := range stores {
		switch store.State {
		case "parsed-zero":
			fmt.Fprintf(w, "  warning      %s files found but newest parsed to zero\n", store.Name)
		case "unreadable":
			// The store is there and mss cannot read it — usually a harness
			// that changed its format. Silence here reads as "you have no
			// history with that agent".
			// With the reason. "Please report it" and nothing to report is how
			// #1642 arrived: a store mss refused, and no way for its owner or
			// for us to tell which refusal it was.
			if store.Error != "" {
				fmt.Fprintf(w, "  warning      %s store cannot be read — %s; please report it\n",
					store.Name, store.Error)
			} else {
				fmt.Fprintf(w, "  warning      %s store cannot be read — its format may have changed; please report it\n", store.Name)
			}
		case "denied":
			// Not a format change and not an empty history: mss is not
			// allowed to read the files. On macOS this is usually Full Disk
			// Access rather than the file mode (#802). A store that is only
			// partly unreadable loses sessions from recall while looking whole
			// everywhere else, so it says which half it is (#816).
			what := "store cannot be read"
			if store.Partial {
				what = "store is only partly readable — some sessions are missing from recall"
			}
			fmt.Fprintf(w, "  warning      %s %s — permission denied on %s; check its permissions (on macOS, also Full Disk Access for your terminal)\n", store.Name, what, store.Denied)
		case "needs-sqlite3", "needs-zstd":
			// Not a format change: the parser could not run at all. Saying so
			// points at installing one package instead of at a bug report
			// against the harness (#792). Which package depends on the store —
			// zed and deepseek need zstd (#1758) — and a partly readable one
			// says so rather than reading as a store that is entirely gone.
			what := "store"
			if store.Partial {
				what = "store is only partly readable — part of it"
			}
			// A sqlite3 that is installed and does not answer needs fixing, not
			// installing, and the reader needs to know which binary it is.
			if strings.Contains(store.Skipped, "sqlite3 at ") {
				fmt.Fprintf(w, "  warning      %s %s needs a working sqlite3 CLI: %s; fix or replace it, then run `mss index`\n", store.Name, what, store.Skipped)
				continue
			}
			fmt.Fprintf(w, "  warning      %s %s needs %s — install it, then run `mss index`\n", store.Name, what, toolFromSkip(store.Skipped))
		}
	}
}

// storeDiskGone distinguishes a store whose disk went away from a harness that
// was never installed. Both leave the path missing; what differs is how much of
// the way there is missing. `~/.kimi-code/sessions` on a machine without kimi
// loses one level and its home is right there; a store on an ejected volume
// loses the whole chain (#933).
// Cursor and aider hand doctor their roots joined for display, and a joined
// string is no path to walk up from: it lost the whole chain by construction
// and cursor's row said `unplugged` on every machine.
func storeDiskGone(location string) bool {
	roots := doctorLocationRoots(location)
	for _, root := range roots {
		if !oneStoreDiskGone(root) {
			return false
		}
	}
	return len(roots) > 0
}

func doctorLocationRoots(location string) []string {
	var roots []string
	for _, part := range strings.Split(location, string(os.PathListSeparator)) {
		for _, root := range strings.Split(part, ", ") {
			if root = strings.TrimSpace(root); root != "" {
				roots = append(roots, root)
			}
		}
	}
	return roots
}

func oneStoreDiskGone(path string) bool {
	// Two levels is not enough for every store: `~/.local/share/goose/sessions`
	// and `~/.cline/data/sessions` lose three on a machine that never installed
	// them. A home directory that is there means the disk is there.
	if home := sources.Home(); home != "" && strings.HasPrefix(path, home+string(os.PathSeparator)) && dirExists(home) {
		return false
	}
	dir := filepath.Dir(path)
	for i := 0; i < 2; i++ {
		if dirExists(dir) {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return true
}

// storeLabels names stores the way the rows below do.
func storeLabels(names []string) []string {
	return append([]string(nil), names...)
}

func doctorHarnesses(w io.Writer, dir string) {
	fmt.Fprintln(w, "Harness stores:")
	// Say the selection out loud. Without this line a narrowed run looks like
	// a machine that has thirty-four stores missing, and the variable is set by
	// whoever started the process — not necessarily by whoever is reading.
	if only, ok := sources.StoresSelected(); ok {
		fmt.Fprintf(w, "  reading only %s (%s)\n", strings.Join(storeLabels(only), ", "), sources.StoresEnv)
	}
	sqlite := sources.SQLite3Available()

	// Files are what mss found; sessions are what they became. The two differ
	// whenever ids collide — a resumed transcript, a copied one, a harness that
	// reuses a thread id — and the difference is invisible in a row that only
	// counts files (#861).
	indexed := index.HarnessSessionCounts(dir)
	sharedRows := index.HarnessSharedCounts(dir)
	keptRows := index.HarnessKeptCounts(dir)
	// Transcripts the index has no state for at all. Audited on a real store,
	// ten of them had never been read — five written eight weeks earlier — and
	// nothing on any surface said so, because a store's session count reads as
	// "nothing written yet" rather than "files never opened" (#3747).
	neverRead := index.HarnessUnreadCounts(dir)

	// The same inspection the JSON form reports, so one command does not give
	// two answers about one store: `found` here and `unreadable` there (#999).
	inspected := map[string]string{}
	unchecked := map[string]bool{}
	partly := map[string]bool{}
	for _, check := range doctorStoreChecks() {
		store, _ := inspectDoctorStore(check)
		inspected[check.name] = store.State
		unchecked[check.name] = store.Unchecked
		partly[check.name] = store.Partial
	}

	// Crush and OpenClaw print a row per database under one name, and the
	// counts below are the harness's, not the row's: repeated on every row, an
	// empty crush.db claimed another project's sessions (#4379). The first row
	// of a harness carries them.
	counted := map[string]bool{}
	printRow := func(name, path string, present bool, detail string) {
		// A store MSS_STORES silences has no row at all. The line above says
		// which stores are being read; a row saying "missing" about one of the
		// others would be answering a question nobody asked.
		if sources.StoreSilenced(name) {
			return
		}
		status := "missing"
		// A store the reader excluded says so whether or not it is on disk:
		// "missing" would read as mss not finding it, and "found" as mss
		// about to read it. Neither is true (#3499).
		if inspected[name] == "excluded" {
			fmt.Fprintf(w, "  %-12s %-9s %s\n", name, "excluded",
				"not read — `harness:"+name+"` is in "+reportPath(sources.ExcludePath()))
			return
		}
		if present {
			status = "found"
			// A directory mss cannot open loses its sessions from recall
			// without a word — the failure #802/#816 closed, but only in
			// `doctor --json`, which has said `denied` all along while this
			// row, the one people read, said `found` (#993).
			switch inspected[name] {
			case "denied", "unreadable", "parsed-zero", "needs-sqlite3", "needs-zstd":
				status = inspected[name]
				if status == "denied" {
					if detail != "" {
						detail += ", "
					}
					// The warning block below names the path and what to do.
					// Whole or in part: the row folded both into "cannot be
					// read" while the warning under it and the search above
					// said the store still answers (#1034, #816).
					if partly[name] {
						detail += "partly unreadable"
					} else {
						detail += "cannot be read"
					}
				}
			}
			// A walk that stopped at its budget looked at part of the store,
			// and `found` on its own claims the whole of it (#1025).
			if unchecked[name] {
				if detail != "" {
					detail += ", "
				}
				detail += "permissions not fully checked"
			}
		} else if storeDiskGone(path) {
			// A store whose whole disk is gone is not a store that was
			// deleted, and "missing" on a row of transcripts reads as the
			// second thing — the failure #906 fixed on every write path, and
			// #931 on the index row one screen below this one (#933).
			status = "unplugged"
		}
		// Also when the store is missing: a machine whose history arrived by
		// `sync import` has no files at all, and doctor said nothing about the
		// sessions it does hold — the only surface that names them was stats
		// (#892).
		first := !counted[name]
		counted[name] = true
		if n, ok := indexed[name]; ok && first {
			if detail != "" {
				detail += ", "
			}
			detail += doctorCount(n, "indexed session")
			// The gap between files and sessions has two causes, and they
			// read the same: a file that failed to parse, or two files
			// sharing an id. The manifest knows which (#1101).
			if sh := sharedRows[name]; sh > 0 {
				detail += fmt.Sprintf(", %d of them shared by two transcripts", sh)
			}
			// And the other way the numbers disagree: a session whose
			// transcript the client cleaned up, kept on purpose (#2970).
			if k := keptRows[name]; k == 1 {
				detail += ", 1 from a transcript no longer on disk"
			} else if k > 1 {
				detail += fmt.Sprintf(", %d from transcripts no longer on disk", k)
			}
		}
		// The gap in the other direction: transcripts the index has never
		// read. Outside the block above on purpose — a store with no indexed
		// session at all has no entry there, and that is exactly the store
		// this is about (#3747).
		if u := neverRead[name]; u > 0 && first {
			if detail != "" {
				detail += ", "
			}
			detail += doctorCount(u, "transcript") + " never read — `mss index`"
		}
		// A store path can come from the environment (MSS_NOTES_FILE) or from
		// disk. On a fixed-width row a newline in it prints a line of its own
		// that reads as one of doctor's.
		line := fmt.Sprintf("  %-12s %-9s %s", name, status, reportPath(path))
		if detail != "" {
			line += "  (" + detail + ")"
		}
		fmt.Fprintln(w, line)
	}

	// printFiles is printRow for the harnesses that answer with a file list.
	// The count comes from the same filter the parser uses, so a store whose
	// layout differs slightly loses those files from every number mss prints
	// — the one thing `doctor` exists to rule out (#701).
	// printFilesSkippingIn is printFiles for a harness that has more than one
	// transcript root and declines some of its own files by a rule.
	// Files named in beside are the store's own bookkeeping, as for
	// printFilesBeside below. Kimi's and Qwen's sub-agent logs were left out
	// whatever MSS_INCLUDE_SUBAGENTS said, so their rows did not name it
	// (#4473, #4475); the switch takes them now and every row names it (#4483).
	printFilesSkippingIn := func(name, loc string, roots []string, present bool, seen []string, skipped func(string) bool, beside ...string) {
		detail := doctorCount(len(seen), "file")
		placed := append(append([]string{}, seen...), beside...)
		unread := 0
		byRule := 0
		for _, root := range roots {
			u, b := unplacedFiles(root, placed, skipped)
			unread += u
			byRule += b
		}
		if byRule > 0 {
			// The variable named the way it was read as the cause of the
			// skip — "skipped (MSS_INCLUDE_SUBAGENTS=1)" — so somebody who
			// wanted those transcripts indexed set the thing the line said
			// was already set. It is the remedy, and it reads as one now.
			detail += fmt.Sprintf(", %d subagent transcripts skipped — set MSS_INCLUDE_SUBAGENTS=1 to index them", byRule)
		} else if short := countSubagentFiles(seen); short > 0 && os.Getenv("MSS_INCLUDE_SUBAGENTS") != "1" {
			// Read, but not in full: what a reader needs to know is which half
			// of those files is searchable, and how to get the rest (#3009).
			detail += fmt.Sprintf(", %d subagent transcripts read as task, answer and what they changed — set MSS_INCLUDE_SUBAGENTS=1 for the whole run", short)
		}
		if unread > 0 {
			detail += fmt.Sprintf(", %d not recognised here", unread)
		}
		printRow(name, loc, present, detail)
	}
	// printFilesSkipping is its one-root form.
	printFilesSkipping := func(name, path string, present bool, seen []string, skipped func(string) bool, beside ...string) {
		printFilesSkippingIn(name, path, []string{path}, present, seen, skipped, beside...)
	}
	printFiles := func(name, path string, present bool, seen []string) {
		printFilesSkipping(name, path, present, seen, nil)
	}
	// printFilesBeside is printFiles for a harness whose store keeps files
	// beside the transcripts that are not transcripts — Continue's
	// sessions.json (the list, which mss reads), Copilot's vscode.metadata.json
	// (the IDE's bookkeeping, #3303), Kimi's per-session state.json (the title
	// and working directory, #3309). Counted with the files, it made `doctor`
	// disagree with `mss sources` by one; counted as unread, it made every
	// store report a file mss could not read (#3297). So it is neither: the
	// count is the transcripts, and the note leaves the list alone.
	// printFilesBesideIn is printFilesBeside for a row whose printed location is
	// not one directory: cline names its modern store and its legacy roots on
	// the same line, and that string cannot be walked (#3360).
	printFilesBesideIn := func(name, loc string, walks []string, dotDirsAreTheStore, present bool, seen []string, beside ...string) {
		detail := doctorCount(len(seen), "file")
		placed := append(append([]string{}, seen...), beside...)
		unread := 0
		for _, walk := range walks {
			u, _ := unplacedFilesIn(walk, placed, nil, dotDirsAreTheStore)
			unread += u
		}
		if unread > 0 {
			detail += fmt.Sprintf(", %d not recognised here", unread)
		}
		printRow(name, loc, present, detail)
	}
	printFilesBeside := func(name, path string, present bool, seen []string, beside ...string) {
		printFilesBesideIn(name, path, []string{path}, false, present, seen, beside...)
	}

	claudeRoots := sources.ClaudeRoots()
	claudeLocation := strings.Join(claudeRoots, string(os.PathListSeparator))
	claudePresent := false
	for _, root := range claudeRoots {
		claudePresent = claudePresent || doctorExists(root)
	}
	printFilesSkippingIn("claude", claudeLocation, claudeRoots, claudePresent, sources.ClaudeFiles(),
		func(p string) bool { return !sources.ClaudeFileWanted(p) }, sources.ClaudeSidecarFiles()...)

	codexRoots := sources.CodexRoots()
	codexLocation := strings.Join(codexRoots, string(os.PathListSeparator))
	codexPresent := false
	for _, root := range codexRoots {
		codexPresent = codexPresent || doctorExists(root)
	}
	printFilesBesideIn("codex", codexLocation, codexRoots, false, codexPresent, sources.CodexFiles(), sources.CodexSidecarFiles()...)

	ocDB := sources.OpencodeDB()
	printRow("opencode", ocDB, doctorFilePresent(ocDB), doctorSQLiteDetail(ocDB, sqlite))

	printRow("cursor", doctorCursorLocation(), doctorCursorPresent(), doctorCursorDetail(sqlite))

	// The store root also holds Grok's settings, credentials and caches, which
	// are not transcripts and never will be; the sessions directory is what the
	// count is about (#3319).
	grokRoot := filepath.Join(sources.GrokRoot(), "sessions")
	printFilesBeside("grok", grokRoot, doctorExists(grokRoot), sources.GrokSessionFiles(), sources.GrokSidecarFiles()...)

	piRoot := sources.PiRoot()
	printFiles("pi", piRoot, doctorExists(piRoot), sources.PiSessionFiles())
	ompRoot := sources.OmpRoot()
	printFiles("omp", ompRoot, doctorExists(ompRoot), sources.OmpSessionFiles())
	dshRoot := sources.DeepSeekRoot()
	printFiles("deepseek", dshRoot, doctorExists(dshRoot), sources.DeepSeekSessionFiles())
}

// toolFromSkip turns a skip reason into the package to install.
func toolFromSkip(reason string) string {
	switch {
	case strings.Contains(reason, "sqlite3") && strings.Contains(reason, "zstd"):
		return "the sqlite3 and zstd CLIs"
	case strings.Contains(reason, "zstd"):
		return "the zstd CLI"
	default:
		return "the sqlite3 CLI"
	}
}

// doctorDBPrereqNote is what a row has to add about the half of a store that
// needs the sqlite3 CLI. Kilo's and ZCode's rows named their database and said
// nothing about the tool that reads it, so on a machine without sqlite3 those
// sessions were missing from recall with the row reporting the store present
// (#3679). The rows for the stores that are only a database say it through
// doctorSQLiteDetail; these two have transcripts as well, so the note rides
// beside the count.
func doctorDBPrereqNote(sqlite bool) string {
	if sqlite {
		return ""
	}
	return " but the sqlite3 CLI is " + sqliteGap() + " — those sessions are unavailable"
}

// sqliteGap words what is wrong with sqlite3 for a row that has no room for the
// whole problem; the Tools section names the binary and what it did.
func sqliteGap() string {
	if sources.SQLite3Broken() {
		return "not working"
	}
	return "missing"
}

func doctorSQLiteDetail(db string, sqlite bool) string {
	fi, err := os.Stat(db)
	if err != nil || fi.Size() == 0 {
		return ""
	}
	d := humanBytes(fi.Size())
	if !sqlite {
		d += ", sqlite3 CLI " + sqliteGap() + " — sessions unavailable"
	}
	return d
}

func doctorCursorDetail(sqlite bool) string {
	parts := []string{doctorCount(len(sources.CursorTranscripts()), "CLI transcript")}
	dbs := sources.CursorDBs()
	if len(dbs) > 0 {
		var size int64
		for _, db := range dbs {
			if fi, err := os.Stat(db); err == nil {
				size += fi.Size()
			}
		}
		seg := fmt.Sprintf("%s IDE %s", doctorCount(len(dbs), "store"), humanBytes(size))
		if !sqlite {
			seg += ", sqlite3 CLI " + sqliteGap() + " — IDE sessions unavailable"
		}
		parts = append(parts, seg)
	}
	// Cursor CLI writes a second, content-addressed store per chat and nothing
	// reads it. While a transcript sits beside every chat that has one, that
	// costs nothing and this stays quiet; if a release stops writing
	// transcripts, this is the only thing that would say so (#3772).
	if n := sources.CursorChatsWithoutTranscript(); n > 0 {
		parts = append(parts, fmt.Sprintf("%s with no transcript — contents not readable yet", doctorCount(n, "CLI chat")))
	}
	return strings.Join(parts, ", ")
}

func doctorCursorPresent() bool {
	// Chats count as present even though nothing parses them: a machine whose
	// Cursor writes only the new store would otherwise drop the row, and the
	// row is where the warning above lives.
	return len(sources.CursorTranscripts()) > 0 || len(sources.CursorDBs()) > 0 ||
		len(sources.CursorChatStores()) > 0
}

func doctorCursorLocation() string {
	// Contracted here rather than by the row: this location is two paths in one
	// string, and the row's own contraction only reaches the first (#2360).
	return strings.Join([]string{reportPath(sources.CursorUserRoot()), reportPath(sources.CursorCLIRoot())}, ", ")
}

func doctorTools(w io.Writer) {
	fmt.Fprintln(w, "Tools:")
	// Found and working are not the same thing: a wrapper that drops its
	// arguments or a stub that prints nothing reads every store as empty, and
	// this line said "found" beside a row of parsed-zero stores.
	status := "not found"
	problem := sources.SQLite3Problem()
	switch {
	case problem == "":
		status = "found"
	case sources.SQLite3Broken():
		status = "found but not working"
	}
	fmt.Fprintf(w, "  %-12s %s (needed for opencode and Cursor IDE stores)\n", "sqlite3", status)
	if sources.SQLite3Broken() {
		fmt.Fprintf(w, "  warning      %s; every database store reads as empty until it is fixed\n", problem)
	}
	// git is not optional decoration: without it a hit loses the line saying
	// its files have changed since, project names lose worktree identity, and
	// the session-start hook loses the task signal. All three degrade in
	// silence, which is fine on a hit and not fine with nowhere to ask (#796).
	gitStatus := "not found"
	if _, err := exec.LookPath("git"); err == nil {
		gitStatus = "found"
	}
	fmt.Fprintf(w, "  %-12s %s (needed for changed-file notes, worktree names and the task signal)\n", "git", gitStatus)
}

// policyWithheldCounts reports, per activation, how many indexed sessions the
// policy in force keeps off that path, and how many are indexed in all.
func policyWithheldCounts(dir string) (map[string]int, int) {
	metas, err := index.AllMeta(dir)
	if err != nil {
		return nil, 0
	}
	pol := policy.Load()
	out := map[string]int{}
	for _, activation := range []string{policy.ActivationSearch, policy.ActivationMCP, policy.ActivationAuto} {
		for _, m := range metas {
			if !pol.Allows(activation, m.Project) {
				out[activation]++
			}
		}
	}
	return out, len(metas)
}

// unmatchedImportGroups lists imported:<group> rules that no session in the
// index answers to.
func unmatchedImportGroups(dir string) []string {
	pol := policy.Load()
	present := map[string]bool{}
	metas, err := index.AllMeta(dir)
	if err != nil {
		return nil
	}
	for _, m := range metas {
		if o := policy.Origin(m.Project); strings.HasPrefix(o, "imported:") {
			present[o] = true
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, rules := range pol.Activations {
		for origin, allowed := range rules {
			if allowed || !strings.HasPrefix(origin, "imported:") || present[origin] || seen[origin] {
				continue
			}
			seen[origin] = true
			out = append(out, origin)
		}
	}
	sort.Strings(out)
	return out
}

// doctorPolicy reports the one mechanism that separates local memory from
// imported. Load falls back to the permissive default on any error, so a
// malformed file changed nothing and said nothing, and a working one was
// invisible — leaving no place at all to find out what the rules are (#661).
func doctorPolicy(w io.Writer, dir string) {
	fmt.Fprintln(w, "Trust policy:")
	exists, unknown, err := policy.Diagnose()
	if !exists {
		// …unless the environment restricts it anyway. Deciding on the file's
		// absence said "every origin activates everywhere" while the auto path
		// was local-only, on the one screen someone opens to find out what is
		// allowed (#939).
		if pol := policy.Load(); pol.Describe(policy.ActivationAuto) != "local+imported" {
			fmt.Fprintf(w, "  %-12s %s\n", "default", noPolicyFileLine())
			withheld, total := policyWithheldCounts(dir)
			for _, activation := range []string{policy.ActivationSearch, policy.ActivationMCP, policy.ActivationAuto} {
				line := pol.Describe(activation)
				if n := withheld[activation]; n > 0 {
					line += fmt.Sprintf(" — withholds %d of %d indexed session%s", n, total, pluralS(total))
				}
				fmt.Fprintf(w, "  %-12s %s\n", activation, line)
			}
			fmt.Fprintf(w, "  %-12s MSS_AUTORECALL_LOCAL_ONLY is set in this environment\n", "from env")
			return
		}
		fmt.Fprintf(w, "  %-12s %s — every origin activates everywhere\n", "default", noPolicyFileLine())
		// Except one thing, which is in force with or without a file and is
		// the reason a directory can be missing from recall (#2050).
		printIgnored(w, policy.Load(), dir)
		return
	}
	if err != nil {
		// The permissive default is what is actually in force, and that is the
		// part worth saying out loud: the file reads like a restriction.
		fmt.Fprintf(w, "  %-12s %s: %v\n", "unreadable", reportPath(policy.Path()), err)
		fmt.Fprintf(w, "  %-12s every origin activates everywhere until it parses\n", "in force")
		return
	}
	pol := policy.Load()
	withheld, total := policyWithheldCounts(dir)
	for _, activation := range []string{policy.ActivationSearch, policy.ActivationMCP, policy.ActivationAuto} {
		line := pol.Describe(activation)
		// The rule's text is not its effect. `search local-only` reads the
		// same whether it withholds nothing or the whole index, and doctor is
		// where someone checks that the rule does what they meant (#978).
		if n := withheld[activation]; n > 0 {
			line += fmt.Sprintf(" — withholds %d of %d indexed session%s", n, total, pluralS(total))
		}
		fmt.Fprintf(w, "  %-12s %s\n", activation, line)
	}
	printIgnored(w, pol, dir)
	for _, u := range unknown {
		fmt.Fprintf(w, "  %-12s %q is not an activation or origin mss consults — this rule does nothing\n", "ignored", u)
	}
	// An `imported:x` rule has the right shape and still matches nothing when
	// no session came from a project starting with x — the group is a project
	// prefix from the exporting machine, not a machine name, and a rule
	// written for a machine reads as in force forever (#955).
	for _, g := range unmatchedImportGroups(dir) {
		fmt.Fprintf(w, "  %-12s %q matches nothing in this index — the part after `imported:` is the first path component of the project on the machine it came from, not that machine's name\n", "inert", g)
	}
}

// indexFormatDirection is a variable so a test can put doctor in front of an
// index this build cannot read without shipping a manifest writer.
var indexFormatDirection = index.FormatDirection

// indexReadState is a variable for the same reason, and it is the one the row
// below asks: how old the index is and what this build may do with it are
// different questions (#3597).
var indexReadState = index.ReadStateOf

func doctorIndex(w io.Writer, idx doctorIndexReport, dir string) {
	fmt.Fprintln(w, "Index:")
	loc := idx.Path
	if loc == "" {
		loc = dir
	}
	fmt.Fprintf(w, "  location %s\n", reportPath(loc))
	// "Active" is what a reader checks this screen for, and it was false in the
	// one state they check it in: the pattern is written, the index is not
	// rebuilt, and the next search still serves the project they meant to hide.
	// The list applies at ingest, so it covers nothing already indexed until a
	// rebuild — the sentence `mss index` prints for the same reason (#1307,
	// #2664).
	if n := len(sources.ExclusionPatterns()); n > 0 && index.ExclusionsChanged(dir) {
		fmt.Fprintf(w, "  exclusions %d pattern%s, not applied to sessions already indexed — `mss index --rebuild`\n", n, pluralS(n))
	} else {
		fmt.Fprintf(w, "  exclusions %d active patterns\n", n)
	}
	// A precise non-claim: users deciding what to trust deserve to read the
	// boundary in the tool itself, not only in the security docs.
	fmt.Fprintln(w, "  security plaintext on disk — protected by file permissions only, no encryption or access control")
	if idx.State == "missing" || idx.State == "path-is-a-file" {
		// A file where the directory belongs: a build refuses rather than
		// deleting it, so "run `mss index`" would send the reader to a
		// command that will not run either (#3610).
		if fi, err := os.Stat(dir); err == nil && !fi.IsDir() {
			fmt.Fprintf(w, "  status   not built — %s is a file, not a directory; move it aside, or point MSS_INDEX_DIR at a directory\n", reportPath(dir))
			return
		}
		// An index whose disk was unplugged is not a missing index, and
		// "run `mss index`" points at a path that is not there. doctor is
		// what someone runs when memory looks broken (#931).
		if parent := filepath.Dir(dir); !dirExists(parent) {
			fmt.Fprintf(w, "  status   not reachable — %s is not there; the disk it lives on may have been unmounted\n", parent)
			return
		}
		// The index directory is there but cannot be read — a permissions
		// problem or a restricted mount, not a missing build. "run `mss
		// index`" would send the reader to a command that cannot read it
		// either, and the index may well be built behind the closed door
		// (#1116).
		if dirExists(dir) {
			if _, err := os.ReadDir(dir); os.IsPermission(err) {
				fmt.Fprintf(w, "  status   unreadable — %s cannot be read (permission denied); fix its permissions or point MSS_INDEX_DIR somewhere readable\n", dir)
				return
			}
		}
		// "run `mss index`" on a location that cannot be written sends the
		// reader to a command that fails the same way. doctor is where someone
		// looks to learn why memory is absent, so it has to name the reason.
		if !indexDirWritable(dir) {
			fmt.Fprintf(w, "  status   not built — %s is not writable, so no build can run there; point MSS_INDEX_DIR somewhere writable\n", filepath.Dir(dir))
			return
		}
		fmt.Fprintln(w, "  status   not built (run `mss index`)")
		return
	}
	updated := "unknown"
	if fi, err := os.Stat(filepath.Join(dir, "manifest.gob")); err == nil {
		updated = fi.ModTime().Format("2006-01-02 15:04")
	}
	// When the index was last written and when mss last read this machine's
	// stores are different facts, and on a machine that syncs they drift apart:
	// an import rewrites the index without opening a transcript (#3747). Said
	// only when they differ by more than an hour, so an ordinary machine keeps
	// the one-line form.
	read := ""
	if at := index.ManifestSourcesReadAt(dir); at.IsZero() {
		read = ", stores never read"
	} else if fi, err := os.Stat(filepath.Join(dir, "manifest.gob")); err == nil && fi.ModTime().Sub(at) > time.Hour {
		read = ", stores read " + at.Format("2006-01-02 15:04")
	}
	fmt.Fprintf(w, "  status   built (size=%s, updated=%s%s)\n", humanBytes(pathSize(dir)), updated, read)
	// An index written by an older format is unreadable to this binary: the
	// hook paths refuse it and ask for a rebuild, which is why memory goes
	// quiet after an upgrade. doctor called that "up to date" — the one
	// command someone runs to find out why nothing is recalled (#877).
	switch indexReadState(dir) {
	case index.ReadStateUnreadable:
		fmt.Fprintln(w, "  format   written by an older mss — this build cannot read it; the next session rebuilds it, or run `mss index` now")
	case index.ReadStateWithheld:
		// It reads. What it holds is text written before mss knew how to
		// redact something, so it answers nothing until the re-read is done —
		// which is a different sentence from "cannot read", and the only one of
		// the three that stops recall.
		fmt.Fprintln(w, "  format   written before mss learned to mask something it now masks — it answers once the sources are re-read; `mss index` does it now")
	case index.ReadStateOlderRules:
		// And the common upgrade: the store reads, answers, and re-derives
		// behind the answer. Calling that unreadable told someone looking for
		// why nothing is recalled that their index was gone (#3597, #3562).
		fmt.Fprintln(w, "  format   written by an older mss — it still answers; the next session re-reads the sources, or run `mss index` now")
	case index.ReadStateNewer:
		// The binary was rolled back, not the index. Saying "older" here sent
		// that reader looking in the wrong direction (#890).
		fmt.Fprintln(w, "  format   written by a newer mss than this one — this build rebuilds it in its own format; upgrading again rebuilds it back")
	}
	// A store whose postings vanished or whose record log was truncated cannot
	// answer anything, and said "up to date" until #735. The next search
	// rebuilds it, which is worth saying too — the reader has not lost memory,
	// only this build of the index.
	if reason := indexDamageReason(dir); reason != "" {
		// What broke, not a summary of the four ways it can: a manifest that
		// will not decode is not missing records, and the sentence sent the
		// reader — and whoever reads the doctor output they paste into an
		// issue — after the wrong file (#2695).
		fmt.Fprintf(w, "  integrity damaged — %s; the next search rebuilds the index\n", reason)
		return
	}
	switch idx.State {
	case "stale":
		if idx.StaleStores == 1 {
			fmt.Fprintln(w, "  freshness 1 store changed since mss last read it — run `mss index`")
		} else {
			fmt.Fprintf(w, "  freshness %d stores changed since mss last read them — run `mss index`\n", idx.StaleStores)
		}
	case "stale-readonly":
		fmt.Fprintf(w, "  freshness %s changed since mss last read it, and the index cannot be written — check the permissions on %s, or point MSS_INDEX_DIR somewhere writable\n",
			doctorCount(idx.StaleStores, "store"), filepath.Dir(idx.Path))
	default:
		fmt.Fprintln(w, "  freshness up to date")
	}
	// A stamp the clock cannot account for puts a session at the top of every
	// surface ordered by date, and leaves it there. The first screen has said
	// so since #696 and the listing since #2105; this is where a reader looks
	// when a store reads wrong (#2106).
	if idx.SessionsAhead > 0 {
		// pluralThatThose fits "— that one is at the top of this list", the
		// sentence `mss last` and `mss log` print. Spliced here it read
		// "leads with that one is".
		lead := "it"
		if idx.SessionsAhead > 1 {
			lead = "one of them"
		}
		fmt.Fprintf(w, "  clock    %s stamped later than this machine's clock — `mss last` leads with %s\n",
			doctorCount(idx.SessionsAhead, "session"), lead)
	}
	health := index.IngestHealth(dir)
	names := make([]string, 0, len(health))
	for h, e := range health {
		if e.MalformedLines > 0 || e.FailedFiles > 0 || e.ClippedMessages > 0 {
			names = append(names, h)
		}
	}
	sort.Strings(names)
	for _, h := range names {
		e := health[h]
		// "malformed" covered only unparseable lines; valid JSON mss cannot
		// use is skipped just as invisibly, and the reader needs the same
		// warning either way (#814).
		clipped := ""
		if e.ClippedMessages > 0 {
			// Named separately from the skipped lines: the session is here and
			// searchable, it is the tail of one message that is not, and a
			// search over that tail answers "no matches" (#1093).
			clipped = fmt.Sprintf(", %d message%s stored short of the transcript (over 64 KB)",
				e.ClippedMessages, pluralS(e.ClippedMessages))
		}
		fmt.Fprintf(w, "  ingest   %s: %d unusable %s%s skipped, %d path%s unreadable%s — see `mss doctor --json`\n",
			h, e.MalformedLines, sources.SkippedNoun(h), pluralS(e.MalformedLines), e.FailedFiles, pluralS(e.FailedFiles), clipped)
	}
	reportFutureDated(w, dir)
}

// reportFutureDated names the sessions stamped ahead of the clock.
//
// One note dated next year sits at the top of `mss last` and stays there. mss
// cannot tell a skewed clock from a deliberate date, so the ordering is left
// alone — but saying nothing leaves the reader with a store that looks wrong
// for no visible reason, and the usual causes are a typo'd year in a
// hand-edited note file or a millisecond stamp read as seconds (#2063).
func reportFutureDated(w io.Writer, dir string) {
	metas, err := index.AllMeta(dir)
	if err != nil {
		return
	}
	// A minute of slack: clocks between machines disagree by seconds, and a
	// session synced from a peer a moment ago is not a finding.
	cutoff := time.Now().Add(time.Minute)
	newest, count := time.Time{}, 0
	var newestID string
	for _, meta := range metas {
		if !meta.Updated.After(cutoff) {
			continue
		}
		count++
		if meta.Updated.After(newest) {
			newest, newestID = meta.Updated, meta.Harness+":"+meta.ID
		}
	}
	if count == 0 {
		return
	}
	fmt.Fprintf(w, "  clock    %d session%s stamped in the future, newest %s (%s) — it sorts above real work in `mss last` until the date is corrected\n",
		count, pluralS(count), newest.Local().Format("2006-01-02"), safeForStatusline(newestID, 80))
}

// reportPath is how the human report names a file: control characters
// sanitised, and a home-prefixed path contracted to ~. The issue template asks
// a reporter to "run mss doctor and redact local paths before pasting", which
// is work this report can do for them — ~/.claude/projects is as actionable as
// the absolute form for the person who ran it (#2360). --json keeps the real
// path: a tool reading it may need one, and nobody pastes JSON by hand.
func reportPath(p string) string {
	if p == "" {
		return p
	}
	// Some rows carry several paths in one string — a store mss looks for in
	// two places, or a root list from the environment. Contracting the whole
	// string would only reach the first, which is how the cursor row came out
	// half in ~ and half in /Users/… .
	parts := strings.Split(p, string(os.PathListSeparator))
	for i, part := range parts {
		parts[i] = search.SafePath(underHome(part))
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

// underHome contracts a home-prefixed path to ~, and leaves everything else
// alone. The boundary check keeps /home/alicia out of alice's tilde.
func underHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(p, home) {
		return p
	}
	rest := strings.TrimPrefix(p, home)
	if rest == "" || rest[0] == '/' || rest[0] == '\\' {
		return "~" + rest
	}
	return p
}

func doctorCount(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func doctorExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// doctorAnyExists reports whether at least one of the paths is on disk.
func doctorAnyExists(paths []string) bool {
	for _, p := range paths {
		if doctorExists(p) {
			return true
		}
	}
	return false
}

func doctorFilePresent(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Size() > 0
}

// printIgnored says which directories mss does not recall from. A rule that
// silently drops history is indistinguishable from history that was never
// there, so it is printed whether it came from the policy file or from the
// built-in default (#2050).
//
// And what it costs, the way the activation rows say what they withhold: the
// rule's text is not its effect. A rule is matched against the project name
// and the transcript's own path, so the natural thing to write — the
// directory's absolute path — matches neither, and the row above reported it
// as in force while it hid nothing (#3584).
func printIgnored(w io.Writer, pol policy.Policy, dir string) {
	pats := pol.IgnorePatterns()
	if len(pats) == 0 {
		return
	}
	what := "default"
	if len(pol.Ignore) > 0 {
		what = "from the file"
	}
	fmt.Fprintf(w, "  %-12s %s (%s)\n", "not recalled", strings.Join(pats, ", "), what)
	// Only for a rule somebody wrote. The default is mss's own and a machine
	// that has never met an agent runtime is not being told about it.
	if len(pol.Ignore) == 0 {
		return
	}
	metas, err := index.AllMeta(dir)
	if err != nil || len(metas) == 0 {
		return
	}
	for _, pat := range pol.Ignore {
		one := policy.Policy{Ignore: []string{pat}}
		n := 0
		for _, m := range metas {
			if one.Ignored(m.Path, m.Project) {
				n++
			}
		}
		if n == 0 {
			fmt.Fprintf(w, "  %-12s %q matches no indexed session — a rule is matched against the project name and the transcript's path, not the directory you ran in\n", "", pat)
			continue
		}
		fmt.Fprintf(w, "  %-12s %q hides %d of %d indexed session%s\n", "", pat, n, len(metas), pluralS(len(metas)))
	}
}

// noPolicyFileLine says where the policy would be, or that there is nowhere
// for it: with no home directory doctor printed "no file at " and stopped
// (#2785), on the screen somebody opens to find out where the file goes.
func noPolicyFileLine() string {
	if p := policy.Path(); p != "" {
		return "no file at " + reportPath(p)
	}
	return "no policy file — mss cannot find a home directory to look in"
}
