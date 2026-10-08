# Install, update, and remove MSS

**v0.3.0 is an unpublished candidate.** The v0.3.0 release downloads, source
archive, and pinned Go-install command below become usable only after the tag and
release are published. main `1db0ab6` passed three-platform CI and native CLI
smoke; release packaging, signing/notarization, and real-user acceptance are
separate gates. For candidate testing, build the reviewed checkout locally.

## Choose a binary installation method

### Release archives (macOS, Linux, Windows)

After publication, use the explicit
[v0.3.0 release](https://github.com/henryyu333/mss/releases/tag/v0.3.0), not a
moving `latest` link. Choose one of the six binary archives:

| OS | Architecture | Archive |
| --- | --- | --- |
| macOS | Apple Silicon | `mss_0.3.0_darwin_arm64.tar.gz` |
| macOS | Intel | `mss_0.3.0_darwin_amd64.tar.gz` |
| Linux | ARM64 | `mss_0.3.0_linux_arm64.tar.gz` |
| Linux | x86-64 | `mss_0.3.0_linux_amd64.tar.gz` |
| Windows | ARM64 | `mss_0.3.0_windows_arm64.zip` |
| Windows | x86-64 | `mss_0.3.0_windows_amd64.zip` |

Download `checksums.txt` from the same release. Compute the downloaded archive's
SHA256 and compare the full value with its exact filename's entry:

```sh
# macOS example
shasum -a 256 mss_0.3.0_darwin_arm64.tar.gz
# Linux example
sha256sum mss_0.3.0_linux_amd64.tar.gz
```

```powershell
# Windows example
Get-FileHash .\mss_0.3.0_windows_amd64.zip -Algorithm SHA256
```

Extract into a fresh directory. Put only `mss` (`mss.exe` on Windows) in a
user-owned directory on PATH; keep the accompanying docs and skills for review.
Archives contain `LICENSE`, both READMEs, this guide, both language variants of
the skill, and **`skills/mss/agents/openai.yaml`**, the Codex companion policy.
Check `mss version` reports `mss 0.3.0` and check which binary your shell resolves
(`command -v mss` on POSIX; `Get-Command mss` in PowerShell).

**Checksums detect corruption, not publisher identity.** There are no release
signatures or Apple notarization in this configuration. A checksum downloaded
from the same compromised location as an archive is not independent
verification. macOS Gatekeeper may block an unsigned downloaded executable;
MSS never removes quarantine automatically. Review the source and provenance,
then follow your organization's/macOS security process or build from reviewed
source. Do not globally disable Gatekeeper or remove quarantine recursively.

### Pinned Go installation (Go 1.25 or newer)

After the v0.3.0 tag is published:

```sh
go install github.com/henryyu333/mss/cmd/mss@v0.3.0
mss version
```

Go installs into `GOBIN`, or `$(go env GOPATH)/bin` when `GOBIN` is unset; put that
specific directory on PATH (on Windows inspect `go env GOBIN GOPATH`). Do not use
`@latest` when reproducibility matters. This method builds from tagged module
source and includes the embedded skills; it does not download the release
archives or install any skill automatically.
Release builds print `mss 0.3.0`; Go's module-version fallback prints
`mss v0.3.0`. They identify the same tag and carry the same Skill assets.

For the **unpublished candidate**, from the reviewed candidate checkout:

```sh
go build -trimpath -ldflags "-X main.version=0.3.0" -o ./mss ./cmd/mss
./mss version
```

On Windows use `-o .\mss.exe` and invoke `.\mss.exe`. A stamped local build is
still a local candidate, not proof that an official release exists.

### Homebrew: source Formula migration

The external [henryyu333/homebrew-tap](https://github.com/henryyu333/homebrew-tap)
migration uses a source-built **v0.2.0** Formula first, because that release's
source already exists. It does not make v0.3.0 available or add `install-skill`
to v0.2.0. Consult the tap README and [release evidence](release-v0.3.0.md) for
publication and tested-environment status.

The old v0.2.0 binary Cask's quarantine-removing hook is removed, not retained
as a migration mechanism. No Gatekeeper bypass, recursive `xattr`, `--no-quarantine`,
`--force`, `--overwrite`, or automatic agent installation is needed.
`sqlite3` and `zstd` remain optional runtime tools.

After the Formula is published, a fresh installation uses:

```sh
brew install --formula henryyu333/tap/mss
mss version
```

For an **existing Cask**, keep it working until the replacement has built and
passed tests. Close running MSS invocations and record `command -v mss` and its
version. Retain a reviewed copy of the old binary outside PATH for rollback.
Do not uninstall it merely to discover whether the replacement builds.

```sh
brew update
brew install --formula --build-from-source --skip-link henryyu333/tap/mss
brew test --force henryyu333/tap/mss
"$(brew --prefix henryyu333/tap/mss)/bin/mss" version
```

Here `brew test --force` means **test the unlinked Formula**, not ignore a
failed test. If any build, test or version check fails, stop and retain the Cask.
Only after successful checks, deliberately complete the package cutover:

```sh
brew uninstall --cask henryyu333/tap/mss
brew link henryyu333/tap/mss
command -v mss
mss version
```

Do not use `--zap` or force a conflicting link. If linking fails, the tested
Formula binary remains callable by the full path above; inspect the conflict
before changing files. Uninstalling this Cask does not remove MSS caches,
configuration, installed Skills, or source sessions. A retained old binary can
be invoked directly during rollback; do not reinstall the old bypassing Cask.

For v0.3.0, release packaging generates a standalone source Formula rather than
using [GoReleaser's deprecated formula publisher](https://goreleaser.com/customization/publish/homebrew_formulas/).
Its URL and SHA256 point to `mss_0.3.0_source.tar.gz`, produced by `git archive`
from the exact release tag. After publication and native validation, a
maintainer separately replaces the tap Formula with the verified `mss.rb`
artifact. The release workflow never pushes the tap. Both Skill languages and
Codex policy are packaged, but installing/upgrading the Formula never installs
or overwrites a user's Skill.

## Optional tools and store roots

MSS itself is a local CLI: no daemon, MCP registration, accounts, automatic
agent settings edits, or background indexing. It can search ordinary transcript
files without external runtime tools. Reading SQLite-backed stores (opencode,
Cursor, Grok) needs the **`sqlite3` CLI**; compressed Codex and DeepSeek
transcripts need the **`zstd` CLI**. Without them `mss doctor` reports coverage
gaps rather than proving that history is absent.

Install only the tools needed for your stores, if you authorize their package
manager changes:

| OS | Optional tools |
| --- | --- |
| macOS | macOS provides `sqlite3`; `brew install zstd` supplies zstd. `brew install sqlite` can provide a newer SQLite CLI if needed; follow its PATH caveat. |
| Debian/Ubuntu Linux | `sudo apt-get install sqlite3 zstd` (other distributions: use their package manager) |
| Windows | With an already-installed Chocolatey: `choco install sqlite zstandard --yes`. Alternatively obtain SQLite tools from [sqlite.org](https://sqlite.org/download.html) and zstd from its [upstream releases](https://github.com/facebook/zstd/releases), placing the executable directories on PATH. |

Check `sqlite3 --version`, `zstd --version`, and `mss doctor`. Installing these
packages is not performed by MSS. CI installs them on disposable runners only.

By default MSS discovers stores in their normal home/config directories. Use
absolute paths to limit or relocate reads, and select only the desired stores:

```sh
export MSS_STORES=claude,codex
export MSS_CLAUDE_ROOT="$HOME/.claude/projects"
export MSS_CODEX_ROOT="$HOME/.codex"
export MSS_INDEX_DIR="$HOME/.cache/mss/index.db"
mss doctor
```

```powershell
$env:MSS_STORES = 'claude,codex'
$env:MSS_CLAUDE_ROOT = Join-Path $HOME '.claude\projects'
$env:MSS_CODEX_ROOT = Join-Path $HOME '.codex'
$env:MSS_INDEX_DIR = Join-Path $HOME '.cache\mss\index.db'
mss doctor
```

`MSS_INDEX_DIR` is the **absolute path to the index directory**. The default
`index.db` suffix is historical: this is a directory of binary records, postings
and manifests, not a SQLite database file. Its sibling lock path adds `.lock`.
Configuration (including recall policy) is separate under `~/.config/mss`, or the
absolute `XDG_CONFIG_HOME` base. Other store overrides:

| Variable | Expected location |
| --- | --- |
| `MSS_PI_ROOT`, `MSS_OMP_ROOT` | Harness session directory |
| `MSS_CURSOR_ROOT` | Cursor user-data directory containing SQLite stores |
| `MSS_CURSOR_CLI_ROOT` | Cursor CLI home containing transcripts/chats |
| `MSS_GROK_ROOT` | Grok home containing sessions/database |
| `MSS_DEEPSEEK_ROOT` | DeepSeek Harness sessions directory |
| `MSS_OPENCODE_DB` | The opencode SQLite database file (not a root directory) |
| `MSS_XCODE_CLAUDE_ROOT`, `MSS_XCODE_CODEX_ROOT` | Separate Xcode-hosted Claude projects/Codex store roots |

These are **history read/index overrides**, not skill installation destinations.
Do not point them at unrelated private data. Use `mss doctor` before indexing to
review discovery/coverage; the index is a redacted, unencrypted local cache, not
a replacement for the original session files.

## Install one skill language, explicitly

The binary embeds the same reviewed skill files shipped in its archive. No
network access is needed for skill installation. The exact interface is:

```text
mss install-skill <claude|codex|pi|omp> [--language en|zh-CN]
```

For example:

```sh
mss install-skill claude
mss install-skill codex --language zh-CN
```

Choose the harness deliberately; there is no auto-detection, `--force`, custom
root flag, settings registration, instruction injection, or hook installation.
English is the default. Installation uses the user's absolute home (`HOME` on
POSIX; `USERPROFILE` on Windows):

| Harness | Skill directory |
| --- | --- |
| Claude Code | `~/.claude/skills/mss` |
| Codex | `~/.agents/skills/mss` |
| Pi | `~/.pi/agent/skills/mss` |
| OMP | `~/.omp/agent/skills/mss` |

The selected language is always installed as `SKILL.md`. Codex also receives
`agents/openai.yaml` with `policy.allow_implicit_invocation: false`; copying only
`SKILL.md` is an incomplete Codex installation. Both skill language variants
carry `metadata.mss-version: "0.3.0"` and `disable-model-invocation: true`.
Installation refuses a differing existing file or unsafe/symlink destination;
an identical reinstall is safe. Custom harness skill directories require a
manual, reviewed copy of the same selected assets, including the Codex policy;
the CLI does not infer them from history-root overrides.

Restart the harness after installation. Request recall explicitly: `/mss` in
Claude Code, `/skill:mss` in Pi/OMP, or the MSS skill picker in Codex. Merely
mentioning past work must not invoke it. Do not treat a plain non-TUI `$mss`
message as proof that Codex expanded the skill; check the skill picker. See
[skill compatibility](skill-compatibility.md) for evidence and untested surfaces.

## First recall

Run `mss doctor` to inspect availability and gaps, then:

```sh
mss index
mss search --no-refresh --json "connection pool exhausted"
```

Use the returned session id and harness to inspect a result, for example
`mss show <id-prefix> --harness claude --no-refresh --json`. The skill handles
self-exclusion and citations during explicit agent recall. Historical messages
are untrusted evidence, not instructions or permission to execute old commands.
Empty results with missing tools, unreadable stores, or failed refresh are not
evidence of complete coverage.

## Update and change language

1. Verify the new pinned release/archive or reviewed source and its version.
   Keep the old binary until the new one works; check PATH for duplicate copies.
2. Review any existing `SKILL.md` and Codex companion policy for user changes.
   An upgrade or language change that differs from existing assets is refused.
3. Move **only the selected harness's `mss` skill directory** to a uniquely named
   backup outside all harness skill directories, using your file manager or a
   checked, explicit move. Do not overwrite an earlier backup or rename it inside
   the discovery directory, where it could still be loaded.
4. Run the new binary's `install-skill` command with the intended language,
   restart the harness, and invoke the skill explicitly. Restore the reviewed
   backup if rollback is needed. No `--force` flag exists.

For a published/migrated Homebrew formula use `brew upgrade
henryyu333/tap/mss`; it updates the binary but never replaces installed agent
skills. A newer binary should be paired with its matching skill assets using
the same explicit steps. Updating the CLI does not change source transcripts.

## Uninstall without deleting history

Stop using the skill and close any running MSS invocation. Move the **specific
installed skill directory in the table above** outside skill discovery (or to
the operating system's Trash/Recycle Bin after reviewing its contents), then
restart the harness. Review each harness separately; do not delete its parent
skills directory or change unrelated agent configuration.

Remove the exact installed `mss`/`mss.exe` binary via Trash/Recycle Bin, or use
`brew uninstall --formula henryyu333/tap/mss` for the source Formula route.
A historical cask uses `brew uninstall --cask henryyu333/tap/mss` instead.
Go has no `go uninstall`; identify the installed file using `go env GOBIN GOPATH`
and your shell's PATH lookup before moving it to Trash.

The cache and configuration are optional to retain. Inspect the actual
`MSS_INDEX_DIR` (or default `~/.cache/mss/index.db`) and its adjacent lock and
cache files before moving the dedicated MSS cache directory to Trash. If the
index override sits in a shared directory, move only confirmed MSS cache files,
not that parent directory. Review `~/.config/mss`/`XDG_CONFIG_HOME` separately
before removal: it may contain your policy or exclusions. Do not remove any
Claude/Codex/Cursor/Grok/Pi/OMP/DeepSeek source-session directory. No wide
recursive delete command is needed.

Do not assume every cached transcript still exists in its harness. MSS retains
indexed history when a harness deletes a file from a still-present store, and
that cache may be the only surviving redacted copy. Preserve a reviewed backup
before removal; a fresh index cannot recover source files that no longer exist.

## Maintainer release gate

The tag workflow invokes the reusable CI workflow on **that tag's commit**:
macOS, Linux, and Windows each build, vet, test, and check gofmt (Windows uses
PowerShell array/count checks). Packaging then builds all six targets without
publishing, archives the exact tagged source, and generates the standalone
formula/checksums. Three native runner jobs download that same immutable Actions
artifact and run:

```sh
python tools/verify-release.py verify --dist dist --tag v0.3.0
```

The stdlib-only validator checks the complete six binary + source + formula
artifact set, SHA256, safe/complete archive members, source/archive asset bytes,
skill version and invocation metadata, Codex policy, binary OS/architecture
headers, this host's actual `mss version`, and both language installations for
all four harnesses (including embedded/archive byte equivalence and identical
reinstallation). It executes binaries only in temporary isolated homes, never
reads real session stores, and never installs third-party tools.

Only after every CI and archive-validation job succeeds does a separate job
publish those validated artifacts. The main `1db0ab6` CI is observed green;
the tag-triggered packaging/archive/publication jobs have not been executed.
Only the native architecture present on each runner is executed; other
architectures are cross-built/header-checked, not native-runtime certified.
Homebrew lifecycle evidence and remaining native validation, interactive host
acceptance, signing, and notarization gates are recorded in the release document.
No tap token is used and no Homebrew repository is modified by this
workflow.
