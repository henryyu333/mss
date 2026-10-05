package index

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// countingProgress records what the reading stage promised and what it
// delivered.
type countingProgress struct {
	mu       sync.Mutex
	phase    string
	total    map[string]int
	advanced map[string]int
}

func (c *countingProgress) Phase(name string, total int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.phase = name
	c.total[name] = total
}

func (c *countingProgress) Advance(units int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.advanced[c.phase] += units
}

func (c *countingProgress) Harness(string, int, int) {}

// The bar is sized in files and moved in stores. It counted a file under its
// kind — a name the parse loop never advances by — so a corpus of sessions
// filled the total and left the bar where it was (#2242).
func TestEveryFileCountedInTheBarAlsoMovesIt(t *testing.T) {
	tmp := t.TempDir()
	setHome(t, tmp)
	t.Setenv("MSS_CLAUDE_ROOT", filepath.Join(tmp, "claude"))
	t.Setenv("MSS_CODEX_ROOT", filepath.Join(tmp, "codex"))
	t.Setenv("MSS_OPENCODE_DB", filepath.Join(tmp, "none.db"))
	pi := filepath.Join(tmp, "pi")
	if err := os.MkdirAll(pi, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MSS_PI_ROOT", pi)

	for i := 0; i < 4; i++ {
		body := `{"type":"session","version":3,"id":"p` + fmt.Sprint(i) + `","timestamp":"2026-01-02T03:04:05Z","cwd":"/work/app"}` + "\n" +
			`{"type":"message","id":"m1","timestamp":"2026-01-02T03:04:06Z","message":{"role":"user","content":[{"type":"text","text":"hello there"}]}}` + "\n"
		if err := os.WriteFile(filepath.Join(pi, fmt.Sprintf("s%d.jsonl", i)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	proj := filepath.Join(tmp, "claude", "-work-app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	line := fmt.Sprintf(`{"type":"user","sessionId":"s1","timestamp":%q,"cwd":"/work/app",`+
		`"message":{"role":"user","content":"hello there"}}`, at)
	if err := os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &countingProgress{total: map[string]int{}, advanced: map[string]int{}}
	SetProgress(p)
	defer SetProgress(nil)
	if err := Ensure(filepath.Join(tmp, "index.db"), "", true, nil); err != nil {
		t.Fatal(err)
	}

	p.mu.Lock()
	total, advanced := p.total["reading sessions"], p.advanced["reading sessions"]
	p.mu.Unlock()
	// The premise: every file on disk is in the total, or the bar being short
	// proves nothing.
	if total != 5 {
		t.Fatalf("the reading stage was sized at %d for five files on disk", total)
	}
	if advanced != total {
		t.Errorf("the reading stage promised %d files and moved the bar by %d", total, advanced)
	}
}
