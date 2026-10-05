package sources

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Each sidecar is fingerprinted from the file the reader opens beside the
// transcript. A missing one counts as nothing, a directory in its place too,
// an unchanged one keeps the fingerprint and a rewrite moves it (#4446).
func TestSidecarsFingerprintTheFileBesideTheTranscript(t *testing.T) {
	root := t.TempDir()
	for _, c := range []struct {
		name       string
		transcript string
		sidecar    string
		stat       func(string) (int64, int64)
	}{
		{"grok summary", filepath.Join(root, "grok", "s1", "updates.jsonl"), filepath.Join(root, "grok", "s1", "summary.json"), besideSidecar("summary.json")},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := os.MkdirAll(filepath.Dir(c.sidecar), 0o755); err != nil {
				t.Fatal(err)
			}
			if size, stamp := c.stat(c.transcript); size != 0 || stamp != 0 {
				t.Fatalf("missing sidecar = %d/%d, want nothing", size, stamp)
			}
			if err := os.Mkdir(c.sidecar, 0o755); err != nil {
				t.Fatal(err)
			}
			if size, stamp := c.stat(c.transcript); size != 0 || stamp != 0 {
				t.Fatalf("a directory in the sidecar's place = %d/%d, want nothing", size, stamp)
			}
			if err := os.Remove(c.sidecar); err != nil {
				t.Fatal(err)
			}
			at := time.Now().Add(-time.Hour)
			write := func(body string) {
				t.Helper()
				at = at.Add(time.Second)
				if err := os.WriteFile(c.sidecar, []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(c.sidecar, at, at); err != nil {
					t.Fatal(err)
				}
			}
			write(`{"title":"fix the retry loop"}`)
			size, stamp := c.stat(c.transcript)
			if size == 0 || stamp == 0 {
				t.Fatalf("sidecar = %d/%d, want its size and time", size, stamp)
			}
			if s2, st2 := c.stat(c.transcript); s2 != size || st2 != stamp {
				t.Error("an unchanged sidecar moved the fingerprint")
			}
			write(`{"title":"cap the retry at three"}`)
			if s2, st2 := c.stat(c.transcript); s2 == size && st2 == stamp {
				t.Error("a renamed session left the fingerprint as it was")
			}
		})
	}
}
