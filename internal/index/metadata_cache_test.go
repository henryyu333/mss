package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeManifestFiles commits a manifest without running an ingest pass, so a
// test can set SourcesReadAt and the ingest reports directly instead of
// folding them through mergeIngestDiag. sessions.gob goes first, as it does
// in writeManifest.
func writeManifestFiles(t *testing.T, dir string, m Manifest) {
	t.Helper()
	if err := writeGobAtomic(filepath.Join(dir, "sessions.gob"), m.Sessions); err != nil {
		t.Fatal(err)
	}
	if err := writeManifestOnly(dir, m); err != nil {
		t.Fatal(err)
	}
}

// The reports read from a shared manifest, so what they hand out has to be a
// copy: a caller writing into a row must not change what the next caller —
// or the cache — is served.
func TestIngestReportMutationCannotContaminateTheCache(t *testing.T) {
	dir := t.TempDir()
	file := "/store/a.jsonl"
	writeManifestFiles(t, dir, Manifest{
		Version:      version,
		Format:       onDiskFormat,
		Files:        map[string]FileState{file: {}},
		Sessions:     map[string]SessionMeta{},
		IngestHealth: map[string]HarnessIngest{"claude": {MalformedLines: 2}},
		IngestFiles: map[string]FileIngest{file: {
			Malformed:       2,
			ClippedSessions: map[string]int{"s1": 1},
			ClippedMsgIdx:   map[string][]int{"s1": {0, 1}},
			Unusable:        map[string]string{"r1": "gone"},
		}},
	})

	// Prime the cache, then vandalise both reports the way a caller owning
	// its own map may: a row's counters, and the nested maps and slices a
	// shallow copy would still share.
	health := IngestHealth(dir)
	health["claude"] = HarnessIngest{MalformedLines: 99}
	health["planted"] = HarnessIngest{FailedFiles: 1}
	files := IngestFilesReport(dir)
	row := files[file]
	row.Malformed = 42
	row.ClippedSessions["s1"] = 7
	row.ClippedMsgIdx["s1"][0] = 9000
	row.Unusable["r1"] = "mutated"
	row.Unusable["planted"] = "x"
	files["/store/planted.jsonl"] = FileIngest{}
	files[file] = row

	if got := IngestHealth(dir); len(got) != 1 || got["claude"].MalformedLines != 2 {
		t.Errorf("the caller's writes reached the cached health: %v", got)
	}
	got := IngestFilesReport(dir)
	if len(got) != 1 {
		t.Fatalf("the caller's planted row reached the cache: %v", got)
	}
	e := got[file]
	if e.Malformed != 2 {
		t.Errorf("row says %d unreadable lines, the manifest recorded 2", e.Malformed)
	}
	if e.ClippedSessions["s1"] != 1 || len(e.ClippedSessions) != 1 {
		t.Errorf("the caller's writes reached the cached clip counts: %v", e.ClippedSessions)
	}
	if len(e.ClippedMsgIdx["s1"]) != 2 || e.ClippedMsgIdx["s1"][0] != 0 {
		t.Errorf("the caller's writes reached the cached clip positions: %v", e.ClippedMsgIdx)
	}
	if len(e.Unusable) != 1 || e.Unusable["r1"] != "gone" {
		t.Errorf("the caller's writes reached the cached unusable rows: %v", e.Unusable)
	}
}

// SourcesReadAt is additive: a manifest written before it existed must keep
// answering from the clocks it has — BuiltAt, then manifest.gob's mtime —
// while an import-built index with no local files still answers "never".
func TestManifestSourcesReadAtKeepsItsLegacyClocks(t *testing.T) {
	dir := t.TempDir()
	built := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	// Import shape: BuiltAt is the peer's clock, nothing local was read.
	writeManifestFiles(t, dir, Manifest{
		Version:  version,
		Format:   onDiskFormat,
		BuiltAt:  built,
		Files:    map[string]FileState{},
		Sessions: map[string]SessionMeta{},
	})
	if got := ManifestSourcesReadAt(dir); !got.IsZero() {
		t.Errorf("an import-built index claims stores were read at %v, want never", got)
	}

	// A store written before SourcesReadAt but carrying BuiltAt falls back
	// to it — the decode the call already did, not a second read.
	writeManifestFiles(t, dir, Manifest{
		Version:  version,
		Format:   onDiskFormat,
		BuiltAt:  built,
		Files:    map[string]FileState{"/store/a.jsonl": {}},
		Sessions: map[string]SessionMeta{},
	})
	if got := ManifestSourcesReadAt(dir); !got.Equal(built) {
		t.Errorf("a store without SourcesReadAt read as %v, want its BuiltAt %v", got, built)
	}

	// Older still: no BuiltAt either, so the answer is manifest.gob's mtime,
	// the clock every manifest has.
	writeManifestFiles(t, dir, Manifest{
		Version:  version,
		Format:   onDiskFormat,
		Files:    map[string]FileState{"/store/a.jsonl": {}},
		Sessions: map[string]SessionMeta{},
	})
	fi, err := os.Stat(filepath.Join(dir, "manifest.gob"))
	if err != nil {
		t.Fatal(err)
	}
	if got := ManifestSourcesReadAt(dir); !got.Equal(fi.ModTime()) {
		t.Errorf("a store without either clock read as %v, want manifest.gob's mtime %v", got, fi.ModTime())
	}
	if got := ManifestBuiltAt(dir); !got.Equal(fi.ModTime()) {
		t.Errorf("BuiltAt-less BuiltAt read as %v, want manifest.gob's mtime %v", got, fi.ModTime())
	}
}

