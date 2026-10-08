# Architecture

This document is for people changing `mss` internals.

## Runtime and installation

`cmd/mss/main.go` dispatches explicit CLI invocations. Nothing starts at agent
startup or between invocations: no daemon, hooks, automatic memory, network
client, MCP server or telemetry. Concurrent parser workers exist only for the
duration of the invoked command.

`skills/bundle.go` embeds the two language variants of the same workflow and
the Codex invocation policy. `cmd/mss/install_skill.go` is a separate, explicit
installation path: it writes only the selected harness's MSS Skill assets,
refuses conflicting files, and does not inspect stores or create an index.
Recall writes only its own index; installation is the documented exception.
Invocation controls and bounded host evidence are in
[skill compatibility](skill-compatibility.md).

## Source parsers

Parsers live in `internal/sources` and return `[]model.Session`. The table is
what the loader registers: the eight coding agents
whose names `mss sources` prints. `docs/registry/` describes each store's
layout in detail, and `internal/sources/registry_test.go` checks that index
against the loader list.

| Source | Code | Input |
| --- | --- | --- |
| Claude Code | `claude.go` | JSONL files under `~/.claude/projects` |
| Codex CLI | `codex.go` | rollout JSONL files (`.jsonl.zst` once Codex compresses them) plus `history.jsonl` under `~/.codex` |
| opencode | `opencode.go`, `opencode_v2.go`, `opencode_diff.go` | SQLite database at `~/.local/share/opencode/opencode.db` (the 1.x tables and the 2.0 `session_v2`/`session_message` ones), plus `storage/session_diff/*.json` beside it |
| Cursor | `cursor.go` | SQLite state stores plus CLI agent transcripts |
| Grok Build | `grok.go` | ACP update streams and session summaries under `~/.grok/sessions` |
| pi | `pi.go` | JSONL transcripts under `~/.pi/agent/sessions` |
| omp (Oh My Pi) | `omp.go` | JSONL transcripts under `~/.omp/agent/sessions` and each profile beside it |
| DeepSeek Harness | `deepseek.go` | zstd-compressed session JSONL under `~/.dsh/sessions` |

File-based sources are parsed with a worker pool sized to `runtime.NumCPU()`. Results are collected by input file index and then appended in sorted path order, so parsing can be parallel while index writes stay deterministic.

Every SQLite store above is read through the local `sqlite3` command — opencode's, Cursor IDE state, and the database Grok keeps beside its JSONL. Cursor CLI transcripts are plain JSONL; their tool results come from the chat's `store.db` beside them. There is no CGO SQLite dependency. Before trusting it, mss asks the `sqlite3` on PATH one JSON query per process: a wrapper that drops its arguments or a stub that prints nothing would otherwise read every store as empty, so a binary that does not answer is reported by path, as a missing one is.

Every SQLite read carries a wall-clock budget, ten minutes by default. A store
that runs out is a read error: `mss doctor` names it and search reports incomplete
coverage rather than equating an unread store with absent history.
`MSS_STORE_TIMEOUT` takes a duration; zero or a negative value disables the cap.

## Index format

Default path: `~/.cache/mss/index.db`.

Files:

- `records.bin`: length-prefixed records. Each record stores session key, source path, role, text, and timestamp. The session key, path and role are interned ids in the record's prefix, outside the body, so a scan for one kind of record reads the prefix and skips the rest; bodies of 8 KiB and up are deflated, smaller ones are not, because the read side pays more for a lower floor than the index saves.
- `buckets/*.bin`: token bucket files. A token maps to compact postings: record offset, session ordinal, and one bit marking the posting as a work record.
- `manifest.gob` / `sessions.gob`: content/format versions, source-file state,
  redaction counters, session metadata, ordinals, build time and search scope.
  Some upstream provenance/import fields remain for on-disk compatibility;
  MSS does not expose sync, handoff or memory-curation commands.
- Derived caches may include `cooccur.gob`, `fixes.gob`, `commands.gob`,
  `commandfails.gob`, `commandfails-state.gob` and `sessionfacts.gob`. Their
  writers run during explicit indexing; small corpora need not produce each file.

### Roles

Beyond `user`, `assistant` and `developer`, records carry what the agent did:

