package search

import "testing"

// A hit line reads "— 1 match", not "— 1 matches".
func TestMatchWordAgreesWithTheCount(t *testing.T) {
	for n, want := range map[int]string{0: "matches", 1: "match", 2: "matches", 15: "matches"} {
		if got := matchWord(n); got != want {
			t.Errorf("matchWord(%d) = %q, want %q", n, got, want)
		}
	}
}
