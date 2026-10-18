package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveObsoleteSidecarsKeepsTheActiveOnes(t *testing.T) {
	dir := t.TempDir()
	obsolete := []string{"fixes.gob", "commandfails.gob", "commandfails-state.gob", "sessionfacts.gob"}
	for _, name := range obsolete {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{commandsFile, cooccurFile} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("active"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	RemoveObsoleteSidecars(dir)

	for _, name := range obsolete {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s survived cleanup: %v", name, err)
		}
	}
	for _, name := range []string{commandsFile, cooccurFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("cleanup removed active sidecar %s: %v", name, err)
		}
	}
}
