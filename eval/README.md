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
