package sources

import (
	"os"
	"path/filepath"
)

// sidecarStat fingerprints the files beside a transcript that its reader
// opens for the title, workspace or clock: their sizes and mtimes summed, a
// missing one counted as nothing. The file state holds it, so a change to any
// of them re-reads the session (#4446).
func sidecarStat(paths ...string) (size, stamp int64) {
	for _, p := range paths {
		if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() {
			size += fi.Size()
			stamp += fi.ModTime().UnixNano()
		}
	}
	return size, stamp
}

func besideSidecar(name string) func(string) (int64, int64) {
	return func(p string) (int64, int64) { return sidecarStat(filepath.Join(filepath.Dir(p), name)) }
}
