# 017: Code Duplication Detector Design

**Status:** Accepted
**Date:** 2026-02-21
**Context:** stringer-9ep (C11: Code Duplication Detector). Adding copy-paste duplication detection as a new signal source to identify exact and near-duplicate code blocks.

## Problem

Stringer's structural analysis collectors (complexity, deadcode, patterns) don't detect copy-paste duplication — the most common form of tech debt. Duplicated code increases maintenance burden: bugs must be fixed in multiple locations, and divergent copies create subtle inconsistencies.

Key questions:
1. How should we detect duplicates without AST parsing?
2. How do we distinguish exact copies from renamed-identifier clones?
3. What minimum block size avoids false positives from boilerplate?

## Options

### Option A: AST-based clone detection (tree-sitter)

Use tree-sitter to parse source into ASTs and compare subtree hashes.

**Pros:**
- Language-aware: handles syntax precisely
- Can detect structural clones even with reformatting

**Cons:**
- Requires tree-sitter bindings and grammar files per language — massive dependency surface
- Doesn't match stringer's zero-external-tooling philosophy
- Overkill for archaeological signals

### Option B: Token-based sliding window with FNV hashing (chosen)

Normalize source lines (strip whitespace, comments, imports), slide a fixed-size window, hash each window with FNV-64a, group matching hashes.

**Pros:**
- Language-agnostic: works across all source files
- Zero external dependencies (stdlib `hash/fnv`)
- Two-pass approach (Type 1 exact + Type 2 identifier-normalized) catches both exact and near-clones
- Same approach used by PMD CPD, proven at scale

**Cons:**
- Cannot detect clones with reordered statements
- Window size is a fixed heuristic (6 lines)

### Option C: Suffix-tree based detection

Build a generalized suffix tree from all source lines and find maximal repeats.

**Pros:**
- Theoretically optimal for finding all repeated substrings

**Cons:**
- High memory usage for large codebases
- Complex implementation for marginal benefit over sliding window
- Harder to tune for precision

## Decision

**Option B — Token-based sliding window with FNV-64a hashing.**

The sliding window approach matches stringer's philosophy: simple, dependency-free, and good enough for archaeological signals. The two-pass normalization (Type 1 for exact clones, Type 2 for identifier-renamed clones) covers the most common duplication patterns.

### Algorithm Detail

1. Walk files using existing `FS.WalkDir()` infrastructure
2. Normalize lines in two passes:
   - Type 1: strip whitespace, skip blank/comment-only/import lines
   - Type 2: same + replace identifiers with `$` placeholder (keep ~50 common keywords)
3. Slide a 6-line window, hash each with FNV-64a
4. Group hashes with 2+ locations, merge adjacent windows into larger blocks
5. Deduplicate: Type 2 pass subtracts ranges already reported as Type 1

### Confidence Formula

- 50+ lines → 0.75 base
- 30–49 lines → 0.60–0.75 (linear interpolation)
- 15–29 lines → 0.45–0.60 (linear interpolation)
- 6–14 lines → 0.35–0.45 (linear interpolation)
- 3+ locations: +0.05, 4+ locations: +0.10
- Near-clone: −0.05
- Cap: 0.80

### Signal Kinds

| Kind | Title Pattern |
|------|--------------|
| `code-clone` | `Duplicated block (N lines, M locations)` |
| `near-clone` | `Near-duplicate block (N lines, M locations, renamed identifiers)` |

One signal per clone group (not per pair). Description lists all locations.

## Consequences

- Adds ~530 lines of new source code (excluding tests)
- No new dependencies
- 10K file cap prevents runaway on monorepos
- False positive rate acceptable for archaeological signals (confidence capped at 0.80)
