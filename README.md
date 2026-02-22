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
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/11942/badge?v=2)](https://www.bestpractices.dev/projects/11942)

> **v1.4.0.** Fifteen collectors — new: code duplication detector and coupling/circular dependency detector. SARIF v2.1.0 output with auto-detection. Cross-collector confidence boosting. Noise reduction: `eval/` default exclude, duplication signal cap. Ten report sections with health trend analysis and module summaries. Language support across 14 ecosystems. HTML dashboard export, parallel pipeline with signal deduplication, delta scanning, monorepo support (6 workspace types), LLM-powered clustering and priority inference, MCP server for agent integration, interactive `stringer init` wizard, and CLI config management.

**Codebase archaeology for developers and AI agents.** Scan any repo for hidden tech debt — TODOs, vulnerabilities, lottery risk, stale branches, unhealthy dependencies — and get structured results you can act on immediately.

```bash
# Install via Homebrew
brew install davetashner/tap/stringer

# Or install via Go
go install github.com/davetashner/stringer/cmd/stringer@latest

# Get a health report
stringer report .

# Scan for actionable issues
stringer scan . -f markdown

# Or output as JSON for your own tooling
stringer scan . -f json -o signals.json
```

## The Problem

Every codebase accumulates hidden debt. TODOs pile up. Dependencies go stale. One engineer becomes the sole owner of a critical module. A vulnerability lands in a transitive dependency and nobody notices.

This debt is already visible in the code — in comments, git history, dependency manifests, and GitHub issues — but no single tool surfaces all of it. Developers context-switch between `grep TODO`, dependency audit tools, and GitHub issue searches. AI agents burn inference tokens rediscovering what's already in the repo.

Stringer extracts these signals automatically, scores them by confidence, and outputs structured results in your format of choice — whether that's a markdown summary for a human, JSON for a CI pipeline, tasks for an AI agent, or [Beads](https://github.com/steveyegge/beads) JSONL for backlog seeding.

## Why Stringer?

**Real scanning, not just TODO grep.** Fifteen collectors cover vulnerability detection across 7 ecosystems, dependency health across 6 ecosystems, lottery risk analysis, code churn, stale branches, coverage gaps, complexity hotspots, dead code, code duplication, coupling & circular dependencies, git hygiene, documentation staleness, configuration drift, API contract drift, and GitHub issues — all in a single command. Most of this runs locally with zero network calls.

**Works without AI, works better with it.** Core scanning is deterministic static analysis — no API keys, no per-request costs. The optional LLM pass adds signal clustering, priority inference, and dependency detection on top. Use `--no-llm` to skip it entirely.

**Output goes where you need it.** Markdown for humans, JSON for CI pipelines, tasks for Claude Code agents, or Beads JSONL for backlog seeding. Same scan, different consumers.

## What It Does Today

### Collectors

- **TODO collector** (`todos`) — Scans source files for `TODO`, `FIXME`, `HACK`, `XXX`, `BUG`, and `OPTIMIZE` comments. Enriched with git blame author and timestamp. Confidence scoring with age-based boosts.
- **Git log collector** (`gitlog`) — Detects reverts, high-churn files, and stale branches from git history.
- **Patterns collector** (`patterns`) — Flags large files and modules with low test coverage ratios. Test detection supports Go, JavaScript/TypeScript, Python, Ruby, Java, Kotlin, Rust, C#, PHP, Swift, Scala, and Elixir.
- **Lottery risk analyzer** (`lotteryrisk`) — Flags directories with low lottery risk (single-author ownership risk) using git blame and commit history with recency weighting.
- **GitHub collector** (`github`) — Imports open issues, pull requests, and actionable review comments from GitHub. With `--include-closed`, also generates pre-closed signals from merged PRs and closed issues with architectural module context. Requires `GITHUB_TOKEN` env var.
- **Dependency health collector** (`dephealth`) — Detects archived, deprecated, and stale dependencies across six ecosystems: Go (`go.mod`), npm (`package.json`), Rust (`Cargo.toml`), Java/Maven (`pom.xml`), C#/.NET (`*.csproj`), and Python (`requirements.txt`/`pyproject.toml`).
- **Vulnerability scanner** (`vuln`) — Detects known CVEs across seven ecosystems via [OSV.dev](https://osv.dev/): Go (`go.mod`), Java/Maven (`pom.xml`), Java/Gradle (`build.gradle`/`.kts`), Rust (`Cargo.toml`), C#/.NET (`*.csproj`), Python (`requirements.txt`/`pyproject.toml`), and Node.js (`package.json`). No language toolchains required — only network access to osv.dev. Severity-based confidence scoring from CVSS vectors.
- **Complexity hotspot collector** (`complexity`) — Detects complex functions using composite scoring (lines + branch count). Surfaces functions that are both complex and high-churn.
- **Dead code detector** (`deadcode`) — Detects unused functions and types via regex heuristic and reference search across the codebase.
- **Git hygiene detector** (`githygiene`) — Detects large binaries, merge conflict markers, committed secrets, and mixed line endings.
- **Documentation staleness detector** (`docstale`) — Detects stale documentation, co-change drift between docs and source files, and broken internal links.
- **Configuration drift detector** (`configdrift`) — Detects env var drift, dead config keys, and inconsistent defaults across environment files.
- **API contract drift detector** (`apidrift`) — Detects drift between OpenAPI/Swagger specs and route handler registrations in code.
- **Code duplication detector** (`duplication`) — Detects copy-paste code duplication using token-based sliding window with FNV-64a hashing. Finds both exact duplicates (Type 1) and near-clones with renamed identifiers (Type 2). Output capped at 200 signals by default.
- **Coupling & circular dependency detector** (`coupling`) — Detects tightly coupled modules and circular dependency chains via import/require analysis.

### Output Formats

- **Beads JSONL** (`beads`) — Produces JSONL ready for `bd import`, with deterministic content-based IDs
- **JSON** (`json`) — Raw signals with metadata envelope, TTY-aware pretty/compact output
- **Markdown** (`markdown`) — Human-readable summary grouped by collector with priority distribution
- **Tasks** (`tasks`) — Claude Code task format for direct agent consumption
- **SARIF** (`sarif`) — [SARIF v2.1.0](https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html) static analysis results for IDE and CI integration

### Pipeline

- **Parallel execution** — Collectors run concurrently via errgroup
- **Per-collector error modes** — skip, warn (default), or fail
- **Signal deduplication** — Content-based SHA-256 hashing merges duplicate signals
- **Beads-aware dedup** — When using Beads output, filters signals already tracked in the repo
- **Delta scanning** — `--delta` mode tracks state between scans, showing only new/removed/moved signals
- **Pre-closed signals** — Generates closed entries from merged PRs, closed issues, and resolved TODOs
- **Dry-run mode** — Preview signal counts without producing output
- **Monorepo support** — Auto-detects workspaces (go.work, pnpm, npm, lerna, nx, cargo) and scans each independently with `--workspace` filtering

```
                              ┌─────────────────────────────────┐
                              │        Target Repository        │
                              └────────────────┬────────────────┘
                                               │
     ┌──────────┬──────────┬──────────┬────────┼────────┬──────────┬──────────┐
     ▼          ▼          ▼          ▼        ▼        ▼          ▼          ▼
 ┌───────┐ ┌───────┐ ┌────────┐ ┌────────┐ ┌──────┐ ┌──────┐ ┌──────┐ ┌──────────┐
 │ TODOs │ │  Git  │ │Patterns│ │Lottery │ │GitHub│ │ Dep  │ │ Vuln │ │Complexity│ (parallel)
 │       │ │  log  │ │        │ │ Risk   │ │      │ │ Hlth │ │      │ │          │
 └───┬───┘ └───┬───┘ └───┬────┘ └───┬────┘ └──┬───┘ └──┬───┘ └──┬───┘ └────┬─────┘
     │         │         │         │          │        │        │           │
     │    ┌────┴────┐ ┌──┴───┐ ┌──┴────┐ ┌───┴───┐ ┌─┴──────┐ │     ┌─────┴─────┐
     │    │  Dead   │ │ Git  │ │  Doc  │ │Config │ │  API   │ │     │Duplication│
     │    │  code   │ │Hygne │ │ Stale │ │ Drift │ │ Drift  │ │     │           │
     │    └────┬────┘ └──┬───┘ └──┬────┘ └───┬───┘ └───┬────┘ │     └─────┬─────┘
     │         │         │        │          │         │      │           │
     │         │         │   ┌────┴─────┐    │         │      │           │
     │         │         │   │ Coupling │    │         │      │           │
     │         │         │   └────┬─────┘    │         │      │           │
     └─────────┴─────────┴────────┴──────────┴─────────┴──────┴───────────┘
                                               ▼
                                        ┌────────────┐
                                        │  Dedup +   │
                                        │ Validation │
                                        └──────┬─────┘
                                               │
                      ┌────────────┬───────────┼────────────┬────────────┐
                      ▼            ▼           ▼            ▼            ▼
                 ┌─────────┐ ┌──────────┐ ┌──────────┐ ┌─────────┐ ┌───────┐
                 │  Beads  │ │   JSON   │ │ Markdown │ │  Tasks  │ │ SARIF │
                 │  JSONL  │ │          │ │          │ │         │ │       │
                 └─────────┘ └──────────┘ └──────────┘ └─────────┘ └───────┘
```

## Real-World Results

Stringer tested against 10 popular open-source repositories across four languages and a wide range of codebase sizes — from 131-file libraries to 28k-file monorepos. All runs completed successfully with zero crashes.

| Repository | Language | Files | Signals | Time | Highlights |
|------------|----------|------:|--------:|-----:|------------|
| [gin](https://github.com/gin-gonic/gin) | Go | 131 | 83 | 5s | 5 toxic hotspots, 27 untested files, 4 TODOs |
| [express](https://github.com/expressjs/express) | JS | 214 | 65 | 2s | 1 CVE found, 6 lottery risks, 45 complex functions |
| [flask](https://github.com/pallets/flask) | Python | 236 | 111 | 6s | 7 vulnerable deps, 42 dead code hits, 9 git hygiene issues |
| [rustlings](https://github.com/rust-lang/rustlings) | Rust | 282 | 312 | 23s | 139 TODOs, 92 coverage gaps, 64 lottery risks |
| [tokio](https://github.com/tokio-rs/tokio) | Rust | 848 | 825 | 36s | 174 dead code hits, 443 coverage gaps, 13 vulnerable deps |
| [fastapi](https://github.com/tiangolo/fastapi) | Python | 2,867 | 607 | 20s | 91 stale docs, 85 lottery risks, 65 complex functions |
| [react](https://github.com/facebook/react) | JS/TS | 6,840 | 4,415 | 2m 23s | 1,060 TODOs, 493 dead code hits, 37 vulnerable deps |
| [django](https://github.com/django/django) | Python | 7,014 | 3,254 | 2m 37s | 1,441 dead code hits, 558 coverage gaps, 81 git hygiene issues |
| [next.js](https://github.com/vercel/next.js) | JS/TS | 27,366 | 10,334 | 26m | 6,574 complex functions, 1,756 coverage gaps, 34 vulnerable deps |
| [kubernetes](https://github.com/kubernetes/kubernetes) | Go | 28,284 | 40,117 | 1h 23m | 19,961 complex functions, 3,585 TODOs, 78 vulnerable deps |

<sub>Tested February 2026 on stringer dev build. Repos cloned with `--depth 100`. Times include both `scan` and `report`.</sub>

**Recommendation:** Use `--dry-run` first to see signal counts, then use `--max-issues` to cap output on your first scan.

```bash
# Preview how many signals exist
stringer scan . --dry-run

# Start with a manageable batch
stringer scan . --max-issues 50 -f markdown
```

## Getting Started

### Quick health check

```bash
# Get a repo health report — lottery risk, churn, coverage gaps, recommendations
stringer report .
```

### Scan for issues

```bash
# 1. Preview signal count
stringer scan . --dry-run

# 2. Scan and review as markdown
stringer scan . -f markdown

# 3. Or save as JSON for programmatic use
stringer scan . -f json -o signals.json

# 4. Focus on security
stringer scan . -c vuln,dephealth -f markdown
```

### Seed a Beads backlog

If you use [Beads](https://github.com/steveyegge/beads) for agent task tracking, stringer's default output format pipes directly into `bd import`:

```bash
stringer scan . --max-issues 20 | bd import -i -
bd ready --json
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

> Install stringer (`brew install davetashner/tap/stringer`), then set it up in this repo — run `stringer init .`, scan the codebase, and give me a summary of what it found. Output as markdown.

### Understand a codebase you just inherited

> Use stringer to scan this project and tell me what needs attention. I want to know about TODOs, stale branches, security vulnerabilities, and any files where only one person understands the code.

### Get a health report

> Run `stringer report .` on this project and walk me through the results. What are the riskiest areas?

### Check for security issues

> Use stringer to scan this repo for known vulnerabilities and unhealthy dependencies. Prioritize anything with a CVE.

### Ongoing maintenance

> Run a stringer delta scan (`stringer scan . --delta`) to find new issues since the last scan. Tell me what changed.

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

**Available collectors:** `todos`, `gitlog`, `patterns`, `lotteryrisk`, `github`, `dephealth`, `vuln`, `complexity`, `deadcode`, `githygiene`, `docstale`, `configdrift`, `apidrift`, `duplication`, `coupling`

**Available formats:** `beads`, `json`, `markdown`, `sarif`, `tasks`

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

## SARIF Integration

Stringer can output [SARIF v2.1.0](https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html) for IDE and CI integration. The format is auto-detected from the `.sarif` file extension, or set explicitly with `--format sarif`.

```bash
# Auto-detected from file extension
stringer scan . -o results.sarif

# Explicit format flag
stringer scan . --format sarif -o results.sarif
```

### VS Code

Install the [SARIF Viewer](https://marketplace.visualstudio.com/items?itemName=MS-SarifVSCode.sarif-viewer) extension, then open the `.sarif` file. Signals appear as inline annotations in the editor with severity levels mapped from stringer priority (P1=error, P2=warning, P3=note, P4=none).

### GitHub Code Scanning

Add stringer to a GitHub Actions workflow to surface signals as code scanning alerts:

```yaml
- name: Run stringer
  run: stringer scan . -o results.sarif

- name: Upload SARIF
  uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: results.sarif
```

## Other Commands

### `stringer report`

Generates a repository health report with analysis sections for lottery risk, code churn, complexity hotspots, TODO age distribution, coverage gaps, module summaries, health trends, git hygiene, and actionable recommendations.

```bash
stringer report .              # print to stdout
stringer report . -o report.txt # write to file
stringer report . --format json     # machine-readable output
stringer report . --format html-dir # HTML dashboard export
stringer report . --sections lottery-risk,churn  # specific sections only
```

| Flag                    | Short | Default | Description                                               |
| ----------------------- | ----- | ------- | --------------------------------------------------------- |
| `--collectors`          | `-c`  | (all)   | Comma-separated list of collectors to run                 |
| `--sections`            |       | (all)   | Comma-separated report sections to include                |
| `--output`              | `-o`  | stdout  | Output file path                                          |
| `--format`              | `-f`  |         | Output format (`json` for machine-readable, `html-dir` for dashboard) |
| `--git-depth`           |       | `0`     | Max commits to examine (default 1000)                     |
| `--git-since`           |       |         | Only examine commits after this duration (e.g., 90d, 6m)  |
| `--anonymize`           |       | `auto`  | Anonymize author names: auto, always, or never            |
| `--exclude-collectors`  | `-x`  |         | Comma-separated list of collectors to skip                |
| `--collector-timeout`   |       |         | Per-collector timeout (e.g. 60s, 2m); 0 = no timeout      |
| `--paths`               |       |         | Restrict scanning to specific files or directories         |
| `--workspace`           |       |         | Report only named workspace(s) (comma-separated)          |

**Available sections:** `lottery-risk`, `churn`, `todo-age`, `coverage`, `recommendations`, `trends`, `hotspots`, `git-hygiene`, `complexity`, `module-summary`

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

Confidence maps to priority:

| Confidence | Priority |
| ---------- | -------- |
| >= 0.8     | P1       |
| >= 0.6     | P2       |
| >= 0.4     | P3       |
| < 0.4      | P4       |

### Content-Based Hashing

Each signal gets a deterministic ID: `SHA-256(source + kind + filepath + line + title)`, truncated to 8 hex characters with a `str-` prefix (e.g., `str-0e4098f9`). Re-scanning the same repo produces the same IDs, making output idempotent and preventing duplicates on reimport.

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

- **Line-sensitive hashing.** Moving a TODO to a different line changes its signal ID. Delta scanning (`--delta`) detects moved signals but downstream consumers may see them as new.

## Roadmap

Planned for future releases:

- **Stable signal IDs** — Content-based hashing that survives line moves within a file

## Design Principles

**Read-only.** Stringer never modifies the target repository. It reads files and git history, writes output to stdout or a file.

**Composable collectors.** Each collector is independent, testable, and implements one Go interface. Adding a new signal source means implementing `Collector` with `Name()` and `Collect()` methods.

**LLM-optional.** Core scanning works without API keys. The LLM pass adds signal clustering, priority inference, and dependency detection but is never required. Use `--no-llm` to skip it entirely.

**Idempotent.** Running stringer twice on the same repo produces the same output. Content-based hashing ensures deterministic IDs.

**Format-agnostic.** The same scan pipeline feeds every output format. Beads JSONL, JSON, Markdown, and Tasks all render the same signal data — pick the format that fits your workflow.

## Requirements

- Go 1.24+ (for building from source)
- Git (for blame enrichment and git log analysis)
- `GITHUB_TOKEN` env var (optional — only needed for the GitHub collector)
- [`bd` CLI](https://github.com/steveyegge/beads) (optional — only needed for Beads JSONL import)

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for development setup, workflow, and guidelines. See [AGENTS.md](./AGENTS.md) for architecture details and the collector interface. This project uses Beads for task tracking — run `bd ready --json` to find open work.

## License

MIT