| role | holds |
| --- | --- |
| `tool-output` | what a tool printed. Claude files this under the user role in its own transcripts, which is why it used to arrive labelled as something a person said |
| `files` | the paths a turn opened or edited |
| `command` | a shell command worth keeping — an allowlist of build, test, VCS and deployment tooling, single-line only |
| `edit` | a span an edit replaced: the path on the first line, the exact removed bytes after it. Only the path earns postings, since nothing searches the body |
| `summary` | a harness's own digest of compacted conversation. Kept separately from speech because it is a retelling, not a new user statement |

These are indexed and searchable by `--role`, and served in ordinary results only when asked for by role: a path that happens to contain the words of a question is not an answer to it. The postings carry a bit for them so the per-session read bound can spend its budget on speech first.

## Secret redaction

`internal/redact` runs before every `writeRecord` path: cold rebuild,
`writeSessions`, non-append replacement and append-only incremental ingest.
Titles and derived caches also see redacted text. `MSS_NO_REDACT=1` is an unsafe
full-rebuild opt-out, not a confidentiality guarantee. Pattern-based redaction
can miss unusual secrets; history recalled by an agent reaches that agent's
model provider. `internal/redact.Outbound` is retained upstream code, not a
protection applied by the MSS recall commands.

The redactor replaces only secret values, keeping keys and surrounding prose searchable. It covers AWS access keys and AWS secret assignments, generic credential assignments in ASCII and in other scripts, bearer tokens and JWTs, PEM and PGP private key blocks, provider token prefixes, connection URLs with `scheme://user:pass@host` credentials, credentials handed to a program on its command line (`sshpass -p`, `mysql -pSecret`, `curl -u user:pass`, `--password` and its siblings), netrc and cookie lines, bare high-entropy values in secret-shaped positions, and a password stated in prose — "the admin password is …", where no delimiter exists for the other rules to find.

Each rule that could match ordinary text carries a gate, and the gates are the part worth reading before adding a rule: the prose form requires the value to hold a digit or a symbol and to end at the first space, so "the password is wrong" and "the password is the same as staging" are left alone; the entropy rule excludes hex digests, UUIDs, paths and identifiers; the long command-line flags leave an ordinary word alone when the separator is a space rather than an `=`.

Redaction counts are accumulated per source file in `FileState.Redactions` and as a manifest total. `mss sources` reads those counters and prints `redacted=` per harness.

Search flow:

1. Tokenize the query.
2. Read posting lists from the token buckets.
3. Intersect posting lists for multi-word searches.
4. Filter postings by session metadata and read every matching candidate record.
5. Group records back into sessions and score them in `internal/search` with BM25
   (`k1=1.2`, `b=0.75`). Document frequency and document length are measured
   over the candidate records at search time. User-message term contributions
   receive a 1.3 multiplier, and the score is multiplied by `0.5 + 0.5/(1+age_days)`,
   so age can halve a score but not erase it.
6. Sort by score, then updated time descending, then session ID ascending; the
   normal result limit is applied after ranking.

`--harness`, `--project`, and `--since` are applied from session metadata before
scoring. `--role` is applied while reading candidate records.

Regex search scans records because arbitrary regex cannot use token postings safely.

## Incremental algorithm

`currentFiles` records path, size, and mtime for known stores.

`EnsureForSearch` compares the current file set with `manifest.gob`:

