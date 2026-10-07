<h1 align="center">mss</h1>

<p align="center">
  <strong>No memory system needed. Just recall your AI coding history.</strong>
</p>

<p align="center">
  <a href="https://github.com/henryyu333/mss/releases/latest"><img src="https://img.shields.io/github/v/release/henryyu333/mss?style=flat-square" alt="Release"></a>
  <a href="https://github.com/henryyu333/mss/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/henryyu333/mss/ci.yml?branch=main&label=CI&style=flat-square" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/henryyu333/mss?style=flat-square" alt="License: MIT"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux-007AFF?style=flat-square" alt="Platform: macOS | Linux">
  <img src="https://img.shields.io/badge/Windows-untested-lightgrey?style=flat-square" alt="Windows: untested">
</p>

<p align="center">
  English · <a href="README.zh-CN.md">简体中文</a>
</p>

---

**mss replaces a memory system for coding agents.** Claude Code, Codex, Cursor,
opencode and others already write every session to your disk. mss indexes those
transcripts, and when you type `/mss <question>` in your agent, the skill
recalls the relevant past-session history and summarizes it into the current
conversation — each point backed by the session id, the date and a verbatim
quote.

No memory system needed: just recall your history directly. There are no notes
to curate, nothing writes "memories" behind your back, and nothing is injected
into every turn. The history is already there; mss finds the right part of it
when you ask, and does nothing the rest of the time.

<p align="center">
  <img src="docs/assets/demo.png" alt="mss searching made-up sample sessions for &quot;connection pool exhausted&quot; and reading one of them" width="820">
</p>
<p align="center"><sub>Screenshot made from made-up sample sessions.</sub></p>

```sh
mss index                                   # refresh the local index
mss "connection pool exhausted"             # where did we hit this before?
mss show 7f3a9c21 --around 3 --brief        # read that session around the match
```

### Local only · No daemon · No MCP · Manual recall

