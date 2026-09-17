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

_Filled in after the run._
