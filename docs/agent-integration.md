# Agent Integration Guide

Stringer exposes its tools via the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/), allowing AI agents to call scan, report, context, and docs directly.

## Prerequisites

- **stringer** installed and on your `PATH` (`brew install davetashner/tap/stringer` or `go install`)
- **Claude Code** or another MCP-compatible client

## Setup

### Option 1: `stringer init` (recommended)

```bash
stringer init .
```

When a `.claude/` directory is detected, `stringer init` automatically generates a `.mcp.json` file with the stringer server entry. It also creates `.stringer.yaml` and appends a stringer section to `AGENTS.md`.

### Option 2: Manual registration

```bash
claude mcp add stringer -- stringer mcp serve
```

This registers stringer as an MCP server with Claude Code. The server communicates over stdio.

### Option 3: `.mcp.json`

Create or update `.mcp.json` in your repository root:

```json
{
  "mcpServers": {
    "stringer": {
      "command": "stringer",
      "args": ["mcp", "serve"]
    }
  }
}
```

## MCP Tools Reference

### `scan`

Scan a repository for actionable work items.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `path` | string | `.` | Repository path to scan |
| `collectors` | string | all | Comma-separated list of collectors (`todos`, `gitlog`, `patterns`, `lotteryrisk`, `github`) |
| `format` | string | `json` | Output format: `json`, `beads`, `markdown`, `tasks` |
| `max_issues` | int | 0 | Cap output count (0 = unlimited) |
| `min_confidence` | float | 0 | Filter signals below this threshold (0.0-1.0) |
| `kind` | string | | Filter by signal kind (comma-separated) |
| `git_depth` | int | 1000 | Max commits to examine |
| `git_since` | string | | Only examine commits after this duration (e.g., `90d`, `6m`, `1y`) |

### `report`

Generate a repository health report.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `path` | string | `.` | Repository path to analyze |
| `collectors` | string | all | Comma-separated list of collectors |
| `sections` | string | all | Comma-separated report sections (`lottery-risk`, `churn`, `todo-age`, `coverage`, `recommendations`) |
| `git_depth` | int | 1000 | Max commits to examine |
| `git_since` | string | | Only examine commits after this duration |

### `context`

Generate a context summary for agent onboarding.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `path` | string | `.` | Repository path to analyze |
| `weeks` | int | 4 | Weeks of git history to include |
| `format` | string | `json` | Output format: `json` or `markdown` |

### `docs`

Generate or update an AGENTS.md scaffold.

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `path` | string | `.` | Repository path to analyze |
| `update` | bool | false | Update existing AGENTS.md, preserving manual sections |

## Example Workflows

### Agent onboarding

An agent starting work on an unfamiliar codebase can bootstrap its understanding:

1. Call `context` to get a high-level overview (tech stack, structure, recent activity)
2. Call `scan` with `format: json` to discover actionable work items
3. Call `report` to understand code health and risk areas

### Continuous scanning

Use `scan` periodically to detect new TODOs, churn hotspots, or ownership risks:

```
scan(path: ".", collectors: "todos,lotteryrisk", min_confidence: 0.6)
```

### Targeted investigation

Focus on specific signal types or high-confidence items:

```
scan(path: ".", kind: "fixme,bug", min_confidence: 0.7)
```

## Configuration

MCP tools respect `.stringer.yaml` in the repository root. File-level configuration is merged with tool parameters, with tool parameters taking precedence. See the [README](../README.md#configuration-file) for config file details.
