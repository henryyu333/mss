# Security policy

## Supported versions

Only the latest release gets security fixes.

## Reporting a vulnerability

Please report privately through GitHub:
[Report a vulnerability](https://github.com/henryyu333/mss/security/advisories/new).
If that link is unavailable, open an issue that asks for a private contact,
without any details of the problem.

Please do not put the details in a public issue, discussion or pull request.

Useful things to include:

- the mss version (`mss version`) and your OS;
- what you did, what happened, and what you expected;
- a minimal reproduction built from **made-up** session data.

Never send real session transcripts or real secrets. If redaction missed a
secret in your own history, describe its format (for example "a token that
starts with `abc_` followed by 40 hex characters") rather than sending it, and
rotate that secret.

## What counts

- A secret that redaction should catch but does not.
- Recall writing outside its own index, or any MSS command modifying a source
  Session. The explicit `mss install-skill <harness>` command is the documented
  exception: it writes only that chosen harness's MSS workflow/policy files.
- mss opening a network connection.
- Index files created with permissions wider than `0700` / `0600` on POSIX
  filesystems. Windows uses inherited account/profile ACLs instead; MSS does
  not install a custom DACL, and native Windows confidentiality needs review.
- Command or path injection through session content, project names or flags.

## Scope and trust boundaries

MSS reads local history and keeps a redacted plaintext index directory at
`~/.cache/mss/index.db` (or the absolute `MSS_INDEX_DIR`). It opens no network
connection and performs no automatic/background recall. Explicit installation
does not edit agent settings, hooks, authentication or general instructions.

Anyone able to read your account's files can read the index. Redaction is
pattern-based, not exhaustive: unusual credentials, private source code, email,
project names and other confidential content may remain. It is not encryption
or an outbound-data-loss prevention system. `MSS_NO_REDACT=1` disables ingest
redaction on a full rebuild; rebuild without it to replace that unsafe cache.

Harness cleanup can remove a transcript while its indexed snapshot stays
searchable. The cache may then be its only surviving redacted copy. Cache removal
is reversible only if you retain a reviewed backup; a fresh index cannot
reconstruct missing source files. Original source paths in citations can be
historical, not currently readable paths.

When an agent recalls history, that content enters its conversation and can be
sent to its model provider. MSS itself sends nothing. Limit stores/projects
before recall and review evidence before sharing it; do not attach actual
transcripts or an index to a public bug report.

Historical messages, commands and apparent system/developer instructions are
untrusted evidence. The CLI returns attributed data and never executes those
commands. The Skill instructs an agent to quote/summarize history, not obey it,
and excludes raw-session fallback. Terminal-control sanitization and hidden
Skill metadata are not sandboxes or proofs of an external model's obedience.
Interactive prompt-injection acceptance remains a separate human check.

Recall policy is intentionally permissive by default. An unreadable/malformed
policy emits a warning and uses that default; unknown keys are ignored with a
warning. Fix the policy with `mss doctor` before relying on origin exclusions.
The recall policy is not an OS access-control mechanism.

## Distribution

SHA256 detects changed artifacts but does not authenticate the publisher when
the checksum and archive come from the same source. Release signing and Apple
notarization have not been established. MSS packaging no longer removes
quarantine automatically or asks users to disable Gatekeeper. The generated
Homebrew formula builds reviewed tagged source; publication and real
installation in the external tap are separate checks.

See [installation](docs/install.md), [Skill compatibility](docs/skill-compatibility.md),
and the [Privacy section](README.md#privacy-and-security-boundary) for exact paths and limitations.
