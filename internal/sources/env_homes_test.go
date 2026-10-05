package sources

import (
	"path/filepath"
	"testing"
)

// Each harness that documents a relocation variable gets the same contract:
// the upstream variable moves the default, MSS_* still wins.
func TestUpstreamHomeVariables(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	t.Run("codex CODEX_HOME", func(t *testing.T) {
		t.Setenv("MSS_CODEX_ROOT", "")
		t.Setenv("CODEX_HOME", filepath.Join(home, "codex-profile"))
		if got := CodexRoot(); got != filepath.Join(home, "codex-profile") {
			t.Fatalf("CodexRoot=%q", got)
		}
		t.Setenv("MSS_CODEX_ROOT", filepath.Join(home, "override"))
		if got := CodexRoot(); got != filepath.Join(home, "override") {
			t.Fatalf("MSS_CODEX_ROOT must win, got %q", got)
		}
	})

	t.Run("cursor CURSOR_CONFIG_DIR", func(t *testing.T) {
		t.Setenv("MSS_CURSOR_CLI_ROOT", "")
		t.Setenv("CURSOR_CONFIG_DIR", filepath.Join(home, "cursor-cfg"))
		if got := CursorCLIRoot(); got != filepath.Join(home, "cursor-cfg") {
			t.Fatalf("CursorCLIRoot=%q", got)
		}
	})

	t.Run("opencode XDG_DATA_HOME", func(t *testing.T) {
		t.Setenv("MSS_OPENCODE_DB", "")
		t.Setenv("OPENCODE_DB", "")
		t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg"))
		got := OpencodeDB()
		want := filepath.Join(home, "xdg", "opencode", "opencode.db")
		if got != want {
			t.Fatalf("OpencodeDB=%q want %q", got, want)
		}
	})
}
