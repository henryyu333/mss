# Session format registry

This registry records the on-disk session formats `mss` parses. Each entry
describes store discovery, file layout, message records, role and timestamp
handling, and known compatibility behavior. These are observations of upstream
output, not specifications published by the harness vendors.

[`registry.json`](registry.json) is the machine-readable index. The files under
[`fixtures/registry`](../../fixtures/registry) are synthetic conformance samples
shaped like upstream records. `internal/sources/registry_test.go` checks the
index against `mss`'s loader list and runs each fixture through its parser.

## Entries

| Harness | Format |
| --- | --- |
| [Claude Code](claude-code.md) | JSONL transcript |
| [Codex CLI](codex.md) | rollout and history JSONL |
| [Cursor](cursor.md) | SQLite key-value store and JSONL transcript |
| [DeepSeek Harness](deepseek.md) | append-only session log, zstd-framed JSONL |
| [Grok Build](grok.md) | ACP update JSONL with JSON metadata |
| [omp (Oh My Pi)](omp.md) | JSONL transcript |
| [opencode](opencode.md) | SQLite relational store, plus a JSON diff file per session |
| [pi](pi.md) | JSONL transcript |

## Reporting drift

Open an issue with the harness name and version, operating system, observed
store path, and the smallest redacted record that shows the difference. State
whether the change affects discovery, session metadata, roles, content, or
timestamps. Do not attach a real session database or unredacted transcript.

## Adding or updating a format

1. Update the harness reference page from observed records and note the drift
   under **Known quirks and drift**.
2. Add a synthetic fixture that contains no user data or credentials.
3. Add or update the `registry.json` entry. Paths are repository-relative;
   store paths use environment-variable placeholders where applicable.
4. Update the parser and its focused tests, then run the registry conformance
   test and the full suite.
5. Set **Last verified** to the observation date, here and in `registry.json` —
   those two are compared, so a page cannot claim a check the registry does not
   have.

SQLite fixture sources are stored as SQL so changes remain reviewable. The
conformance test materializes them in a temporary directory before parsing.
