package index

import (
	"fmt"
	"hash/crc32"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

// Read-only retrieval asks for the manifest several times per command;
// decoding the manifest and a thousand session metas on every call is pure
// waste, so the decoded Manifest is cached per process.
//
// The cache is validated by file identity and by content. mtime+size alone is
// not enough: a rewrite that keeps the size and lands inside one tick of the
// filesystem's timestamp resolution (common on overlayfs and tmpfs, where the
// kernel stamps files from a coarse clock) leaves the pair identical, and the
// cache then answers from the manifest before it — a corrupted manifest reads
// as whole. So a lookup checks three things before trusting the decoded copy:
//
//   - the same file (os.SameFile): every write goes through temp file + rename,
//     so any committed rewrite, of either manifest.gob or the sessions.gob
//     written just before it, arrives as a new inode;
//   - the same mtime and size, as before;
//   - the same CRC-32C of manifest.gob's bytes, which catches a same-size,
//     same-tick rewrite in place. CRC-32C is hardware-accelerated on amd64
//     and arm64, so hashing even a multi-megabyte manifest costs well under a
//     millisecond — far less than the gob decode it saves.
//
// A writer in this process still drops the entry itself (writeManifest).
//
// Contract: the cached Manifest is shared — read-only paths must not mutate
// it. Ingestion keeps using readManifest directly.
var manifestCache struct {
	mu  sync.Mutex
	dir string
	fi  os.FileInfo
	sum uint32
	m   Manifest
	ok  bool
}

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// manifestFingerprint opens manifest.gob once and returns its FileInfo and
// the CRC-32C of its contents.
func manifestFingerprint(dir string) (os.FileInfo, uint32, error) {
	f, err := os.Open(filepath.Join(dir, "manifest.gob"))
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	h := crc32.New(castagnoli)
	if _, err := io.Copy(h, f); err != nil {
		return nil, 0, err
	}
	return fi, h.Sum32(), nil
}

func sameManifestFile(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) &&
		a.ModTime().Equal(b.ModTime()) && a.Size() == b.Size()
}

func readManifestCached(dir string) (Manifest, error) {
	// A failed read falls through to readManifest, which waits an in-flight
	// swap out itself, so a wait added here cannot be made to fail.
	fi, sum, err := manifestFingerprint(dir)
	if err != nil {
		return readManifest(dir)
	}
	manifestCache.mu.Lock()
	if manifestCache.ok && manifestCache.dir == dir &&
		manifestCache.sum == sum && sameManifestFile(manifestCache.fi, fi) {
		m := manifestCache.m
		manifestCache.mu.Unlock()
		return m, nil
	}
	manifestCache.mu.Unlock()
	// readManifest opens the file again. If a swap lands in between, the
	// stored fingerprint belongs to the older file, so the next lookup
	// misses and decodes again: one wasted decode, never a stale answer.
	m, err := readManifest(dir)
	if err != nil {
		return m, err
	}
	manifestCache.mu.Lock()
	manifestCache.dir, manifestCache.fi, manifestCache.sum = dir, fi, sum
	manifestCache.m, manifestCache.ok = m, true
	manifestCache.mu.Unlock()
	return m, nil
}

// invalidateManifestCache drops this process's shared manifest snapshot after
// a core-only write. Compaction packets deliberately update manifest.gob
// without touching sessions.gob; a subsequent index writer must not reuse an
// in-memory Manifest from before that atomic replacement.
func invalidateManifestCache(dir string) {
	manifestCache.mu.Lock()
	defer manifestCache.mu.Unlock()
	if manifestCache.dir == dir {
		manifestCache.ok = false
	}
}

// The token catalog is every distinct token in the corpus — 180k strings on a
// real store. The stem and fuzzy tiers each build it, so a single query that
// falls through the ladder built it twice, ~63 ms of pure duplication.
//
// Keyed on the bucket files themselves, not the manifest: a bucket can be
// corrupted without the manifest changing, and tokenCatalog reporting that
// corruption is what makes the search ladder rebuild. A manifest-keyed cache
// would have hidden it.
//
// Contract: the returned map is shared and must not be mutated.
var catalogCache struct {
	mu  sync.Mutex
	dir string
	sig string
	idx *tokenIndex
	ok  bool
}

// tokenIndex is the catalog plus the same tokens bucketed by rune length.
// Fuzzy matching only ever considers tokens within its edit limit of the
// query's length, so bucketing turns a scan of every token in the corpus into
// a walk of a few short slices.
type tokenIndex struct {
	set   map[string]bool
	byLen [][]string
	// The tokens that carry a combining mark. The close tier treats a mark as
	// free, and a marked token is as many runes longer than its unmarked form
	// as it has marks, so the ordinary length window never reaches it
	// (#1941). Collected here on the way past and keyed by their unmarked
	// form on first use: a lookup rather than a walk, because on a corpus
	// where most tokens are marked — the Arabic and Thai stores this exists
	// for — walking them cost 36 ms per term, and the only thing that walk
	// could find is what the lookup returns. Built lazily because a query
	// that has no unmarked form to ask about never pays for it.
	marked     []string
	byUnmarked map[string][]string
	markedOnce sync.Once
}

