package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/henryyu333/mss/skills"
)

func cmdInstallSkill(_ string, args []string) error {
	if wantsHelp(args) {
		fmt.Print(helpForCommand("install-skill"))
		return nil
	}
	if len(args) == 0 {
		return fmt.Errorf("install-skill needs a harness — run `mss install-skill --help`")
	}
	harness, language := args[0], "en"
	for i := 1; i < len(args); i++ {
		if args[i] != "--language" || i+1 >= len(args) {
			return fmt.Errorf("install-skill does not accept %q — run `mss install-skill --help`", args[i])
		}
		i++
		language = args[i]
	}
	text := skills.English
	switch language {
	case "en":
	case "zh-CN":
		text = skills.Chinese
	default:
		return fmt.Errorf("install-skill language must be en or zh-CN — run `mss install-skill --help`")
	}
	var parts []string
	switch harness {
	case "claude":
		parts = []string{".claude", "skills", "mss"}
	case "codex":
		parts = []string{".agents", "skills", "mss"}
	case "pi", "omp":
		parts = []string{"." + harness, "agent", "skills", "mss"}
	default:
		return fmt.Errorf("install-skill harness must be claude, codex, pi or omp — run `mss install-skill --help`")
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return fmt.Errorf("install-skill cannot find your home directory — set HOME (USERPROFILE on Windows) to an absolute path")
	}
	dir := filepath.Join(append([]string{home}, parts...)...)
	type installFile struct{ path, content string }
	files := make([]installFile, 0, 2)
	if harness == "codex" {
		// Publish the invocation policy before discovery can see a new Skill.
		files = append(files, installFile{filepath.Join(dir, "agents", "openai.yaml"), skills.CodexPolicy})
	}
	files = append(files, installFile{filepath.Join(dir, "SKILL.md"), text})

	// Check every destination before writing either file. Existing customizations
	// belong to the user; identical reinstalls are harmless, upgrades are explicit.
	dirs := []string{dir}
	if harness == "codex" {
		dirs = append(dirs, filepath.Join(dir, "agents"))
	}
	for _, parent := range dirs {
		if info, err := os.Lstat(parent); err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("install-skill refuses non-directory or symlink %s — move it aside before running `mss install-skill %s`", parent, harness)
			}
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	for _, file := range files {
		info, err := os.Lstat(file.path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("install-skill refuses non-regular file %s — move it aside before running `mss install-skill %s`", file.path, harness)
		}
		old, err := os.ReadFile(file.path)
		if err != nil {
			return err
		}
		if string(old) != file.content {
			return fmt.Errorf("install-skill will not overwrite %s — move the existing file aside, then run `mss install-skill %s` again", file.path, harness)
		}
	}
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(file.path), 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(file.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if os.IsExist(err) {
			// A concurrent install must not turn an earlier preflight into clobber.
			old, readErr := os.ReadFile(file.path)
			if readErr == nil && string(old) == file.content {
				continue
			}
			return fmt.Errorf("install-skill destination changed: %s — inspect it before running `mss install-skill %s` again", file.path, harness)
		}
		if err != nil {
			return err
		}
		_, writeErr := f.WriteString(file.content)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(file.path)
			if writeErr != nil {
				return writeErr
			}
			return closeErr
		}
	}
	fmt.Printf("mss: installed %s skill (%s) at %s; restart the harness and invoke it explicitly\n", harness, language, dir)
	return nil
}
