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

| Repo | Why |
|------|-----|
| `httpie/cli` | Python CLI, long history, active issues (default) |
| `pallets/flask` | Python web framework, many contributors |
| `charmbracelet/bubbletea` | Go TUI, good for testing Go-specific patterns |
| `junegunn/fzf` | Go CLI, moderate size, clean history |
| `astral-sh/ruff` | Rust linter, large codebase, many TODOs |

## Adding Analysis Checks

Edit `eval/analyze.sh` — each check follows the pattern:

```bash
check "Description" PASS  # or WARN or FAIL
```

The `check` function tracks results and prints formatted output.
