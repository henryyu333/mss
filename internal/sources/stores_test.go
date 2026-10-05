package sources

import (
	"strings"
	"testing"
)

func names(hs []Harness) []string {
	out := make([]string, 0, len(hs))
	for _, h := range hs {
		out = append(out, h.Name)
	}
	return out
}

func TestMssStoresNarrowsTheRegistry(t *testing.T) {
	full := len(Registry())
	if full != 8 {
		t.Fatalf("the registry has %d harnesses; want 8", full)
	}

	t.Setenv(StoresEnv, "claude,codex")
	got := names(Registry())
	if len(got) != 2 || got[0] != "claude" || got[1] != "codex" {
		t.Fatalf("MSS_STORES=claude,codex left %v", got)
	}
	// Load order is the registry's own, not the order they were written in the
	// variable: the index walks stores in that order and the path matches rely
	// on it staying deterministic.
	t.Setenv(StoresEnv, "codex, claude")
	if got := names(Registry()); len(got) != 2 || got[0] != "claude" {
		t.Fatalf("order came from the variable, not the registry: %v", got)
	}

	// What mss can read is not what it is reading now. The count in the
	// documentation, the set --harness accepts and the names it suggests all
	// come from the full list; narrowing one run must not shrink any of them.
	if n := len(AllHarnesses()); n != full {
		t.Errorf("AllHarnesses is %d under a narrowed selection; it should stay %d", n, full)
	}
	if !IsKnownHarness("grok") {
		t.Error("--harness grok is rejected while MSS_STORES silences grok; a known store stays a known name")
	}
	if len(HarnessNames()) != full {
		t.Error("HarnessNames shrank with the selection")
	}
}

func TestMssStoresUnsetReadsEverything(t *testing.T) {
	t.Setenv(StoresEnv, "")
	if len(Registry()) != len(AllHarnesses()) {
		t.Error("an empty MSS_STORES narrowed the registry; empty means the default, which is everything")
	}
	if StoreSilenced("claude") {
		t.Error("a store is silenced with no selection in force")
	}
	if _, ok := StoresSelected(); ok {
		t.Error("an empty variable reports a selection in force")
	}
	// A variable holding only separators is the same mistake as an empty one,
	// and reading it as "no stores" would index nothing at all.
	t.Setenv(StoresEnv, " , ")
	if len(Registry()) != len(AllHarnesses()) {
		t.Error("MSS_STORES=\" , \" silenced every store")
	}
}

func TestMssStoresRefusesANameThatIsNotAStore(t *testing.T) {
	t.Setenv(StoresEnv, "claude,claud")
	err := StoresSelectionError()
	if err == nil {
		t.Fatal("a typo in MSS_STORES is accepted; it silences every store and the run looks like a machine with no history")
	}
	if !strings.Contains(err.Error(), "claud,") && !strings.Contains(err.Error(), "claud ") {
		t.Errorf("the error does not name the typo: %v", err)
	}
	if !strings.Contains(err.Error(), "grok") {
		t.Errorf("the error does not name the stores: %v", err)
	}

	t.Setenv(StoresEnv, "claude,grok")
	if err := StoresSelectionError(); err != nil {
		t.Errorf("a correct selection is refused: %v", err)
	}
}

func TestMssStoresReportsWhatItSelected(t *testing.T) {
	t.Setenv(StoresEnv, "grok,claude")
	got, ok := StoresSelected()
	if !ok {
		t.Fatal("a selection is in force and StoresSelected says otherwise")
	}
	if strings.Join(got, ",") != "claude,grok" {
		t.Errorf("StoresSelected returned %v", got)
	}
}
