# Reproducible synthetic CLI benchmarks

`tools/benchmark.py` generates an isolated, deterministic corpus and invokes a
supplied **actual MSS executable** through its existing `index`, `search`, and
`show` commands. It uses Python's standard library only. It does not add an MSS
command, a model, telemetry, a background service, or a third-party benchmark
dependency.

This measures a specified synthetic retrieval task and local subprocess costs.
It is **not a real-user history corpus, an external benchmark score, a claim of
universal precision/recall, or proof that an assistant follows the Skill**.

## Run the same binary at three scales

Requirements: Python 3.10+, a native MSS binary, and enough temporary storage.
Building MSS requires Go 1.25+. No sqlite3 or zstd executable is needed for the
four JSONL formats generated here. Run from the repository root, after other
builds/tests have finished; competing CPU or disk work invalidates comparisons.

The following POSIX commands create a new output directory and stamp the locally
built binary with its intended version. The report also records its SHA-256 and
Go build metadata; a version string alone is not binary provenance.

```sh
BENCH_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/mss-benchmark-results.XXXXXX")"
go build -trimpath -ldflags '-X main.version=0.3.0' -o "$BENCH_ROOT/mss" ./cmd/mss

# Small end-to-end CI smoke: all assertions, not a performance gate.
python3 tools/benchmark.py --binary "$BENCH_ROOT/mss" \
  --sessions 64 --queries 13 --output "$BENCH_ROOT/synthetic-64.json"

# Default-size reproducible measurement.
python3 tools/benchmark.py --binary "$BENCH_ROOT/mss" \
  --sessions 2000 --queries 25 --output "$BENCH_ROOT/synthetic-2000.json"

# Larger corpus with the same gold and scaled distractors.
python3 tools/benchmark.py --binary "$BENCH_ROOT/mss" \
  --sessions 10000 --queries 25 --output "$BENCH_ROOT/synthetic-10000.json"

# Optional isolated consumer/oracle tests; these do NOT replace the real runs.
python3 -m unittest discover -s tools -p 'test_benchmark.py'
printf 'Reports: %s\n' "$BENCH_ROOT"
```

To benchmark a release artifact instead, replace `--binary` with its actual local
executable path. On Windows, supply the native `.exe` to the same Python runner;
POSIX setup commands are not a claim of a native Windows run.

`--sessions` defaults to 2000 and must be at least 64 so the fixed labeled corpus
is present. `--queries` defaults to 25 and is the **total** number of latency
repetitions, round-robin over all 13 labeled queries, not 25 repetitions per
query. Every labeled query is independently checked once before timing, even if
`--queries` is smaller than 13. `--output` is required and must be a new path:
the runner refuses to overwrite an existing report. Reports are written on
consumer/assertion failure as well as success, and failure exits nonzero.

## Isolation and reproducibility

Each run creates a new temporary directory and removes only that directory when
it finishes. No existing history or index is opened or changed. The subprocess
working directory, `HOME`, `USERPROFILE`, `APPDATA`, `LOCALAPPDATA`, the five
`XDG_*` directories, and temporary-file directories are isolated. Harness
configuration roots and all current `MSS_*` source-root/database overrides point
inside that directory; `MSS_STORES=claude,codex,pi,omp` is explicit. Caller policy,
exclusion, source-instance, and redaction environment overrides are not inherited.
Only executable discovery and essential Windows runtime environment values are
preserved. `MSS_INDEX_DIR` is an **absolute directory**, not a SQLite filename.

Generator version 1 uses no randomness, fixed historical UTC timestamps, stable
native IDs, one file per session, and 12 parser-shaped messages per session:

- Claude: roled message records with `sessionId`, timestamps and content blocks.
- Codex: `session_meta` plus roled `response_item` message/content records under
  `sessions/2026/01/01/rollout-*.jsonl`.
- Pi and OMP: versioned session headers, cwd, parent-linked message records and
  content blocks, in their separate source roots.

Sessions cycle evenly through the four harnesses. At 2000 sessions, the initial
corpus therefore contains 24,000 messages; at 10,000 it contains 120,000. These
counts describe the generator, **not an observed performance result**.

The first 16 sessions contain fixed gold for several topics; the next 16 contain
an assistant-only role-boundary topic. All later sessions are deterministic,
lexically related distractors with generic conversational context. Scaling
changes distractor density, not the labeled positive IDs. The largest initial
gold set is 32, below the `search --sessions` 500-row limit. The benchmark fails
if an affirmative gold answer is capped rather than silently calling a truncated
window complete recall. Ranked candidate windows are separately counted and may
be capped; they are never counted as positive evidence.

