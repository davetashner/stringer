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
| kubernetes/kubernetes | Go | 31,373 | 51,542 | 20% | 1h 57m | 1h 50m | 3h 06m* / 1h 50m | 2,152 MB | 40,117 | 1h 23m |

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
| kubernetes/kubernetes | 24,140 | 102 | 952 | 187 | 72 | 5,166 | 199 | 4,998 | 89 | 11,937 | 3,531 | 169 |

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
- Kubernetes: 11,030 signals (21%) are in generated files (`zz_generated*`,
  `*.pb.go`), and 39,390 (76%) are under `staging/`, the published library
  modules of the go.work workspace.
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
| kubernetes | vuln (169) and dephealth (187) fire on the 34 go.work modules; churn (4,862) is per-workspace | complex-function: 8,622 of 24,140 in `_test.go`, 5,774 in generated files | 11,030 signals (21%) in `zz_generated*` / `*.pb.go`; 76% of all signals under `staging/` |

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
| kubernetes/kubernetes | deadcode | 1h 27m | 47% |

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

Memory: 2.2 GB peak on kubernetes, 1.1 GB on Kafka, 660 MB on next.js.

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

## Results after the fixes (main at d85a249, 2026-09-19)

Every bead filed from this benchmark was implemented and merged between
2026-09-18 and 2026-09-19 (epic stringer-nxx, PRs #431 to #448, plus
follow-ups #450 to #454 under stringer-jfh). The same 13 repositories were
re-run with the same runner on a build of main at d85a249 under
`caffeinate -i`, so wall clock and monotonic timings agree this time.
Repositories were re-cloned at `--depth 100`, so their HEADs moved by a few
days; the commit SHAs are listed at the end.

What changed in the tool between the two runs:

| Area | Change | PR |
|------|--------|----|
| deadcode | Inverted identifier index replaces per-symbol regex over every file | #433 |
| deadcode | Trait impls, tests, decorated handlers skipped; library public API suppressed by default | #442, #454 |
| excludes | `compiled/`, minified and generated files excluded from noise-prone collectors | #431 |
| duplication | Overlapping windows merged, adjacent-line artifacts dropped, test-only clones gated at 12 lines, directory-aware test detection, stable sort | #438, #450 |
| complexity | Test code skipped, JS test callbacks named, non-Go floor raised to score 12 | #434 |
| lotteryrisk | Shallow clones capped at 0.5 and annotated; tiny and static directories skipped | #432 |
| patterns | Repo-wide test index (Maven, C# sibling projects, prefixed names), config/DTO/demo exclusions, mirrored-tree ratios | #436, #445, #448 |
| vuln, dephealth | Version floors reported as floors at 0.6x confidence; lockfiles preferred; workspace members skipped | #437, #444 |
| vuln, dephealth | NuGet Central Package Management; Gradle catalogs and `libs.*`; Gradle in dephealth | #446, #447, #453 |
| dephealth | 10 s registry timeout, 8-way parallel lookups, repo1.maven.org metadata instead of solrsearch | #451, #452 |
| githygiene | Secrets detector skips docs, templates, docstrings and obvious placeholders | #435 |
| docstale | Template placeholders, site-root and directory links resolved against detected static-site layouts | #441 |
| C# | Added to complexity, deadcode and coupling | #443 |
| pipeline | Per-collector completion logged as it happens | #440 |

#### Signals and timing: v1.10.0 versus main

| Repository | Signals before | Signals after | Change | High before | High after | Scan before | Scan after | Report before | Report after |
|------------|---------------:|--------------:|-------:|------------:|-----------:|------------:|-----------:|--------------:|-------------:|
| gin-gonic/gin | 299 | 166 | -44% | 7% | 8% | 4s | 2s | 4s | 2s |
| expressjs/express | 259 | 97 | -63% | 5% | 0% | 6s | 2s | 10s | 2s |
| pallets/flask | 283 | 100 | -65% | 3% | 0% | 4s | 2s | 3s | 2s |
| rust-lang/rustlings | 499 | 356 | -29% | 14% | 1% | 4s | 3s | 4s | 3s |
| tokio-rs/tokio | 1,834 | 1,141 | -38% | 1% | 1% | 36s | 21s | 37s | 21s |
| jellyfin/jellyfin | 2,542 | 2,969 | +17% | 9% | 11% | 46s | 35s | 46s | 34s |
| tiangolo/fastapi | 868 | 407 | -53% | 16% | 12% | 8s | 7s | 9s | 7s |
| laravel/framework | 4,169 | 1,589 | -62% | 3% | 2% | 3m 29s | 18s | 3m 26s | 18s |
| django/django | 4,297 | 1,293 | -70% | 10% | 13% | 2m 35s | 21s | 2m 37s | 20s |
| facebook/react | 8,970 | 5,587 | -38% | 5% | 9% | 2m 48s | 1m 46s | 2m 47s | 1m 47s |
| apache/kafka | 12,069 | 4,509 | -63% | 4% | 8% | 52m 32s | 1m 09s | 52m 18s | 1m 07s |
| kubernetes/kubernetes | 51,542 | 28,388 | -45% | 20% | 10% | 1h 57m | 34m 43s | 1h 50m | 34m 09s |
| vercel/next.js | 14,027 | 5,027 | -64% | 17% | 9% | 34m 59s | 9m 40s | 35m 05s | 9m 29s |

#### Signals by collector: before -> after

| Repository | complexity | coupling | deadcode | dephealth | docstale | duplication | githygiene | gitlog | lotteryrisk | patterns | todos | vuln |
|------------|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| gin-gonic/gin | 57 -> 39 | 0 -> 0 | 1 -> 0 | 0 -> 0 | 2 -> 2 | 200 -> 96 | 0 -> 0 | 0 -> 0 | 7 -> 4 | 29 -> 22 | 2 -> 2 | 1 -> 1 |
| expressjs/express | 39 -> 4 | 0 -> 0 | 0 -> 0 | 0 -> 0 | 1 -> 1 | 200 -> 78 | 0 -> 0 | 1 -> 1 | 6 -> 4 | 10 -> 7 | 0 -> 0 | 2 -> 2 |
| pallets/flask | 36 -> 14 | 0 -> 0 | 43 -> 1 | 0 -> 0 | 2 -> 2 | 158 -> 53 | 9 -> 2 | 0 -> 0 | 6 -> 3 | 20 -> 16 | 0 -> 0 | 9 -> 9 |
| rust-lang/rustlings | 29 -> 14 | 0 -> 0 | 3 -> 1 | 0 -> 0 | 1 -> 1 | 161 -> 106 | 0 -> 0 | 1 -> 1 | 65 -> 30 | 97 -> 62 | 141 -> 141 | 1 -> 0 |
| tokio-rs/tokio | 365 -> 76 | 1 -> 1 | 180 -> 6 | 2 -> 2 | 8 -> 8 | 692 -> 436 | 0 -> 0 | 40 -> 90 | 0 -> 0 | 455 -> 431 | 76 -> 76 | 15 -> 15 |
| jellyfin/jellyfin | 0 -> 915 | 0 -> 42 | 0 -> 227 | 0 -> 0 | 1 -> 1 | 200 -> 200 | 9 -> 8 | 30 -> 30 | 228 -> 122 | 1,918 -> 1,268 | 156 -> 156 | 0 -> 0 |
| tiangolo/fastapi | 111 -> 67 | 0 -> 0 | 104 -> 1 | 0 -> 0 | 39 -> 0 | 200 -> 200 | 32 -> 13 | 2 -> 2 | 90 -> 35 | 233 -> 32 | 50 -> 50 | 7 -> 7 |
| laravel/framework | 1,140 -> 261 | 0 -> 0 | 1,208 -> 364 | 0 -> 0 | 1 -> 0 | 200 -> 200 | 24 -> 12 | 0 -> 0 | 57 -> 32 | 1,505 -> 686 | 0 -> 0 | 34 -> 34 |
| django/django | 1,626 -> 520 | 15 -> 15 | 1,491 -> 7 | 0 -> 0 | 2 -> 2 | 200 -> 200 | 88 -> 76 | 2 -> 4 | 236 -> 111 | 563 -> 284 | 68 -> 68 | 6 -> 6 |
| facebook/react | 1,748 -> 1,043 | 29 -> 29 | 514 -> 2 | 0 -> 0 | 36 -> 36 | 3,807 -> 2,147 | 0 -> 0 | 120 -> 120 | 4 -> 0 | 1,606 -> 1,104 | 1,020 -> 1,020 | 86 -> 86 |
| apache/kafka | 5,208 -> 1,367 | 0 -> 0 | 1,186 -> 594 | 0 -> 0 | 530 -> 0 | 200 -> 200 | 63 -> 47 | 0 -> 0 | 96 -> 63 | 4,625 -> 2,077 | 161 -> 161 | 0 -> 0 |
| kubernetes/kubernetes | 24,140 -> 8,823 | 102 -> 102 | 952 -> 934 | 187 -> 187 | 72 -> 105 | 5,166 -> 3,174 | 199 -> 199 | 4,998 -> 4,624 | 89 -> 40 | 11,937 -> 6,581 | 3,531 -> 3,522 | 169 -> 97 |
| vercel/next.js | 8,262 -> 1,093 | 79 -> 79 | 145 -> 92 | 0 -> 0 | 20 -> 20 | 1,564 -> 1,087 | 3 -> 1 | 1,012 -> 176 | 1 -> 0 | 2,051 -> 1,610 | 802 -> 777 | 88 -> 92 |

#### Slowest collector after the fixes

| Repository | Collector | Time | Share of scan |
|------------|-----------|-----:|--------------:|
| gin-gonic/gin | duplication | 2s | 100% |
| expressjs/express | duplication | 2s | 100% |
| pallets/flask | vuln | 2s | 100% |
| rust-lang/rustlings | todos | 3s | 100% |
| tokio-rs/tokio | gitlog | 16s | 78% |
| jellyfin/jellyfin | patterns | 35s | 100% |
| tiangolo/fastapi | duplication | 7s | 100% |
| laravel/framework | patterns | 18s | 100% |
| django/django | complexity | 21s | 100% |
| facebook/react | gitlog | 1m 33s | 87% |
| apache/kafka | patterns | 1m 09s | 100% |
| kubernetes/kubernetes | gitlog | 30m 23s | 88% |
| vercel/next.js | gitlog | 9m 39s | 100% |


#### Pinned commits (re-run)

| Repository | Commit | Date | Errors/Warnings (scan stderr) |
|------------|--------|------|------------------------------|
| gin-gonic/gin | `5c6a15f8f9` | 2026-09-16 | 0/1 |
| expressjs/express | `9a34acf03c` | 2026-09-15 | 0/1 |
| pallets/flask | `d73fa1cdcb` | 2026-09-08 | 0/1 |
| rust-lang/rustlings | `a650509c78` | 2026-08-29 | 0/1 |
| tokio-rs/tokio | `cf782c5b91` | 2026-09-17 | 0/10 |
| jellyfin/jellyfin | `50866380c9` | 2026-09-16 | 0/1 |
| tiangolo/fastapi | `50113da16f` | 2026-09-01 | 0/1 |
| laravel/framework | `1d9727160a` | 2026-09-18 | 0/1 |
| facebook/react | `59aff3e18c` | 2026-09-18 | 0/40 |
| django/django | `862ade3409` | 2026-09-18 | 0/1 |
| apache/kafka | `995cfcf99f` | 2026-09-19 | 0/1 |
| vercel/next.js | `7b58e5880c` | 2026-09-18 | 0/44 |
| kubernetes/kubernetes | `96b5e4e3ae` | 2026-09-18 | 0/34 |

Kafka dependency health in this run reported nothing because repo1.maven.org rate-limited every lookup (HTTP 429) after concurrent test runs; re-measured alone afterwards: 5 stale Maven artifacts (jopt-simple, jaxb-api, activation, metrics-core, argparse4j) in under one second (stringer-jfh.6 tracks making rate limiting visible). The warning counts in the last column are the per-workspace shallow-history notices from the lottery-risk collector, not errors.


Reading the comparison:

- The signal totals fall on every repository except jellyfin, where C#
  support adds complexity, dead-code and coupling signals that did not exist
  before. Jellyfin's remaining bulk is missing-tests.
- The "High" share falls on several small repos. That is the intended
  effect: the high-confidence signals in the first run were the mislabelled
  version floors and complexity hits on test files. What remains at 0.8 or
  above is a shorter, honest list.
- Timing is dominated by the dead-code fix. Kafka's scan goes from 52
  minutes of collector time to about a minute, next.js from 35 minutes to
  under ten, kubernetes from two hours to 35 minutes. Dependency health
  on Gradle repos is bounded by the registry timeout.
- Duplication still hits its per-workspace cap on the larger repos. The
  cap is now applied to merged regions rather than raw windows, so the 200
  signals describe 200 distinct regions.

### Open follow-ups

- stringer-jfh.5: gitlog re-walks the shared history once per workspace; it is now the slowest collector on kubernetes (30 min summed over 34 workspaces) and next.js.
- stringer-jfh.6: registry rate limiting (HTTP 429) is invisible; dephealth reports zero findings instead of "lookups failed".
- Jellyfin still reports 1,268 missing-tests; the C# data-class rule may need to cover `Dto` suffixes and `Models/` directories.
- Duplication reaches its per-workspace cap on every repository over 3k files. The cap is per workspace and applied after merging, so the count is a floor, not a measurement.
- The "High" share is now 0% on express and flask: their only high-confidence signals were the version floors that are now reported at 0.5. A repository with no real findings above 0.8 should read as such.

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
