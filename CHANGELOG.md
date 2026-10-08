# Changelog

All notable changes to mss are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- `mss install-skill <claude|codex|pi|omp> [--language en|zh-CN]` installs the
  binary's embedded, version-matched workflow without downloading from `main`,
  auto-detecting agents, or overwriting existing customizations.
- Windows CI, reusable tag-commit checks, and native archive validation before
  publication. A standard-library validator checks SHA256, archive paths,
  six binary targets, version consistency and the embedded installer.

### Changed

- Releases generate a separately published, source-built Homebrew formula.
  Cask publishing and automatic quarantine removal are removed; migration of
  the external tap and its validation remain separate release gates.

### Fixed

- Manual Skill routing uses `disable-model-invocation` for Claude Code, OMP and
  Pi, plus a Codex-specific `allow_implicit_invocation: false` policy. The former
  OMP-only `hide` field did not suppress Pi/Codex automatic selection.
- Both Skill languages treat recalled instructions and commands as untrusted
  evidence, avoid raw-session fallback, and provide a PowerShell nonce command.
- Recall warns about unreadable, malformed or unrecognized policy settings.
  Parse failures discard partial rules and retain the documented permissive
  default; valid wildcard rules no longer produce a false diagnostic.

### Documentation

- A version-bounded Skill compatibility matrix and real-host offline probes;
  Codex non-TUI invocation and untested interactive surfaces are explicit limits.
- Exact installation, verification, update and uninstall instructions; unsigned
  archives and pending external/platform validation are clearly identified.

## [0.2.0] - 2026-10-07

### Added

- `LICENSE` and both READMEs credit [deja-vu](https://github.com/vshulcz/deja-vu):
  mss is derived from it, and both projects are MIT licensed.

### Changed

- The README and the skill now describe mss as a replacement for a memory
  system: `/mss` recalls the relevant past-session history and summarizes it
  into the current conversation. The trigger is still manual.
- The skill ships in English as `skills/mss/SKILL.md`; the Chinese version moved
  to `skills/mss/SKILL.zh-CN.md`. Release archives carry both.
- Search output says "1 match" instead of "1 matches".
- `mss doctor` describes the recall policy in plain words and explains what
  `git` is used for.

### Removed

- Unused auto-recall and MCP policy modes. `search` is the only activation mode
  in the recall policy file (`~/.config/mss/policy.json`).

### Fixed

- The index could reuse a stale cached manifest when `manifest.gob` was
  rewritten with the same size within one file-timestamp tick. The cache is now
  checked against the file's identity and a checksum of its contents.

### Documentation

- A Privacy section: where the index lives, what is redacted, what reaches an
  agent's model, `MSS_NO_REDACT`, excluding projects, and deleting the index.
- A screenshot made from made-up sample sessions.
- CONTRIBUTING.md, SECURITY.md, issue templates and a pull request template.
- Windows is marked untested.

## [0.1.0] - 2026-10-07

First public release.

- `mss index`, `search`, `show`, `ctx`, `last`, `sources`, `doctor` and
  `version`, with `--json` output for scripts.
- Reads Claude Code, Codex CLI, opencode, Cursor, Grok Build, pi, omp and
  DeepSeek Harness sessions.
- A redacted, incrementally updated local index under `~/.cache/mss/`.
- The `/mss` skill.
- Prebuilt archives for macOS, Linux and Windows (Windows untested), and a
  Homebrew cask.

[Unreleased]: https://github.com/henryyu333/mss/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/henryyu333/mss/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/henryyu333/mss/releases/tag/v0.1.0
