package index

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/henryyu333/mss/internal/query"
	"github.com/henryyu333/mss/internal/sources"
)

// zstdMinFrame is one valid, empty zstd frame (RFC 8878): the magic, a
// single-segment frame header with content size 0, and a last raw block of
// length 0. dsh writes one frame per line, so a file of these stands in for
// a log whose frames this machine cannot decode.
var zstdMinFrame = []byte{0x28, 0xB5, 0x2F, 0xFD, 0x20, 0x00, 0x01, 0x00, 0x00}

// The run narrates "N lines skipped, mss could not read them" on stderr, and
// the JSON answer has to carry the same number or an agent reading it takes
// the store for the only gap. The count used to die in the manifest fold: the
// file is never indexed, and the fold's cleanup dropped its row — but the
// second pass is where it bit, because the incremental paths skip a file
// without recording it as a failed file the way the full load does.
func TestSearchCoverageKeepsSkippedLinesOfAnUnreadableStore(t *testing.T) {
	if sources.ZstdAvailable() {
		t.Skip("zstd installed; the frames decode and the store reads")
	}
	tmp := hermeticIndexEnv(t)
	dsh := filepath.Join(tmp, "dsh")
	t.Setenv("DSH_HOME", dsh)
	// Files() walks MSS_DEEPSEEK_ROOT while SkipReason follows DSH_HOME;
	// point both at the same store.
	t.Setenv("MSS_DEEPSEEK_ROOT", filepath.Join(dsh, "sessions"))
	path := filepath.Join(dsh, "sessions", "session-aaaa", "session.v4.jsonl.zstd")
	write(t, path, string(bytes.Repeat(zstdMinFrame, 5)))
	dir := filepath.Join(tmp, "idx")
	if err := EnsureForSearch(dir, query.Options{All: true}, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	// The store grows: the incremental pass re-reads the file whole, fails
	// on every frame again, and the count has to survive that pass too.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(bytes.Repeat(zstdMinFrame, 3)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := EnsureForSearch(dir, query.Options{All: true}, false, io.Discard); err != nil {
		t.Fatal(err)
	}
	cov := SearchCoverage(dir)
	sk, ok := cov.Skipped["deepseek"]
	if !ok || sk.Records == 0 {
		t.Fatalf("the skipped lines never reached coverage: %+v", cov)
	}
	// The file was re-read whole, so the count is that pass's — eight frames,
	// not the two passes summed.
	if sk.Records != 8 {
		t.Fatalf("skipped lines counted %d, want the 8 frames of the file", sk.Records)
	}
	if cov.Complete {
		t.Fatal("coverage claimed complete beside skipped lines")
	}
}
