# Contributing to mss

Thanks for helping. mss is small on purpose: one binary and one skill, and it
runs only when a person or a script invokes it. Changes that keep it that way
are the easiest to accept.

## Before you start

- For a bug, open an issue with the bug template first, unless the fix is a
  one-liner.
- For a new feature or a new harness, open a feature request first so we can
  agree it fits. A behaviour nobody asked for counts as a bug here, and deleting
  code is preferred to adding a flag.
- Security problems go through [SECURITY.md](SECURITY.md), not public issues.

## Never commit real session data

Your agent history can hold source code, credentials and private conversations.

- Fixtures must be synthetic. Write them by hand or generate them; never copy
  them from your own `~/.claude`, `~/.codex`, `~/.cursor` or similar.
- When you try mss on a branch, point it at a throwaway home so it cannot read
  your real history, for example:

  ```sh
  mkdir -p /tmp/mss-home
  HOME=/tmp/mss-home XDG_CONFIG_HOME=/tmp/mss-home/.config ./mss index
  ```

- Check screenshots, logs and `mss doctor` output for real paths, user names and
  project names before sharing. Public issue feedback must use synthetic
  reproductions, not real private transcripts, databases, or credentials.

## Build and test

You need Go 1.25 or newer. `sqlite3` and `zstd` are optional; tests that need
them skip when they are missing.

```sh
make build            # writes ./mss
gofmt -l .            # must print nothing
go vet ./...          # must be clean
go test ./...         # the full suite
```

CI is configured to build, vet, test, and check formatting on macOS, Linux, and
Windows. Configuration alone is not evidence: main `1db0ab6` passed
[three-platform CI and native CLI smoke](https://github.com/henryyu333/mss/actions/runs/37793748699).
That does not certify every architecture or external user's environment.

Some tests enforce contracts between code and docs; [AGENTS.md](AGENTS.md)
lists them. In short:

- `docs/registry/registry.json` must match the loaders in
  `internal/sources/registry.go`, and every fixture it names must exist.
- `docs/ARCHITECTURE.md` keeps one parser-table row per harness.
- The JSON emitted by `search`, `show` and `last` must match
  `docs/json-output.md`.

Optional verification tools do not become runtime dependencies:

- Python's standard library runs `tools/test_verify_release.py` and the versioned
  release validator described in [installation](docs/install.md#maintainer-release-gate).
  Release checks must use the exact tag commit and its immutable artifact, not
  a different branch build; checksums, archive contents, embedded Skill assets,
  version output, and real host installation are separate gates.
- The real-host offline Codex/Pi probes in
  [Skill compatibility](docs/skill-compatibility.md) need the stated host
  versions and Python/Node. They do not prove interactive model execution.
- [Synthetic benchmarks](docs/benchmarks.md) document reproducible corpus,
  search, miss, and citation checks. Record actual environment and measured
  output; never substitute upstream numbers or infer real-history accuracy.
- [First-user acceptance](docs/first-user-checklist.md) remains a genuine
  external-human check, not a claim made by fixtures or offline probes.

Missing optional tools and skipped tests must be reported as coverage gaps,
not passed gates. Publishing a release or changing the external Homebrew tap
requires separate maintainer authorization.

## Style

- Comments explain why, not what.
- `#NNNN` references in code comments (for example `#1692`) are issue numbers
  from the upstream [deja-vu](https://github.com/vshulcz/deja-vu) project that
  mss is derived from, not issues in this repository.
- User-facing messages are lowercase sentences that name the exact command to
  run next.
- Keep commits focused, with a subject line like `search: say "1 match"`.
- Add a line under `Unreleased` in [CHANGELOG.md](CHANGELOG.md) for anything a
  user would notice.

## Pull requests

Fill in the pull request template. A maintainer reviews every change before it
is merged. By contributing you agree that your work is released under the
project's [MIT license](LICENSE).
