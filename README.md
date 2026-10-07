<h1 align="center">mss</h1>

<p align="center">
  <strong>Search your AI coding history. On demand.</strong>
</p>

<p align="center">
  <a href="https://github.com/henryyu333/mss/releases/latest"><img src="https://img.shields.io/github/v/release/henryyu333/mss?style=flat-square" alt="Release"></a>
  <a href="https://github.com/henryyu333/mss/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/henryyu333/mss/ci.yml?branch=main&label=CI&style=flat-square" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/henryyu333/mss?style=flat-square" alt="License: MIT"></a>
  <img src="https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-007AFF?style=flat-square" alt="Platform">
</p>

<p align="center">
  English · <a href="README.zh-CN.md">简体中文</a>
</p>

---

**mss is an on-demand history recall engine for coding agents.** It indexes the
sessions Claude Code, Codex, Cursor, opencode and others already leave on your
disk, and hands back the exact turns — verbatim — when someone asks.

History is not memory. mss never remembers on its own, never recalls on its
own, and never puts anything into a context nobody asked for. When an agent
needs the past, it searches precisely. The rest of the time, mss does nothing.

```sh
mss index                                   # refresh the local index
mss "connection pool exhausted"             # where did we hit this before?
mss show 01a00feb --around 42 --brief       # read that session around the match
```

### Local only · No daemon · No MCP · Explicit recall

- **Local only** — reads files already on your machine; no network, no upload, no account.
- **No daemon** — nothing runs between invocations. No watcher, no hooks, no background writes.
- **No MCP** — a plain CLI any agent can call through its shell; nothing to register or keep alive.
- **Explicit recall** — it searches only when a person or a script asks. A miss says *no match*, never a near-miss dressed up as one.

### Not memory, not a session manager

| | Memory systems | Session managers | **mss** |
| --- | --- | --- | --- |
| Who it is for | the agent, automatically | a person, in a GUI | **the agent, on explicit request** |
| When it runs | every turn | while the app is open | **only when invoked** |
| What it returns | summaries the agent wrote | a browsable transcript | **verbatim turns, with session id and date** |
| What it adds to context | injected recall | — | **nothing until asked** |

mss is one binary and one skill, and stays that way:

- **the binary** is the search engine;
- **[the skill](skills/mss/SKILL.md)** teaches an agent how to use it correctly — only on `/mss <query>`, refresh once, cite verbatim, say what it could not cover.

## Install

**Homebrew** (macOS, Linux)

```sh
brew install henryyu333/tap/mss
```

**Prebuilt binary** — download the archive for your OS from the
[latest release](https://github.com/henryyu333/mss/releases/latest), unpack it,
and put `mss` on your `PATH`. Each archive also carries `SKILL.md`.

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

The skill is what makes an agent use mss the right way. Copy
[`skills/mss/`](skills/mss/) into your agent's skills directory, for example:

```sh
mkdir -p ~/.claude/skills/mss
curl -fsSL https://raw.githubusercontent.com/henryyu333/mss/main/skills/mss/SKILL.md \
  -o ~/.claude/skills/mss/SKILL.md
```

Then ask for history explicitly: `/mss why did we drop the redis cache`.
The skill never fires on ordinary conversation, even when it says "before" or
"we discussed".

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
`MSS_INCLUDE_SUBAGENTS`, plus the per-harness root overrides above.

Internals are described in [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## License

MIT. See [`LICENSE`](LICENSE).
