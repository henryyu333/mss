package sources

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Claude Code encodes "/" and "-" identically; resolving against the
// filesystem must recover hyphenated project names instead of mangling
// mss-vu into mss/vu.
func TestClaudeProjectNameResolvesHyphenatedDirs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("claude encodes unix-style absolute paths; resolution is a no-op on windows")
	}
	tmp := t.TempDir()
	real := filepath.Join(tmp, "projects", "mss-vu")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	encoded := strings.ReplaceAll(real, string(filepath.Separator), "-")
	if got := claudeProjectName(filepath.Join("/claude/projects", encoded)); got != "projects/mss-vu" {
		t.Fatalf("resolved name = %q, want projects/mss", got)
	}
	// Non-existent paths keep the old heuristic.
	if got := claudeProjectName("/claude/projects/-no-such-root-mss-vu"); got != "mss/vu" {
		t.Fatalf("fallback name = %q, want mss/vu", got)
	}
}
