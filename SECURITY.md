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
- mss writing anywhere other than its own index, or modifying a session file.
- mss opening a network connection.
- Index files created with permissions wider than `0700` / `0600`.
- Command or path injection through session content, project names or flags.

## Scope

mss reads files already on your machine and keeps a redacted plaintext index
under `~/.cache/mss/`. Anyone who can read your user account's files can read
that index, and history that you recall with `/mss` is sent to your agent's
model provider. Those are documented limits, not vulnerabilities; see the
Privacy section of the [README](README.md#privacy).
