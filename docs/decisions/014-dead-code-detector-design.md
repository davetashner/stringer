# 014: Dead Code Detector Design

**Status:** Accepted
**Date:** 2026-02-21
**Context:** stringer-7el (C8: Dead Code Detector). Adding dead code detection as a new signal source to identify unused functions and types.

## Problem

Stringer detects complexity hotspots and churn patterns but does not identify code that is never referenced — dead code. Unused functions and types are maintenance debt: they confuse readers, increase build times, and rot over time. Detecting dead code provides actionable cleanup signals.

Key questions:
1. How should we detect unused symbols without AST parsing?
2. What confidence levels are appropriate given false-positive risk?
3. How do we handle exported symbols that may be used by external consumers?

## Options

### Option A: AST-based dead code analysis

Use `go/ast`, tree-sitter, or language-specific tools for precise symbol resolution and usage tracking.

**Pros:**
- Accurate symbol resolution including imports and qualified references
- Handles shadowing, overloading, and namespaces correctly

**Cons:**
- Requires AST parser per language — massive dependency surface
- Overkill for archaeological signals where ~80% accuracy suffices
- Doesn't match stringer's zero-external-tooling philosophy

### Option B: Regex heuristic + in-memory reference search

Two-pass algorithm: extract symbol definitions via regex, then search all cached file contents for word-boundary references.

**Pros:**
- Zero external dependencies
- Reuses existing `langSpecs`/`extToSpec` from complexity collector for function detection
- ~80% accuracy is acceptable for identifying cleanup candidates
- Fast with `strings.Contains` pre-filter before regex matching
- Easy to extend to new languages

**Cons:**
- Cannot resolve imports or qualified references precisely
- May miss references in string interpolation, reflection, or codegen
- False positives for exported symbols used by external packages

### Option C: External tool integration (deadcode, vulture, etc.)

Shell out to language-specific dead code tools.

**Pros:**
- Leverages battle-tested, accurate tools

**Cons:**
- Requires tools to be installed
- Different output formats per tool
- Doesn't match stringer's self-contained approach

## Recommendation

**Option B: Regex heuristic + in-memory reference search.**

Consistent with DR-013's philosophy — ~80% accuracy is sufficient for archaeological signals. Lower confidence for exported symbols mitigates false-positive risk.

### Algorithm

1. **Extract** — Walk source files, use regex to build a symbol index (name, file, line, visibility)
2. **Search** — For each symbol, scan all cached file contents for word-boundary matches. If count==1 in def file and 0 elsewhere, it's dead code. If only referenced in test files, flag with lower confidence.

### Signal kinds

- `unused-function` — function/method defined but never referenced elsewhere
- `unused-type` — type/class/struct defined but never referenced elsewhere

### Skip list

Never flag: `main`, `init`, `Test*`, `Benchmark*`, `Example*`, dunder methods, framework lifecycle methods (`constructor`, `render`, `componentDidMount`, etc.), names <= 2 chars, symbols in test files, generated/binary files.

### Confidence tiers

| Context | Confidence |
|---------|-----------|
| Go unexported, zero refs | 0.7 |
| Go exported in `internal/` | 0.6 |
| Rust non-pub | 0.6 |
| Other unexported | 0.5 |
| Other exported | 0.4 |
| Only referenced in test files | 0.3 |
| Go exported in public pkg | 0.3 |

### Performance guards

- File count cap: 10,000 (skip with warning if exceeded)
- `strings.Contains` fast pre-filter before regex match
- Context cancellation checks in both walk and search loops

### Languages

Go, Python, JS/TS, Java, Rust, Ruby — same as complexity collector.

## Decision

Option B accepted. Regex heuristic with in-memory reference search, conservative confidence tiers for exported symbols.
