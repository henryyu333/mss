package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/henryyu333/mss/internal/sources"
)

// `mss stats` prints redaction counts grouped by these keys, so a codex user
// read a heading — "codex-history" — that no other screen uses. #2234 fixed
// the same shape for ingest_health and left this counter alone (#2238).
func TestRedactionCountsAreFiledUnderTheStore(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	claudeRoot := filepath.Join(tmp, "claude")
	codexRoot := filepath.Join(tmp, "codex")
	t.Setenv("MSS_CLAUDE_ROOT", claudeRoot)
	t.Setenv("MSS_CODEX_ROOT", codexRoot)
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(tmp, "none.db"))
	if err := os.MkdirAll(codexRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	secret := "ghp_" + strings.Repeat("a", 36)
	stamp := time.Now().Add(-time.Hour).Unix()
	hist := fmt.Sprintf(`{"session_id":"h1","ts":%d,"text":"the token is %s"}`, stamp, secret)
	if err := os.WriteFile(filepath.Join(codexRoot, "history.jsonl"), []byte(hist+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	proj := filepath.Join(claudeRoot, "-work-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	line := fmt.Sprintf(`{"type":"user","sessionId":"s1","timestamp":%q,"cwd":"/work/app",`+
		`"message":{"role":"user","content":"the token is %s"}}`, at, secret)
	if err := os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(tmp, "index.db")
	if err := Ensure(dir, "", true, nil); err != nil {
		t.Fatal(err)
	}
	stats, err := Redactions(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The premise: both stores had something redacted, or the names prove
	// nothing.
	if len(stats.Rules) < 2 {
		t.Fatalf("only %d store reported a redaction: %v", len(stats.Rules), stats.Rules)
	}
	for name := range stats.Rules {
		if sources.HarnessForKind(name) != name {
			t.Errorf("redactions are filed under %q, which is a file kind; every screen says %q",
				name, sources.HarnessForKind(name))
		}
	}
	if _, ok := stats.Rules["codex"]; !ok {
		t.Errorf("nothing is filed under the name the rest of mss prints: %v", stats.Rules)
	}
}

// An index written before the fix holds keys by file kind, and a pass since
// then writes them by store — one manifest, both shapes. The reader folds, or
// the screen shows two stores where there is one (#2238).
func TestOldKindKeysFoldIntoTheStoreOnRead(t *testing.T) {
	dir := t.TempDir()
	m := Manifest{
		Version: version,
		RedactionRules: map[string]int{
			"codex-history:github-token": 2,
			"codex:github-token":         1,
			"codex:aws-key":              3,
			"claude:github-token":        4,
			"nothing-like-a-store:rule":  5,
		},
	}
	if err := writeManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	stats, err := Redactions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := stats.Rules["codex"]["github-token"]; got != 3 {
		t.Errorf("the two spellings of one store report %d github tokens, want 2+1", got)
	}
	if got := stats.Rules["codex"]["aws-key"]; got != 3 {
		t.Errorf("the other kind of the same store reports %d aws keys, want 3", got)
	}
	if _, ok := stats.Rules["codex-history"]; ok {
		t.Errorf("a file kind is still a heading: %v", stats.Rules)
	}
	if got := stats.Rules["claude"]["github-token"]; got != 4 {
		t.Errorf("a store whose kind carries its own name reports %d, want 4", got)
	}
	// A name mss does not know is left as it is rather than dropped: it came
	// from somewhere, and losing the count would be worse than an odd heading.
	if got := stats.Rules["nothing-like-a-store"]["rule"]; got != 5 {
		t.Errorf("an unknown name reports %d, want 5", got)
	}
}
