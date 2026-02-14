# Stringer

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/hero-github-dark.png">
  <source media="(prefers-color-scheme: light)" srcset="assets/hero-github-light.png">
  <img alt="Stringer" src="assets/hero-github-light.png">
</picture>

[![CI](https://github.com/davetashner/stringer/actions/workflows/ci.yml/badge.svg)](https://github.com/davetashner/stringer/actions/workflows/ci.yml)
[![Coverage](https://img.shields.io/badge/coverage-91%25-brightgreen)](https://github.com/davetashner/stringer/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/davetashner/stringer)](https://goreportcard.com/report/github.com/davetashner/stringer)
[![Release](https://img.shields.io/github/v/release/davetashner/stringer)](https://github.com/davetashner/stringer/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/davetashner/stringer/badge)](https://securityscorecards.dev/viewer/?uri=github.com/davetashner/stringer)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/11942/badge)](https://www.bestpractices.dev/projects/11942)

> **v1.0.0.** Seven collectors with multi-ecosystem vulnerability and dependency health scanning, four output formats, report command, parallel pipeline with signal deduplication, delta scanning, monorepo support (6 workspace types), LLM-powered clustering and priority inference, MCP server for agent integration, interactive `stringer init` wizard, and CLI config management.

**Codebase archaeology for [Beads](https://github.com/steveyegge/beads).** Mine your repo for actionable work items, output them as Beads-formatted issues, and give your AI agents instant situational awareness.

```bash
# Install via Homebrew
brew install davetashner/tap/stringer

# Or install via Go
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

## Why Stringer?

**Static analysis on your laptop, not your token budget.** Stringer runs locally — no API keys, no per-request costs. Most collectors use deterministic static analysis with zero network calls. The vuln and GitHub collectors make targeted API calls (to osv.dev and GitHub) but require no paid subscriptions.

**Pre-process the easy stuff so agents start informed.** Without Stringer, an AI agent boots up on a new codebase and has to discover TODOs, stale branches, lottery-risk modules, and open GitHub issues on its own — burning inference tokens on mechanical work. Stringer does that extraction up front on local CPU, so agents begin with structured context instead of a blank slate.

**LLM-optional by design.** Core scanning needs zero API keys. The LLM pass (signal clustering, priority inference, dependency detection) adds intelligence on top but is never required. You control where the money goes.

## What It Does Today

### Collectors

- **TODO collector** (`todos`) — Scans source files for `TODO`, `FIXME`, `HACK`, `XXX`, `BUG`, and `OPTIMIZE` comments. Enriched with git blame author and timestamp. Confidence scoring with age-based boosts.
- **Git log collector** (`gitlog`) — Detects reverts, high-churn files, and stale branches from git history.
- **Patterns collector** (`patterns`) — Flags large files and modules with low test coverage ratios.
- **Lottery risk analyzer** (`lotteryrisk`) — Flags directories with low lottery risk (single-author ownership risk) using git blame and commit history with recency weighting.
- **GitHub collector** (`github`) — Imports open issues, pull requests, and actionable review comments from GitHub. With `--include-closed`, also generates pre-closed beads from merged PRs and closed issues with architectural module context. Requires `GITHUB_TOKEN` env var.
- **Dependency health collector** (`dephealth`) — Detects archived, deprecated, and stale dependencies across six ecosystems: Go (`go.mod`), npm (`package.json`), Rust (`Cargo.toml`), Java/Maven (`pom.xml`), C#/.NET (`*.csproj`), and Python (`requirements.txt`/`pyproject.toml`).
- **Vulnerability scanner** (`vuln`) — Detects known CVEs across seven ecosystems via [OSV.dev](https://osv.dev/): Go (`go.mod`), Java/Maven (`pom.xml`), Java/Gradle (`build.gradle`/`.kts`), Rust (`Cargo.toml`), C#/.NET (`*.csproj`), Python (`requirements.txt`/`pyproject.toml`), and Node.js (`package.json`). No language toolchains required — only network access to osv.dev. Severity-based confidence scoring from CVSS vectors.

### Output Formats

- **Beads JSONL** (`beads`) — Produces JSONL ready for `bd import`, with deterministic content-based IDs
- **JSON** (`json`) — Raw signals with metadata envelope, TTY-aware pretty/compact output
- **Markdown** (`markdown`) — Human-readable summary grouped by collector with priority distribution
- **Tasks** (`tasks`) — Claude Code task format for direct agent consumption

### Pipeline

- **Parallel execution** — Collectors run concurrently via errgroup
- **Per-collector error modes** — skip, warn (default), or fail
- **Signal deduplication** — Content-based SHA-256 hashing merges duplicate signals
- **Beads-aware dedup** — Filters signals already tracked as beads in the repo
- **Delta scanning** — `--delta` mode tracks state between scans, showing only new/removed/moved signals
- **Pre-closed beads** — Generates closed beads from merged PRs, closed issues, and resolved TODOs
- **Dry-run mode** — Preview signal counts without producing output
- **Monorepo support** — Auto-detects workspaces (go.work, pnpm, npm, lerna, nx, cargo) and scans each independently with `--workspace` filtering

```
                     ┌─────────────────────────────────┐
                     │        Target Repository        │
                     └────────────────┬────────────────┘
                                      │
     ┌─────────┬─────────┬────────────┼────────────┬─────────┬─────────┐
     ▼         ▼         ▼            ▼            ▼         ▼         ▼
 ┌───────┐ ┌───────┐ ┌────────┐ ┌─────────┐ ┌──────────┐ ┌──────┐ ┌──────┐
 │ TODOs │ │  Git  │ │Patterns│ │ Lottery │ │  GitHub  │ │ Dep  │ │ Vuln │  (parallel)
 │       │ │  log  │ │        │ │  Risk   │ │Issues/PRs│ │Health│ │      │
 └───┬───┘ └───┬───┘ └───┬────┘ └────┬────┘ └────┬─────┘ └──┬───┘ └──┬───┘
     └─────────┴─────────┴───────────┴───────────┴──────────┴────────┘
                                      ▼
                               ┌────────────┐
                               │  Dedup +   │
                               │ Validation │
                               └──────┬─────┘
                                      │
                  ┌───────────────────┼───────────┬────────────┐
                  ▼                   ▼           ▼            ▼
             ┌─────────┐       ┌──────────┐ ┌─────────┐ ┌─────────┐
             │  Beads  │       │   JSON   │ │Markdown │ │  Tasks  │
             │  JSONL  │       │          │ │         │ │         │
             └─────────┘       └──────────┘ └─────────┘ └─────────┘
```

## What to Expect

Output volume depends on codebase size and coding style:

| Codebase             | Approximate signals |
|----------------------|---------------------|
| Small (<5k LOC)      | 5-30                |
| Medium (10k-50k LOC) | 20-200              |
| Large (100k+ LOC)    | 100-1,000+          |

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

## Example Prompts

You don't need to memorize flags or read docs. Stringer is designed for agents. Copy-paste any of these into Claude Code, Cursor, Windsurf, or your agent of choice.

### Bootstrap a new project

> Install stringer (`brew install davetashner/tap/stringer`), then set it up in this repo — run `stringer init .`, scan the codebase, and import the results into beads. Give me a summary of what it found.

### Understand a codebase you just inherited

> Use stringer to scan this project and tell me what needs attention. I want to know about TODOs, stale branches, security vulnerabilities, and any files where only one person understands the code.

### Get a health report

> Run `stringer report .` on this project and walk me through the results. What are the riskiest areas?

### Check for security issues

> Use stringer to scan this repo for known vulnerabilities and unhealthy dependencies. Prioritize anything with a CVE.

### Ongoing maintenance

> Run a stringer delta scan (`stringer scan . --delta`) to find new issues since the last scan. Import anything new into beads and tell me what changed.

### Set up agent integration

> Set up stringer's MCP server so you can use it as a tool. Run `stringer init .` if there's no config yet, then register the MCP server with `claude mcp add stringer -- stringer mcp serve`.

### Scope a scan to what matters

> Use stringer to scan only the `src/api/` directory for TODOs and code patterns. Skip the git log and GitHub collectors — I just want local code issues. Use `stringer scan . --paths src/api/ -c todos,patterns`.

### Generate agent docs

> Use stringer to generate an AGENTS.md for this project (`stringer docs . -o AGENTS.md`). This will give future agents a map of the codebase.

## Usage Reference

```
stringer scan [path] [flags]
```

| Flag               | Short | Default | Description                                               |
| ------------------ | ----- | ------- | --------------------------------------------------------- |
| `--collectors`     | `-c`  | (all)   | Comma-separated list of collectors to run                 |
| `--format`         | `-f`  | `beads` | Output format                                             |
| `--output`         | `-o`  | stdout  | Output file path                                          |
| `--dry-run`        |       |         | Show signal count without producing output                |
| `--delta`          |       |         | Only output new signals since last scan                   |
| `--json`           |       |         | Machine-readable output for `--dry-run`                   |
| `--max-issues`     |       | `0`     | Cap output count (0 = unlimited)                          |
| `--min-confidence` |       | `0`     | Filter signals below this threshold (0.0-1.0)            |
| `--kind`           |       |         | Filter by signal kind (comma-separated)                   |
| `--strict`         |       |         | Exit non-zero on any collector failure                    |
| `--git-depth`      |       | `0`     | Max commits to examine (default 1000)                     |
| `--git-since`      |       |         | Only examine commits after this duration (e.g., 90d, 6m)  |
| `--exclude`             | `-e`  |         | Glob patterns to exclude from scanning                    |
| `--exclude-collectors`  | `-x`  |         | Comma-separated list of collectors to skip                |
| `--include-closed`      |       |         | Include closed/merged issues and PRs from GitHub          |
| `--history-depth`       |       |         | Filter closed items older than this duration (e.g., 90d)  |
| `--anonymize`           |       | `auto`  | Anonymize author names: auto, always, or never            |
| `--collector-timeout`   |       |         | Per-collector timeout (e.g. 60s, 2m); 0 = no timeout      |
| `--paths`               |       |         | Restrict scanning to specific files or directories         |
| `--include-demo-paths`  |       |         | Include demo/example/tutorial paths in noise-prone signals |
| `--infer-priority`      |       |         | Use LLM to infer priority from signal context             |
| `--infer-deps`          |       |         | Use LLM to detect dependencies between signals            |
| `--no-llm`              |       |         | Skip all LLM passes (clustering, priority, dependencies)  |
| `--workspace`           |       |         | Scan only named workspace(s) (comma-separated)            |
| `--no-workspaces`       |       |         | Disable monorepo auto-detection, scan root as single dir  |

**Global flags:** `--quiet` (`-q`), `--verbose` (`-v`), `--no-color`, `--help` (`-h`)

**Available collectors:** `todos`, `gitlog`, `patterns`, `lotteryrisk`, `github`, `dephealth`, `vuln`

**Available formats:** `beads`, `json`, `markdown`, `tasks`

## Configuration File

Place a `.stringer.yaml` in your repository root to set persistent scan options. CLI flags override config file values.

```yaml
# .stringer.yaml
output_format: json
max_issues: 50
no_llm: true

collectors:
  todos:
    enabled: true
    error_mode: warn
    min_confidence: 0.5
    include_patterns:
      - "*.go"
      - "*.ts"
    exclude_patterns:
      - vendor/**
      - node_modules/**
  gitlog:
    git_depth: 500
    git_since: 6m
  patterns:
    include_demo_paths: true  # report missing-tests / low-test-ratio in example dirs
  lotteryrisk:
    include_demo_paths: true  # report lottery-risk in example dirs
  github:
    include_closed: true
    history_depth: 90d
```

**Precedence:** CLI flags > `.stringer.yaml` > global config > defaults

Stringer also supports a global config at `~/.config/stringer/config.yaml` (or `$XDG_CONFIG_HOME/stringer/config.yaml`). Repo-level settings override global settings. Use `stringer config set --global` to manage it.

If no config file exists, stringer uses its built-in defaults (all collectors enabled, beads format, no issue cap).

By default, stringer suppresses noise-prone signals (`missing-tests`, `low-test-ratio`, `low-lottery-risk`) in demo/example/tutorial directories (`examples/`, `tutorials/`, `demos/`, `samples/`, and variants). Use `--include-demo-paths` or set `include_demo_paths: true` per collector to scan these paths.

## Other Commands

### `stringer report`

Generates a repository health report with analysis sections for lottery risk, code churn, TODO age distribution, coverage gaps, and actionable recommendations.

```bash
stringer report .              # print to stdout
stringer report . -o report.txt # write to file
stringer report . --format json # machine-readable output
stringer report . --sections lottery-risk,churn  # specific sections only
```

| Flag                    | Short | Default | Description                                               |
| ----------------------- | ----- | ------- | --------------------------------------------------------- |
| `--collectors`          | `-c`  | (all)   | Comma-separated list of collectors to run                 |
| `--sections`            |       | (all)   | Comma-separated report sections to include                |
| `--output`              | `-o`  | stdout  | Output file path                                          |
| `--format`              | `-f`  |         | Output format (`json` for machine-readable)               |
| `--git-depth`           |       | `0`     | Max commits to examine (default 1000)                     |
| `--git-since`           |       |         | Only examine commits after this duration (e.g., 90d, 6m)  |
| `--anonymize`           |       | `auto`  | Anonymize author names: auto, always, or never            |
| `--exclude-collectors`  | `-x`  |         | Comma-separated list of collectors to skip                |
| `--collector-timeout`   |       |         | Per-collector timeout (e.g. 60s, 2m); 0 = no timeout      |
| `--paths`               |       |         | Restrict scanning to specific files or directories         |
| `--workspace`           |       |         | Report only named workspace(s) (comma-separated)          |

**Available sections:** `lottery-risk`, `churn`, `todo-age`, `coverage`, `recommendations`

### `stringer docs`

Auto-generates an `AGENTS.md` scaffold from your repository structure, documenting modules, entry points, and conventions for AI agents.

```bash
stringer docs .              # print to stdout
stringer docs . -o AGENTS.md # write to file
stringer docs . --update     # update existing AGENTS.md, preserving manual sections
```

| Flag       | Short | Default | Description                                              |
| ---------- | ----- | ------- | -------------------------------------------------------- |
| `--output` | `-o`  | stdout  | Output file path                                         |
| `--update` |       |         | Update existing AGENTS.md, preserving manual sections    |

### `stringer context`

Generates a compact context summary of the repository for use in AI prompts. Includes project structure, recent git activity, and open work items.

```bash
stringer context .
stringer context . --format json  # machine-readable output
stringer context . --weeks 8      # include 8 weeks of history
```

| Flag       | Short | Default | Description                                              |
| ---------- | ----- | ------- | -------------------------------------------------------- |
| `--output` | `-o`  | stdout  | Output file path                                         |
| `--format` | `-f`  |         | Output format: `json` or `markdown`                      |
| `--weeks`  |       | `4`     | Weeks of git history to include                          |

### `stringer init`

Bootstraps stringer in a repository. Detects project characteristics and generates starter configuration. Non-destructive by default — skips files that already exist.

```bash
stringer init .          # bootstrap stringer config
stringer init . --force  # overwrite existing .stringer.yaml
```

When run, `stringer init`:
- Creates `.stringer.yaml` with sensible defaults based on project detection
- Appends a stringer integration section to `AGENTS.md`
- Generates `.mcp.json` when a `.claude/` directory is detected (for MCP server integration)

| Flag      | Short | Default | Description                          |
| --------- | ----- | ------- | ------------------------------------ |
| `--force` |       |         | Overwrite existing `.stringer.yaml`  |

### `stringer config`

View and modify stringer configuration from the CLI. Supports dot-notation key paths and both repo-level and global config.

```bash
stringer config list                          # show all settings with source
stringer config get output_format             # get a single value
stringer config set output_format json        # set a value in .stringer.yaml
stringer config set collectors.todos.min_confidence 0.8
stringer config set --global no_llm true      # set in global config
```

| Subcommand | Description |
|------------|-------------|
| `get <key>` | Get a config value by dot-notation key path |
| `set <key> <value>` | Set a config value (auto-detects type) |
| `list` | List all values with source annotations (repo/global) |

Use `--global` on `get`/`set` to target `~/.config/stringer/config.yaml` instead of the repo-level `.stringer.yaml`.

### `stringer collectors`

List and inspect registered collectors.

```bash
stringer collectors list         # table of all collectors with status
stringer collectors info todos   # detailed info, signal types, config options
```

| Subcommand | Description |
|------------|-------------|
| `list` | Show all collectors with name, status, and description |
| `info <name>` | Show detailed info including signal types and config options |

## Agent Integration

Stringer includes an [MCP](https://modelcontextprotocol.io/) server so AI agents can call stringer tools directly.

### Quick Setup

```bash
# Option 1: Auto-detect and configure
stringer init .

# Option 2: Register manually with Claude Code
claude mcp add stringer -- stringer mcp serve
```

### MCP Tools

| Tool | Description |
|------|-------------|
| `scan` | Scan a repository for actionable work items (TODOs, git patterns, code smells) |
| `report` | Generate a repository health report with metrics and recommendations |
| `context` | Generate a context summary for agent onboarding |
| `docs` | Generate or update an AGENTS.md scaffold |

See [docs/agent-integration.md](docs/agent-integration.md) for detailed usage, parameters, and example workflows.

## How Output Works

### Confidence Scoring

Each signal gets a confidence score (0.0-1.0) based on keyword severity and age from git blame:

**Base scores by keyword:**

| Keyword    | Base Score |
| ---------- | ---------- |
| `BUG`      | 0.7        |
| `FIXME`    | 0.6        |
| `HACK`     | 0.55       |
| `TODO`     | 0.5        |
| `XXX`      | 0.5        |
| `OPTIMIZE` | 0.4        |

**Age boost from git blame:**
- Older than 1 year: +0.2
- Older than 6 months: +0.1
- No blame data or recent: +0.0

Score is capped at 1.0. See [DR-004](docs/decisions/004-confidence-scoring-semantics.md) for the full design rationale.

### Priority Mapping

Confidence maps to bead priority:

| Confidence | Priority |
| ---------- | -------- |
| >= 0.8     | P1       |
| >= 0.6     | P2       |
| >= 0.4     | P3       |
| < 0.4      | P4       |

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

## Exit Codes

| Code | Name              | Meaning                                          |
| ---- | ----------------- | ------------------------------------------------ |
| `0`  | OK                | All collectors succeeded                         |
| `1`  | Invalid Args      | Invalid arguments or bad path                    |
| `2`  | Partial Failure   | Some collectors failed, partial output written   |
| `3`  | Total Failure     | No output produced                               |

## Current Limitations

- **Line-sensitive hashing.** Moving a TODO to a different line changes its ID, which means `bd import` sees it as a new issue. Delta scanning (`--delta`) detects moved signals but doesn't update beads IDs.

## Roadmap

Planned for future releases:

- **Additional language support** — Expand test detection heuristics to more ecosystems (PHP, Swift, Scala, Elixir)
- **Stable signal IDs** — Content-based hashing that survives line moves within a file

## Design Principles

**Read-only.** Stringer never modifies the target repository. It reads files and git history, writes output to stdout or a file. You decide when to `bd import`.

**Composable collectors.** Each collector is independent, testable, and implements one Go interface. Adding a new signal source means implementing `Collector` with `Name()` and `Collect()` methods.

**LLM-optional.** Core scanning works without API keys. The LLM pass adds signal clustering, priority inference, and dependency detection but is never required. Use `--no-llm` to skip it entirely.

**Idempotent.** Running stringer twice on the same repo produces the same output. Content-based hashing ensures deterministic IDs.

**Beads-native output.** JSONL output is validated against `bd import` expectations. If `bd import` can't consume it, that's a stringer bug.

## Requirements

- Go 1.24+
- Git (for blame enrichment)
- `bd` CLI (for importing output into beads)

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for development setup, workflow, and guidelines. See [AGENTS.md](./AGENTS.md) for architecture details and the collector interface. This project uses Beads for task tracking — run `bd ready --json` to find open work.

## License

MIT
