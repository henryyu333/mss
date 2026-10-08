# Session format registry

This registry records the on-disk session formats `mss` parses. Each entry
describes store discovery, file layout, message records, role and timestamp
handling, and known compatibility behavior. These are observations of upstream
output, not specifications published by the harness vendors.

[`registry.json`](registry.json) is the machine-readable index. The files under
[`fixtures/registry`](../../fixtures/registry) are synthetic conformance samples
shaped like upstream records. `internal/sources/registry_test.go` checks the
index against `mss`'s loader list and runs each fixture through its parser.

The eight entries describe **parser families**, not eight verified Skill hosts.
Skill installation and manual invocation have a separate
[compatibility record](../skill-compatibility.md) for Claude, Codex, Pi, and OMP.
No entry grants MSS MCP, automatic recall, resume, or handoff capabilities.

`last_verified` is the historical date of the format observation documented on
the corresponding reference page. Running synthetic conformance tests on
2026-10-08 does not refresh that date or prove a current device was inspected.
The dates remain unchanged unless a new actual format observation is recorded.
The registry retains schema version 1, discovery paths, format kinds, fixture
paths, parser source paths, display names, and observation dates.

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
store path (with private components removed), and a minimal synthetic record
that reproduces the difference. State whether the change affects discovery,
session metadata, roles, content, or timestamps. Do not attach real private
transcripts, credentials, or a session database; redaction alone does not
guarantee a private record is safe to publish.

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
