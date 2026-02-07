# Stringer

[![CI](https://github.com/davetashner/stringer/actions/workflows/ci.yml/badge.svg)](https://github.com/davetashner/stringer/actions/workflows/ci.yml)
[![Coverage](https://img.shields.io/badge/coverage-55%25+-brightgreen)](https://github.com/davetashner/stringer/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/davetashner/stringer)](https://goreportcard.com/report/github.com/davetashner/stringer)
[![Release](https://img.shields.io/github/v/release/davetashner/stringer)](https://github.com/davetashner/stringer/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

> **Status: v0.1.0 — MVP.** Works today with TODO/FIXME scanning and Beads JSONL output. One collector, one output format. See [Current Limitations](#current-limitations) for what's not here yet.

**Codebase archaeology for [Beads](https://github.com/steveyegge/beads).** Mine your repo for actionable work items, output them as Beads-formatted issues, and give your AI agents instant situational awareness.

```bash
# Install
go install github.com/davetashner/stringer/cmd/stringer@latest

# Scan a repo and seed beads
cd your-project
stringer scan . | bd import -i -

# That's it. Your agents now have context.
bd ready --json
```

## The Problem

You adopt [Beads](https://github.com/steveyegge/beads) to give your coding agents persistent memory. On a new project, agents file issues as they go and the dependency graph grows organically.

But most real work happens on **existing codebases**. When an agent boots up on a 50k-line repo with an empty `.beads/` directory, it has zero context. It doesn't know about the 47 TODOs scattered across the codebase or the half-finished refactor that's been sitting there for six months.

Stringer solves the cold-start problem. It mines signals already present in your repo and produces structured Beads issues that agents can immediately orient around.

## What It Does Today

Stringer v0.1.0 has one collector and one output format:

- **TODO collector** (`todos`) — Scans source files for `TODO`, `FIXME`, `HACK`, `XXX`, `BUG`, and `OPTIMIZE` comments across common comment styles (`//`, `#`, `/* */`, `*`, `--`)
- **Git blame enrichment** — Each signal is enriched with author name and timestamp from `git blame`
- **Confidence scoring** — Keyword-specific base scores with age-based boosts (see [How Output Works](#how-output-works))
- **Beads JSONL output** — Produces JSONL ready for `bd import`, with deterministic content-based IDs
- **Dry-run mode** — Preview signal counts without producing output

```
┌─────────────────────────────────┐
│       Target Repository         │
└────────────────┬────────────────┘
                 │
          ┌──────▼──────┐
          │ TODO/FIXME  │  ... extensible
          │   Scanner   │
          └──────┬──────┘
                 │
          ┌──────▼──────┐
          │ Git Blame   │
          │ Enrichment  │
          └──────┬──────┘
                 │
          ┌──────▼──────┐
          │ Beads JSONL │
          │   Output    │
          └──────┬──────┘
                 │
                 ▼
           bd import -i -
```

## What to Expect

Output volume depends on codebase size and coding style:

| Codebase | Approximate signals |
|----------|-------------------|
| Small (<5k LOC) | 5-30 |
| Medium (10k-50k LOC) | 20-200 |
| Large (100k+ LOC) | 100-1,000+ |

**Recommendation:** Use `--dry-run` first to see signal counts, then use `--max-issues` to cap output on your first scan.

```bash
# Preview how many signals exist
stringer scan . --dry-run

# Start with a manageable batch
stringer scan . --max-issues 50 | bd import -i -
```

## Getting Started

Start small. You can always scan again.

```bash
# 1. Preview signal count
stringer scan . --dry-run

# 2. Import a capped first batch (highest-confidence signals first)
stringer scan . --max-issues 20 | bd import -i -

# 3. See what your agents can now work on
bd ready --json

# 4. When ready, import everything
stringer scan . | bd import -i -
```

### Save to file for review

```bash
stringer scan . -o signals.jsonl
cat signals.jsonl          # review
bd import -i signals.jsonl
```

### Machine-readable dry run

```bash
stringer scan . --dry-run --json
```

```json
{
  "total_signals": 70,
  "collectors": [
    {
      "name": "todos",
      "signals": 70,
      "duration": "303.6685ms"
    }
  ],
  "duration": "303.724958ms",
  "exit_code": 0
}
```

## Usage Reference

```
stringer scan [path] [flags]
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--collectors` | `-c` | (all) | Comma-separated list of collectors to run |
| `--format` | `-f` | `beads` | Output format |
| `--output` | `-o` | stdout | Output file path |
| `--dry-run` | | | Show signal count without producing output |
| `--json` | | | Machine-readable output for `--dry-run` |
| `--max-issues` | | `0` | Cap output count (0 = unlimited) |
| `--no-llm` | | | Skip LLM clustering pass (noop — reserved for future use) |

**Global flags:** `--quiet` (`-q`), `--verbose` (`-v`), `--no-color`, `--help` (`-h`)

**Available collectors:** `todos`

**Available formats:** `beads`

## How Output Works

### Confidence Scoring

Each signal gets a confidence score (0.0-1.0) based on keyword severity and age from git blame:

**Base scores by keyword:**

| Keyword | Base Score |
|---------|-----------|
| `BUG` | 0.7 |
| `FIXME` | 0.6 |
| `HACK` | 0.55 |
| `TODO` | 0.5 |
| `XXX` | 0.5 |
| `OPTIMIZE` | 0.4 |

**Age boost from git blame:**
- Older than 1 year: +0.2
- Older than 6 months: +0.1
- No blame data or recent: +0.0

Score is capped at 1.0. See [DR-004](docs/decisions/004-confidence-scoring-semantics.md) for the full design rationale.

### Priority Mapping

Confidence maps to bead priority:

| Confidence | Priority |
|-----------|----------|
| >= 0.8 | P1 |
| >= 0.6 | P2 |
| >= 0.4 | P3 |
| < 0.4 | P4 |

### Content-Based Hashing

Each signal gets a deterministic ID: `SHA-256(source + kind + filepath + line + title)`, truncated to 8 hex characters with a `str-` prefix (e.g., `str-0e4098f9`). Re-scanning the same repo produces the same IDs, preventing duplicate beads on reimport.

### Labels

Every signal is tagged with:
- The keyword kind (e.g., `todo`, `fixme`, `hack`)
- `stringer-generated` — distinguishes stringer output from manually filed issues
- The collector name (`todos`)

### Sample Output

Given this source file:

```go
// TODO: Add proper CLI argument parsing
// FIXME: This will panic on nil input
// HACK: Temporary workaround until upstream fixes the API
```

Stringer produces:

```jsonl
{"id":"str-0e4098f9","title":"TODO: Add proper CLI argument parsing","description":"Location: main.go:6","type":"task","priority":3,"status":"open","created_at":"","created_by":"stringer","labels":["todo","stringer-generated","stringer-generated","todos"]}
{"id":"str-11e6af70","title":"FIXME: This will panic on nil input","description":"Location: main.go:9","type":"bug","priority":2,"status":"open","created_at":"","created_by":"stringer","labels":["fixme","stringer-generated","stringer-generated","todos"]}
{"id":"str-3afa7732","title":"HACK: Temporary workaround until upstream fixes the API","description":"Location: main.go:15","type":"chore","priority":3,"status":"open","created_at":"","created_by":"stringer","labels":["hack","stringer-generated","stringer-generated","todos"]}
```

The `type` field is derived from keyword: `bug`/`fixme` -> `bug`, `todo` -> `task`, `hack`/`xxx`/`optimize` -> `chore`.

## Current Limitations

- **Single collector.** Only TODO/FIXME scanning. No git log analysis, GitHub issue import, or pattern detection yet.
- **No delta scanning.** Every run scans the full repo. No way to find only new signals since the last scan.
- **No LLM clustering.** The `--no-llm` flag exists but is a noop. There is no LLM pass to cluster related signals or infer dependencies.
- **No config file.** No `.stringer.yaml` or global config. All options are CLI flags.
- **Line-sensitive hashing.** Moving a TODO to a different line changes its ID, which means `bd import` sees it as a new issue.
- **No `--min-confidence` flag.** Use `--max-issues` to cap output volume. Confidence-based filtering is planned.
- **Manual cleanup needed.** If you delete a TODO from source and re-scan, the old bead remains in `.beads/`. You need to close it manually with `bd close`.

## Roadmap

Planned for future releases:

- **Git log collector** — Detect reverts, high-churn files, WIP/stale branches
- **GitHub issues collector** — Import open issues and PRs as beads
- **Pattern collector** — Flag large files, missing tests, deep nesting
- **Markdown output format** — Human-readable summary
- **JSON output format** — Raw signals for custom processing
- **LLM clustering pass** — Group related signals, infer dependencies, prioritize
- **Config file support** — `.stringer.yaml` for persistent scan configuration
- **Delta scanning** — Only find signals added since last scan
- **`--min-confidence` flag** — Filter by confidence threshold with named presets

## Design Principles

**Read-only.** Stringer never modifies the target repository. It reads files and git history, writes output to stdout or a file. You decide when to `bd import`.

**Composable collectors.** Each collector is independent, testable, and implements one Go interface. Adding a new signal source means implementing `Collector` with `Name()` and `Collect()` methods.

**LLM-optional.** Core scanning works without API keys. The LLM pass (when implemented) will add clustering and dependency inference but won't be required.

**Idempotent.** Running stringer twice on the same repo produces the same output. Content-based hashing ensures deterministic IDs.

**Beads-native output.** JSONL output is validated against `bd import` expectations. If `bd import` can't consume it, that's a stringer bug.

## Requirements

- Go 1.24+
- Git (for blame enrichment)
- `bd` CLI (for importing output into beads)

## Contributing

See [AGENTS.md](./AGENTS.md) for architecture details, the collector interface, and development workflow. This project uses Beads for task tracking — run `bd ready --json` to find open work.

## License

MIT
