# 001: Minimum Go Version

**Status:** Accepted
**Date:** 2026-02-07
**Context:** F1 project scaffold (stringer-aks). Establishes the minimum Go version for building and testing stringer.

## Problem

Which Go version should stringer require? This affects available language features, standard library APIs, and CI matrix configuration.

## Options

### Option A: Go 1.24 (current stable)

**Pros:**
- Latest stable features (range-over-func, enhanced servemux patterns)
- Strongest toolchain support (govulncheck, go vet improvements)
- Matches CI matrix lower bound already configured

**Cons:**
- Users on older Go versions cannot build stringer

### Option B: Go 1.22

**Pros:**
- Broader compatibility with existing installations
- Still receives security patches

**Cons:**
- Misses useful language improvements
- Extra CI matrix entries to maintain
- Go 1.22 reaches EOL soon after Go 1.26 ships

### Option C: Go 1.23

**Pros:**
- Middle ground on compatibility
- Has range-over-int and most modern features

**Cons:**
- No strong reason to pick 1.23 over 1.24 — stringer has no existing user base to migrate

## Recommendation

**Option A: Go 1.24.** Stringer is a new project with no legacy users. Requiring the latest stable version keeps the codebase clean and avoids maintaining compatibility shims. The CI matrix already tests 1.24 and 1.25.

## Decision

Option A accepted. Go 1.24 is the minimum version. The CI matrix tests 1.24 and 1.25, go.mod specifies 1.24, and the project has shipped multiple releases on this basis.
