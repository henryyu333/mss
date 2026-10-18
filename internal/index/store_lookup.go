package index

import (
	"path/filepath"
	"sort"

	"github.com/henryyu333/mss/internal/sources"
)

// storeLookup reuses a registry for one operation. It is not cached globally:
// store selection and root overrides must take effect on the next call.
type storeLookup struct {
	selected []sources.Harness
	stores   map[string]string
}

func newStoreLookup() *storeLookup {
	return &storeLookup{selected: sources.Registry()}
}

func (l *storeLookup) matchPath(p string) string {
	for _, h := range l.selected {
		for _, k := range h.Kinds {
			if k.Match(p) {
				return k.Name
			}
		}
	}
	return ""
}

// Extension-less diagnostic paths may name unreadable directories. Probe
// possible transcript names exactly as the discovery classifiers do.
func (l *storeLookup) pathKind(p string) string {
	if kind := l.matchPath(p); kind != "" {
		return kind
	}
	if filepath.Ext(p) != "" {
		return ""
	}
	for _, name := range []string{"probe.jsonl", "probe.json", "probe.md"} {
		if kind := l.matchPath(filepath.Join(p, name)); kind != "" {
			return kind
		}
	}
	return ""
}

// Fold kind names lazily so path-only callers do not construct another registry.
func (l *storeLookup) storeFor(kind string) string {
	if l.stores == nil {
		l.stores = map[string]string{}
		for _, h := range sources.AllHarnesses() {
			if _, ok := l.stores[h.Name]; !ok {
				l.stores[h.Name] = h.Name
			}
			for _, k := range h.Kinds {
				if _, ok := l.stores[k.Name]; !ok {
					l.stores[k.Name] = h.Name
				}
			}
		}
	}
	return l.stores[kind]
}

func (l *storeLookup) storeForPath(p string) string {
	return l.storeFor(l.pathKind(p))
}

// healthFromFiles sums the per-file counts per harness.
//
// Paths in order, because LastError is one of them: taking whichever the map
// handed over last gave the same index a different error on every run, so a
// script diffing `doctor --json` saw a change where nothing changed (#2245).
// The first failing path is the one quoted; ingest_files holds them all.
func (l *storeLookup) healthFromFiles(files map[string]FileIngest) map[string]HarnessIngest {
	out := map[string]HarnessIngest{}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		e := files[p]
		// The store, not the file kind: the run narrates "cline" and doctor
		// filed the same fact under "cline-sdk", while the documentation calls
		// this key a harness. Five stores have kinds by another name, so for
		// those a script keyed on the documented name found nothing (#2234).
		h := l.storeForPath(p)
		if h == "" {
			continue
		}
		cur := out[h]
		cur.MalformedLines += e.Malformed
		cur.ClippedMessages += e.Clipped
		if e.Error != "" {
			cur.FailedFiles++
			if cur.LastError == "" {
				cur.LastError = e.Error
			}
		}
		out[h] = cur
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
