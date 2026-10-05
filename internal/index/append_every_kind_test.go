package index

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/henryyu333/mss/internal/search"
)

// The gate lets a kind append because it declares an offset parser; this is
// the other half of that claim — that reading from where the last pass stopped
// actually keeps what was already there and adds what arrived, once each. omp
// gained the path when the gate stopped naming harnesses (#2870), and a parser
// that resumes wrongly loses turns silently.
func TestEveryAppendableKindKeepsWhatItAlreadyHad(t *testing.T) {
	for _, c := range []struct {
		harness string
		env     string
		// rel is where the transcript sits under the harness root, and line is
		// one more record of the shape that file holds.
		rel     []string
		fixture string
		line    string
	}{
		{
			harness: "omp", env: "MSS_OMP_ROOT",
			rel:     []string{"-workspace-demo", "session.jsonl"},
			fixture: filepath.Join("..", "..", "fixtures", "registry", "omp", "session.jsonl"),
			line:    `{"type":"message","id":"m2","timestamp":"2026-07-17T09:01:00Z","message":{"role":"user","content":[{"type":"text","text":"appendneedle the second question"}]}}`,
		},
	} {
		t.Run(c.harness, func(t *testing.T) {
			tmp := t.TempDir()
			setHome(t, tmp)
			root := filepath.Join(tmp, c.harness)
			t.Setenv(c.env, root)
			path := filepath.Join(append([]string{root}, c.rel...)...)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			body, err := os.ReadFile(c.fixture)
			if err != nil {
				t.Fatalf("no fixture for %s: %v", c.harness, err)
			}
			if err := os.WriteFile(path, body, 0o644); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(tmp, "index.db")
			if err := Ensure(dir, "", true, nil); err != nil {
				t.Fatal(err)
			}
			was := sessionsOf(t, dir, c.harness)
			if len(was) == 0 {
				t.Fatalf("%s: the fixture indexed nothing, so an append proves nothing", c.harness)
			}

			// One more record, the way the harness writes them.
			if err := os.WriteFile(path, append(body, []byte(c.line+"\n")...), 0o644); err != nil {
				t.Fatal(err)
			}
			var progress bytes.Buffer
			if err := Ensure(dir, "", false, &progress); err != nil {
				t.Fatal(err)
			}
			// The append path, not a re-read that happens to give the same
			// answer: a whole-file pass is correct too, and the point of the
			// gate is that it does not happen (#2870).
			if !strings.Contains(progress.String(), "updated 1 file") {
				t.Errorf("growth did not take the append path:\n%s", progress.String())
			}
			// The same sessions, still carrying what the header told the
			// parser. An offset parser that resumes past the header loses the
			// session's identity rather than its bytes: the turn lands under a
			// key of its own and the session it belongs to never grows.
			now := sessionsOf(t, dir, c.harness)
			for key, before := range was {
				after, ok := now[key]
				if !ok {
					t.Errorf("%s is gone after the append", key)
					continue
				}
				if after.Counted < before.Counted {
					t.Errorf("%s lost turns: %d, was %d", key, after.Counted, before.Counted)
				}
				if after.Project != before.Project {
					t.Errorf("%s changed project: %q, was %q", key, after.Project, before.Project)
				}
				if after.Title != before.Title {
					t.Errorf("%s changed title: %q, was %q", key, after.Title, before.Title)
				}
			}
			if len(now) != len(was) {
				t.Errorf("the append made %d sessions out of %d", len(now), len(was))
			}
			ss, err := Search(dir, search.Options{Query: "appendneedle", All: true})
			if err != nil {
				t.Fatal(err)
			}
			if len(ss) != 1 {
				t.Errorf("the appended turn is in %d sessions, want 1", len(ss))
			}
			for _, s := range ss {
				n := 0
				for _, m := range s.Messages {
					if strings.Contains(m.Text, "appendneedle") {
						n++
					}
				}
				if n != 1 {
					t.Errorf("the appended turn is in the index %d times", n)
				}
			}
		})
	}
}

// sessionsOf is what a harness has in the store, by key.
func sessionsOf(t *testing.T, dir, harness string) map[string]SessionMeta {
	t.Helper()
	metas, err := AllMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]SessionMeta{}
	for _, m := range metas {
		if m.Harness == harness {
			out[m.Harness+":"+m.ID] = m
		}
	}
	return out
}
