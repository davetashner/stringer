# Stringer 🧵

**Codebase archaeology for [Beads](https://github.com/steveyegge/beads). Mine your repo's history and code for actionable work items, output them as Beads-formatted issues, and give your AI agents instant situational awareness on any codebase.**

```bash
# Install
go install github.com/yourusername/stringer/cmd/stringer@latest

# Scan a repo and seed beads
cd your-project
stringer scan . | bd import -i -

# That's it. Your agents now have context.
bd ready --json
```

## The Problem

You adopt [Beads](https://github.com/steveyegge/beads) to give your coding agents persistent memory. On a new project, this works great — agents file issues as they go, and the dependency graph grows organically.

But most real work happens on **existing codebases**. When an agent boots up on a 50k-line repo with an empty `.beads/` directory, it has zero context. It doesn't know about the 47 TODOs scattered across the codebase, the module that gets reverted every other week, or the authentication refactor that's been half-finished for six months.

Stringer solves the cold-start problem. It mines signals that already exist in your repo — TODO comments, git history patterns, open GitHub issues — and produces a structured set of Beads issues that agents can immediately orient around.

## How It Works

Stringer runs a pipeline of **collectors** that extract raw signals, then optionally uses an LLM pass to cluster, deduplicate, and prioritize them into well-structured Beads issues.

```
┌─────────────────────────────────────────────┐
│              Target Repository               │
└──────────┬──────────┬──────────┬────────────┘
           │          │          │
     ┌─────▼──┐ ┌─────▼──┐ ┌────▼─────┐
     │ TODO/  │ │  Git   │ │ GitHub   │  ... extensible
     │ FIXME  │ │  Log   │ │ Issues   │
     │ Scan   │ │Analysis│ │ Import   │
     └─────┬──┘ └─────┬──┘ └────┬─────┘
           │          │          │
           ▼          ▼          ▼
     ┌─────────────────────────────────┐
     │       Raw Signal Stream         │
     │  (deduplicated, normalized)     │
     └──────────────┬──────────────────┘
                    │
              ┌─────▼──────┐
              │ LLM Pass   │  (optional)
              │ - Cluster   │
              │ - Prioritize│
              │ - Infer deps│
              └─────┬──────┘
                    │
              ┌─────▼──────┐
              │ Beads JSONL │
              │   Output    │
              └─────────────┘
                    │
                    ▼
              bd import -i -
```

## Collectors

| Collector | What it finds | Flag |
|-----------|--------------|------|
| **todos** | `TODO`, `FIXME`, `HACK`, `XXX`, `BUG`, `OPTIMIZE` comments with file location and git blame authorship | `--collectors=todos` |
| **gitlog** | Reverted commits, high-churn files (edited frequently, likely unstable), WIP/stale branches, repeated changes to the same code | `--collectors=gitlog` |
| **github** | Open issues and PRs from the GitHub API, mapped to beads issue types | `--collectors=github` |
| **patterns** | Large files, dead code indicators, deeply nested directories, files with no test coverage (heuristic) | `--collectors=patterns` |

All collectors are enabled by default. Disable any with `--collectors=todos,gitlog` (only listed collectors run).

## Usage

### Basic: scan and import

```bash
# Scan current directory, pipe directly to beads
stringer scan . | bd import -i -

# Preview what would be generated (no output file)
stringer scan . --dry-run

# Save to file, review, then import
stringer scan . -o signals.jsonl
cat signals.jsonl     # review
bd import -i signals.jsonl
```

### Selective scanning

```bash
# Only TODO/FIXME comments
stringer scan . --collectors=todos

# Only git history analysis
stringer scan . --collectors=gitlog

# Git history + GitHub issues (requires GITHUB_TOKEN)
stringer scan . --collectors=gitlog,github
```

### Without LLM (deterministic mode)

```bash
# Skip clustering/prioritization, one bead per signal
stringer scan . --no-llm

# Useful for CI, air-gapped environments, or quick scans
```

### Tuning

```bash
# Only high-confidence signals
stringer scan . --min-confidence=0.7

# Limit git log depth
stringer scan . --git-depth=500

# Include closed/merged GitHub issues as pre-closed beads (for agent context)
stringer scan . --include-closed

# Custom priority threshold for churn detection
stringer scan . --churn-threshold=10
```

### Output formats

```bash
# Beads JSONL (default, ready for bd import)
stringer scan . -f beads

# Human-readable markdown summary
stringer scan . -f markdown

# Raw signals as JSON (for debugging or custom processing)
stringer scan . -f json
```

## What Stringer Produces

A typical scan of a mature repo might generate beads like:

```
bd-a3f2  P1  bug    Fix authentication bypass in session handler
               └─ Source: TODO comment in auth/session.go:142 (authored by alice, 8 months old)

bd-e8c1  P2  task   Stabilize payment processing module
               └─ Source: gitlog churn detection (payments/ modified 34 times in 60 days, 3 reverts)

bd-7b19  P2  chore  Remove deprecated v1 API endpoints
               └─ Source: TODO scan (12 related TODOs across api/v1/), clustered by LLM

bd-d4a0  P3  task   Add test coverage for user registration flow
               └─ Source: pattern detection (user/registration.go: 450 lines, 0 test files)

bd-91fe  P1  bug    Fix race condition in worker pool
               └─ Source: GitHub issue #234 (open, 3 months, 7 comments)
```

When the LLM pass is enabled, related signals get clustered — those 12 scattered TODO comments about removing the v1 API become a single actionable bead with the individual locations listed in the description. Parent-child relationships are inferred where possible (e.g., the v1 API removal becomes an epic with child tasks per endpoint group).

## Integration with Beads

Stringer is designed as a companion tool, not a replacement for any part of Beads. The workflow:

1. **Bootstrap:** Run `stringer scan` once to seed `.beads/` with discovered work
2. **Agent takeover:** Agents use `bd ready --json` to find work, file new issues with `bd create` as they go
3. **Periodic refresh:** Optionally re-run `stringer scan --delta` to find new signals since last scan (coming soon)

Stringer-generated beads are tagged with `stringer-generated` so agents and humans can distinguish them from manually filed issues.

## Configuration

Stringer looks for `.stringer.yaml` in the repo root or `~/.config/stringer/config.yaml` globally:

```yaml
# .stringer.yaml
collectors:
  todos:
    patterns: ["TODO", "FIXME", "HACK", "XXX", "BUG", "OPTIMIZE"]
    ignore_paths: ["vendor/", "node_modules/", ".git/"]
  gitlog:
    depth: 1000
    churn_threshold: 10      # file changes in 90-day window
    revert_lookback: 180     # days to scan for reverts
  github:
    token_env: "GITHUB_TOKEN"
    include_prs: true
    include_closed: false
  patterns:
    max_file_lines: 500      # flag files longer than this
    min_test_ratio: 0.1      # flag modules below this test-to-source ratio

analyzer:
  model: "claude-sonnet-4-5-20250929"
  min_confidence: 0.5
  cluster_similarity: 0.7    # threshold for grouping related signals

output:
  format: "beads"
  add_label: "stringer-generated"
```

## Design Principles

**Read-only.** Stringer never modifies the target repository. It reads git history, scans files, and writes output to stdout or a specified file. You decide when to `bd import`.

**Composable collectors.** Each collector is independent, testable, and can run in isolation. Adding new signal sources (Jira, GitLab, Sentry, etc.) means implementing one Go interface.

**LLM-optional.** The core scanning works without any API keys. The LLM pass adds intelligence (clustering, priority inference, dependency detection) but isn't required. `--no-llm` mode is fully supported and deterministic.

**Idempotent.** Running stringer twice on the same repo produces the same output. Signal deduplication uses content-based hashing. This means you can safely re-run scans without creating duplicate beads.

**Beads-native output.** The JSONL output is validated against `bd import` expectations. If `bd import` can't consume it, that's a stringer bug.

## Requirements

- Go 1.22+
- Git (for git log analysis)
- `bd` CLI (for importing output into beads)
- `GITHUB_TOKEN` (optional, for GitHub issues collector)
- Anthropic API key (optional, for LLM clustering pass)

## Contributing

See [AGENTS.md](./AGENTS.md) for architecture details and development workflow. This project uses Beads for task tracking — run `bd ready --json` to find open work.

## License

MIT
