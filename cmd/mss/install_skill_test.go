package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func installHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestInstallSkillDoesNotInspectHistoryOrIndex(t *testing.T) {
	home := installHome(t)
	indexDir := filepath.Join(home, "must-not-create-index")
	t.Setenv("MSS_INDEX_DIR", indexDir)
	t.Setenv("MSS_STORES", "not-a-store")
	t.Setenv("MSS_POLICY_FILE", filepath.Join(home, "missing-policy"))
	_, _, err := runCaptured(t, func() error { return run([]string{"install-skill", "pi"}) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(indexDir); !os.IsNotExist(err) {
		t.Fatalf("installation touched the index: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".pi", "agent", "sessions")); !os.IsNotExist(err) {
		t.Fatalf("installation touched sessions: %v", err)
	}
}

func TestInstallSkillPreservesCustomFile(t *testing.T) {
	home := installHome(t)
	path := filepath.Join(home, ".claude", "skills", "mss", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	const original = "user's customized workflow\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := runCaptured(t, func() error { return run([]string{"install-skill", "claude"}) })
	if err == nil {
		t.Fatal("installation overwrote a custom workflow")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil || string(got) != original {
		t.Fatalf("custom workflow changed: %q, %v", got, readErr)
	}
}

func TestInstallSkillPreflightsCodexPolicy(t *testing.T) {
	home := installHome(t)
	dir := filepath.Join(home, ".agents", "skills", "mss")
	policy := filepath.Join(dir, "agents", "openai.yaml")
	if err := os.MkdirAll(filepath.Dir(policy), 0o700); err != nil {
		t.Fatal(err)
	}
	const original = "policy:\n  allow_implicit_invocation: true\n"
	if err := os.WriteFile(policy, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := runCaptured(t, func() error { return run([]string{"install-skill", "codex"}) })
	if err == nil {
		t.Fatal("conflicting invocation policy was overwritten")
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("skill published despite conflicting policy: %v", err)
	}
	got, readErr := os.ReadFile(policy)
	if readErr != nil || string(got) != original {
		t.Fatalf("policy changed: %q, %v", got, readErr)
	}
}

func TestInstallSkillReinstallDoesNotRewrite(t *testing.T) {
	home := installHome(t)
	invoke := func() error { return run([]string{"install-skill", "omp", "--language", "zh-CN"}) }
	if _, _, err := runCaptured(t, invoke); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".omp", "agent", "skills", "mss", "SKILL.md")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCaptured(t, invoke); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("identical reinstall rewrote the workflow: %v", err)
	}
	if _, _, err := runCaptured(t, func() error { return run([]string{"install-skill", "omp"}) }); err == nil {
		t.Fatal("language change silently overwrote existing workflow")
	}
}

func TestInstallSkillRejectsSymlinkDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges are not guaranteed on Windows runners")
	}
	home := installHome(t)
	outside := t.TempDir()
	parent := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(parent, "mss")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runCaptured(t, func() error { return run([]string{"install-skill", "claude"}) }); err == nil {
		t.Fatal("installation followed a symlink destination")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("installation wrote outside target: %v, %v", entries, err)
	}
}

func TestInstallSkillRejectsInvalidArgumentsBeforeWrites(t *testing.T) {
	for _, args := range [][]string{{}, {"unknown"}, {"pi", "--language", "unknown"}, {"codex", "--language"}, {"claude", "--force"}} {
		t.Run(filepath.Join(args...), func(t *testing.T) {
			home := installHome(t)
			if _, _, err := runCaptured(t, func() error { return run(append([]string{"install-skill"}, args...)) }); err == nil {
				t.Fatal("invalid installation request succeeded")
			}
			entries, err := os.ReadDir(home)
			if err != nil || len(entries) != 0 {
				t.Fatalf("invalid request wrote files: %v, %v", entries, err)
			}
		})
	}
}
