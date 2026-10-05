package index

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "mss-index-test-")
	if err != nil {
		panic(err)
	}
	stores := map[string]string{
		"HOME":        root,
		"USERPROFILE": root,
		// The exclude list lives under XDG_CONFIG_HOME. A developer who has it
		// set would otherwise have the suite write into their real ~/.config.
		"XDG_CONFIG_HOME":   filepath.Join(root, "config"),
		"XDG_DATA_HOME":     "",
		"APPDATA":           filepath.Join(root, "AppData", "Roaming"),
		"CLAUDE_CONFIG_DIR": "",
		"CODEX_HOME":        "",
		"CURSOR_CONFIG_DIR": "",
		// A developer with MSS_INDEX_DIR exported reads their own index here,
		// and the suite went red on their machine only.
		"MSS_INDEX_DIR":        "",
		"MSS_EXCLUDE_PROJECTS": "",
		"MSS_CLAUDE_ROOT":      filepath.Join(root, "claude"),
		"MSS_CODEX_ROOT":       filepath.Join(root, "codex"),
		"MSS_OPENCODE_DB":      filepath.Join(root, "opencode.db"),
		"MSS_CURSOR_ROOT":      filepath.Join(root, "cursor"),
		"MSS_CURSOR_CLI_ROOT":  filepath.Join(root, "cursor-cli"),
		"MSS_GROK_ROOT":        filepath.Join(root, "grok"),
		"MSS_PI_ROOT":          filepath.Join(root, "pi"),
		"MSS_OMP_ROOT":         filepath.Join(root, "omp"),
		"MSS_DEEPSEEK_ROOT":    filepath.Join(root, "deepseek"),
	}
	scrubEnv(stores)
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

// scrubEnv keeps the suite off the developer's real stores: every variable
// that names a place is unset unless pinned names it.
var envLocation = regexp.MustCompile(`^[A-Z0-9]+(_[A-Z0-9]+)*_(HOME|DIR|DIRS|DIR_NAME|ROOT|ROOTS|DB|FILE|PATH|CONFIG)$`)

func scrubEnv(pinned map[string]string) {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if _, ok := pinned[name]; ok {
			continue
		}
		// The cross-process child carries its parent's stores under these
		// names, put back after this scrub: unsetting them here leaves the
		// child passing over an empty environment (#2221's shape).
		if strings.HasPrefix(name, "MSS_PASS_") {
			continue
		}
		if strings.HasPrefix(name, "MSS_") || envLocation.MatchString(name) {
			if err := os.Unsetenv(name); err != nil {
				panic(err)
			}
		}
	}
	for k, v := range pinned {
		if err := os.Setenv(k, v); err != nil {
			panic(err)
		}
	}
}