// Answering from the cache must not make a rewrite invisible: the fingerprint
// checks the manifest's bytes, so a rewrite that keeps the size and lands on
// the same timestamp — a different process, a coarse fs clock, a vandal —
// still reads through. Written in place so even the inode matches.
func TestManifestAccessorsSeeASameSizeRewrite(t *testing.T) {
	dir := t.TempDir()
	early := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	later := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	m := Manifest{
		Version:       version,
		Format:        onDiskFormat,
		SourcesReadAt: early,
		Files:         map[string]FileState{"/store/a.jsonl": {}},
		Sessions:      map[string]SessionMeta{},
	}
	writeManifestFiles(t, dir, m)
	if got := ManifestSourcesReadAt(dir); !got.Equal(early) {
		t.Fatalf("the first manifest records %v, the answer was %v", early, got)
	}
	path := filepath.Join(dir, "manifest.gob")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	// Rewrite in place, same inode, then put the clock back where it was.
	// writeGob encodes a manifestCore directly — no writer-side invalidate,
	// so only the fingerprint can see this.
	m.SourcesReadAt = later
	core := manifestCore{Version: m.Version, Format: m.Format, Files: m.Files, BuiltAt: m.BuiltAt, SourcesReadAt: m.SourcesReadAt}
	if err := writeGob(path, core); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	if again, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if again.Size() != fi.Size() {
		t.Skipf("the two manifests differ in size (%d against %d) — the collision this test needs is gone",
			again.Size(), fi.Size())
	}

	if got := ManifestSourcesReadAt(dir); !got.Equal(later) {
		t.Errorf("after a same-size rewrite the index says stores were read at %v, want %v — the cache answered from the manifest before it", got, later)
	}
}

// Same for damage: bytes that no longer decode are a different CRC, so the
// cached manifest must not stand in for them. The clock accessors keep their
// corrupt-store answer — manifest.gob's mtime — and the reports go quiet.
func TestManifestAccessorsSeeACorruptManifest(t *testing.T) {
	dir := t.TempDir()
	m := Manifest{
		Version:       version,
		Format:        onDiskFormat,
		SourcesReadAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Files:         map[string]FileState{"/store/a.jsonl": {}},
		Sessions:      map[string]SessionMeta{},
		IngestHealth:  map[string]HarnessIngest{"claude": {MalformedLines: 2}},
	}
	writeManifestFiles(t, dir, m)
	if got := IngestHealth(dir); got["claude"].MalformedLines != 2 {
		t.Fatalf("the manifest recorded 2 unreadable lines, the answer was %v", got)
	}
	path := filepath.Join(dir, "manifest.gob")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Same size, same inode, same stamp once it is put back: only the bytes
	// differ, which is exactly what the CRC is there for.
	if err := os.WriteFile(path, make([]byte, fi.Size()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}

	if got := IngestHealth(dir); got != nil {
		t.Errorf("a corrupt manifest still reports ingest health %v — the cache answered from the manifest before it", got)
	}
	if got := IngestFilesReport(dir); got != nil {
		t.Errorf("a corrupt manifest still reports ingest files %v — the cache answered from the manifest before it", got)
	}
	if got := ManifestBuiltAt(dir); !got.Equal(fi.ModTime()) {
		t.Errorf("a corrupt manifest's BuiltAt read as %v, want the mtime fallback %v", got, fi.ModTime())
	}
	if got := ManifestSourcesReadAt(dir); !got.Equal(fi.ModTime()) {
		t.Errorf("a corrupt manifest's SourcesReadAt read as %v, want the mtime fallback %v", got, fi.ModTime())
	}
}
