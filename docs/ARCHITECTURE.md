# Architecture

This document is for people changing `mss` internals.

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

Every one of those reads carries a wall-clock budget, ten minutes by default. One sqlite3 child once ran 13m54s with 0.75s of CPU in mss itself, and nothing in the tree set a deadline, so the run looked hung rather than slow. A store that runs out is an ordinary read error: the harness reports as unreadable, `mss doctor` names it, and the rest of the index still builds. `MSS_STORE_TIMEOUT` takes a duration, and a zero or negative one turns the cap off for someone who would rather wait than lose a store.

## Index format

Default path: `~/.cache/mss/index.db`.

Files:

- `records.bin`: length-prefixed records. Each record stores session key, source path, role, text, and timestamp. The session key, path and role are interned ids in the record's prefix, outside the body, so a scan for one kind of record reads the prefix and skips the rest; bodies of 8 KiB and up are deflated, smaller ones are not, because the read side pays more for a lower floor than the index saves.
- `buckets/*.bin`: token bucket files. A token maps to compact postings: record offset, session ordinal, and one bit marking the posting as a work record.
- `manifest.gob` / `sessions.gob`: index version, source file state, redaction counters, sync export watermarks, imported-record dedupe keys, session metadata (including ordinals, the files a session touched most, and hashes of the questions it asked), build time, and search scope.

### Roles

Beyond `user`, `assistant` and `developer`, records carry what the agent did:

| role | holds |
| --- | --- |
| `tool-output` | what a tool printed. Claude files this under the user role in its own transcripts, which is why it used to arrive labelled as something a person said |
| `files` | the paths a turn opened or edited |
| `command` | a shell command worth keeping — an allowlist of build, test, VCS and deployment tooling, single-line only |
| `edit` | a span an edit replaced: the path on the first line, the exact removed bytes after it. Only the path earns postings, since nothing searches the body |
| `summary` | a harness's own digest of a conversation it compacted away. Kept, because it is the only record of the turns that went, and not filed as speech: on one store 1,906 of them were 19.6% of everything indexed in the sessions that had them, and none of them ever won a quoted line for a real question |

These are indexed and searchable by `--role`, and served in ordinary results only when asked for by role: a path that happens to contain the words of a question is not an answer to it. The postings carry a bit for them so the per-session read bound can spend its budget on speech first.

## Secret redaction

`internal/redact` runs before every `writeRecord` path: cold rebuild, `writeSessions`, non-append incremental replacement, and append-only incremental ingest. The pass is disabled only when `MSS_NO_REDACT=1` is set; that escape hatch is unsafe because plaintext credentials will be written to the local index.

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
- removed files or non-append changes: rewrite the index while preserving unchanged records and replacing changed sessions.

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

Cold rebuild does all parsing first, then writes `records.bin`, buckets, and manifest from one goroutine. That keeps the on-disk index coherent and avoids concurrent writers. The five sidecars derived afterwards — the co-occurrence map, fix pairs, the recurring-command table, the failures and the session facts — each write their own file and read nothing the others write, so they run together, and fix mining and the co-occurrence map also run per session across cores. That is most of the difference between a 51s and a 30s rebuild on a real store.

## Ranking

Search intersects postings to find candidates, then scores each with BM25 over the query tokens and multiplies in a few bounded signals — each able to break a tie, none able to outrank plain relevance:

- **decision** — a session that reached a conclusion is lifted, and the decision-carrying line (not the query-match line) is what recall shows.
- **outcome** — a session whose own text reports it reverted an approach and settled nothing else is damped; one that reverted and then settled keeps the decision lift.
- **worn (reuse)** — a session agents keep recalling is lifted on a `log2` curve capped at +50%, on the theory that what the machine keeps needing is worth surfacing; the cap keeps popularity below relevance.
- **freshness** — recency decays the score gently so time is a hint, not a filter.

`ctx` and `last` reuse the same scored order; the decision line comes from the same conclusion extraction the digest uses.

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
