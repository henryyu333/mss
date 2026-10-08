package main

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestCooccurrenceNeighboursRemainCandidates(t *testing.T) {
	root, dir := cmdEnv(t)
	distractors := []string{
		"Quartz amber deployment requires queue tuning.",
		"Cobalt silver deployment checks the transport parser.",
		"Orchid lock retry analysis for the scheduler.",
		"Semaphore budget retry analysis for the scheduler.",
		"Velvet polished copper routing is noncontiguous.",
		"Copper polished velvet routing reverses the words.",
		"Lattice archive stores build notes.",
		"Beacon cache stores build notes.",
	}
	for i := range 256 {
		id := fmt.Sprintf("semantic-%04d", i)
		if i < 16 {
			claudeSession(t, root, id,
				claudeUser(id, "2026-01-02T03:04:05Z", "routine network parser discussion"),
				claudeAssistant(id, "2026-01-02T03:04:06Z", "Lattice beacon resolves the assistant-only boundary condition."),
			)
		} else {
			claudeSession(t, root, id, claudeUser(id, "2026-01-02T03:04:05Z", distractors[i%len(distractors)]))
		}
	}
	mustEnsure(t, dir)
	for _, output := range [][]string{{"--sessions"}, {"--json"}} {
		args := append([]string{"search", "--no-refresh", "--role", "user"}, output...)
		args = append(args, "lattice beacon")
		out, _, err := runCaptured(t, func() error { return run(args) })
		if err != nil {
			t.Fatal(err)
		}
		var envelope struct {
			Match string
			Total int
		}
		if err := json.Unmarshal([]byte(out), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Match != "candidates" || envelope.Total == 0 {
			t.Fatalf("correlated replacement words were presented as actual query matches: %s", out)
		}
	}
}

func TestQuotedRelaxationRemainsCandidates(t *testing.T) {
	root, dir := cmdEnv(t)
	claudeSession(t, root, "quoted-aaaa",
		claudeUser("quoted-aaaa", "2026-01-02T03:04:05Z", "Copper polished velvet routing reverses the important word order."),
	)
	mustEnsure(t, dir)
	out, _, err := runCaptured(t, func() error {
		return run([]string{"search", "--sessions", "--no-refresh", `copper routing "velvet copper"`})
	})
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Match string
		Total int
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Match != "candidates" || envelope.Total != 1 {
		t.Fatalf("dropping a missing quoted phrase was presented as an original query match: %s", out)
	}
}
