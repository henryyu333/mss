# v0.3.0 release evidence and gates

This document records what the v0.3.0 candidate **actually proves** and what it
does not. It exists so promotion decisions are made from evidence, not from
README confidence. The original remediation evidence was measured on 2026-10-08;
the distribution-preparation evidence below is recorded separately.

**Verified runtime baseline:** main `1db0ab6`. Its three-platform
[CI run 37793748699](https://github.com/henryyu333/mss/actions/runs/37793748699)
passed on Ubuntu, macOS, and Windows, including Go regression tests and native
synthetic CLI smoke. This supersedes the original `633fb76f` baseline and
remediation-only branch status. v0.3.0 remains unpublished; no v0.3.0 tag or
Release has been created.

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
| Workflow configs | `.goreleaser.yaml`, `ci.yml`, `release.yml` parse as YAML; `1db0ab6` three-OS CI passed; release gated on reusable CI + native archive validation | CI PASS; tag-triggered packaging/publication pending |

## Distribution preparation (2026-10-08)

Homebrew 7.0.8 on macOS 27.0.1 arm64, Go 1.27.1. A temporary local tap used
the real Formula name `mss` with `--skip-link`; the existing v0.2.0 Cask was
deliberately preserved. These are technical checks, not external-human acceptance.

| Check | Observed evidence | Result |
| --- | --- | --- |
| Stable source Formula | Published `v0.2.0.tar.gz`, SHA256 `df007cd20e279fe675ff603ac588d9476b44847fc28d19e6968e398053d657cb`; native Homebrew source build | PASS |
| Stable audit/test | `brew audit --strict --online --formula <temporary-tap>/mss`; `brew test --force <temporary-tap>/mss` indexes synthetic Claude history and checks exact retrieval | PASS |
| Generated v0.3.0 Formula | Strict Homebrew audit of the unmodified generated artifact; URL-derived stable and prerelease version detection | PASS after removing the redundant explicit `version` stanza |
| Formula update scenario | Source Formula v0.2.0 → candidate v0.3.0 installed with `brew install --build-from-source --skip-link`; candidate source is `git archive` of `1db0ab6`, SHA256 `a0a3aefc6862e85951306e88849360c7dcf9f40c584f9117785e436c6d1bc81f` | PASS; only the fixture's URL was changed to a local source archive |
| Candidate runtime/test | `mss version`, exact search of v0.2.0's index without rebuild (manifest bytes unchanged), `brew test --force`, explicit Codex installer and custom-Skill overwrite refusal | PASS |
| Formula uninstall | Candidate and retained stable kegs uninstalled separately without force; test tap removed | PASS; source/index/custom Skill unchanged |
| Existing installation safety | Cask version, `/opt/homebrew/bin/mss` target and binary SHA256 unchanged throughout; no Cask uninstall or prefix relink | PASS for preservation; actual cutover untested |
| Release artifact compatibility | Same `formula()` generator, source SHA256, Go build, packaged Skill paths and test block; validator's 8 tests pass | PASS locally; actual tag-triggered GoReleaser/release download remains untested |

The external tap migration replaces `Casks/mss.rb` (including its recursive
quarantine-removal hook) with `Formula/mss.rb` for **published v0.2.0**, not a
nonexistent v0.3.0 download. Only Go is a build dependency; sqlite3/zstd remain
optional. The safe staged cutover and retained-binary rollback are documented
in the tap README and [installation](install.md#homebrew-source-formula-migration).
The tap migration passed independent standards/spec review and was pushed to
[`henryyu333/homebrew-tap` main `dbec7d0`](https://github.com/henryyu333/homebrew-tap/commit/dbec7d0d41f9447b932c316cbb1589b361800aae).
GitHub's published tree contains `Formula/mss.rb` and no `Casks/mss.rb`.
The MSS template/docs diff independently passed standards/spec review.
v0.3.0's generated Formula is published separately only after its source
Release asset exists.

Homebrew's install diagnostics also reported a user-local `mss` earlier on PATH.
All lifecycle checks invoked the Formula binary by its full path; users must
inspect `command -v mss` instead of assuming `brew link` wins PATH precedence.


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
| GoReleaser packaging | Not installed locally; rehearsal used equivalent manual packaging | CI `release` workflow on the tag |
| Homebrew native coverage and cutover | macOS arm64 source lifecycle passed; Linux Homebrew, Intel macOS and actual Cask uninstall/link were not exercised (existing Cask preservation was requested) | Native hosts and a separately approved real cutover |
| Published v0.3.0 Formula | No v0.3.0 tag/source Release asset; local fixture substituted only the download URL, so public fetch/online audit and real `brew upgrade` after cutover are not proven | Publish and verify Release first, then validate/publish its exact `mss.rb` in the tap |
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

Distribution preparation has local technical evidence, not release authorization.
The observed three-platform main CI does not substitute for checks on the eventual
tag commit. Do not claim signed distribution, every native architecture, actual
Cask cutover, or interactive/external-user acceptance until those gates close.
No v0.3.0 tag or Release is created by this preparation stage.
