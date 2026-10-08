# v0.3.0 release evidence and gates

This document records what the v0.3.0 candidate **actually proves** and what it
does not. It exists so promotion decisions are made from evidence, not from
README confidence. The original remediation evidence was measured on 2026-10-08;
the distribution-preparation evidence below is recorded separately.

**Release status:** v0.3.0 was released from the commit this document ships in
(tag `v0.3.0` on `12e6ab0`), after the v0.3.0-rc.1 prerelease exercised the
complete tag-triggered release workflow. The final tag ran the same workflow on
this content; the published-asset verification and the tap publication are
appended below. The earlier baselines (`633fb76f`, then `1db0ab6` with
[CI run 37793748699](https://github.com/henryyu333/mss/actions/runs/37793748699))
remain recorded below.

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
| Release validator | 8 stdlib unit tests; full local rehearsal over a manually packaged six-target bundle of the committed tree: eight artifacts, SHA256, safe paths, six binary headers, native darwin/arm64 version + all four embedded skill installs in both languages | PASS locally; tag-triggered packaging exercised by the RC (below) |
| Workflow configs | `.goreleaser.yaml`, `ci.yml`, `release.yml` parse as YAML; main `1db0ab6` three-OS CI passed; release gated on reusable CI + native archive validation | CI PASS; tag-triggered packaging/publication exercised by the RC (below) |

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
| Release artifact compatibility | Same `formula()` generator, source SHA256, Go build, packaged Skill paths and test block; validator's 8 tests pass | PASS locally; tag-triggered packaging/download exercised by the RC (below) |

The external tap migration replaces `Casks/mss.rb` (including its recursive
quarantine-removal hook) with `Formula/mss.rb` for **published v0.2.0**, not a
nonexistent v0.3.0 download. Only Go is a build dependency; sqlite3/zstd remain
optional. The safe staged cutover and retained-binary rollback are documented
in the tap README and [installation](install.md#homebrew-source-built-formula).
The tap migration passed independent standards/spec review and was pushed to
[`henryyu333/homebrew-tap` main `dbec7d0`](https://github.com/henryyu333/homebrew-tap/commit/dbec7d0d41f9447b932c316cbb1589b361800aae).
GitHub's published tree contains `Formula/mss.rb` and no `Casks/mss.rb`.
The MSS template/docs diff independently passed standards/spec review.
The v0.3.0 Formula is published to the tap only after its source Release asset
exists (see the RC section below).

Homebrew's install diagnostics also reported a user-local `mss` earlier on PATH.
All lifecycle checks invoked the Formula binary by its full path; users must
inspect `command -v mss` instead of assuming `brew link` wins PATH precedence.

## Release candidate v0.3.0-rc.1 (2026-10-09)

The prerelease exercised the exact release path before the final tag: CI legs,
packaging, three-runner archive validation, and publication passed; the one
failure was infrastructure and the full gate was re-executed rather than
skipped.

| Check | Observed evidence | Result |
| --- | --- | --- |
| Tag & workflow | `v0.3.0-rc.1` on `a0bfda1`; [run 37806317173](https://github.com/henryyu333/mss/actions/runs/37806317173): three `checks` legs, `package`, three `archives` legs, `publish` | PASS (attempt 2) |
| Infrastructure flake | Attempt 1: `macos-latest` was cancelled after ~15 min with "The job was not acquired by Runner of type hosted" (GitHub-hosted macOS capacity); ubuntu/windows legs passed, downstream jobs skipped | Re-run of the same commit and gates |
| Prerelease state | GitHub release with `isPrerelease: true`, 10 assets (6 archives + source + `checksums.txt` + `mss.rb`) | PASS |
| Asset validation | `python3 tools/verify-release.py verify --dist <downloaded> --tag v0.3.0-rc.1` against the `a0bfda1` checkout: eight artifacts, SHA256, safe paths, source/asset/metadata bytes, six binary headers, native darwin/arm64 version, all embedded installs | PASS |
| Real artifact E2E | Downloaded `mss_0.3.0-rc.1_darwin_arm64.tar.gz` (SHA256 `f5628d11e830e789702c70b602223a087c59ef45961ad601141b96ae8d852714`); binary reports `mss 0.3.0-rc.1`; isolated index/search on synthetic Claude history returned the exact session; `install-skill codex` wrote the rc-stamped skill plus companion policy, identical reinstall succeeded, and a locally modified skill was refused with a non-zero exit | PASS |
| Homebrew RC Formula | Published `mss.rb` matched the template for the published source SHA256; `brew audit --strict --online` clean; `brew install --build-from-source --skip-link` built the real release asset; `brew test --force` passed; the temporary keg and tap were removed and the machine's v0.2.0 Cask binary was byte-identical before and after | PASS |
| Host invocation — Pi 1.0.4 | Real model run with the published skill (`--skill`, `/skill:mss`): nonce → `mss index --quiet` → `mss search --sessions --exclude-self …` → `mss show rc-synthetic-s1 …`, then the correct verbatim answer with coverage note; tool-level JSON evidence captured | PASS (headless) |
| Host invocation — OMP 18.8.0 | Same workflow via `/skill:mss`; the transcript shows the RC skill body (`mss-version: "0.3.0-rc.1"`) injected and the same CLI sequence; correct answer | PASS (headless) |
| Host invocation — Codex 0.160.1 | Non-TUI `$mss` still does not auto-expand (upstream #40600); the model explicitly read `.agents/skills/mss/SKILL.md` (RC) and executed the same workflow end-to-end | PASS (explicit skill-file path, headless) |
| Host invocation — Claude Code 2.1.284 | This machine's automation context has no Claude Code credentials (no credentials file, no keychain item; the CLI reports "Not logged in"), so no model invocation was possible | PENDING |
| TUI sessions | None of the four hosts was driven through its TUI picker or slash command | PENDING [first-user checklist](first-user-checklist.md) |

## Final release verification (2026-10-09)

`v0.3.0` was tagged on `12e6ab0` and published by
[run 37813213821](https://github.com/henryyu333/mss/actions/runs/37813213821):
all eight jobs (three `checks` legs, `package`, three `archives` legs,
`publish`) passed on the first attempt, and the GitHub release is a full
release (`isPrerelease: false`) with ten assets.

| Check | Observed evidence | Result |
| --- | --- | --- |
| Published assets | All ten assets downloaded; `python3 tools/verify-release.py verify --dist <downloaded> --tag v0.3.0` against the `12e6ab0` checkout: eight artifacts, SHA256, safe paths, source/asset/metadata bytes, six binary headers, native `mss 0.3.0`, all embedded installs | PASS |
| Real artifact E2E | `mss_0.3.0_darwin_arm64.tar.gz` (SHA256 `0b1a86419944ea654b42ac7f7f1fdd67111f24e9cb7f8b03ed43fd7629bfde45`): binary reports `mss 0.3.0`; isolated index/search on synthetic Claude history returned the exact session; `install-skill omp` wrote the version-matched skill, identical reinstall succeeded, and a locally modified skill was refused with a non-zero exit; binary, extracted assets, cache, and skill were then removed | PASS |
| Homebrew Formula | Release `mss.rb` matched the template for the published source SHA256 `0f5a2b2b624d93d3efced289a9b0a2892af97eeaf579badc40703175040c8a78`; `brew audit --strict --online` clean; `brew install --build-from-source --skip-link` built the real asset; `brew test --force` passed | PASS |
| Tap publication | [`henryyu333/homebrew-tap` `b2fe6b8`](https://github.com/henryyu333/homebrew-tap/commit/b2fe6b8bbd70a357bf513762a0b445934a730d44) publishes the release-generated `mss.rb` verbatim; the tap tree carries `Formula/` and `README.md` only. After `brew update`, `brew info henryyu333/tap/mss` reports stable 0.3.0 | PASS |
| Installation safety | The machine's existing v0.2.0 Cask binary, symlink target, and Caskroom were byte-identical before and after every lifecycle check; no Cask was uninstalled or relinked | PASS |

Remaining, unchanged: signing/notarization, TUI and external-human acceptance,
non-native Homebrew hosts, and real-history retrieval quality.

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
| GoReleaser packaging | **Closed:** v0.3.0-rc.1 was packaged on CI, validated on three native runners, and published; the final tag runs the same workflow | — |
| Homebrew native coverage and cutover | macOS arm64 source lifecycle passed (stable, candidate, and RC); Linux Homebrew, Intel macOS, and the actual Cask uninstall/link were not exercised because the existing installation was deliberately preserved | Native hosts and a separately approved real cutover |
| Published v0.3.0 Formula | **Closed:** the release-generated Formula was audited, installed from the real asset, tested, and published to the tap (`b2fe6b8`) | — |
| Signing / notarization | No certificate or signing pipeline configured | Maintainer; until then checksums are integrity-only |
| Interactive host invocation | Headless real-model invocations passed for Pi, OMP, and Codex (explicit skill-file path); Claude Code is PENDING (no credentials in this context); TUI sessions were not driven | First-user checklist, one human per host |
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

v0.3.0 is released after the RC exercised the full tag-triggered pipeline,
local asset validation, Homebrew lifecycle checks, and headless host
invocations; the final tag runs the same workflow, and the published-asset
verification plus the tap update are appended in a follow-up commit. Remaining
gates are signed/notarized distribution, TUI and external-human acceptance,
non-native Homebrew hosts, and real-history retrieval quality. No claim beyond
this evidence is made.