const maxIndexedTokenLen = 64

func newTokenIndex(set map[string]bool) *tokenIndex {
	idx := &tokenIndex{set: set, byLen: make([][]string, maxIndexedTokenLen+2)}
	for tok := range set {
		n := len(tok)
		ascii := isASCIIString(tok)
		if !ascii {
			n = utf8.RuneCountInString(tok)
		}
		idx.byLen[bucketFor(n)] = append(idx.byLen[bucketFor(n)], tok)
		if ascii {
			continue // no ASCII byte is a combining mark
		}
		if hasMark(tok) {
			idx.marked = append(idx.marked, tok)
		}
	}
	return idx
}

// markedForms keys the marked tokens by their unmarked form, once.
func (t *tokenIndex) markedForms() map[string][]string {
	t.markedOnce.Do(func() {
		if len(t.marked) == 0 {
			return
		}
		t.byUnmarked = make(map[string][]string, len(t.marked))
		for _, tok := range t.marked {
			if bare, _ := unmarked(tok); bare != "" {
				t.byUnmarked[bare] = append(t.byUnmarked[bare], tok)
			}
		}
		for _, forms := range t.byUnmarked {
			sort.Strings(forms) // one order, whatever the map hands back
		}
	})
	return t.byUnmarked
}

// bucketFor clamps a rune length to the bucket that holds it; anything longer
// than maxIndexedTokenLen shares one overflow bucket, which is always scanned.
func bucketFor(n int) int {
	if n > maxIndexedTokenLen {
		return maxIndexedTokenLen + 1
	}
	return n
}

// candidates visits the tokens whose rune length is within limit of n, plus
// the overflow bucket, which holds tokens too long to bucket exactly.
func (t *tokenIndex) candidates(n, limit int, fn func(string)) {
	lo, hi := n-limit, n+limit
	if lo < 0 {
		lo = 0
	}
	if hi > maxIndexedTokenLen {
		hi = maxIndexedTokenLen
	}
	for l := lo; l <= hi && l < len(t.byLen); l++ {
		for _, tok := range t.byLen[l] {
			fn(tok)
		}
	}
	for _, tok := range t.byLen[maxIndexedTokenLen+1] {
		fn(tok)
	}
}

// markedFormsOf is the tokens that are word with combining marks on it, or —
// when word itself carries marks — the tokens that carry different ones, plus
// the bare form if the corpus holds it. Nothing else: a mark is free on the
// close tier, and nothing else is.
func (t *tokenIndex) markedFormsOf(word string) []string {
	bare, marked := unmarked(word)
	if bare == "" {
		return nil
	}
	forms := t.markedForms()[bare]
	if !marked {
		return forms
	}
	// A marked query reaches the bare word as well as its other spellings,
	// and never itself.
	out := make([]string, 0, len(forms)+1)
	if t.set[bare] {
		out = append(out, bare)
	}
	for _, f := range forms {
		if f != word {
			out = append(out, f)
		}
	}
	return out
}

// bucketsSignature changes whenever any bucket file is added, removed, resized
// or rewritten. Stat-only, so it costs a fraction of building the catalog.
func bucketsSignature(dir string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "buckets"))
	if err != nil {
		return "", err
	}
	h := fnv.New64a()
	for _, de := range entries {
		fi, err := de.Info()
		if err != nil {
			return "", err
		}
		_, _ = h.Write([]byte(de.Name()))
		_, _ = fmt.Fprintf(h, "|%d|%d;", fi.Size(), fi.ModTime().UnixNano())
	}
	return strconv.FormatUint(h.Sum64(), 16), nil
}

func tokenCatalogCached(dir string) (map[string]bool, error) {
	idx, err := tokenIndexCached(dir)
	if err != nil {
		return nil, err
	}
	return idx.set, nil
}

// catalogReads counts how often the token catalog was consulted, for the test
// that pins the fuzzy tier's early exit (#2898). Reading it is the only way to
// tell "answered no without looking" from "looked, then answered no".
var catalogReads atomic.Int64

func tokenIndexCached(dir string) (*tokenIndex, error) {
	catalogReads.Add(1)
	sig, err := bucketsSignature(dir)
	if err != nil {
		c, cerr := tokenCatalog(dir)
		if cerr != nil {
			return nil, cerr
		}
		return newTokenIndex(c), nil
	}
	catalogCache.mu.Lock()
	if catalogCache.ok && catalogCache.dir == dir && catalogCache.sig == sig {
		i := catalogCache.idx
		catalogCache.mu.Unlock()
		return i, nil
	}
	catalogCache.mu.Unlock()
	c, err := tokenCatalog(dir)
	if err != nil {
		return nil, err
	}
	i := newTokenIndex(c)
	catalogCache.mu.Lock()
	catalogCache.dir, catalogCache.sig = dir, sig
	catalogCache.idx, catalogCache.ok = i, true
	catalogCache.mu.Unlock()
	return i, nil
}
