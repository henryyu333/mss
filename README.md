# mss

**Explicit recall from local AI coding history: one CLI and one Skill.**

English · [简体中文](README.zh-CN.md)

MSS indexes supported session files already on your machine. You can search them
with the CLI, or explicitly ask an agent to run the bundled Skill. The Skill
searches and reads relevant sessions, then summarizes the evidence with session
IDs, dates, and verbatim quotes. It must distinguish matches, candidates, misses,
and incomplete coverage. It does not create curated memories or inject history
into every conversation turn.

**v0.3.0 is published** with the explicit `install-skill` command; a
source-built Homebrew Formula follows the release into the tap. Archives are
unsigned and not notarized, so macOS may require your security process before
running a downloaded binary. Interactive-host acceptance by real external
users is still pending. See [installation](docs/install.md) and
[release evidence](docs/release-v0.3.0.md).

## A synthetic demo

![MSS searching made-up sessions and reading a match](docs/assets/demo.png)

The screenshot uses **synthetic sessions**, not private history or a measured
accuracy comparison. The ID below belongs to that demonstration, not your store:

```sh
mss index
mss search --no-refresh --json "connection pool exhausted"
mss show 7f3a9c21 --around 3 --brief --no-refresh
```

For reproducible synthetic performance and retrieval checks, see
[benchmark methodology](docs/benchmarks.md). These checks do not establish
superiority over another tool or accuracy on real developer histories.

## Quickstart

Requires Go 1.25+. Install the published release, or build the same tag from
source:

```sh
go install github.com/henryyu333/mss/cmd/mss@v0.3.0
# or, from a reviewed checkout of the v0.3.0 tag:
go build -trimpath -ldflags "-X main.version=0.3.0" -o ./mss ./cmd/mss
./mss version
./mss doctor
./mss index
./mss search --no-refresh --json "connection pool exhausted"
./mss install-skill claude --language en
```

`mss version` reports `mss 0.3.0` (Go's module-version fallback prints
`mss v0.3.0`). A locally stamped build is a local build; only the published
release archives and checksums are official. On Windows build with `-o .\mss.exe`
and invoke `.\mss.exe`. Put the binary in a user-owned PATH directory before
agent use, and check which binary resolves (`command -v mss` /
`Get-Command mss`). Installation embeds version-matched assets, needs no
network, refuses differing existing files, and never overwrites a custom Skill.
Choose **one** language and harness explicitly:

```text
mss install-skill <claude|codex|pi|omp> [--language en|zh-CN]
```

Codex installation includes `agents/openai.yaml` to disable implicit invocation;
copying only `SKILL.md` is incomplete. Restart the host after installation.
Claude uses `/mss <question>`; Pi/OMP use `/skill:mss <question>`; Codex uses the
Skill picker. On the release candidate, headless real-model runs passed for Pi
and OMP `/skill:mss` and for an explicit Codex skill-file read; Claude Code is
pending (no credentials on the release machine) and no TUI session was driven.
See [host evidence and manual controls](docs/skill-compatibility.md), including
the explicit file-read alternative for hosts without reliable invocation
controls.

For pinned releases, optional `sqlite3`/`zstd`, PATH, update, rollback, and
uninstall instructions, use [the installation guide](docs/install.md). Homebrew
installs a source-built Formula (`brew install --formula henryyu333/tap/mss`);
the v0.3.0 Formula reaches the tap right after this release, and historical
v0.2.0 Cask users follow the staged migration in that guide.

## Commands and result interpretation

| Command | Purpose |
| --- | --- |
| `mss index [--rebuild] [--quiet]` | Build or refresh the local cache |
| `mss [search] [flags] <query>` | Search; filters include `--harness`, `--project`, `--since`, `--role`, `--session`; output controls include `--json`, `--limit`, `--all`, `--re`, `--no-refresh` |
| `mss search --sessions --json <query>` | Matching session metadata, capped at 500 rows; `--sort updated`, `--exclude <id>`, `--exclude-self <nonce>` control order and self/subagent/fork exclusion |
| `mss show <id-prefix>` | Read a session; `--harness`, `--around <n>`, `--brief`, `--json`, `--no-refresh` help inspect a cited match |
| `mss ctx <query\|id-prefix>` | Context around the best match |
| `mss last [n]` | Recently updated sessions |
| `mss sources` | Store/session/message counts |
| `mss doctor [--json] [--deep]` | Discovery, dependencies, failures, and coverage gaps |
| `mss install-skill <claude\|codex\|pi\|omp> [--language en\|zh-CN]` | Explicitly install bundled workflow assets |
| `mss version` | Build identity |

