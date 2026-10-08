# v0.3.0 release evidence and gates

This document records what the v0.3.0 candidate **actually proves** and what it
does not. It exists so promotion decisions are made from evidence, not from
README confidence. Everything here was measured or observed on
2026-10-08 in the remediation worktree unless marked pending.

**Baseline:** main `633fb76f`. All work is on branch `sc-pinned-fermion-df37`;
`main` was never modified. No remote push, release, tag, or tap publication was
performed or is implied.

## Verified on this machine (macOS 27.0.1, Apple Silicon, Go 1.27.1)

| Area | Evidence | Result |
| --- | --- | --- |
| Build & unit/integration suite | `gofmt -l .` empty, `go vet ./...`, `go test ./...`, `go test ./internal/...`, `make build` | PASS |
| Skill manual routing | Real Pi 1.0.4 loader + Codex 0.160.1 `debug prompt-input`, both languages | PASS — see [skill-compatibility](skill-compatibility.md) |
| Explicit installer | All four harnesses × both languages from a real binary; idempotent reinstall, conflict/symlink refusal, no index/store I/O | PASS |
| Recall policy diagnostics | Malformed/unreadable/unknown-key policy → one stderr warning, stdout/JSON unchanged; partial rules discarded | PASS |
| History as untrusted data | Historical instruction returned as attributed evidence, never executed; source transcript bytes/mtime/inode unchanged | PASS |
| Secret redaction | Invented provider-shaped key redacted in real `search`/`show`; marker retained | PASS |
| Index compatibility | v0.2.0 public binary's index read by candidate with no rebuild; manifest digest unchanged | PASS |
| Synthetic benchmark | 64 / 2,000 / 10,000 sessions, four formats; precision 1.0 / recall 1.0 on 13 frozen labels, zero false positives; fault injection PASS | PASS — [benchmarks](benchmarks.md) |
| Six release targets | `darwin/linux/windows × amd64/arm64` cross-compile with release ldflags; Windows test binaries compile | PASS (compile only) |
| Release validator | 8 stdlib unit tests; full local rehearsal over a manually packaged six-target bundle of the committed tree: eight artifacts, SHA256, safe paths, six binary headers, native darwin/arm64 version + all four embedded skill installs in both languages | PASS locally (GoReleaser packaging on CI still pending) |
| Workflow configs | `.goreleaser.yaml`, `ci.yml`, `release.yml` parse as YAML; release gated on three-OS reusable CI + native archive validation | Syntax PASS; remote execution pending |

## Fixed during this remediation

1. **P0** — `hide: true` did not suppress automatic selection in Pi/Codex; the
   Skill now uses `disable-model-invocation` plus the Codex
   `allow_implicit_invocation` policy. Historical content is untrusted evidence;
   raw-session fallback is gone; nonce works on POSIX and PowerShell.
2. **P1** — Skill and CLI versions are bound by embedding; installation is one
   explicit command; malformed recall policy warns instead of silently ignoring;
   Windows CI leg added; quarantine bypass removed; releases are gated and
   validated.
3. **P2 evidence semantics** — found by the new benchmark, fixed and pinned:
   - `search --sessions` with a strict head + relevance tail presented the tail
     as matched evidence (4 false positives in the first 64-session run);
   - `--session <id>` did not scope the relevance ranking;
   - co-occurrence substitution and quoted-phrase relaxation were classified as
     strict `found` matches at scale (246 false positives at 2,000 sessions).

## Not proven — pending external gates

| Gate | Why it is open | Who unblocks |
| --- | --- | --- |
| Native Linux/Windows CI | Docker daemon unavailable locally; no remote run authorized | Maintainer pushes a PR; watch all three legs |
| GoReleaser packaging | Not installed locally; rehearsal used equivalent manual packaging | CI `release` workflow on the tag |
| External Homebrew tap | Separate repository; migration needs its own authorization | Maintainer publishes `Formula/mss.rb`, removes the old cask |
| Signing / notarization | No certificate or signing pipeline configured | Maintainer; until then checksums are integrity-only |
| Interactive host invocation | Offline probes cannot exercise TUI pickers or model behavior | First-user checklist, one human per host |
| External user acceptance | No external user has run this build | [first-user checklist](first-user-checklist.md) |
| Real-history retrieval quality | Synthetic corpus is not real developer history | Optional real-corpus evaluation, separately scoped |

## Review follow-ups recorded, not blocking

Independent review found these after the fixes above. They are logged here as
maintenance items and were deliberately left out of the v0.3.0 scope:

1. `search --sessions` on narrowed co-occurrence candidates reports
   `matched_indices` by original-query terms rather than the substituted AND;
   navigation precision, not evidence correctness (`cmd/mss/main.go`).
2. The `strict` count is computed before recall-policy filtering, so a withheld
   strict session makes the count disagree with the served list
   (`internal/index/retrieval.go`, `cmd/mss/main.go`).
3. `mss install-skill` rejects a symlinked skill directory but follows symlinks
   on intermediate parents; documented limitation, acceptable because it needs
   write access to the destination anyway.
4. `tools/verify-release.py` reports a missing archive member as a KeyError
   traceback instead of a formatted `FAIL:` line (CI still fails correctly).
5. `verify-release.py check_native` runs the artifact with the host environment
   minus `MSS_*`; switching to the benchmark's PATH allowlist would be tighter.
6. The publish job trusts the artifact store between validation and upload;
   re-running `verify` in the publish job would close the gap cheaply.

## Promotion verdict

The candidate is ready for **public code review and a tagged release candidate**
— the documented local gates pass and every remaining claim is explicitly
bounded. It is not ready to claim production-grade cross-platform support,
signed distribution, or verified interactive Skill behavior until the pending
gates above close.