The report's normalized source-tree SHA-256 hashes relative filenames and each
file's bytes, independently of the temporary absolute path. Identical parameters
and generator versions produce identical initial source hashes. MSS command
arguments, measured intervals, byte counts, and per-invocation before/after
source digests remain in the JSON for inspection.

## Frozen query labels and citation checks

Gold is defined by the generator, not inferred from whatever MSS returns. Native
ID plus harness is the unit of evidence. The initial labeled cases are:

| Label | Query / boundary | Expected answer | Gold sessions |
| --- | --- | --- | ---: |
| `and_mixed_harness` | `quartz cobalt` | found | 32 |
| `and_user_role` | same query, `--role user` | found | 16 |
| `and_assistant_role` | same query, `--role assistant` | found | 16 |
| `and_codex_filter` | same query, `--harness codex` | found | 8 |
| `and_second_topic` | `orchid semaphore` | found | 16 |
| `phrase_mixed_harness` | `"velvet copper"` | found | 16 |
| `phrase_pi_filter` | same phrase, `--harness pi` | found | 4 |
| `phrase_exact_session` | same phrase, exact OMP ID and harness | found | 1 |
| `assistant_only_topic` | `lattice beacon --role assistant` | found | 16 |
| `wrong_role_boundary` | same topic, `--role user`; user distractors hold only one term | candidates, never found | 0 |
| `near_phrase_reversed` | `"copper velvet"`, against separated/reordered distractors | none | 0 |
| `near_multiterm_candidates` | `quartz semaphore velvet copper lattice sodium unobtainiumsubject` | candidates | 0 |
| `absent_vocabulary` | three absent, deliberately unrelated tokens | none | 0 |

AND positives put all query terms in a single message. Phrase positives are
contiguous and preserve order. Near-match candidate text has topical overlap,
but no message satisfies the complete query. A `candidates` response is not a
`found` response; a `none` response is not a candidate ranking. Those labels are
asserted, not interchangeable ways for a test to pass.

`search --sessions` already forces JSON. The consumer reads the real version-2
envelope: `match`, `coverage`, `total`, `capped`, and each row's nested `session`,
`hit_count`, and `matched_indices`. It rejects flat-row assumptions, broken
counts, duplicate identities and malformed indices. For each affirmative answer,
it compares the complete returned identity set and exact record positions with
the frozen gold. It then reads those sessions with, for example:

```sh
mss show benchmark-codex-000001 --harness codex --json \
  --no-refresh --offset 0 --limit 200
```

**`show --json` requires `--harness` and the exact native session ID.** The runner
checks the composite identity, complete bounded window, stable 0-based message
indices, role, and gold passage text. It caches previously verified windows, not
invented CLI responses. Expected IDs/harnesses/indices and verified window
metadata are preserved in the report.

Precision is `TP / (TP + FP)` and recall is `TP / (TP + FN)`, over affirmative
composite session identities. An undefined denominator is JSON `null`, not an
invented 100%. False-positive and false-negative counts and IDs are explicit.
Micro metrics aggregate the 13 initial cases only; repeated latency checks and
fault-injection cases do not inflate the quality denominator. Candidate counts
remain separate. These are task-specific synthetic metrics, not model answer
quality or a measure of real-user retrieval accuracy.

## Timings, sizes and environment

The runner measures actual subprocess wall time with `time.perf_counter`:

1. **First fresh-index build:** `mss index --quiet` when no index directory exists.
2. **Unchanged refresh:** another `mss index --quiet` without source changes.
3. **Known-session incremental append:** append one parser-shaped user message to
   each of the first `min(100, N)` existing sessions, then `mss index --quiet`.
   It verifies every expected ID and appended index, then shows a tail window
   from each harness. This is 100 appended messages at 2000/10,000 scale and 64
   at smoke scale; it does not pretend to be a 100-new-session import.
4. **CLI query p50/p95:** the specified number of round-robin
   `search --sessions --no-refresh` subprocesses over the labeled queries,
   using nearest-rank percentiles. Samples include process startup, index reads,
   MSS JSON encoding and pipe collection; they exclude Python JSON decoding and
   the runner's source hashes. The pooled distribution mixes different query
   shapes; it is not a per-query latency percentile. Every sample is labeled in
   the report.

The first build is a **fresh index**, not a cold operating-system disk cache.
Query runs use warmed filesystem caches, and source hashing itself warms them.
There is no cache-dropping, in-process microbenchmark, RSS measurement, concurrent
query-load test, universal performance threshold, or native cross-OS comparison.
The fixed gold is intentionally small; scaled distractors exercise a different
workload from a corpus with thousands of positive sessions.

