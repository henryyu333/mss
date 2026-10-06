package search

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/henryyu333/mss/internal/jsonout"
	"github.com/henryyu333/mss/internal/model"
	"github.com/henryyu333/mss/internal/query"
)

// docs/json-output.md is the contract mss publishes for `search --json`. This
// pins the emitted key set to that document in both directions: a new field the
// doc does not mention fails here (additive changes must be documented), and a
// renamed or removed field drops a required key. It marshals the same envelope
// the JSON path builds, with every optional field populated so all of them show.
func TestSearchJSONKeysMatchTheDocumentedContract(t *testing.T) {
	documented := map[string]bool{
		// envelope
		"schema_version": true, "tier": true, "total": true, "capped": true,
		"policy_withheld": true, "hits": true, "fuzzy": true, "stemmed": true,
		"strict": true, "match": true, "produced_by": true, "coverage": true,
		"unread": true, "skipped": true, "records": true, "files": true,
		"clipped": true, "self_requested": true, "self_excluded": true,
		"complete": true, "refresh": true, "refreshed": true,
		"last_refresh": true,
		"semantic":     true, "variants": true,
		// hit
		"session": true, "count": true, "snippets": true, "score": true,
		"tier_detail": true, "superseded": true, "reused": true, "moved": true,
		"lifecycle": true, "lifecycle_note": true, "lifecycle_at": true,
		// session
		"id": true, "harness": true, "project": true, "path": true, "title": true,
		"started": true, "updated": true, "messages": true, "source": true,
		"touched": true, "agent_title": true, "orig_id": true,
		// source
		"origin": true, "instance": true,
		// message
		"role": true, "text": true, "time": true, "index": true,
	}
	now := time.Now()
	idx := 3
	env := searchJSONEnvelope{
		SchemaVersion: jsonout.Version, Tier: "exact", Total: 1, Strict: 1, Capped: true,
		Withheld: 1, Fuzzy: true, Stemmed: true, Semantic: true,
		Variants: map[string][]string{"a": {"b"}},
		Coverage: &query.Coverage{
			Unread:        []string{"deepseek: zstd CLI not found"},
			Skipped:       map[string]query.SkippedIngest{"deepseek": {Records: 6, Files: 2}},
			Clipped:       2,
			SelfRequested: true, SelfExcluded: true,
			Complete: false,
		},
		Refresh: &query.Refresh{LastRefresh: now},
		Hits: []Hit{{
			Session: model.Session{
				ID: "i", Harness: "claude", Project: "p", Path: "/x", Title: "t",
				Started: now, Updated: now,
				Messages: []model.Message{{Role: "user", Text: "x", Time: now, Index: &idx}},
				Source:   &model.Source{Origin: "local", Instance: "w"},
				Touched:  []string{"f"}, AgentTitle: true, OrigID: "o",
				Lifecycle: "accepted", LifecycleNote: "n", LifecycleAt: "2026",
			},
			Count: 1, Snippets: []string{"s"}, Score: 1, Tier: "exact",
			TierDetail: "d", Superseded: "2026", Reused: 1, Moved: "2026",
			Lifecycle: "accepted", LifecycleNote: "n", LifecycleAt: "2026",
			Strict: true,
		}},
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var generic any
	if err := json.Unmarshal(b, &generic); err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	// variants is map[queryTerm][]string and skipped is map[harness]counts:
	// their keys are user data, not schema, so neither subtree is walked for
	// schema keys.
	var walk func(v any, underData bool)
	walk = func(v any, underData bool) {
		switch node := v.(type) {
		case map[string]any:
			for k, sub := range node {
				if underData {
					continue
				}
				keys[k] = true
				walk(sub, k == "variants" || k == "skipped")
			}
		case []any:
			for _, sub := range node {
				walk(sub, underData)
			}
		}
	}
	walk(generic, false)
	for k := range keys {
		if !documented[k] {
			t.Errorf("emitted JSON key %q is not in docs/json-output.md — document it or drop it", k)
		}
	}
	for _, req := range []string{
		"schema_version", "tier", "total", "hits",
		"session", "count", "snippets", "score",
		"harness", "id", "messages", "role", "text",
	} {
		if !keys[req] {
			t.Errorf("documented key %q vanished from the emitted JSON", req)
		}
	}
}
