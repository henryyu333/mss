package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/henryyu333/mss/internal/model"
	"github.com/henryyu333/mss/internal/redact"
	"github.com/henryyu333/mss/internal/search"
	"github.com/henryyu333/mss/internal/sources"
)

// showBriefDefault is how much of a message `show --brief` prints before the
// line says the rest is one command away. A window of forty turns at 200
// characters is a page an agent can scan without parsing JSON, which is the
// work the flag replaces.
const showBriefDefault = 200

// briefMatches reports whether a message's role is one the reader asked for;
// no roles means every role. "tool" is the documented spelling, "tool-output"
// the stored one — the same alias `--role` has on search.
func briefMatches(m model.Message, roles []string) bool {
	if len(roles) == 0 {
		return true
	}
	for _, r := range roles {
		if r == "tool" {
			r = sources.RoleToolOutput
		}
		if m.Role == r {
			return true
		}
	}
	return false
}

// briefMatchCount counts the messages of the requested roles.
func briefMatchCount(ms []model.Message, roles []string) int {
	n := 0
	for _, m := range ms {
		if briefMatches(m, roles) {
			n++
		}
	}
	return n
}

// briefPreview is one message's body as a single line: whitespace folded, cut
// at n runes. cut says the rest was left out.
func briefPreview(text string, n int) (line string, cut bool) {
	line = strings.Join(strings.Fields(search.SafeText(text)), " ")
	r := []rune(line)
	if len(r) <= n {
		return line, false
	}
	return string(r[:n]), true
}

// printSessionBrief is `show --brief`: one header per message — record index,
// role, time — and the body cut short, with the command that reads that
// message whole. The JSON window stays the surface for exact quotes; this one
// exists so an agent scanning a transcript does not write a parser to do it.
func printSessionBrief(w io.Writer, s model.Session, o showOptions) {
	for i, m := range s.Messages {
		if !briefMatches(m, o.roles) {
			continue
		}
		idx := o.offset + i
		when := "-"
		if !m.Time.IsZero() {
			when = m.Time.Format(time.RFC3339)
		}
		fmt.Fprintf(w, "[%d] %s %s\n", idx, redact.SafeForDisplay(m.Role), when)
		line, cut := briefPreview(m.Text, o.briefLen)
		if !cut {
			fmt.Fprintln(w, redact.SafeForDisplay(line))
			continue
		}
		fmt.Fprintf(w, "%s … (cut at %d characters — `mss show %s --harness %s --around %d --limit 1` reads this message whole)\n",
			redact.SafeForDisplay(line), o.briefLen, pasteSafe(s.ID), pasteSafe(s.Harness), idx)
	}
}
