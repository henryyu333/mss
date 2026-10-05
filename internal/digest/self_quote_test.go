package digest

import "testing"

// mss asks an agent to credit the recall it used, and that sentence reports
// what an earlier session decided — so every decision marker fires on it and
// the line came back as a decision of its own. Found on a real store at the
// file line: `doctor_auto.go has been worked on in 2 sessions — prior decision:
// mss recalled: …`, which is mss quoting itself quoting a session.
func TestMssQuotingItselfIsNotADecision(t *testing.T) {
	quotes := []string{
		"déjà vu: the retry budget stays at four (claude, Aug 11, mss:2fc1d1ef) — reusing it.",
		"mss recalled: earlier experiments already fixed the ranking, so that is settled",
		"Recalled from this machine's history: the deploy key was rotated in March",
		"mss found sessions whose wording matches this request",
	}
	for _, q := range quotes {
		if CarriesDecision(q) {
			t.Errorf("mss's own line read as a decision:\n  %s", q)
		}
	}
}

// And a person's own decision about mss still is one — the guard is about who
// is speaking, not about the word.
func TestADecisionAboutMssIsStillADecision(t *testing.T) {
	decisions := []string{
		"решили: mss больше не трогает живой индекс, только копию",
		"we settled on keeping the mss hook out of the release build",
	}
	for _, d := range decisions {
		if !CarriesDecision(d) {
			t.Errorf("a real decision was dropped:\n  %s", d)
		}
	}
}