The report records source bytes and the entire index-directory byte size after
fresh and incremental builds, plus recovered index bytes. Sizes include real
index files, not a guessed SQLite file. Environment fields include OS/release,
architecture, logical CPU count, CPU name when available, Python version, the
supplied binary's `mss --version`, SHA-256, and Go version from
`go version -m <binary>` when the Go executable is available. Unavailable CPU/Go
metadata is `null` with its availability stated; it is not fabricated.

## Fault injection and local safety assertions

All mutations below affect only runner-generated data and are individually
logged with before/after digests. Source files are hashed before **and after every
MSS invocation**, including failed commands, `show`, version discovery, and
latency repetitions. Any source creation, replacement, append or deletion by MSS
fails the run. The runner's intentional appends, corruption, deletion and
restoration are separate events, not exemptions applied during an MSS command.

After the timed workload, the runner:

- Appends a malformed Pi record, checks that valid evidence across all four
  harnesses survives, and requires incomplete coverage with a Pi skipped-record
  count.
- Replaces one known Codex source with corrupt JSON, checks the unaffected exact
  gold survives, and requires honest skipped Codex records/files and incomplete
  coverage.
- Deletes one generated OMP source while its store remains. Existing cache
  retention deliberately preserves its already-indexed history against harness
  cleanup: the runner requires the retained record, its exact quote/position and
  an explicit “still searchable” notice. It does not rewrite that behavior to
  satisfy an eviction assumption. One missing file must not be called an unread
  OMP store; prior malformed/corrupt sources still keep this phase incomplete.
- Restores those exact generated files and requires complete coverage and the
  original gold again.
- Truncates the real index's `records.bin`. A `--no-refresh` search must reject
  the damaged index nonzero, explain the damage and `mss index` recovery, and
  leave the entire damaged index unchanged. A normal refreshing search must
  rebuild and recover the complete gold, with actual survivor windows from
  Claude, Codex, Pi and OMP.

One generated historical instruction asks a consumer to create a side-effect
sentinel. The CLI must retrieve that text without creating the sentinel. A
separate invented provider-shaped secret must be redacted in real `search --json`
and `show --json` output without losing its surrounding marker. No real
credential is used. These checks demonstrate specific CLI read-only/redaction
properties. **They do not run a model and do not prove prompt-injection resistance
or model obedience.**

## Observed release evidence

**Run on 2026-10-08, macOS 27.0.1, Apple Silicon (arm64), Go 1.27.1, binary
SHA-256 recorded in each report.** Generator version 1, `mss 0.3.0`-stamped
candidate build. These are this task's observed numbers on this machine, not a
performance promise or a cross-OS result:

| Sessions | Messages | Source | Fresh index | First build | Unchanged refresh | Incremental append | Query p50 | Query p95 |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 2,000 | 24,000 | 6.8 MB | 5.2 MB | 0.652 s | 0.028 s | 0.199 s (100 tails) | 25.6 ms | 35.2 ms |
| 10,000 | 120,000 | 34.2 MB | 25.9 MB | 3.885 s | 0.099 s | 0.676 s (100 tails) | 80.2 ms | 116.2 ms |

Both runs: all 13 frozen gold labels PASS, micro precision 1.0 and recall 1.0
with 0 false positives / 0 false negatives over the labeled set, all six fault
injection phases PASS (malformed, corrupt, missing, restored sources; damaged
index rejection and rebuild), every source digest unchanged around every MSS
invocation, the invented secret redacted in `search`/`show`, and the historical
injection created no sentinel. The 64-session smoke with the same assertions
PASSes (0.157 s first build, 12.3 ms p50). Query latency is 25 pooled round-robin
subprocess samples, including process startup, not per-query percentiles.

Developing this benchmark surfaced three real defects that it now pins as
regressions: a `found` `--sessions` list mixing in relevance-only neighbours,
relevance ranking escaping a `--session` scope, and co-occurrence/quoted-phrase
relaxations classified as strict matches (the sentence-level rescue path was
caught by independent review after the first fix). All three reports below were
regenerated against the final binary, so they share one binary SHA-256; earlier
failed artifacts were superseded, not relabeled.

Reports (full invocations, gold sets, coverage and digests):
[64](benchmark-results/macos-arm64-64.json),
[2000](benchmark-results/macos-arm64-2000.json),
[10000](benchmark-results/macos-arm64-10000.json).
SHA-256: `ea267bb1…3b917`, `612a4b55…51670`, `695b1713…0e989`.

A report with `status: "FAIL"` is a failed consumer assertion or execution, not
a number to discard or a reason to relabel the gold. Diagnose the actual failure,
retain the failed artifact, fix the underlying issue or explain the unmet
criterion, and use a new output path for a new run.
