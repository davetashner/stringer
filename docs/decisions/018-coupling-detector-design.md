# 018: Coupling and Circular Dependency Detector Design

**Status:** Accepted
**Date:** 2026-02-21
**Context:** stringer-qg8 (C12: Coupling and Circular Dependency Detector). Adding import graph analysis to detect circular dependencies and high-coupling modules.

## Problem

Stringer's structural analysis collectors don't detect coupling problems — circular imports and modules with excessive dependencies. These issues cause:
- Build failures in languages with strict cycle rules (Go, Rust)
- Tangled architectures that resist refactoring
- High-fan-out modules that break when any dependency changes

Key questions:
1. How should we build the import graph without AST parsing?
2. How do we detect cycles efficiently?
3. What threshold separates normal fan-out from problematic coupling?

## Options

### Option A: AST-based import extraction (tree-sitter)

Use tree-sitter grammars to parse import/use statements precisely.

**Pros:**
- Handles all edge cases (string concatenation, conditional imports)

**Cons:**
- Requires tree-sitter bindings per language — massive dependency
- Doesn't match stringer's zero-external-tooling philosophy
- Overkill for detecting import relationships

### Option B: Regex-based import extraction + Tarjan's SCC (chosen)

Per-language regex patterns extract import statements, build a directed graph, run Tarjan's strongly connected components algorithm for cycle detection, and compute fan-out metrics.

**Pros:**
- Zero external dependencies (stdlib only)
- Tarjan's SCC is O(V+E) — optimal for cycle detection
- Regex patterns cover standard import syntax in all supported languages
- Same regex-over-AST philosophy as other stringer collectors

**Cons:**
- Cannot detect dynamic imports (exec-based, string-concatenated)
- May miss unusual import syntax variations

### Option C: Simple DFS cycle detection

Walk the graph with DFS, detect back edges.

**Pros:**
- Simpler implementation

**Cons:**
- Reports individual cycles rather than complete strongly connected components
- May report redundant cycles (same SCC discovered via different paths)
- Tarjan's SCC is equally efficient and produces cleaner results

## Decision

**Option B — Regex-based import extraction with Tarjan's SCC algorithm.**

### Algorithm

1. Walk source files using existing `FS.WalkDir()` infrastructure
2. Extract import statements with per-language regex patterns
3. Build directed graph: module → imported modules (intra-project only)
4. Run Tarjan's SCC to find cycles (components with 2+ nodes)
5. Compute fan-out per module, flag modules exceeding threshold

### Module Resolution

Import statements map to "modules" per language — package paths for Go, relative paths for JS/TS, dotted names for Python, etc. External/stdlib imports are filtered out to focus on intra-project coupling.

### Confidence Formula

Circular dependency (by cycle length):
- 2 nodes: 0.80 (direct mutual import)
- 3 nodes: 0.75
- 4+ nodes: 0.70

High coupling (by fan-out count):
- 20+ imports: 0.70
- 15–19: 0.55–0.70 (linear)
- 10–14: 0.40–0.55 (linear)
- <10: no signal

### Signal Kinds

| Kind | Title Pattern |
|------|--------------|
| `circular-dependency` | `Circular dependency: A → B → C → A` |
| `high-coupling` | `High coupling: <module> imports N modules` |

One signal per cycle (listing all participants). One signal per high-coupling module.

## Consequences

- Adds ~550 lines of new source code (excluding tests)
- No new dependencies
- 10K file cap prevents runaway on monorepos
- Regex patterns cover Go, JS/TS, Python, Java, Rust, Ruby, PHP, C/C++