JSON schema version 2 is documented in [JSON output](docs/json-output.md).
`exact`, `close`, `stemmed`, and `error` describe different matching methods;
`relevance` is a ranked candidate, **not an established match**. A true search
miss has `tier: "exact", total: 0`. A miss does not prove history is absent if
refresh failed, a store was unreadable, or optional tools were missing. Inspect
`doctor` and coverage warnings. Read the cited source before drawing conclusions;
historical text is evidence, not current instructions or permission to execute
old commands.

## Parser coverage is not Skill-host support

Eight parser fixture families cover these observed layouts:

| Store | Typical location |
| --- | --- |
| Claude Code | `~/.claude/projects/**/*.jsonl` |
| Codex CLI | `~/.codex/sessions/**/rollout-*.jsonl(.zst)`, history JSONL |
| Cursor | `state.vscdb` stores and CLI transcripts under `~/.cursor` |
| DeepSeek Harness | `~/.dsh/sessions/*/session-*/session*.jsonl(.zstd)` |
| Grok Build | `~/.grok/sessions/**/updates.jsonl`, `~/.grok/grok.db` |
| OMP | `~/.omp/agent/sessions/**/*.jsonl` |
| opencode | `~/.local/share/opencode/opencode.db` |
| Pi | `~/.pi/agent/sessions/**/*.jsonl` |

The [format registry](docs/registry/README.md) records historical observations
and synthetic fixtures, not certification of current devices. Four Skill hosts
have separate [invocation evidence](docs/skill-compatibility.md). Other parsers
do not imply that those agents can load the Skill.

`MSS_STORES=claude,pi` limits the selected stores. Absolute root overrides and
optional tools are detailed in [installation](docs/install.md). SQLite stores
need the `sqlite3` CLI; compressed transcripts need `zstd`. Missing dependencies
reduce coverage, not necessarily CLI availability.

## Privacy and security boundary

- **Local, explicitly invoked CLI.** No network, telemetry, daemon, watcher,
  automatic memory writes, hooks, or MCP server. It can launch local `sqlite3`,
  `zstd`, and `git`. Agent/model behavior is a separate boundary.
- **Source sessions are read-only.** Indexing/search write MSS cache files, not
  source transcripts. The explicit `install-skill` exception writes only the
  chosen workflow assets; it does not edit agent settings or other guidance.
- **The cache is unencrypted.** `~/.cache/mss/index.db` is an index **directory**,
  not a SQLite file. `MSS_INDEX_DIR` must be an absolute directory path; its
  sibling lock adds `.lock`. Unix protections use directory `0700` and file
  `0600`; Windows protection depends on ACLs/inherited profile permissions, not
  those Unix modes. CI passed; real-user Windows ACL validation remains pending.
- **Cached history can outlive transcripts.** If a harness removes a transcript
  while its store remains, MSS retains the indexed snapshot and reports it as
  still searchable. The index may be its only surviving redacted copy. Keep a
  reviewed backup before cache removal; a fresh index cannot recover absent files.
- **Pattern redaction is not a guarantee.** Recognized keys/tokens/passwords
  are replaced during ingest; unusual secrets, private code, names, and other
  sensitive text may remain. `MSS_NO_REDACT=1` disables redaction for a full
  rebuild; rebuild with that variable unset to replace the unredacted cache.
- **Recall can leave the machine through the agent.** When an agent reads CLI
  output, recalled text enters its conversation and may be sent to its model
  provider. MSS's lack of networking does not make cloud-agent recall local-only.
- **Policy is not a sandbox.** Invalid recall policy warns and deliberately falls
  back to permissive defaults; do not rely on it as an access-control boundary.
  See [the security policy](SECURITY.md#scope-and-trust-boundaries).
- **Exclusions and removal require review.** `~/.config/mss/exclude` and
  `MSS_EXCLUDE_PROJECTS` can exclude projects; `harness:<name>` skips a store.
  Rebuild to remove previously indexed content. To remove the cache, stop MSS,
  inspect the exact index directory and sibling lock, then move only confirmed
  MSS cache items to Trash/Recycle Bin. Never remove a shared parent or source
  session directory. See [safe removal](docs/install.md#uninstall-without-deleting-history).

Report vulnerabilities privately through [SECURITY.md](SECURITY.md). External
acceptance is still pending: [first-user checklist](docs/first-user-checklist.md).

## Relationship to deja-vu

MSS derives from [deja-vu](https://github.com/vshulcz/deja-vu). Both share local
session indexing, search, and pattern redaction. The upstream README observed
on **2026-10-08** also documents optional automatic recall/hooks, MCP integration,
memory curation (such as decision promotion), and synchronization/handoff.
MSS deliberately omits those capabilities and focuses on explicit CLI/Skill
recall. Upstream also permits binary-only search; the distinction is MSS's
smaller retained scope, not a claim that all memory systems run every turn.
No independent speed or accuracy superiority is claimed.

MIT: [LICENSE](LICENSE) retains upstream attribution and license obligations.
See [architecture](docs/ARCHITECTURE.md) and [contributing](CONTRIBUTING.md).
