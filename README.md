# mss

English | [简体中文](README.zh-CN.md)

Search your coding agents' past sessions — when you ask, not when they guess.

`mss` is one Go binary that indexes the session transcripts your coding agents
already leave on disk and brings the matching turns back as **original text**.
It runs only when you run it: no background daemon, no memory the agent writes
on its own, no recall injected into a conversation nobody asked for. Your
sessions stay where the harnesses put them; the index is a local, rebuildable
cache beside them.

## Install

```sh
go install github.com/henryyu333/mss/cmd/mss@latest
```

Or from a checkout:

```sh
make build   # writes ./mss
```

The SQLite-backed stores (opencode, Cursor) are read through the `sqlite3` CLI
on `PATH`; every other harness is plain files. There is no CGO dependency.

## Quick start

```sh
mss index                          # build or update the index
mss "connection pool exhausted"    # search everything
mss --harness claude --since 30d "panic in indexer"
mss last 20 --role user            # the last twenty user turns
mss show 01a00feb --harness codex  # read one session, by id prefix
mss ctx "schema migration rollback" > context.md
mss sources                        # what stores it reads, and what they hold
```

A query that matches nothing says so instead of guessing: results carry a
`tier` — `exact`, plus `close`/`stemmed` for spelling-corrected and word-form
matches; `relevance` for a nearest-neighbour ranking (no real match);
`error` for signature matches — so a caller never mistakes a neighbour for a
real hit. A true miss is `tier: "exact", total: 0`. `--json` returns the same envelope a script can parse; the schema is
documented in [`docs/json-output.md`](docs/json-output.md).

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
- **Nothing leaves the machine.** There is no network surface: no daemon, no
  upload, no MCP server, no hooks. Search is a local lookup over a local file.

Useful environment variables: `MSS_INDEX_DIR` (index location),
`MSS_STORES` (stores to read), `MSS_STORE_TIMEOUT` (per-store read budget),
`MSS_INCLUDE_SUBAGENTS`, plus the per-harness root overrides above.

## Agents

`mss` is built to be driven **only on explicit request** — a person typing
`/mss <query>`, or a script that says so. The point of the design is the
opposite of a memory system: nothing is searched, injected, or remembered until
someone asks, and nothing the agent concludes is saved.

[`skills/mss/SKILL.md`](skills/mss/SKILL.md) is the skill that drives it:
refresh the index once (`mss index --quiet`), then list the candidate sessions
with `search --sessions --no-refresh --sort updated`, scan windows with
`show --brief --no-refresh`, read the passages that matter with
`show --json --around`, and report a timeline that cites harness, date and
session id (and quotes verbatim), marks later reversals and paraphrases,
states what the search could not cover (`coverage` in the JSON envelope), and
keeps any "how it stands now" sentence tied to a current read-only check
instead of to the history.

## License

MIT. See [`LICENSE`](LICENSE).
