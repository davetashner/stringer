# Security Assurance Case

This document provides an assurance case justifying why Stringer's security requirements are met. It follows a claims-evidence structure per [OpenSSF Best Practices](https://www.bestpractices.dev/) silver badge requirements.

## Threat Model

### What Stringer Does

Stringer is a **read-only CLI tool** that analyzes git repositories and produces structured reports. It:

- Reads files from the local filesystem (within a git repository)
- Executes `git` subcommands to inspect history, blame, and refs
- Makes HTTPS requests to OSV.dev (vulnerability database) and GitHub API
- Optionally calls the Anthropic API for LLM-powered analysis
- Writes output to stdout or a specified file

### What Stringer Does NOT Do

- Does not run as a daemon or long-lived service (except MCP stdio server, which has no network listener)
- Does not accept network connections
- Does not modify the scanned repository
- Does not execute arbitrary code from scanned repositories
- Does not store credentials (API keys are passed via environment variables)

### Trust Boundaries

| Boundary | Trust Level | Validation |
|----------|-------------|------------|
| Local filesystem (scanned repo) | Semi-trusted | Path traversal prevention, symlink resolution |
| Git CLI output | Semi-trusted | Parsed with strict format expectations |
| OSV.dev API responses | Untrusted | JSON schema validation, HTTPS with TLS cert verification |
| GitHub API responses | Untrusted | JSON parsing with error handling, HTTPS with TLS cert verification |
| Anthropic API responses | Untrusted | Response parsed as text, not executed |
| MCP tool inputs | Untrusted | Input validation against registries, path resolution with symlink checks |
| User CLI arguments | Untrusted | Validated by cobra flag parsing, collector/format names checked against registries |

## Security Claims and Evidence

### Claim 1: Stringer does not execute untrusted code

**Argument:** Stringer only reads and analyzes files. It never evaluates, compiles, or executes code found in scanned repositories.

**Evidence:**
- No use of `os/exec` except for invoking the `git` binary with controlled arguments
- Git commands are constructed with fixed subcommands and validated arguments — no shell interpolation
- `internal/gitcli/gitcli.go` uses `exec.CommandContext` with argument arrays, not shell strings
- gosec (G204) exclusion is scoped only to git CLI invocation, with justification documented in `.golangci.yml`

### Claim 2: Path traversal is prevented

**Argument:** All file access is constrained to the target repository directory.

**Evidence:**
- `internal/mcpserver/resolve.go` resolves paths with `filepath.Abs()` + `filepath.EvalSymlinks()`
- MCP server validates resolved paths are within the git repository root
- Collector file enumeration uses `git ls-files` (respects .gitignore) rather than raw filesystem walks
- Fuzz testing on path resolution: `FuzzResolvePath` in CI

### Claim 3: Network communications are secure

**Argument:** All outbound network requests use HTTPS with TLS certificate verification.

**Evidence:**
- OSV.dev client (`internal/collectors/vuln_osv.go`) uses `http.DefaultClient` which enforces TLS cert verification
- GitHub collector (`internal/collectors/github.go`) uses `http.DefaultClient` with HTTPS URLs
- Anthropic LLM client (`internal/llm/anthropic.go`) uses HTTPS endpoint
- Go's `net/http` enforces TLS 1.2+ and certificate verification by default
- No `InsecureSkipVerify` anywhere in the codebase

### Claim 4: No secrets are leaked

**Argument:** Stringer does not store, log, or output credentials.

**Evidence:**
- API keys (Anthropic, GitHub) are read from environment variables, never written to disk
- `internal/redact/redact.go` redacts sensitive patterns from output
- gitleaks pre-commit hook prevents credential commits (`.githooks/pre-commit`)
- CI runs gitleaks on every commit
- No credential files in repository

### Claim 5: Dependencies are monitored and secure

**Argument:** Third-party dependencies are continuously monitored for known vulnerabilities.

**Evidence:**
- `govulncheck` runs in CI on every PR and before every release
- Dependabot monitors Go modules and GitHub Actions weekly (`.github/dependabot.yml`)
- License compliance check ensures only approved licenses (MIT, BSD, Apache, ISC, MPL-2.0)
- Archived dependency check warns when upstream repos are abandoned
- `go.sum` provides integrity verification for all module downloads

### Claim 6: Static and dynamic analysis catch vulnerabilities

**Argument:** Multiple analysis tools run continuously to detect security issues.

**Evidence:**
- **Static analysis:** gosec (Go security linter), CodeQL (GitHub SAST), golangci-lint with errcheck/staticcheck
- **Dynamic analysis:** Race detector (`-race` flag on all test runs), fuzz testing (4 fuzz targets in CI)
- **Coverage:** 91%+ test coverage with 90% CI gate
- All analysis runs on every PR and must pass before merge
- See `.github/workflows/ci.yml` and `.github/workflows/codeql.yml`

### Claim 7: Releases are tamper-resistant

**Argument:** Published artifacts are signed and verifiable.

**Evidence:**
- All release artifacts signed with Sigstore cosign (`.goreleaser.yml`)
- SLSA Level 2 provenance generated via `slsa-framework/slsa-github-generator`
- SHA-256 checksums published with every release
- SBOMs (SPDX) generated for all archives
- Release pipeline: GoReleaser (draft) → SLSA provenance → publish
- See `docs/release-strategy.md` for full details

### Claim 8: Input validation follows allowlist principle

**Argument:** All inputs from untrusted sources are validated against known-good values.

**Evidence:**
- Collector names validated against `collector.List()` registry
- Output format names validated against `output.GetFormatter()` registry
- Report section names validated against `report` registry
- CLI flags parsed by cobra with type enforcement
- MCP server inputs validated before processing (see `docs/security/mcp-server-audit.md`)

## Residual Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| Git binary could have vulnerabilities | Low | Stringer uses git as an external tool; users control their git version |
| OSV.dev API could return malicious data | Low | Responses are parsed as JSON data, never executed |
| Large repositories could cause resource exhaustion | Low | Timeouts on git commands, context cancellation support |
| Shallow clones reduce lottery risk accuracy | Low | Documented limitation, not a security issue |

## Review Schedule

This assurance case is reviewed when:
- New collectors or network-facing features are added
- Security-relevant dependencies are updated
- A vulnerability is reported and resolved