- **Local only** — reads files already on your machine; no network, no telemetry, no account. See [Privacy](#privacy).
- **No daemon** — nothing runs between invocations. No watcher, no hooks, no background writes.
- **No MCP** — a plain CLI any agent can call through its shell; nothing to register or keep alive.
- **Manual recall** — the skill runs only when you type `/mss`. A miss says *nothing found*, never a near-miss dressed up as one.

### Why recall instead of a memory system

| | Memory systems | **mss** |
| --- | --- | --- |
| Where the knowledge comes from | notes an agent decided to write | **the sessions you already had** |
| Upkeep | curate, prune, fix stale notes | **none — the index is a rebuildable cache** |
| When it runs | every turn, automatically | **only when you type `/mss`** |
| What reaches the conversation | injected notes, every turn | **a summary of the relevant history, when asked** |
| What backs it up | the note's own wording | **session id, date and verbatim quotes** |

mss is one binary and one skill, and stays that way:

- **the binary** is the search engine;
- **[the skill](skills/mss/SKILL.md)** turns `/mss <question>` into a recall: it
  refreshes the index once, searches, reads the matching sessions, and
  summarizes what they say into the current conversation with citations — and
  says plainly what it could not find or could not cover.

## Install

**Homebrew** (macOS, Linux)

```sh
brew install henryyu333/tap/mss
```

**Prebuilt binary** — download the archive for your OS from the
[latest release](https://github.com/henryyu333/mss/releases/latest), unpack it,
and put `mss` on your `PATH`. Each archive also carries the skill
(`skills/mss/SKILL.md` in English, `skills/mss/SKILL.zh-CN.md` in Chinese).
Windows archives are built but untested.

**From source** (Go 1.25+)

```sh
go install github.com/henryyu333/mss/cmd/mss@latest
```

Runtime tools: stores kept in SQLite (opencode, Cursor, Grok) are read through
the `sqlite3` CLI, and zstd-compressed transcripts (newer Codex rollouts,
DeepSeek Harness) through the `zstd` CLI. macOS ships `sqlite3`; `brew install
zstd` covers the other. Without them mss still runs and `mss doctor` names the
stores it could not read.

## Install the skill

The skill is what turns `/mss` into a recall. It comes in two languages with the
same behaviour; install **one** of them as `SKILL.md` in your agent's skills
directory. For Claude Code:

```sh
mkdir -p ~/.claude/skills/mss

# English
curl -fsSL https://raw.githubusercontent.com/henryyu333/mss/main/skills/mss/SKILL.md \
  -o ~/.claude/skills/mss/SKILL.md

# or Chinese (still saved as SKILL.md)
curl -fsSL https://raw.githubusercontent.com/henryyu333/mss/main/skills/mss/SKILL.zh-CN.md \
  -o ~/.claude/skills/mss/SKILL.md
```

Other agents that read `SKILL.md` skills take the same file in their own skills
directory. Then ask for history explicitly: `/mss why did we drop the redis
cache`. The skill never fires on ordinary conversation, even when it says
"before" or "we discussed".

## Commands

| Command | What it does |
| --- | --- |
| `mss index [--rebuild] [--quiet]` | Build or incrementally update the index |
| `mss [search] [flags] <query>` | Search; `--json`, `--harness`, `--project`, `--since`, `--role`, `--session`, `--limit`, `--all`, `--re`, `--no-refresh` |
| `mss search --sessions --json <query>` | Every matching session as metadata only — hit count and the record positions that matched — capped at 500 rows; `--sort updated` orders them newest-first; `--exclude <id>` and `--exclude-self <nonce>` drop a session together with its subagents and forks |
| `mss show <id-prefix>` | Read one session's transcript, by the id a hit prints; `--around <n>` centres the window on record `n`; `--brief` prints one header per message (index, role, time) with the body cut short, for scanning; `--no-refresh` reads the index as it was instead of refreshing first |
| `mss ctx <query\|id-prefix>` | A larger window around the best match, for pasting into a conversation |
| `mss last [n]` | The most recently updated sessions |
| `mss sources` | Every store mss looks at, with session and message counts |
| `mss doctor [--json] [--deep]` | What is installed, what it found, and what it could not read |
| `mss version` | Build identity |

Every result carries a `tier` — `exact`, plus `close`/`stemmed` for
spelling-corrected and word-form matches; `relevance` for a nearest-neighbour
ranking (no real match); `error` for signature matches — so a caller never
mistakes a neighbour for a real hit. A true miss is `tier: "exact", total: 0`.
`--json` returns the same envelope a script can parse; the schema is in
[`docs/json-output.md`](docs/json-output.md).

## Supported harnesses

| Harness | Where sessions live |
| --- | --- |
| Claude Code | `~/.claude/projects/**/*.jsonl` |
| Codex CLI | `~/.codex/sessions/**/rollout-*.jsonl(.zst)`, `~/.codex/history.jsonl` |
| opencode | `~/.local/share/opencode/opencode.db` |
| Cursor | Cursor's `state.vscdb` stores and CLI transcripts under `~/.cursor` |
| Grok Build | `~/.grok/sessions/**/updates.jsonl`, `~/.grok/grok.db` |
| pi | `~/.pi/agent/sessions/**/*.jsonl` |
| omp | `~/.omp/agent/sessions/**/*.jsonl` |
| DeepSeek Harness | `~/.dsh/sessions/*/session-*/session*.jsonl(.zstd)` |

Each store can be pointed elsewhere with its own `MSS_…_ROOT` variable, and
`MSS_STORES=claude,pi` limits a run to the stores named. Recognized formats and
their fixtures are described in [`docs/registry/`](docs/registry/).

## How it works

- **The index is a cache.** `mss index` reads session files, stores a redacted
  copy in `~/.cache/mss/index.db`, and updates incrementally: a transcript that
  only grew is read from its last safe offset, a changed store replaces the
  sessions it touched. Deleting the index costs a rebuild, nothing else.
- **Secrets are redacted on the way in.** API keys, tokens and password-shaped
  strings are replaced during ingest, so `show` and `ctx` never hand them back.
  `mss doctor` reports what a store yielded, including lines it could not read.
- **Session files are never written.** They stay where each harness put them;
  mss only reads them.

Useful environment variables: `MSS_INDEX_DIR` (index location),
`MSS_STORES` (stores to read), `MSS_STORE_TIMEOUT` (per-store read budget),
`MSS_INCLUDE_SUBAGENTS`, `MSS_EXCLUDE_PROJECTS`, `MSS_NO_REDACT`, plus the
per-harness root overrides above.

Internals are described in [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## Privacy

- **It runs locally.** mss reads session files already on your disk and writes
  only its own index. It opens no network connections and has no telemetry,
  analytics or update check. The only programs it starts are the local
  `sqlite3`, `zstd` and `git`.
- **The index is redacted plaintext.** It is the directory
  `~/.cache/mss/index.db`, with a lock file `~/.cache/mss/index.db.lock` beside
  it (or `$MSS_INDEX_DIR` and `$MSS_INDEX_DIR.lock` if you set that variable).
  API keys, tokens and password-shaped strings are replaced while indexing;
  everything else is stored as plain text, not encrypted, and protected only by
  file permissions (directory `0700`, files `0600`). Redaction is
  pattern-based, so a secret in an unusual format can slip through.
- **What you recall reaches your agent's model.** When `/mss` runs, the
  recalled history goes into the agent's conversation, so it is sent to the
  model provider that agent uses — usually a cloud service — like anything else
  in the chat. mss itself sends nothing anywhere.
- **Turning redaction off.** `MSS_NO_REDACT=1` stores text unredacted, and it
  takes effect only on a full rebuild: `MSS_NO_REDACT=1 mss index --rebuild`.
  The unredacted copy stays until you rebuild without it: `mss index --rebuild`.
- **Keeping projects out.** Project patterns in `~/.config/mss/exclude` (one
  per line; a `harness:<name>` line skips a whole store) or in
  `MSS_EXCLUDE_PROJECTS` keep those sessions out of the index; run
  `mss index --rebuild` to drop sessions that were indexed before.
- **Deleting the index.** `rm -rf ~/.cache/mss` (or `rm -rf "$MSS_INDEX_DIR"
  "$MSS_INDEX_DIR.lock"`). It is only a cache: your session files are not
  touched, and the next `mss index` builds it again.

See [`SECURITY.md`](SECURITY.md) to report a vulnerability or a secret that
redaction missed.

## License

MIT. See [`LICENSE`](LICENSE).
