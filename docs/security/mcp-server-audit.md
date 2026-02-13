# MCP Server Security Assessment

**Date:** 2026-02-12
**Scope:** `internal/mcpserver/` (10 files, 4 tool handlers)
**Beads ID:** stringer-kyk

## Attack Surface Summary

Stringer exposes an MCP server via `stringer mcp serve` with 4 tools:

| Tool | Handler | Purpose |
|------|---------|---------|
| `scan` | `handleScan` | Scan repo for actionable work items |
| `report` | `handleReport` | Generate repository health report |
| `context` | `handleContext` | Generate CONTEXT.md summary |
| `docs` | `handleDocs` | Generate/update AGENTS.md scaffold |

**Transport:** stdio only (no network listeners, no HTTP, no SSE).

**All tools are read-only:** output goes to `bytes.Buffer`, no disk writes from MCP handlers.

## Trust Model

The MCP server has **no built-in authentication**. This is intentional and correct for stdio transport:

- The server communicates exclusively via stdin/stdout with its parent process
- Access control is delegated to the host (e.g., Claude Code, which controls which MCP servers are launched)
- No network exposure means no remote attack vector
- The server cannot be connected to by arbitrary processes

This follows the standard MCP stdio security model where the parent process is the trust boundary.

## Input Validation Inventory

### Path Validation (`resolve.go`)
- `filepath.Abs()` converts to absolute path
- `filepath.EvalSymlinks()` resolves symlinks (prevents symlink attacks)
- `os.Stat()` + `IsDir()` validates the target is an existing directory
- Git root detection walks up the directory tree

### Collector Names
- Validated against the collector registry (`collector.List()`)
- Unknown names rejected with explicit error listing available collectors

### Format Strings
- Validated against the formatter registry (`output.GetFormatter()`)
- Unknown formats rejected before any processing

### Report Sections
- Validated against section registry in `report.RenderJSON()`

### Numeric Bounds
- `MinConfidence` checked: `0.0 <= value <= 1.0`
- `GitDepth` used as-is (positive int, bounded by git history)
- `MaxIssues` used as-is (0 = unlimited, positive = cap)

### Error Redaction
- All handler error returns pass through `redactErr()`, which calls `redact.String()` to strip sensitive environment variable values (tokens, API keys) before they reach the MCP client

## Architectural Strengths

1. **No shell execution** - Git operations use go-git library and `os/exec` with explicit argument lists, not shell invocation
2. **Stderr isolation** - `slog.Warn` output goes to stderr, never mixed into MCP response content
3. **No disk writes** - All handlers write to `bytes.Buffer`, returning content via MCP protocol
4. **Tool annotations** - All 4 tools annotated with `ReadOnlyHint: true`, `DestructiveHint: false`, `OpenWorldHint: false`
5. **Error redaction** - `redactErr()` strips sensitive env var values from all error messages returned to MCP clients

## Test Coverage

### Security Test Files (7 test files total)
- `resolve_test.go` - Path resolution unit tests
- `resolve_security_test.go` - Path traversal, symlink, and injection tests
- `resolve_fuzz_test.go` - Fuzz testing for path resolution
- `server_test.go` - Server lifecycle tests
- `tools_test.go` - Handler functional tests
- `tools_security_test.go` - 12 security-specific tests
- `tools_fuzz_test.go` - Fuzz testing for tool handlers

### Security Test Categories
- **Injection:** command injection, null byte, newline, pipe, backtick in collector names
- **Path traversal:** parent traversal (`../`), absolute paths, null bytes in paths
- **Unicode:** emoji, CJK, RTL override, zero-width space in collector names
- **Format validation:** template injection, HTML/script, command injection in format strings
- **Bounds checking:** negative confidence, above-1.0 confidence
- **Env var leakage:** verified in both scan and report success output
- **Error redaction:** verified sensitive values are `[REDACTED]` in error messages
- **Stderr isolation:** verified slog warnings don't leak into MCP responses

## Findings and Mitigations

### 1. Error messages not redacted at MCP handler level (Medium) - FIXED
**Finding:** CLI path ran `redact.String()` on errors (`main.go`), but MCP handlers returned raw errors. If a downstream error included a token value, it would be exposed to the MCP client.

**Fix:** Added `redactErr()` helper that wraps errors through `redact.String()`. Applied to all error return points across all 4 handlers.

### 2. `docs` tool `ReadOnlyHint: false` annotation incorrect (Low) - FIXED
**Finding:** `handleDocs` writes to `bytes.Buffer`, not disk. The `ReadOnlyHint: false` annotation was misleading to MCP clients.

**Fix:** Changed to `ReadOnlyHint: true`.

### 3. No documentation of MCP trust model (Low) - FIXED
**Finding:** The stdio-only, no-auth design is correct but was undocumented.

**Fix:** This document serves as the formal documentation.

### 4. Missing test for error redaction (Low) - FIXED
**Finding:** Existing tests verified env vars don't appear in successful output, but no test verified error messages are redacted.

**Fix:** Added `TestHandleScan_SecurityErrorRedaction` that sets `GITHUB_TOKEN`, triggers an error containing the token value, and verifies `[REDACTED]` replacement.
