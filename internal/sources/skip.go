package sources

import "strings"

// SkipReason says why a harness mss can see on disk produced nothing. That is
// a missing external tool, or a sqlite3 on PATH that does not answer (see
// SQLite3Problem): the database stores are read through the sqlite3 CLI, and an
// index run that names every harness it read while staying silent about the
// one it could not made an empty mss look like an empty history (#794).
//
// Zed needs a second tool. Its store is SQLite like the others, but every
// thread body inside it is a zstd frame, so sqlite3 alone opens the store and
// reads nothing out of it — the failure this exists to stop, one layer down.
//
// It returns "" when there is nothing to explain — including on a machine that
// never used the harness, where a note about a tool would be noise.
func SkipReason(harness string) string {
	if harness == "deepseek" {
		// Only the framed ones need the tool. A store of plain session.jsonl
		// reads without it, and saying part of it could not be read there
		// names a problem the reader does not have (#1758).
		if ZstdAvailable() || !anyZstdFramed(DeepSeekSessionFiles()) {
			return ""
		}
		return "zstd CLI not found"
	}
	// Codex compresses a rollout once it is seven days old, so a store can hold
	// most of its history behind zstd — the same failure as DeepSeek Harness's,
	// and the same rule: only the compressed ones need the tool, and a store of
	// plain rollouts must not claim a problem it does not have (#3640).
	if harness == "codex" {
		if ZstdAvailable() || len(CodexCompressedFiles()) == 0 {
			return ""
		}
		return "zstd CLI not found"
	}
	var present bool
	switch harness {
	case "opencode":
		present = fileExists(OpencodeDB())
	case "cursor":
		present = len(CursorDBs()) > 0
	case "grok":
		present = fileExists(GrokDB())
	}
	// Checked after the store: a harness with no database on this machine has
	// nothing to explain, and asking sqlite3 would cost a process for nothing.
	if !present {
		return ""
	}
	return SQLite3Problem()
}

func anyFileExists(paths []string) bool {
	for _, p := range paths {
		if fileExists(p) {
			return true
		}
	}
	return false
}

// anyZstdFramed reports whether any of these files is a zstd frame rather than
// plain text — the DeepSeek Harness writes either, depending on its settings.
func anyZstdFramed(files []string) bool {
	for _, f := range files {
		if strings.HasSuffix(f, ".zstd") || strings.HasSuffix(f, ".zst") {
			return true
		}
	}
	return false
}
