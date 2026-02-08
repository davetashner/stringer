# 003: Go Module Path

**Status:** Accepted
**Date:** 2026-02-07
**Context:** F1 project scaffold (stringer-aks). The module path is set in go.mod and affects import paths throughout the codebase.

## Problem

What module path should stringer use? This determines how the project is imported and where `go install` fetches it from.

## Options

### Option A: github.com/davetashner/stringer

**Pros:**
- Standard convention for GitHub-hosted Go projects
- `go install github.com/davetashner/stringer/cmd/stringer@latest` works immediately
- Clear ownership signal
- No custom domain infrastructure required

**Cons:**
- Tied to a specific GitHub account — transferring ownership requires module redirect
- Not a vanity/branded path

### Option B: Custom vanity domain (e.g., stringer.dev/stringer)

**Pros:**
- Portable across hosting platforms
- Professional/branded appearance
- Can redirect to any VCS host

**Cons:**
- Requires DNS + web server for go-import meta tags
- Additional infrastructure to maintain
- Overkill for a new project with no users yet

## Recommendation

**Option A: github.com/davetashner/stringer.** This is already set in go.mod. Vanity domains add operational overhead with zero benefit until the project has meaningful adoption. If stringer grows, a vanity domain can be introduced later with a module redirect — Go's module system handles this gracefully.

## Decision

Option A accepted. The module path github.com/davetashner/stringer is established in go.mod, published on GitHub Releases, and installable via `go install`. Homebrew tap also uses this path.
