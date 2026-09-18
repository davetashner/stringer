# Real-World Benchmark, September 2026

**Date:** 2026-09-16
**Stringer version:** v1.10.0 (Homebrew release binary)
**Beads issue:** stringer-2cs
**Previous run:** February 2026, v1.3.0 dev build, 13 collectors (PR #233)

## Purpose

The README "Real-world results" table dates from February 2026. Since then
stringer gained two collectors (duplication, coupling), four vulnerability
ecosystems (Composer, Swift, Scala, Hex), nesting-weighted complexity
(DR-024), the coupling aggregator exemption (DR-025), vuln reachability and
CVSS severity (DR-023), and tracked-files-only git hygiene. The February
numbers no longer describe what the tool produces.

This run has three goals:

1. Refresh the README table from a pinned, reproducible run on a release
   binary rather than a dev build.
2. Capture metrics the February run did not: per-collector signal counts,
   confidence distribution, scan versus report time, peak memory, and the
   commit each repository was scanned at.
3. Find false-positive patterns and performance hot spots, and file beads
   for them.

## What the February run measured, and what it missed

The February table recorded files, total signals, combined scan+report
time, and a hand-picked highlights phrase per repo. It did not record:

| Gap | Why it matters |
|-----|----------------|
| Commit SHA per repo | The repos have moved on; the numbers cannot be reproduced. |
| Which collectors ran | 13 then, 15 now. Totals are not comparable without a per-collector breakdown. |
| Confidence distribution | A total of 40,117 signals says nothing about how many a user should act on. UX2.3 (noise tolerance) needs this. |
| Scan vs report time | Users run one or the other. The combined number hides which is slow. |
| Peak memory | Large-repo guidance (docs/large-repos.md) has no memory numbers. |
| Slowest collector | Needed to decide where to spend optimization effort. |
| stderr diagnostics | Collector errors, timeouts and warnings were not captured. |
| Machine spec | Timings without hardware are not comparable. |

## Methodology

Runner: `eval/bench.sh` (added in this PR). For each repository:

1. `git clone --depth 100` (same as February) at whatever HEAD is on the run
   date. The SHA and commit date are recorded.
2. `stringer scan <repo> -f json -x github --no-color`, timed, peak RSS of
   the stringer process sampled once per second.
3. `stringer report <repo> -x github --no-color`, timed the same way.
4. `summary.json` written with signal counts by collector and kind,
   confidence buckets (high >= 0.8, medium 0.5 to 0.8, low < 0.5),
   per-collector durations parsed from the report header, and stderr
   error/warning/panic/timeout counts.

Repositories run sequentially, smallest first, so timings are not skewed
by contention. No collector timeout is set, matching February. The GitHub
collector is excluded because it needs a token, would pull thousands of open
issues on the large repos, and was not part of the February highlights.

"Files" is `git ls-files | wc -l` on the shallow clone.

Host: Apple Silicon, 10 cores, 16 GB RAM, macOS (Darwin 25.6). Recorded in
`environment.txt` alongside the results.

### Known confounders

- Repository drift. Every repo has months of new commits since February.
  Signal deltas mix upstream change with stringer change. Where a delta is
  surprising, the per-collector breakdown and a spot check of the signals
  decide which it is.
- Shallow history. `--depth 100` underreports lottery risk (stringer-8qi).
  A side experiment below quantifies this on one repo.
- Duplication cap. The duplication collector caps at 200 signals by default.
  A repo showing exactly 200 duplication signals is capped, not measured.

## Repositories

### The ten from February

| Repository | Language | Feb files | Feb signals | Feb time |
|------------|----------|----------:|------------:|---------:|
| gin-gonic/gin | Go | 131 | 83 | 5s |
| expressjs/express | JS | 214 | 65 | 2s |
| pallets/flask | Python | 236 | 111 | 6s |
| rust-lang/rustlings | Rust | 282 | 312 | 23s |
| tokio-rs/tokio | Rust | 848 | 825 | 36s |
| tiangolo/fastapi | Python | 2,867 | 607 | 20s |
| facebook/react | JS/TS | 6,840 | 4,415 | 2m 23s |
| django/django | Python | 7,014 | 3,254 | 2m 37s |
| vercel/next.js | JS/TS | 27,366 | 10,334 | 26m |
| kubernetes/kubernetes | Go | 28,284 | 40,117 | 1h 23m |

### Three new repositories

Selection criteria: an ecosystem or repo shape the February set does not
cover, support for it shipped after February, and a size that fits in the
run budget (under 10k files).

| Repository | Language | Why |
|------------|----------|-----|
| apache/kafka | Java + Scala, Gradle | First JVM repo in the table. Exercises the Gradle dependency parser for vuln and dephealth against Maven Central, Scala L1 support (v1.8), and JVM test-naming detection. Large, mature, many contributors: a realistic lottery-risk and churn profile. |
| laravel/framework | PHP, Composer | First PHP repo. PHP L1 support and the Composer vuln parser are both post-February and have only been tested on fixtures. Laravel is the highest-profile PHP codebase available, with a large test suite for test-detection heuristics. |
| jellyfin/jellyfin | C#, NuGet | First C# repo, and an application rather than a framework or library. The February cross-language evaluation validated .csproj parsing only on the small dotnet/aspire-samples collection. Jellyfin has many project files, real dependency churn, and configuration files that give configdrift something to find. |

Considered and not chosen: rails/rails (Ruby has test detection but no
Composer-style vuln parser, so vuln and dephealth would be N/A),
phoenixframework/phoenix (Elixir, small; good follow-up), grafana/grafana
(would exercise apidrift with a checked-in OpenAPI spec, but at 25k files it
costs as much as next.js).

## Metrics reported

Per repo, in the README table: files, signals, scan time, report time,
peak memory, share of signals at high confidence, and highlights.

In this document: signals by collector, slowest collector per repo,
pinned commits, February-to-September deltas with attribution, and the
precision spot check.

## Precision spot check

Signal totals reward noise. For each repository, the 10 highest-confidence
signals and 10 sampled from the most numerous kind are read against the
source and labelled true positive, false positive, or unclear. Systematic
false-positive patterns become beads. This is a spot check, not a
measurement; it is reported as counts, not as a precision percentage.

## Side experiment: shallow versus full history for lottery risk

stringer-8qi notes that shallow clones underreport lottery risk. On
pallets/flask, the lotteryrisk collector is run on the `--depth 100` clone
and on a full clone, and the signal counts and top files compared. The
result goes into the collector description or docs, and closes the bead.

## Results

Run on 2026-09-16/17. Host: Apple Silicon, 10 cores, 16 GB RAM, macOS 26.6.
Binary: stringer 1.10.0 from Homebrew. Raw outputs are under
`eval/results/bench-2026-09/` (gitignored); each repo has `summary.json`.

Three network outages hit the host during the run (after the Kafka report,
during the next.js report, and before the kubernetes clone). Affected runs
are marked. Vulnerability counts for next.js are partial: 16 OSV batch
requests failed during its scan.

### Overview

Scan and Report are the pipeline's collector phase, taken from the
monotonic clock (per-collector `duration=` in the scan log, summed over
workspaces along the slowest collector; the `Duration:` line in the report
header). Wall is `date`-based wall clock for the same commands and includes
time the host spent asleep; see "Clocks" below. Peak RSS is the stringer
process only. "High conf" is the share of signals with confidence >= 0.8.

| Repository | Language | Files | Signals | High conf | Scan | Report | Wall (scan / report) | Peak RSS | Feb signals | Feb time |
|------------|----------|------:|--------:|----------:|-----:|-------:|---------------------:|---------:|------------:|---------:|
| gin-gonic/gin | Go | 130 | 299 | 7% | 4s | 4s | 5s / 5s | 54 MB | 83 | 5s |
| expressjs/express | JS | 214 | 259 | 5% | 6s | 10s | 7s / 11s | 51 MB | 65 | 2s |
| pallets/flask | Python | 236 | 283 | 3% | 4s | 3s | 4s / 4s | 51 MB | 111 | 6s |
| rust-lang/rustlings | Rust | 293 | 499 | 14% | 4s | 4s | 5s / 5s | 48 MB | 312 | 23s |
| tokio-rs/tokio | Rust | 874 | 1,834 | 1% | 36s | 37s | 36s / 38s | 111 MB | 825 | 36s |
| jellyfin/jellyfin (new) | C# | 2,619 | 2,542 | 9% | 46s | 46s | 47s / 47s | 173 MB | - | - |
| tiangolo/fastapi | Python | 3,139 | 868 | 16% | 8s | 9s | 9s / 9s | 113 MB | 607 | 20s |
| laravel/framework (new) | PHP | 3,411 | 4,169 | 3% | 3m 29s | 3m 26s | 3m 30s / 3m 26s | 334 MB | - | - |
| facebook/react | JS/TS | 7,240 | 8,970 | 5% | 2m 48s | 2m 47s | 2m 49s / 2m 48s | 147 MB | 4,415 | 2m 23s |
| django/django | Python | 7,091 | 4,297 | 10% | 2m 35s | 2m 37s | 2m 35s / 16m 17s* | 419 MB | 3,254 | 2m 37s |
| apache/kafka (new) | Java/Scala | 7,547 | 12,069 | 4% | 52m 32s | 52m 18s | 1h 10m* / 1h 12m* | 1,077 MB | - | - |
| vercel/next.js | JS/TS | 32,471 | 14,027 | 17% | 34m 59s | 35m 05s | 2h 10m* / 4h 49m* | 660 MB | 10,334 | 26m |
| kubernetes/kubernetes | Go | 31,373 | K8S_SIGNALS | K8S_HIGH | K8S_SCAN | K8S_REPORT | K8S_WALL* | K8S_RSS | 40,117 | 1h 23m |

\* Host slept during the run; wall time is not meaningful for these.

February totals are not comparable with September totals. The February
build had 13 collectors; v1.10.0 has 15, and the duplication collector
alone adds up to 200 signals per workspace. See "Where the signals come
from" below.

#### Clocks

The django February time (2m 37s) equals this run's report `Duration:`
to the second (2m 37.4s). The February "Time" column was the report's
printed pipeline duration, so this run's Scan and Report columns use the
same definition and are directly comparable.

Wall clock diverged from that on the long runs because the host went to
sleep. `pmset -g log` shows the machine entering sleep in 15-minute cycles
with two-minute dark wakes throughout the Kafka, django and next.js runs
(for example asleep 07:05 to 07:18 on the 17th, which is the django
report's extra 13 minutes). Go's monotonic clock pauses during sleep, so
collector durations and the report `Duration:` exclude it, while `date`
does not. The three "network failures" that interrupted the run were wake
transitions. A CPU profile of the Kafka scan confirmed there is no hidden
work: 95% of the process's wall time was sampled and 91% of samples were in
one collector (see Performance). Future runs should use `caffeinate -i`.

### Where the signals come from

| Repository | complexity | coupling | deadcode | dephealth | docstale | duplication | githygiene | gitlog | lotteryrisk | patterns | todos | vuln |
|------------|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| gin-gonic/gin | 57 | 0 | 1 | 0 | 2 | 200 | 0 | 0 | 7 | 29 | 2 | 1 |
| expressjs/express | 39 | 0 | 0 | 0 | 1 | 200 | 0 | 1 | 6 | 10 | 0 | 2 |
| pallets/flask | 36 | 0 | 43 | 0 | 2 | 158 | 9 | 0 | 6 | 20 | 0 | 9 |
| rust-lang/rustlings | 29 | 0 | 3 | 0 | 1 | 161 | 0 | 1 | 65 | 97 | 141 | 1 |
| tokio-rs/tokio | 365 | 1 | 180 | 2 | 8 | 692 | 0 | 40 | 0 | 455 | 76 | 15 |
| jellyfin/jellyfin | 0 | 0 | 0 | 0 | 1 | 200 | 9 | 30 | 228 | 1,918 | 156 | 0 |
| tiangolo/fastapi | 111 | 0 | 104 | 0 | 39 | 200 | 32 | 2 | 90 | 233 | 50 | 7 |
| laravel/framework | 1,140 | 0 | 1,208 | 0 | 1 | 200 | 24 | 0 | 57 | 1,505 | 0 | 34 |
| facebook/react | 1,748 | 29 | 514 | 0 | 36 | 3,807 | 0 | 120 | 4 | 1,606 | 1,020 | 86 |
| django/django | 1,626 | 15 | 1,491 | 0 | 2 | 200 | 88 | 2 | 236 | 563 | 68 | 6 |
| apache/kafka | 5,208 | 0 | 1,186 | 0 | 530 | 200 | 63 | 0 | 96 | 4,625 | 161 | 0 |
| vercel/next.js | 8,262 | 79 | 145 | 0 | 20 | 1,564 | 3 | 1,012 | 1 | 2,051 | 802 | 88 |
| kubernetes/kubernetes | K8S_ROW |

Observations:

- Duplication hits its 200 cap on a 130-file repo and is 56% to 77% of all
  signals on every repo under 300 files. The cap is per workspace, so
  monorepos exceed it (tokio 692, react 3,807).
- Three collectors dominate the large repos: complexity, patterns
  (missing tests) and deadcode. Together they are 60% to 90% of signals on
  Kafka, Laravel, Django and Jellyfin.
- Half of next.js's signals (7,035 of 14,027) are in
  `packages/next/src/compiled/`, which holds vendored precompiled bundles.
- C# produced zero complexity, deadcode and coupling signals: the language
  is not in those collectors' tables.
- Kafka and Jellyfin produced zero vuln and dephealth signals because their
  dependency declarations (Gradle `libs.*` references, NuGet Central Package
  Management) are not parsed.

### Precision spot check

For each repo the 6 to 10 highest-confidence signals and 4 to 8 samples from
the most numerous kind were read against the source. Counts are what the
sample showed, not a measured precision.

| Repository | Top-confidence sample | Most numerous kind sample | Systematic issues found |
|------------|-----------------------|---------------------------|-------------------------|
| gin | 8/8 real complexity hot spots, though one is a test function | near-clone: 8/8 are 6-line test blocks, several overlapping the same region | overlapping windows, test-only clones |
| express | lottery risk on `test/fixtures` (40%) and `test/support` (100%) are trivial dirs; complexity flags `describe`/`it` callbacks | code-clone: 8/8 in test/ | test callbacks as functions, tiny dirs |
| flask | vuln: all 9 are `>=` floors reported as pinned versions; lottery risk on `tests/static` and `tests/templates` | near-clone: 5/8 test-only, one adjacent-line pair | version floors, docs secrets (6/9 in .rst), decorator-registered functions as dead |
| rustlings | todos are exercise instructions by design (141) | duplication 161 across exercises | expected for a tutorial repo |
| tokio | dephealth "yanked futures-util@0.3.0" is a `^0.3.0` floor; vuln on a workspace-internal crate | near-clone: mixed | trait impl methods and `#[test]` fns flagged as dead |
| jellyfin | large-file and churn are correct; 171 of 228 lottery-risk signals show the identical "60%" | missing-tests: 5/5 have tests in `tests/<Project>.Tests/` | C# unsupported in three collectors, CPM not parsed, sibling test projects |
| fastapi | vuln: `starlette>=0.46.0` floor; churn on release notes is bot traffic | missing-tests: 160/183 are `docs_src/` tutorials | demo paths, documented JWT key flagged 32 times across translations |
| laravel | vuln: composer `^7.4.0 \|\| ^8.0.0` floors (34) | missing-tests: `DynamoDbStore.php` has `DynamoDbStoreTest.php`; config files flagged | deadcode on a framework's public API (1,208) |
| react | vuln on devtools electron and `^1.2.3` minimist | code-clone: 4/4 test files, one adjacent-line pair | 2,291 of 3,807 duplication signals in `__tests__` |
| django | complexity and dead code plausible in `django/` | deadcode 1,153/1,491 in `tests/`; secrets 77/88 in tests and templates | test directories in deadcode and githygiene |
| kafka | top complexity hits are real (700-line generators, coordinator methods) | complex-function: 3,137 of 5,208 below 0.5 confidence | 1,305 of 3,677 flagged `src/main/java` files have `src/test/java/.../<Name>Test.java`; Gradle deps unparsed; 528 doc links with `{version}` placeholders |
| next.js | vuln and BUG markers real | complex-function: 6,463 of 8,262 in `compiled/` | vendored bundles not excluded |

### Performance

Slowest collector per repo, monotonic, summed across workspaces:

| Repository | Collector | Time | Share of scan |
|------------|-----------|-----:|--------------:|
| tokio-rs/tokio | deadcode | 23s | 63% |
| jellyfin/jellyfin | patterns | 46s | 98% |
| laravel/framework | deadcode | 3m 29s | 100% |
| django/django | deadcode | 2m 35s | 100% |
| facebook/react | gitlog | 1m 49s | 64% |
| apache/kafka | deadcode | 52m 32s | 100% |
| vercel/next.js | deadcode | 24m 14s | 69% |
| kubernetes/kubernetes | K8S_SLOWEST |

**The deadcode collector is superlinear in file count and dominates every
repo over 3k files.** 3m on Laravel (3.4k files), 52m on Kafka (7.5k),
88m across kubernetes's 34 workspaces. It runs single-threaded at 100% CPU
long after every other collector has finished. A CPU profile of the Kafka
scan (dev build of the same source) puts 91% of CPU in
`DeadCodeCollector.isDeadSymbol`, which for every declared symbol runs a
`strings.Contains` pre-filter and then a word-boundary regexp over the
full content of every file: O(symbols x files x bytes). An inverted index
of identifiers built in one pass would make each lookup constant time.
(stringer-nxx.1)

Second tier: gitlog on monorepos (react 1m 49s over 40 workspaces,
kubernetes 42m over 34) re-reads the same shared history per workspace;
patterns on Jellyfin (46s for 2.6k files) is slow for a file walk.

Memory stays modest: 1.1 GB peak on Kafka, K8S_RSS on kubernetes, 660 MB
on next.js.

### Side experiment: shallow versus full history

pallets/flask at the same commit (`d73fa1c`), lotteryrisk collector only:

| Clone | Signals | Directories flagged |
|-------|--------:|---------------------|
| `--depth 100` | 6 | src/flask (95%), tests (91%), tests/static (40%), tests/templates (40%), tests/test_apps (100%), tests/type_check (60%) |
| full (5,557 commits) | 1 | src/flask (64%) |

The shallow clone over-reports, the opposite of what stringer-8qi assumed.
Shallow blame attributes every line older than the clone boundary to the
boundary commit's author, so recent single-author activity looks like total
ownership. Jellyfin shows the same effect at scale: 171 of its 228
lottery-risk signals carry the identical "60%" figure. (stringer-nxx.6)

### Beads filed

Epic stringer-nxx with 13 children, each carrying the evidence above:

| Bead | Area | Priority |
|------|------|----------|
| nxx.1 | deadcode superlinear reference search | P1 |
| nxx.2 | version floors and ranges reported as pinned versions | P2 |
| nxx.3 | deadcode: trait impls, tests, decorated and public API functions | P2 |
| nxx.4 | duplication: overlapping windows, adjacent lines, test-only clones | P2 |
| nxx.5 | complexity: test files, JS test callbacks, Java threshold | P2 |
| nxx.6 | lotteryrisk: shallow history, trivial directories | P2 |
| nxx.7 | githygiene: secrets in docs and test fixtures | P3 |
| nxx.8 | patterns: test detection misses, config/DTO/demo files | P2 |
| nxx.9 | L1: C# in complexity, deadcode, coupling | P3 |
| nxx.10 | NuGet CPM, Gradle catalogs, Gradle in dephealth | P3 |
| nxx.11 | docstale: placeholders and site-root links | P3 |
| nxx.12 | log collector completion as it happens | P3 |
| nxx.13 | exclude compiled/vendored bundles by default | P2 |

### Reproducing

```bash
eval/bench.sh gin-gonic/gin pallets/flask ...        # clones at --depth 100
python3 eval/bench-table.py eval/results/bench-2026-09
python3 eval/bench-sample.py eval/results/bench-2026-09/pallets-flask
```

Commits scanned: gin `5c6a15f8f9`, express `9a34acf03c`, flask `d73fa1cdcb`,
rustlings `a650509c78`, tokio `66e13876a7`, jellyfin `50866380c9`,
fastapi `50113da16f`, laravel `59991e4511`, kafka `da07836344`,
react `2b19aecd0e`, django `8cbdd4a814`, next.js `22fffea9d4`,
kubernetes `516bfa2d8a`.