- fresh manifest: do nothing;
- version or scope mismatch: rebuild. Two versions are tracked: the content version, which moves when mss derives something new from a transcript, and the on-disk format, which moves only when an older layout would be mis-read;
- append-only JSONL changes: append new records and update touched buckets. A harness whose sessions are re-read whole when they change (opencode, Cursor, Grok) replaces those sessions instead (#4207, #4450);
- non-append changes: rewrite while preserving unchanged records and replacing
  changed sessions. A transcript deleted from a still-present store is
  deliberately retained as indexed history against harness cleanup, with a
  “still searchable” notice; this cache can outlive the original file.

A file takes the append path only when the prefix mss already read is still
byte-for-byte what it read: a rewind that truncates and regrows past the old
length looks exactly like growth, and appending onto it would leave the replaced
turns in the index for good. The recorded prefix hash is what tells the two
apart, so a live transcript that only grew is read from its last safe offset
rather than reparsed whole.

A file mss has never read has no prefix to check and no offset to resume from,
so it takes the append path whatever its kind: read whole, its sessions new, the
records already on file untouched. The gate used to demand a resume parser even
there, and because it applies to the whole batch, one new file of a kind without
one — a Cursor CLI transcript, a dsh session log — sent every other
changed file down the rewrite branch too.

Search waits for the refresh it needs. An invocation is explicit — a person or
a script asked a question — so the cheap half and the rewrite half both run
before the answer: appending is what it costs for a live transcript, and a
rewrite is what it costs when a store changed shape. The answer is always
computed over what is on disk now, never over a snapshot that predates the
question, and a long refresh narrates itself on stderr rather than reporting
stale. A caller that has refreshed once itself — the manual-recall flow runs
`mss index --quiet` and then queries in parallel — passes `--no-refresh` to
skip that pass: the answer is the index as it was, the envelope says so
(`refresh.refreshed: false` and `refresh.last_refresh`), and no blocking lock
is taken, so concurrent reads cannot queue behind a build. `--no-refresh`
cannot build an index that is not there and does not repair a damaged one;
both say to run `mss index`.

Cold rebuild parses first, then writes records, buckets and manifests through
the index writer. Derived-cache builders use separate files and complete before
publication; cold rebuild and replacement/append paths have different cache
update strategies. Locking and directory-swap tests cover writer coordination.
For reproducible MSS timing evidence, use [the synthetic benchmark](benchmarks.md);
upstream corpus timing anecdotes are not MSS performance measurements.

## Ranking

Exact search intersects postings, then scores candidates with BM25. Additional
bounded signals include proximity, title terms, decision/outcome text and age;
they do not turn a lexical candidate into verified historical evidence:

- **decision** — a session that reached a conclusion is lifted, and the decision-carrying line (not the query-match line) is what recall shows.
- **outcome** — a session whose own text reports it reverted an approach and settled nothing else is damped; one that reverted and then settled keeps the decision lift.
- **legacy optional inputs** — the search library retains an optional reuse map
  and curated-note handling for compatibility. The MSS CLI does not populate
  automatic recall counts or expose a memory-curation command.
- **freshness** — recency decays the score gently so time is a hint, not a filter.

`ctx` resolves a session or the best query match and prints a context window.
`last` orders recent sessions by update time, not BM25 relevance. Exact/close/
stemmed/error tiers and relevance-only candidates are distinguished in JSON;
the public envelope contract is in [json-output.md](json-output.md).

## Trust and policy

Historical roles, commands and apparent instructions remain attributed data.
Display sanitization neutralizes terminal controls; it does not establish model
obedience or make a historical instruction authoritative. The Skill quotes
history and prohibits following its commands or using raw-session fallback.

Recall policy defaults are permissive. An invalid/unreadable file discards
partial rules and emits one actionable stderr warning; unknown keys are also
reported. Valid `*` origin rules are recognized. Policy diagnostics leave
stdout/JSON data intact; `mss doctor` supplies details. This policy is a recall
filter, not an operating-system permission sandbox.

## Maintainability decisions for v0.3.0

`ingest.go` coordinates discovery, append/replacement decisions and publication;
`retrieval.go` reads/filter records; `search.go` scores/renders; `main.go` routes
commands. Their size alone is not a reason to change index semantics or split
interdependent invariants before a release. Existing environment, index-writer,
query and CLI capture seams exercise those paths. This remediation isolates
Skill installation and the one-per-invocation policy diagnostic, removes an
unused lexical-prefix helper, and otherwise avoids speculative refactoring.

## Add a new harness

Implement the same shape as the existing sources.

Interface:

```go
func LoadNewHarness() []model.Session
func ParseNewHarnessFile(path string) ([]model.Session, error)
func ParseNewHarnessFileFromOffset(path string, offset int64) ([]model.Session, error) // if append-only
```

Five steps:

1. Add parser code in `internal/sources` that returns `model.Session` with stable `Harness`, `ID`, `Project`, `Path`, `Started`, `Updated`, and `Messages`.
2. Add a `Harness` entry to `allHarnesses()` in `internal/sources/registry.go`: `Load`, `Files`, and one `FileKind` per file shape with `Match`, `Parse` and, for append-only formats, `ParseFrom`. `Match` has to accept every root `Files` walks: the incremental index finds a changed file's parser by its path.
3. Discovery, path matching and incremental parsing in `internal/index` read from that entry; nothing there needs editing.
4. Add an entry in `docs/registry/registry.json` and a `docs/registry/<id>.md` page linked from its README; the `registry*_test.go` files in `internal/sources` check them against the loader list and each other.
5. Add fixtures and tests for parsing, indexing, and search.
