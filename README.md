# Stringer

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/hero-github-dark-v2.png">
  <source media="(prefers-color-scheme: light)" srcset="assets/hero-github-light.png">
  <img alt="Stringer" src="assets/hero-github-light.png">
</picture>

[![CI](https://github.com/davetashner/stringer/actions/workflows/ci.yml/badge.svg)](https://github.com/davetashner/stringer/actions/workflows/ci.yml)
[![Coverage](https://img.shields.io/badge/coverage-91%25-brightgreen)](https://github.com/davetashner/stringer/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/davetashner/stringer)](https://github.com/davetashner/stringer/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/davetashner/stringer/badge)](https://securityscorecards.dev/viewer/?uri=github.com/davetashner/stringer)
[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/11942/badge?v=2)](https://www.bestpractices.dev/projects/11942)

> **v1.9.0** is an accuracy release: reachability-aware vulnerability scanning, severity from real CVSS base scores, nesting-aware complexity scoring for non-Go languages, and fewer false positives from the coupling, githygiene, docstale, and duplication collectors. Full details in the [release notes](https://github.com/davetashner/stringer/releases/latest) and DR-023/024/025.

**Codebase archaeology for developers and AI agents.** Stringer scans a repo for the tech debt already recorded in it — TODOs, vulnerable dependencies, single-owner code, complexity hotspots, stale branches — and turns it into structured output you can act on.

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

## Why

The evidence of tech debt is already sitting in your repo: code comments, git history, dependency manifests, GitHub issues. What's missing is a single tool that reads all of it. Developers end up juggling `grep TODO`, a dependency audit tool, and issue searches, while AI agents burn tokens rediscovering the same context every session.

Stringer runs fifteen collectors in one command, scores each finding by confidence, and writes results in whatever format your workflow needs: markdown for a human, JSON or SARIF for CI, tasks for a Claude Code agent, or [Beads](https://github.com/steveyegge/beads) JSONL for seeding a backlog. Nearly all of it is deterministic static analysis that runs locally, with no API keys or per-request costs. An optional LLM pass adds signal clustering, priority inference, and dependency detection on top; `--no-llm` skips it.

## Collectors

| Collector | What it finds |
|-----------|---------------|
| `todos` | `TODO`, `FIXME`, `HACK`, `XXX`, `BUG`, `OPTIMIZE` comments, enriched with git blame author and age |
| `vuln` | Known CVEs via [OSV.dev](https://osv.dev/) across 11 ecosystems (Go, npm, Maven, Gradle, Cargo, .NET, Python, Composer, Swift, sbt, Mix) — no language toolchains required |
| `dephealth` | Archived, deprecated, and stale dependencies across 10 ecosystems |
| `lotteryrisk` | Directories where one author owns most of the code, weighted by recency |
| `complexity` | Complex functions via Go AST analysis (or heuristics for other languages), cross-referenced with churn |
| `deadcode` | Unused functions and types |
| `duplication` | Copy-paste and near-clone duplication via token-based hashing |
| `coupling` | Tightly coupled modules and circular dependency chains |
| `gitlog` | Reverts, high-churn files, and stale branches |
| `githygiene` | Large binaries, merge conflict markers, committed secrets, mixed line endings |
| `patterns` | Large files and low test coverage ratios, with test detection for 12 languages |
| `docstale` | Stale docs, doc/source co-change drift, broken internal links |
| `configdrift` | Env var drift, dead config keys, inconsistent defaults across environment files |
| `apidrift` | Drift between OpenAPI/Swagger specs and route handlers |
| `github` | Open issues, PRs, and actionable review comments (requires `GITHUB_TOKEN`) |

Run `stringer collectors info <name>` for signal types and tunable thresholds.

## Output formats

| Format | Use case |
|--------|----------|
| `beads` (default) | JSONL for [Beads](https://github.com/steveyegge/beads), with deterministic content-based IDs |
| `json` | Raw signals with metadata envelope |
| `markdown` | Human-readable summary grouped by collector |
| `sarif` | [SARIF v2.1.0](https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html) for IDE and CI integration |
| `tasks` | Claude Code task format for direct agent consumption |
| `html` / `html-dir` | Self-contained dashboard, or split into `index.html` plus assets |

## Pipeline

Collectors run concurrently, then signals are deduplicated (content-based SHA-256 hashing) and validated before formatting. Per-collector error modes (skip, warn, fail), delta scanning with move detection, baseline suppression, beads-aware dedup, and monorepo workspace auto-detection all happen in this pipeline.

```
                            ┌─────────────────────┐
                            │  Target Repository  │
                            └──────────┬──────────┘
                                       │
                                       ▼
┌────────────────────────────────────────────────────────────────────────────┐
│              15 collectors — all run concurrently (errgroup)               │
│                                                                            │
│ ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐ │
│ │   TODOs    │ │  Patterns  │ │ Dead Code  │ │ Complexity │ │Duplication │ │
│ └────────────┘ └────────────┘ └────────────┘ └────────────┘ └────────────┘ │
│                                                                            │
│ ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐ │
│ │   Gitlog   │ │Git Hygiene │ │Lottery Risk│ │   GitHub   │ │  Coupling  │ │
│ └────────────┘ └────────────┘ └────────────┘ └────────────┘ └────────────┘ │
│                                                                            │
│ ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐ │
│ │    Vuln    │ │ Dep Health │ │ Doc Stale  │ │Config Drift│ │ API Drift  │ │
│ └────────────┘ └────────────┘ └────────────┘ └────────────┘ └────────────┘ │
│                                                                            │
└──────────────────────────────────────┬─────────────────────────────────────┘
                                       │
                                       ▼
                           ┌──────────────────────┐
                           │  Dedup + Validation  │
                           └───────────┬──────────┘
                                       │
      ┌──────────┬──────────┬──────────┼──────────┬──────────┬──────────┐
      ▼          ▼          ▼          ▼          ▼          ▼          ▼
 ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐
 │ beads  │ │  json  │ │markdown│ │ sarif  │ │ tasks  │ │  html  │ │html-dir│
 └────────┘ └────────┘ └────────┘ └────────┘ └────────┘ └────────┘ └────────┘
```

## Real-world results

Runs against 10 popular open-source repositories, from a 131-file library to a 28k-file monorepo:

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

On a large repo, preview first and cap the output:

```bash
# Preview how many signals exist
stringer scan . --dry-run

# Start with a manageable batch
stringer scan . --max-issues 50 -f markdown
```

## Getting started

```bash
# Repo health report: lottery risk, churn, coverage gaps, recommendations
stringer report .

# Preview signal count, then scan
stringer scan . --dry-run
stringer scan . -f markdown

# Save as JSON for programmatic use
stringer scan . -f json -o signals.json

# Focus on security
stringer scan . -c vuln,dephealth -f markdown

# Machine-readable dry run
stringer scan . --dry-run --json
```

### Seed a Beads backlog

If you use [Beads](https://github.com/steveyegge/beads) for agent task tracking, stringer's default output is beads-compatible JSONL. Until a native `bd import` lands ([requested upstream](https://github.com/steveyegge/beads/issues/2505)), import via `bd create`:

```bash
stringer scan . --max-issues 20 -q | while IFS= read -r line; do
  title=$(echo "$line" | jq -r .title)
  desc=$(echo "$line" | jq -r .description)
  bd create "$title" -d "$desc"
done

bd ready --json
```

## Example prompts

Stringer is built to be driven by agents. Paste any of these into Claude Code, Cursor, or Windsurf:

> Install stringer (`brew install davetashner/tap/stringer`), then set it up in this repo — run `stringer init .`, scan the codebase, and give me a summary of what it found.

> Use stringer to scan this project and tell me what needs attention. I want to know about TODOs, stale branches, security vulnerabilities, and any files where only one person understands the code.

> Use stringer to scan this repo for known vulnerabilities and unhealthy dependencies. Prioritize anything with a CVE.

> Set up stringer's MCP server so you can use it as a tool. Run `stringer init .` if there's no config yet, then register the MCP server with `claude mcp add stringer -- stringer mcp serve`.

## Usage

```
stringer scan [path] [flags]
```

The flags you'll reach for most:

| Flag | Description |
|------|-------------|
| `-c, --collectors` | Comma-separated list of collectors to run |
| `-f, --format` | Output format (`beads`, `json`, `markdown`, `sarif`, `tasks`, `html`, `html-dir`) |
| `-o, --output` | Output file path (default stdout) |
| `--dry-run` | Show signal counts without producing output |
| `--delta` | Only output new signals since the last scan |
| `--max-issues` | Cap output count |
| `-e, --exclude` | Glob patterns to exclude from scanning |
| `--paths` | Restrict scanning to specific files or directories |
| `--no-llm` | Skip all LLM passes |

The full flag tables for `scan` and every other command live in [docs/cli-reference.md](docs/cli-reference.md), or run `stringer <command> --help`.

### Other commands

| Command | What it does |
|---------|--------------|
| `stringer report .` | Repo health report: lottery risk, churn, hotspots, trends, recommendations. `--format html-dir` exports a dashboard |
| `stringer docs .` | Generate an `AGENTS.md` scaffold from repo structure (`--update` preserves manual sections) |
| `stringer context .` | Compact repo summary for AI prompts: structure, recent activity, open work |
| `stringer init .` | Bootstrap `.stringer.yaml`, an AGENTS.md section, and MCP registration |
| `stringer config` | Get/set config values with dot-notation key paths, repo-level or global |
| `stringer baseline` | Suppress known findings so they're filtered from future scans |
| `stringer collectors` | List collectors and inspect their signal types and thresholds |

## Configuration

Place a `.stringer.yaml` in your repository root for persistent scan options. Precedence: CLI flags > `.stringer.yaml` > global config (`~/.config/stringer/config.yaml`) > built-in defaults.

```yaml
# .stringer.yaml
output_format: json
max_issues: 50
no_llm: true

collectors:
  todos:
    min_confidence: 0.5
    exclude_patterns:
      - vendor/**
      - node_modules/**
  gitlog:
    git_depth: 500
    git_since: 6m
  complexity:
    min_complexity_score: 6
  coupling:
    coupling_fan_out_threshold: 15
```

Each collector accepts its own options (`enabled`, `error_mode`, thresholds, patterns); run `stringer collectors info <name>` to see them, or `stringer config list` to see every setting with its source.

By default, stringer suppresses noise-prone signals (`missing-tests`, `low-test-ratio`, `low-lottery-risk`) in demo, example, and tutorial directories. Use `--include-demo-paths` or set `include_demo_paths: true` per collector to scan those paths too.

## SARIF integration

SARIF output is auto-detected from a `.sarif` file extension, or set explicitly with `--format sarif`. It includes `automationDetails` for run correlation, code snippets with 3-line context (disable with `--no-snippets`), and `--sarif-baseline previous.sarif` for differential analysis.

```bash
stringer scan . -o results.sarif
```

For VS Code, install the [SARIF Viewer](https://marketplace.visualstudio.com/items?itemName=MS-SarifVSCode.sarif-viewer) extension; signals appear as inline annotations with severity mapped from stringer priority (P1=error, P2=warning, P3=note, P4=none). For GitHub code scanning, upload the file in a workflow:

```yaml
- name: Run stringer
  run: stringer scan . -o results.sarif

- name: Upload SARIF
  uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: results.sarif
```

## Agent integration

Stringer ships an [MCP](https://modelcontextprotocol.io/) server exposing `scan`, `report`, `context`, and `docs` as tools.

```bash
# Auto-detect and configure
stringer init .

# Or register manually with Claude Code
claude mcp add stringer -- stringer mcp serve
```

See [docs/agent-integration.md](docs/agent-integration.md) for parameters and example workflows.

## How output works

Each signal gets a confidence score (0.0–1.0). For TODO signals, the base score comes from the keyword (`BUG` 0.8, `FIXME` 0.65, `HACK` 0.55, `TODO` 0.5, `XXX` 0.45, `OPTIMIZE` 0.35), with a +0.1 boost if git blame shows it's under 30 days old. See [DR-004](docs/decisions/004-confidence-scoring-semantics.md) for the design rationale. Confidence maps to priority: ≥0.8 is P1, ≥0.6 is P2, ≥0.4 is P3, below that P4.

Signal IDs are deterministic: `SHA-256(source + kind + filepath + line + title)`, truncated to 8 hex characters with a `str-` prefix. Re-scanning the same repo produces the same IDs, so output is idempotent and reimports don't duplicate. Every signal is labeled with its kind (`todo`, `fixme`, ...), its collector name, and `stringer-generated` to distinguish it from manually filed issues.

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

The `type` field derives from the keyword: `bug`/`fixme` -> `bug`, `todo` -> `task`, `hack`/`xxx`/`optimize` -> `chore`.

## Limitations and roadmap

Signal IDs are line-sensitive: moving a TODO to a different line changes its ID. Delta scanning (`--delta`) detects moves, but other consumers may see a moved signal as new. A content-based ID scheme that survives line moves is planned.

## Design principles

- **Read-only.** Stringer never modifies the target repository.
- **Composable.** Each collector is independent and implements one small Go interface (`Name()` and `Collect()`).
- **LLM-optional.** Core scanning needs no API keys; the LLM pass is additive.
- **Idempotent.** Same repo in, same output out, with deterministic IDs.
- **Format-agnostic.** One scan pipeline feeds every output format.

## Requirements

- Go 1.25+ (for building from source)
- Git (for blame enrichment and git log analysis)
- `GITHUB_TOKEN` env var (optional, for the GitHub collector)
- [`bd` CLI](https://github.com/steveyegge/beads) (optional, for Beads JSONL import)

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) for development setup and [AGENTS.md](./AGENTS.md) for architecture details and the collector interface. This project uses Beads for task tracking; run `bd ready --json` to find open work.

## License

MIT
