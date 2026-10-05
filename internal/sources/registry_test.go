package sources

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/henryyu333/mss/internal/model"
)

type formatRegistry struct {
	SchemaVersion int                   `json:"schema_version"`
	Harnesses     []formatRegistryEntry `json:"harnesses"`
}

type formatRegistryEntry struct {
	ID           string   `json:"id"`
	StorePaths   []string `json:"store_paths"`
	FormatKind   string   `json:"format_kind"`
	FixturePaths []string `json:"fixture_paths"`
	ParserSource string   `json:"parser_source"`
	LastVerified string   `json:"last_verified"`
}

func TestFormatRegistryConformance(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, key := range []string{
		"CLAUDE_CONFIG_DIR", "CODEX_HOME", "CURSOR_CONFIG_DIR",
		"MSS_CLAUDE_ROOT", "MSS_CODEX_ROOT", "MSS_CURSOR_CLI_ROOT",
		"MSS_CURSOR_ROOT", "MSS_GROK_ROOT", "MSS_PI_ROOT", "MSS_OMP_ROOT",
		"MSS_INCLUDE_SUBAGENTS", "MSS_OPENCODE_DB", "MSS_DEEPSEEK_ROOT",
		"GROK_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("MSS_CLAUDE_ROOT", filepath.Join(home, "claude"))

	registry := readFormatRegistry(t, filepath.Join(root, "docs", "registry", "registry.json"))
	if registry.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", registry.SchemaVersion)
	}

	registered := make([]string, 0, len(registry.Harnesses))
	seen := map[string]bool{}
	for _, entry := range registry.Harnesses {
		if entry.ID == "" || seen[entry.ID] {
			t.Fatalf("invalid or duplicate harness id %q", entry.ID)
		}
		seen[entry.ID] = true
		registered = append(registered, entry.ID)
		validateRegistryEntry(t, root, entry)

		entry := entry
		t.Run(entry.ID, func(t *testing.T) {
			for _, fixture := range entry.FixturePaths {
				fixture := filepath.Join(root, filepath.FromSlash(fixture))
				sessions := parseRegistryFixture(t, entry.ID, fixture)
				validateRegistrySessions(t, entry.ID, sessions)
			}
		})
	}

	loaders := registryHarnessIDs()
	sort.Strings(registered)
	if strings.Join(registered, ",") != strings.Join(loaders, ",") {
		t.Fatalf("format registry harnesses = %v, source registry = %v", registered, loaders)
	}
}

// registryHarnessIDs lists the real harnesses in the source registry (excluding
// the notes pseudo-source) to compare against the published format registry.
func registryHarnessIDs() []string {
	var ids []string
	for _, h := range Registry() {
		if h.Name == "mss" {
			continue
		}
		ids = append(ids, h.Name)
	}
	sort.Strings(ids)
	return ids
}

func readFormatRegistry(t *testing.T, path string) formatRegistry {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var registry formatRegistry
	if err := json.Unmarshal(b, &registry); err != nil {
		t.Fatal(err)
	}
	return registry
}

func validateRegistryEntry(t *testing.T, root string, entry formatRegistryEntry) {
	t.Helper()
	if len(entry.StorePaths) == 0 || entry.FormatKind == "" || len(entry.FixturePaths) == 0 || entry.ParserSource == "" {
		t.Fatalf("incomplete registry entry for %q: %#v", entry.ID, entry)
	}
	if _, err := time.Parse("2006-01-02", entry.LastVerified); err != nil {
		t.Fatalf("%s last_verified: %v", entry.ID, err)
	}
	paths := append(append([]string(nil), entry.FixturePaths...), entry.ParserSource)
	for _, path := range paths {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("%s path %q: %v", entry.ID, path, err)
		}
	}
}

func parseRegistryFixture(t *testing.T, id, path string) []model.Session {
	return parseRegistryFixtureIn(t, id, path, t.TempDir())
}

// parseRegistryFixtureIn is parseRegistryFixture with the working directory
// given rather than made, so the hostile-path test can hand it a directory
// whose name carries a space, a percent octet and a non-ASCII character — the
// shape a store is under on somebody else's machine, and the one the Copilot
// Chat reader was wrong about until a contributor hit it (#3498, #3505).
func parseRegistryFixtureIn(t *testing.T, id, path, work string) []model.Session {
	t.Helper()
	var (
		sessions []model.Session
		err      error
	)
	switch id {
	case "claude":
		sessions, err = ParseClaudeFile(path)
	case "codex":
		sessions, err = ParseCodexRollout(path)
	case "opencode":
		// Two stores under one harness: the database, and the per-session diff
		// files beside it (#3791). The fixture's own name says which it is,
		// the way the reader does.
		if strings.HasSuffix(filepath.Base(path), ".json") {
			sessions, err = ParseOpencodeDiff(path)
			break
		}
		if !SQLite3Available() {
			t.Skip("sqlite3 not installed")
		}
		sql, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		// Named after the fixture: the 1.x and 2.x fixtures are two stores,
		// and one file holding both schemas is a store neither reader
		// describes.
		db := filepath.Join(work, strings.TrimSuffix(filepath.Base(path), ".sql")+".db")
		if out, runErr := exec.Command("sqlite3", db, string(sql)).CombinedOutput(); runErr != nil {
			t.Fatalf("create sqlite fixture: %v: %s", runErr, out)
		}
		sessions, err = ParseOpencodeDB(db)
	case "cursor":
		sessions, err = ParseCursorTranscript(path)
	case "grok":
		sessions, err = ParseGrokFile(path)
	case "pi":
		sessions, err = ParsePiFile(path)
	case "omp":
		sessions, err = ParseOmpFile(path)
	case "deepseek":
		// The fixture is stored raw: the harness writes zstd frames by default,
		// and a registry fixture that needs an external tool to read cannot be
		// checked on a machine that lacks it.
		sessions, err = ParseDeepSeekFile(path)
	default:
		t.Fatalf("no conformance parser for %q", id)
	}
	if err != nil {
		t.Fatalf("parse %s fixture: %v", id, err)
	}
	return sessions
}

// registryFixturesWithCalls are the registry fixtures whose tool calls are
// read into work records.
var registryFixturesWithCalls = map[string]bool{"deepseek": true, "continue": true}

func validateRegistrySessions(t *testing.T, id string, sessions []model.Session) {
	t.Helper()
	if len(sessions) == 0 {
		t.Fatalf("%s fixture produced no sessions", id)
	}
	for _, session := range sessions {
		if session.Harness != id || session.ID == "" || session.Project == "" || session.Path == "" {
			t.Fatalf("%s fixture produced invalid session: %#v", id, session)
		}
		if session.Started.IsZero() || session.Updated.IsZero() || len(session.Messages) == 0 {
			t.Fatalf("%s fixture produced incomplete session: %#v", id, session)
		}
		for _, message := range session.Messages {
			// tool-output is a role the index stores and the search filters by
			// (retrieval.go: roleToolOutput); a fixture whose harness records
			// what a tool printed should be able to show it. The work records a
			// tool call leaves — files, command, edit, wrote — are allowed only
			// for a fixture known to carry calls, so a parser that starts
			// emitting them from prose still fails here.
			role := message.Role
			work := registryFixturesWithCalls[id] &&
				(role == RoleFiles || role == RoleCommand || role == RoleEdit || role == RoleWrote)
			if (role != "user" && role != "assistant" && role != "tool-output" && !work) ||
				strings.TrimSpace(message.Text) == "" || message.Time.IsZero() {
				t.Fatalf("%s fixture produced invalid message: %#v", id, message)
			}
		}
	}
}
