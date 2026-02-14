# Roadmap

Last updated: 2026-02-14

This document describes what Stringer intends to do and not do over the next year (through February 2027).

## Current State (v1.0.x)

Stringer is a stable codebase archaeology CLI tool with seven collectors, four output formats, a report command, MCP server for AI agent integration, and LLM-powered analysis features. See the [README](./README.md) for full feature list.

## Planned (2026)

### H1 2026: Quality & Reach

- **User research and persona validation** — define target personas, validate noise thresholds and LLM-optional positioning
- **README rewrite** — broaden positioning beyond Beads to highlight standalone analysis value
- **Real-world stress testing** — run against diverse repos (large monorepos, polyglot projects, minimal repos) to find edge cases
- **Language support expansion** — add test detection for PHP, Swift, Scala, and Elixir/Erlang ecosystems
- **HTML dashboard report** (R3) — browser-viewable report output format

### H2 2026: Ecosystem & Integration

- **Additional output integrations** — explore integrations beyond Beads (e.g., GitHub Issues, Jira, Linear)
- **Performance optimization** — improve scan times for very large repositories (100k+ files)
- **Plugin system exploration** — evaluate whether user-contributed collectors are feasible and desirable

## Not Planned

The following are explicitly out of scope:

- **GUI or web application** — Stringer is a CLI tool and MCP server. We will not build a standalone web UI.
- **Language server (LSP)** — IDE integration is handled through MCP, not a custom language server.
- **Real-time file watching** — Stringer scans on demand. Continuous monitoring is out of scope.
- **Package registry publishing** — We will not publish to npm, PyPI, or other non-Go registries. Distribution is via Homebrew and `go install`.
- **Multi-repo orchestration** — Stringer scans one repository at a time. Cross-repo analysis is left to the caller.
- **Paid features or hosted service** — Stringer is and will remain fully open source (MIT) with no paywalled features.

## How Priorities Are Decided

Priorities are tracked in the [Beads backlog](./.beads/) using priority levels P1 (critical) through P5 (nice-to-have). Run `bd ready` to see current priorities. The maintainer makes final decisions on roadmap changes (see [GOVERNANCE.md](./GOVERNANCE.md)).

## Suggesting Changes

Open a GitHub issue to propose roadmap additions. Include the problem you're trying to solve and why it matters to you.
