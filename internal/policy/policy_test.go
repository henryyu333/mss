package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultsAllowEverything(t *testing.T) {
	t.Setenv("MSS_POLICY_FILE", filepath.Join(t.TempDir(), "missing.json"))
	p := Load()
	for _, act := range []string{ActivationSearch} {
		for _, proj := range []string{"mss", "imported:mini/mss"} {
			if !p.Allows(act, proj) {
				t.Fatalf("default policy must allow %s/%s", act, proj)
			}
		}
		if got := p.Describe(act); got != "local+imported" {
			t.Fatalf("Describe(%s) = %q", act, got)
		}
	}
}

func TestOriginClassification(t *testing.T) {
	cases := map[string]string{
		"mss":                    "local",
		"imported:mini/mss":      "imported:mini",
		"imported:mini":          "imported",
		"imported:work/api/auth": "imported:work",
	}
	for proj, want := range cases {
		if got := Origin(proj); got != want {
			t.Fatalf("Origin(%q) = %q, want %q", proj, got, want)
		}
	}
}

func TestFileDeniesImportedOnSearch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	t.Setenv("MSS_POLICY_FILE", path)
	if err := os.WriteFile(path, []byte(`{"activations":{"search":{"imported":false}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := Load()
	if p.Allows(ActivationSearch, "imported:mini/mss") {
		t.Fatal("search must deny imported")
	}
	if !p.Allows(ActivationSearch, "mss") {
		t.Fatal("local sessions must stay allowed")
	}
	if got := p.Describe(ActivationSearch); got != "local-only" {
		t.Fatalf("Describe(search) = %q, want local-only", got)
	}
}

func TestPeerSpecificRuleWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	t.Setenv("MSS_POLICY_FILE", path)
	if err := os.WriteFile(path, []byte(`{"activations":{"search":{"imported":true,"imported:untrusted":false}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := Load()
	if p.Allows(ActivationSearch, "imported:untrusted/box") {
		t.Fatal("peer rule must deny untrusted")
	}
	if !p.Allows(ActivationSearch, "imported:mini/mss") {
		t.Fatal("other peers stay allowed")
	}
	if got := p.Describe(ActivationSearch); got != "deny imported:untrusted" {
		t.Fatalf("Describe = %q", got)
	}
}

func TestMalformedFileFallsBackToDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	t.Setenv("MSS_POLICY_FILE", path)
	if err := os.WriteFile(path, []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !Load().Allows(ActivationSearch, "imported:mini/x") {
		t.Fatal("malformed policy must not lock recall out")
	}
}

func TestFilterDropsBlocked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	t.Setenv("MSS_POLICY_FILE", path)
	if err := os.WriteFile(path, []byte(`{"activations":{"search":{"imported":false}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	items := []string{"mss", "imported:mini/mss", "other"}
	got := Filter(Load(), ActivationSearch, items, func(s string) string { return s })
	if len(got) != 2 || got[0] != "mss" || got[1] != "other" {
		t.Fatalf("Filter = %v", got)
	}
}

// Load falls back to the permissive default on any error, so a malformed file
// changed nothing and said nothing (#661). Diagnose is what doctor reads.
func TestDiagnose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	t.Setenv("MSS_POLICY_FILE", path)

	if exists, unknown, err := Diagnose(); exists || err != nil || unknown != nil {
		t.Fatalf("no file: exists=%v err=%v unknown=%v", exists, err, unknown)
	}
	if err := os.WriteFile(path, []byte("{ oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if exists, _, err := Diagnose(); !exists || err == nil {
		t.Fatalf("malformed: exists=%v err=%v", exists, err)
	}
	// Rules that name something mss never consults are silently doing nothing,
	// which reads exactly like a rule that works.
	body := `{"activations":{"search":{"nosuchorigin":false,"imported:peer1":false,"local":true},"nosuch":{"local":false}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	exists, unknown, err := Diagnose()
	if !exists || err != nil {
		t.Fatalf("valid: exists=%v err=%v", exists, err)
	}
	want := []string{"activation nosuch", "search.nosuchorigin"}
	if strings.Join(unknown, "|") != strings.Join(want, "|") {
		t.Errorf("unknown = %v, want %v", unknown, want)
	}
}

// A path that exists but cannot be read as a file — the config directory left
// where the file should be — is neither "no policy" nor "malformed policy".
func TestDiagnoseUnreadablePath(t *testing.T) {
	dir := t.TempDir()
	asDir := filepath.Join(dir, "policy.json")
	if err := os.MkdirAll(asDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MSS_POLICY_FILE", asDir)
	exists, _, err := Diagnose()
	if !exists || err == nil {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
}

// A key mss never consults restricts nothing, and naming it in the summary put
// two claims on one screen: "deny tmp/other" here and "this rule does nothing"
// from Diagnose three lines down (#941).
func TestDescribeSkipsKeysMssNeverConsults(t *testing.T) {
	p := Policy{Activations: map[string]map[string]bool{
		ActivationSearch: {"local": true, "tmp/other": false},
	}}
	if got := p.Describe(ActivationSearch); got != "local+imported" {
		t.Errorf("Describe with a project-shaped key = %q, want the unrestricted summary", got)
	}
	// Real rules still show, including a single machine.
	p.Activations[ActivationSearch]["imported:laptop"] = false
	if got := p.Describe(ActivationSearch); got != "deny imported:laptop" {
		t.Errorf("Describe dropped a rule it does consult: %q", got)
	}
	p.Activations[ActivationSearch]["imported"] = false
	if got := p.Describe(ActivationSearch); got != "local-only" {
		t.Errorf("Describe stopped naming local-only: %q", got)
	}
}
