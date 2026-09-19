# Stringer Evaluation Harness

Stress-tests stringer against real open-source repositories to discover bugs, quality issues, and improvement opportunities.

## Prerequisites

- `go` (builds stringer from source)
- `jq` (JSON analysis)
- `git`
- `gh` (optional, for GitHub collector — needs `GITHUB_TOKEN`)

## Quick Start

```bash
# Run against the default target (httpie/cli)
./eval/run-eval.sh

# With GitHub collector enabled
GITHUB_TOKEN=$(gh auth token) ./eval/run-eval.sh

# Specify a different repo
./eval/run-eval.sh pallets/flask

# Re-run without re-cloning
./eval/run-eval.sh httpie/cli --reuse
```

## Output

Results land in `eval/results/<repo-name>/`:

| File | Description |
|------|-------------|
| `scan-beads.jsonl` | Beads JSONL output |
| `scan-json.json` | JSON envelope output |
| `scan-markdown.md` | Markdown output |
| `scan-tasks.txt` | Tasks output |
| `scan-dryrun.json` | Dry-run machine-readable summary |
| `report.txt` | Full report output |
| `stderr-*.log` | Stderr from each command |
| `timing.txt` | Per-command wall-clock times |
| `analysis.txt` | Quality analysis report |

## Interpreting Results

The analysis report uses three labels:

- **PASS** — Check passed, no issues found
- **WARN** — Potential issue worth investigating
- **FAIL** — Known bug or quality problem confirmed

A summary line at the end shows totals: `Summary: X PASS, Y WARN, Z FAIL`

## Suggested Repos

| Repo | Language | Signals | Time | Notes |
|------|----------|---------|------|-------|
| `httpie/cli` | Python | ~200+ | ~30s | Default target; long history, active issues |
| `charmbracelet/bubbletea` | Go | ~120 | ~4s | Fast; good Go pattern/lottery-risk coverage |
| `pallets/flask` | Python | ~150+ | ~15s | Many contributors, good lottery risk spread |
| `junegunn/fzf` | Go | ~80+ | ~5s | Moderate size, clean history |
| `astral-sh/ruff` | Rust | ~300+ | ~60s | Large codebase, many TODOs |

Signal counts and timings are approximate and will vary with repo activity.

### Quick regression check

For fast iteration, use bubbletea — it completes in ~4s and exercises all non-GitHub collectors:

```bash
./eval/run-eval.sh charmbracelet/bubbletea --reuse
```

## Adding Analysis Checks

Edit `eval/analyze.sh` — each check follows the pattern:

```bash
check "Description" PASS  # or WARN or FAIL
```

The `check` function tracks results and prints formatted output.

## Benchmark regression check

`.github/workflows/bench-regression.yml` guards collector accuracy and speed
without re-running the full benchmark in `docs/research/benchmark-2026-09.md`.
It runs nightly, on demand, and on pull requests that touch
`internal/collectors/`, `internal/pipeline/`, `internal/gitcli/`,
`cmd/stringer/`, `eval/` or `go.mod`/`go.sum`.

How it works:

- `eval/baseline/<owner>-<repo>.json` pins each repo to a full commit SHA and
  records per-collector signal counts, per-kind counts and durations.
- `eval/bench-regression.sh` builds stringer (or takes `--bin`), then runs
  `eval/bench.sh --no-report --depth 100 --exclude github,vuln,dephealth
  owner/repo@<sha> ...`. The `@<sha>` target syntax fetches exactly that
  commit with 100 commits of history below it, so every run scans the same
  bytes and the same history.
- `eval/bench-check.py` compares the results with the baseline, prints a
  table per repo, and exits non-zero on drift. On a failed nightly run the
  workflow opens (or comments on) a "Benchmark regression on main" issue. The
  results directory, minus the clones, is uploaded as an artifact.

What is compared, and what is not:

| Rule | Why |
|------|-----|
| `github`, `vuln`, `dephealth` are not run | They query GitHub, OSV and package registries, which change daily. |
| gitlog `churn` and `stale-branch` kinds are ignored | Churn counts commits in the last 90 days and stale branches are 30 days old, both measured from now, so they drift with the calendar even at a fixed commit. |
| docstale `doc-code-drift` is ignored | Its co-change window is `git log --since=1y` relative to now (and git reads `1y` as the first of the current month, see stringer-q6y). `stale-doc` compares commit dates and is kept. |
| lotteryrisk is compared | Recency decay scales every commit's weight by the same factor as time passes, so ownership shares, and the signal count, do not change with the date. `GITHUB_TOKEN` is unset for the scan so review analysis stays off. |
| Confidence is not compared | TODO recency boosts and churn co-location boosts depend on the date. |
| Counts must match exactly | The inputs are pinned. A collector entry can carry `"tolerance": N` to allow an absolute difference of N. |
| Durations use generous ceilings | A collector fails only above `perf.max_seconds` (60 s), or above both `perf.max_ratio` (10x) times its baseline and `perf.ratio_floor_seconds` (5 s). |

Run it locally (about 15 seconds on an M-series laptop):

```bash
eval/bench-regression.sh                 # builds ./cmd/stringer, results in eval/results/bench-regression
```

### Updating the baseline on purpose

When a pull request changes signal counts intentionally (a new detector, a
false-positive fix), regenerate the baseline with the PR's code and commit it
in the same PR, so the reviewer sees the count change next to the code change:

```bash
eval/bench-regression.sh --update        # or: python3 eval/bench-check.py --update <results-dir>
git add eval/baseline && git commit -s -m "test: update benchmark baseline for <change>"
```

`--update` keeps each file's `ignored_kinds`, `perf` and per-collector
`tolerance` settings. To add or re-pin a repo, run
`eval/bench.sh --no-report --exclude github,vuln,dephealth owner/repo@<full-sha>`
into a results directory and pass that directory to `bench-check.py --update`.
