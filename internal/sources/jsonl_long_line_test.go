package sources

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A small initial reader buffer must not become a line-length limit. This line
// is several times the buffer, contains multibyte text, and is followed by a
// normal record so a reader that stops at the buffer fails visibly.
func TestJSONLReadersGrowPastTheInitialBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long.jsonl")
	long := strings.Repeat("长", 30000)
	longHeader := strings.Repeat("头", 30000)
	records := []map[string]any{
		{"type": "session", "id": "s1", "text": longHeader},
		{"type": "message", "text": long},
		{"type": "message", "text": "after"},
	}
	var body strings.Builder
	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		body.Write(line)
		body.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	var texts []string
	if err := scanJSONLFromOffset(path, 0, func(m map[string]any) {
		if typ, _ := m["type"].(string); typ != "message" {
			return
		}
		if text, ok := m["text"].(string); ok {
			texts = append(texts, text)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if len(texts) != 2 || texts[0] != long || texts[1] != "after" {
		first := 0
		if len(texts) > 0 {
			first = len(texts[0])
		}
		t.Fatalf("long line or following record was lost: %d texts, first length %d", len(texts), first)
	}

	headerLine, err := json.Marshal(records[0])
	if err != nil {
		t.Fatal(err)
	}
	var header, resumed []string
	if err := scanJSONLWithHeaderFromOffset(path, int64(len(headerLine)+1), func(m map[string]any) {
		if typ, _ := m["type"].(string); typ == "session" {
			header = append(header, typ)
			return
		}
		if text, ok := m["text"].(string); ok {
			resumed = append(resumed, text)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if len(header) != 1 || len(resumed) != 2 || resumed[0] != long || resumed[1] != "after" {
		t.Fatalf("resumed read lost the header or long line: header=%v texts=%d", header, len(resumed))
	}
}
