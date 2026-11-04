package search

import (
	"testing"

	"github.com/henryyu333/mss/internal/model"
)

func TestApplyWornLiftsServedSessions(t *testing.T) {
	hits := []Hit{
		{Session: model.Session{ID: "fresh"}, Score: 100},
		{Session: model.Session{ID: "worn"}, Score: 90},
	}
	got := ApplyWorn(hits, map[string]int{"worn": 40})
	if got[0].Session.ID != "worn" || got[0].Reused != 40 {
		t.Fatalf("a served session must gain Reused and the lift: %+v", got[0])
	}
	if got[1].Reused != 0 {
		t.Fatalf("an unserved session must not be marked: %+v", got[1])
	}
}

func TestApplyWornCannotOutrankADominantMatch(t *testing.T) {
	hits := []Hit{
		{Session: model.Session{ID: "strong"}, Score: 100},
		{Session: model.Session{ID: "worn"}, Score: 40},
	}
	got := ApplyWorn(hits, map[string]int{"worn": 1000})
	if got[0].Session.ID != "strong" {
		t.Fatalf("the 1.5 cap must keep reuse from buying the top: %+v", got[0])
	}
}

func TestApplyWornEmptyMapIsIdentity(t *testing.T) {
	hits := []Hit{{Session: model.Session{ID: "a"}, Score: 1}}
	got := ApplyWorn(hits, nil)
	if len(got) != 1 || got[0].Reused != 0 {
		t.Fatalf("no history must change nothing: %+v", got)
	}
}
