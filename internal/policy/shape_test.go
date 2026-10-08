package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func policyAt(t *testing.T, body string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MSS_POLICY_FILE", p)
}

// Diagnose caught a typo inside a structure that was otherwise right, and said
// nothing about a file whose whole shape is wrong — which is the one someone
// writes from memory or from another tool's config. It parses, denies nothing,
// and every surface stays silent (#2504).
func TestDiagnoseNamesAKeyMssDoesNotConsult(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "a shape from somewhere else",
			body: `{"rules":[{"project":"work","search":"deny"}]}`,
			want: "rules",
		},
		{
			name: "the right idea under the wrong name",
			body: `{"activation":{"search":{"local":false}}}`,
			want: "activation",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policyAt(t, tc.body)
			exists, unknown, err := Diagnose()
			if err != nil || !exists {
				t.Fatalf("exists=%v err=%v", exists, err)
			}
			if !strings.Contains(strings.Join(unknown, " "), tc.want) {
				t.Errorf("a file that denies nothing is reported as %v; want it to name %q", unknown, tc.want)
			}
		})
	}
}

// The keys mss does consult stay quiet.
func TestDiagnoseIsQuietOnAFileItUnderstands(t *testing.T) {
	policyAt(t, `{"activations":{"search":{"local":false}},"ignore":["/tmp/scratch"]}`)
	_, unknown, err := Diagnose()
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown) != 0 {
		t.Errorf("a policy mss reads in full is reported as unknown: %v", unknown)
	}
}

func TestLoadDiscardsPartialPolicyOnTypeErrors(t *testing.T) {
	for name, body := range map[string]string{
		"invalid origin value": `{"activations":{"search":{"imported":false,"local":"deny"}},"ignore":["/private"]}`,
		"invalid ignore value": `{"activations":{"search":{"local":false}},"ignore":["/private",42]}`,
		"invalid activation":   `{"activations":{"search":{"local":false},"other":false},"ignore":["/private"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			policyAt(t, body)
			p := Load()
			if p.Activations != nil || p.Ignore != nil {
				t.Fatalf("a type error left a partially applied policy: %#v", p)
			}
			for _, project := range []string{"work", "imported:group/work"} {
				if !p.Allows(ActivationSearch, project) {
					t.Errorf("malformed policy withheld %q instead of using the permissive default", project)
				}
			}
			if exists, _, err := Diagnose(); !exists || err == nil {
				t.Fatalf("type error was not diagnosed: exists=%v err=%v", exists, err)
			}
		})
	}
}

func TestDiagnoseAcceptsEffectiveWildcardRules(t *testing.T) {
	policyAt(t, `{"activations":{"search":{"*":false,"local":true}}}`)
	if exists, unknown, err := Diagnose(); !exists || err != nil || len(unknown) != 0 {
		t.Fatalf("valid wildcard policy diagnosed as exists=%v unknown=%v err=%v", exists, unknown, err)
	}
	p := Load()
	if !p.Allows(ActivationSearch, "work") {
		t.Fatal("specific local allowance did not override the wildcard")
	}
	if p.Allows(ActivationSearch, "imported:group/work") {
		t.Fatal("wildcard denial did not withhold an otherwise unmatched origin")
	}
}

func TestLoadKeepsValidRulesAlongsideUnknownKeys(t *testing.T) {
	policyAt(t, `{"activations":{"search":{"local":false}},"rules":[{"origin":"imported","allow":false}]}`)
	p := Load()
	if p.Allows(ActivationSearch, "work") {
		t.Fatal("unknown top-level key disabled a valid local denial")
	}
	if !p.Allows(ActivationSearch, "imported:group/work") {
		t.Fatal("unknown top-level rule affected recall")
	}
	if exists, unknown, err := Diagnose(); !exists || err != nil || len(unknown) != 1 || unknown[0] != "rules" {
		t.Fatalf("unknown top-level rule was not diagnosed: exists=%v unknown=%v err=%v", exists, unknown, err)
	}
}
