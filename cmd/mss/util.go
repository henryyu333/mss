package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/henryyu333/mss/internal/index"
	"github.com/henryyu333/mss/internal/termwidth"
)

const (
	logoAccent = "\x1b[38;5;141m"
	logoBold   = "\x1b[1m"
	logoDim    = "\x1b[2m"
	logoReset  = "\x1b[0m"
)

// pluralS keeps "1 sessions" off the screen.
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func verbIs(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

func indexDirWritable(dir string) bool {
	// The directory the rebuild actually writes into. Probing the parent alone
	// read a read-only index directory inside a writable parent as writable —
	// a cache directory owned by another user, a read-only subtree (#2499).
	// The parent is still the right probe when the index directory does not
	// exist yet, since mss creates it.
	if !dirWritable(filepath.Dir(dir)) {
		return false
	}
	if dirExists(dir) {
		return dirWritable(dir)
	}
	return true
}

// printableWidth is the width the answer is laid out for: COLUMNS when the
// reader set it, the terminal when it reports one, 80 otherwise.
func printableWidth(w io.Writer) int {
	if os.Getenv("COLUMNS") != "" {
		return briefWidth()
	}
	return briefWidth()
}

func briefWidth() int {
	// COLUMNS first: a reader who exports it is overriding the terminal on
	// purpose, and scripts set it to pin the layout.
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n >= 20 && n <= 400 {
		return n
	}
	if n, ok := terminalWidth(); ok && n >= 20 && n <= 400 {
		return n
	}
	return 80
}

// safeForStatusline strips what a terminal would act on and bounds the width.
func safeForStatusline(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if termwidth.Columns(s) > max {
		return strings.TrimSpace(termwidth.Cut(s, max)) + "…"
	}
	return s
}

// logoWanted answers whether the reader has a terminal that can take live
// progress display: NO_COLOR and TERM=dumb readers get plain lines.
var logoWanted = defaultLogoWanted

func defaultLogoWanted(f *os.File) bool {
	// TERM=dumb is a terminal that cannot do any of this: emacs shell-mode, a
	// CI shell, an editor's built-in console (#903).
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	return !isNullDevice(fi)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func pluralSessions(n int) string {
	if n == 1 {
		return "1 session"
	}
	return fmt.Sprintf("%d sessions", n)
}

// fold collapses runs of whitespace, for comparing a query against a command
// without touching the command itself.
func fold(s string) string { return strings.Join(strings.Fields(s), " ") }

// commandMentions is the membership test for command hints: the term has to
// appear in the command as a word, not inside a longer one (#1630).
func commandMentions(low, term string) bool {
	if term == "" {
		return false
	}
	if strings.ContainsFunc(term, func(r rune) bool { return !isCommandWordRune(r) }) {
		if strings.Contains(low, term) {
			return true
		}
		if strings.ContainsFunc(term, unicode.IsSpace) {
			return strings.Contains(fold(low), fold(term))
		}
		return false
	}
	for at := 0; ; {
		i := strings.Index(low[at:], term)
		if i < 0 {
			return false
		}
		i += at
		beforeOK := i == 0 || !isCommandWordRune(rune(low[i-1]))
		end := i + len(term)
		afterOK := end == len(low) || !isCommandWordRune(rune(low[end]))
		if beforeOK && afterOK {
			return true
		}
		at = i + 1
	}
}

func isCommandWordRune(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// nearestTarget finds a target within one edit of what was typed, which covers
// the ways a name is actually got wrong: a dropped letter, a doubled one, a
// transposition.
func nearestTarget(typed string, names []string) string {
	typed = strings.ToLower(strings.TrimSpace(typed))
	if typed == "" {
		return ""
	}
	for _, n := range names {
		if strings.HasPrefix(n, typed) {
			return n
		}
	}
	best, bestDist := "", 3
	for _, n := range names {
		if d := editDistance(typed, n); d < bestDist {
			best, bestDist = n, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// verbShare keeps "1 session share" off the screen.
func verbShare(n int) string {
	if n == 1 {
		return "shares"
	}
	return "share"
}

// indexDamageReason is the seam for the sentence doctor prints: which file is
// not what the manifest says, rather than a summary of every way a store can
// break (#2695).
var indexDamageReason = index.DamageReason

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// xdgConfigHome honours only absolute XDG_CONFIG_HOME values, per the spec: an
// implementation that encounters a relative path ignores it (#1693).
func xdgConfigHome() string {
	if base := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(base) {
		return base
	}
	return filepath.Join(homeDir(), ".config")
}
