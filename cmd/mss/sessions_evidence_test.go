package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/henryyu333/mss/internal/model"
)

func TestSessionsFoundSetExcludesRelevanceOnlyRows(t *testing.T) {
	root, dir := cmdEnv(t)
	claudeSession(t, root, "strict-aaaa",
		claudeUser("strict-aaaa", "2026-01-02T03:04:05Z", "quartz cobalt deployment"),
		claudeUser("strict-aaaa", "2026-01-02T03:04:06Z", "quartz amber deployment"),
		claudeUser("strict-aaaa", "2026-01-02T03:04:07Z", "cobalt silver deployment"),
	)
	for i := range 15 {
		id := fmt.Sprintf("neighbor-%04d", i)
		text := "quartz amber deployment"
		if i%2 == 0 {
			text = "cobalt silver deployment"
		}
		claudeSession(t, root, id, claudeUser(id, "2026-01-03T03:04:05Z", text))
	}
	mustEnsure(t, dir)
	out, _, err := runCaptured(t, func() error {
		return run([]string{"search", "--sessions", "--no-refresh", "--harness", "claude", "--sort", "updated", "quartz cobalt"})
	})
	if err != nil {
		t.Fatal(err)
	}
	type evidenceEnvelope struct {
		Match    string
		Total    int
		Sessions []struct {
			Session  model.Session
			HitCount int   `json:"hit_count"`
			Indices  []int `json:"matched_indices"`
		}
	}
	var envelope evidenceEnvelope
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Match != "found" || envelope.Total != 1 || len(envelope.Sessions) != 1 {
		t.Fatalf("found list admitted relevance-only neighbors: %s", out)
	}
	row := envelope.Sessions[0]
	if row.Session.ID != "strict-aaaa" || row.HitCount != 1 || len(row.Indices) != 1 || row.Indices[0] != 0 {
		t.Fatalf("found evidence included partial-word records: %#v", row)
	}

	// A genuinely relaxed query still exposes candidates, not a fake miss.
	out, _, err = runCaptured(t, func() error {
		return run([]string{"search", "--sessions", "--no-refresh", "quartz cobalt imaginarysubject"})
	})
	if err != nil {
		t.Fatal(err)
	}
	envelope = evidenceEnvelope{}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Match != "candidates" || envelope.Total == 0 {
		t.Fatalf("pure candidate ranking was lost or presented as evidence: %s", out)
	}

	for _, selected := range []struct{ id, query string }{
		{"strict-aaaa", "quartz cobalt"},
		{"neighbor-0001", "quartz amber"},
	} {
		out, _, err := runCaptured(t, func() error {
			return run([]string{"search", "--json", "--no-refresh", "--session", selected.id, selected.query})
		})
		if err != nil {
			t.Fatal(err)
		}
		var ranked struct {
			Match string
			Hits  []struct{ Session model.Session }
		}
		if err := json.Unmarshal([]byte(out), &ranked); err != nil {
			t.Fatal(err)
		}
		if len(ranked.Hits) != 1 || ranked.Hits[0].Session.ID != selected.id {
			t.Fatalf("relevance ranking escaped --session %q: %s", selected.id, out)
		}
		if ranked.Match != "found" {
			t.Fatalf("scope lost the selected session's complete query match: %s", out)
		}
	}
}
