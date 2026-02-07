# Competitive Analysis: TODO/Signal Scanners

**Date:** 2026-02-07
**Context:** UX1.6 (stringer-4qs.6) — Understand the landscape of TODO scanning and code archaeology tools to position stringer clearly.

## Landscape

Tools that extract TODO/FIXME comments (or broader code signals) from source code fall into four categories: standalone CLI scanners, IDE extensions, enterprise quality platforms, and manual grep-based approaches.

Stringer occupies a unique niche: it is the only tool designed to produce **beads-native JSONL output** with **git blame enrichment** and **confidence scoring**, targeting AI agent workflows rather than human dashboards.

## Tools

### leasot (Node.js CLI/library)

**What it does:** Parses TODO, FIXME, and custom annotation comments from source files across 49+ programming languages. Outputs in JSON, XML, Markdown, or table format.

**Strengths:**
- Broad language support (49+ languages)
- Multiple output formats (JSON, XML, Markdown, table, VS Code, GitLab)
- Custom tag definitions via CLI flags or programmatic API
- Recognizes author annotations like `TODO(alice): message`

**Gaps relative to stringer:**
- No git blame integration — only parses file content, no author/age metadata
- No confidence scoring — every match is weighted equally
- No content-based hashing — no stable IDs for idempotent re-scanning
- No beads output — requires custom transformation to import into `bd`
- Not designed for AI agent consumption

### fixme (Node.js CLI)

**What it does:** Scans source code for NOTE, OPTIMIZE, TODO, HACK, XXX, FIXME, and BUG comments and prints color-coded results to stdout.

**Strengths:**
- Recognizes 7 annotation types (same set as stringer's keywords)
- Configurable file patterns and ignored directories
- Simple, zero-config usage

**Gaps relative to stringer:**
- Text-only output — no structured JSON/JSONL export
- No git integration — no blame, author, or commit metadata
- No stable IDs or hashing — cannot track issues across scans
- Appears lightly maintained

### SonarQube (Enterprise platform)

**What it does:** Self-hosted or cloud static analysis platform covering bugs, vulnerabilities, code smells, and security issues across 35+ languages. TODO/FIXME detection is one rule among 6,500+.

**Strengths:**
- Comprehensive analysis far beyond TODOs (security, complexity, coverage)
- CI/CD integration with quality gates
- Rich web UI with historical trending
- Enterprise support and compliance features

**Gaps relative to stringer:**
- Heavy infrastructure — requires server, database, ongoing maintenance
- TODO scanning is incidental, not the primary purpose
- No beads output — reports via proprietary web UI/API
- Not composable — monolithic platform, not a pipeline component
- Overkill for "give my agents context on a new codebase"

### CodeClimate (SaaS platform)

**What it does:** Cloud-based code quality platform providing automated code review, technical debt tracking, and engineering analytics. Detects TODOs as part of broader quality analysis.

**Strengths:**
- Automated PR-level code review comments
- Technical debt quantification
- Team velocity metrics
- GitHub/GitLab/Bitbucket integration

**Gaps relative to stringer:**
- SaaS-only — requires cloud account, no offline or air-gapped use
- Paid service beyond minimal free tier
- TODO detection is a small feature in a large platform
- No beads output or agent-oriented design
- No content-based hashing for idempotent scanning

### todo-tree (VS Code extension)

**What it does:** Displays TODO, FIXME, and custom tags in a VS Code sidebar tree view with click-to-navigate and in-editor highlighting.

**Strengths:**
- Fast in-editor discovery using ripgrep
- Customizable colors, icons, and tag patterns
- Direct navigation to comment location

**Gaps relative to stringer:**
- IDE-bound — no CLI, CI/CD, or headless usage
- No structured export — visual only
- No git metadata, scoring, or stable IDs
- Cannot feed into an agent workflow

### grep / ripgrep (manual approach)

**What it does:** General-purpose text search. Teams commonly use `rg 'TODO|FIXME' --glob '*.go'` to find annotations.

**Strengths:**
- Universal availability (grep on all Unix systems)
- Extremely fast (especially ripgrep)
- Flexible regex patterns with context lines

**Gaps relative to stringer:**
- Raw text output requiring manual parsing
- No git blame, confidence scoring, or deduplication
- No stable IDs — every run is a fresh, unstructured list
- User must maintain patterns and post-processing scripts

## Comparison Matrix

| Capability | leasot | fixme | SonarQube | CodeClimate | todo-tree | grep/rg | **stringer** |
|---|---|---|---|---|---|---|---|
| Standalone CLI | Yes | Yes | No (server) | No (SaaS) | No (IDE) | Yes | **Yes** |
| Beads JSONL output | No | No | No | No | No | No | **Yes** |
| Git blame enrichment | No | No | Partial | Partial | No | No | **Yes** |
| Confidence scoring | No | No | Severity rules | Severity rules | No | No | **Yes** |
| Content-based hashing | No | No | No | No | No | No | **Yes** |
| Idempotent re-scanning | No | No | No | No | No | No | **Yes** |
| Agent-oriented design | No | No | No | No | No | No | **Yes** |
| Composable collectors | No | No | Plugin rules | Plugin engines | No | No | **Yes** |
| Works offline | Yes | Yes | Self-hosted | No | Yes | Yes | **Yes** |
| Free | Yes | Yes | Community ed. | Limited | Yes | Yes | **Yes** |

## Stringer Differentiators

1. **Beads-native output.** The only tool that produces JSONL ready for `bd import`. No transformation step required.

2. **Git blame enrichment.** Each signal carries author and timestamp from `git blame`, enabling age-based confidence boosting and attribution.

3. **Confidence scoring (DR-004).** Keyword-specific base scores with age-based boosts produce a 0.0-1.0 confidence float. This maps to bead priority (P1-P4) and enables meaningful filtering via `--max-issues`.

4. **Content-based hashing.** `SHA-256(source + kind + filepath + line + title)` with a `str-` prefix produces deterministic IDs. Re-scanning the same repo produces the same IDs, preventing duplicate beads on reimport.

5. **Agent-oriented design.** Output is structured for AI agent consumption (`bd ready --json`), not human dashboards. The `--dry-run --json` flag supports machine-readable previews.

6. **Composable collector architecture.** The `Collector` interface is designed for extensibility. Adding a new signal source (git log analysis, GitHub issues, pattern detection) means implementing one Go interface. The pipeline runs all enabled collectors and merges their output.

7. **Lightweight and offline.** Single Go binary, no server, no API keys required. Works in air-gapped and CI environments.
