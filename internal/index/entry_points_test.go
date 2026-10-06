package index

import (
	"path/filepath"
	"testing"
)

// The PreToolUse warning asks by command shape: a run that failed before is
// found under another spelling of the same command, one that passed is not,
// and the trust policy can hide a project's failures.
func TestCommandFailedBeforeFindsTheShapeThatFailed(t *testing.T) {
	c, tmp := newCFStore(t)
	for i := 0; i < 4; i++ {
		c.write("app", i, "fix the store test")
	}
	dir := filepath.Join(tmp, "index.db")
	if err := Ensure(dir, "", true, nil); err != nil {
		t.Fatal(err)
	}
	f, ok := CommandFailedBefore(dir, "  go test ./...  ", nil)
	if !ok {
		t.Fatalf("a command that failed in every session is not on file: %+v", ReadCommandFails(dir))
	}
	if f.Head != CommandHead("go test ./...") || f.Sessions < 2 || f.Line == "" {
		t.Errorf("failure on file = %+v", f)
	}
	if _, ok := CommandFailedBefore(dir, "go build ./...", nil); ok {
		t.Error("a command that always passed reads as failed before")
	}
	if _, ok := CommandFailedBefore(dir, "cd", nil); ok {
		t.Error("a command with no shape matched a failure")
	}
	if _, ok := CommandFailedBefore(dir, "go test ./...", func(string) bool { return false }); ok {
		t.Error("a failure from a project the policy hides was handed back")
	}
}

// `mss index` says whether there was anything to do: true right after a
// build, false once a transcript grew, and false for a store that is not there.
func TestUpToDateFollowsTheTranscripts(t *testing.T) {
	c, tmp := newCFStore(t)
	c.write("app", 0, "fix the store test")
	c.write("app", 1, "fix the build")
	dir := filepath.Join(tmp, "index.db")
	if ok, n := UpToDate(dir, ""); ok || n != 0 {
		t.Errorf("before any build: UpToDate = %v, %d", ok, n)
	}
	if err := Ensure(dir, "", true, nil); err != nil {
		t.Fatal(err)
	}
	if ok, n := UpToDate(dir, ""); !ok || n != 2 {
		t.Errorf("right after a build: UpToDate = %v, %d, want true, 2", ok, n)
	}
	c.appendTurn("app", 1, 5, "go vet ./...", "./store.go:12:2: undefined: Flock")
	if ok, n := UpToDate(dir, ""); ok || n != 2 {
		t.Errorf("after a transcript grew: UpToDate = %v, %d, want false, 2", ok, n)
	}
}

// Callers outside the package read a session's turns of several kinds in the
// order they were written, and only those kinds.
func TestEachRecordInRolesKeepsOnlyTheAskedKinds(t *testing.T) {
	dir := lineageStore(t)
	var order []string
	err := EachRecordInRoles(dir, []string{"user", "assistant"}, func(m SessionMeta, r Record) {
		if r.Role != "user" && r.Role != "assistant" {
			t.Errorf("record of role %q handed back", r.Role)
		}
		if m.ID == "a" {
			order = append(order, r.Role+":"+r.Text)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"user:the retry loop spins forever", "assistant:the backoff never resets", "user:reset it on success"}
	if len(order) != len(want) {
		t.Fatalf("a's turns = %q, want %q", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Errorf("turn %d = %q, want %q", i, order[i], want[i])
		}
	}
	n := 0
	if err := EachRecordInRoles(dir, []string{"assistant"}, func(SessionMeta, Record) { n++ }); err != nil {
		t.Fatal(err)
	}
	// a, c and d each hold the one assistant turn they share.
	if n != 3 {
		t.Errorf("assistant turns = %d, want 3", n)
	}
}
