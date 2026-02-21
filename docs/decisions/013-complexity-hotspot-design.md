# 013: Complexity Hotspot Collector Design

**Status:** Proposed
**Date:** 2026-02-21
**Context:** stringer-b9u (C9: Complexity Hotspot Detector). Adding complexity detection as a new signal source to identify high-value refactoring targets.

## Problem

Stringer detects churn (files that change often) via the gitlog collector but not *why* those files are painful to change. Complex functions that also churn frequently are the highest-value refactoring targets — the "toxic hotspots" from Adam Tornhill's *Your Code as a Crime Scene*. We need a way to detect function-level complexity and cross-reference it with churn data.

Key questions:
1. How should we measure complexity without AST parsing?
2. Should complexity detection live in its own collector or extend an existing one?
3. How should the hotspot cross-reference work?

## Options

### Option A: AST-based complexity analysis

Use language-specific AST parsers (go/ast for Go, tree-sitter for others) for precise function detection and cyclomatic complexity calculation.

**Pros:**
- Accurate function boundary detection
- Standard cyclomatic complexity metric
- Handles edge cases (nested functions, closures) correctly

**Cons:**
- Requires AST parser per language — massive dependency surface
- tree-sitter CGO bindings add build complexity
- Overkill for archaeological signals where ~80% accuracy suffices
- Doesn't match stringer's zero-external-tooling philosophy

### Option B: Regex-based function detection with composite scoring

Use regex patterns to detect function boundaries and count control flow keywords. Score = lines/50 + branches.

**Pros:**
- Zero external dependencies
- Fast — single-pass line-by-line scan
- ~80% accuracy is acceptable for identifying refactoring candidates
- Easy to extend to new languages (add a langSpec struct)
- Composite metric balances length and branching: 50 lines = 1 point, 1 branch = 1 point

**Cons:**
- Cannot handle all edge cases (string literals containing keywords, complex nesting)
- No true cyclomatic complexity — heuristic approximation
- Function boundary detection imperfect for languages with unusual syntax

### Option C: External tool integration (complexity-report, radon, etc.)

Shell out to existing complexity analysis tools per language.

**Pros:**
- Leverages battle-tested tools
- Accurate per-language metrics

**Cons:**
- Requires tools to be installed
- Different output formats per tool
- Doesn't match stringer's self-contained approach
- Maintenance burden scales with language count

## Recommendation

**Option B: Regex-based function detection with composite scoring.**

For archaeological signal detection, ~80% accuracy is sufficient. The composite score (lines/50 + branches) balances file length with control flow complexity — a 200-line function with no branches scores 4.0, while a 50-line function with 10 branches scores 11.0. Both are meaningful signals, but the branching-heavy function is correctly ranked higher.

### Composite metric

`score = lines/50 + branches`

Where:
- `lines` = non-blank lines in the function body
- `branches` = count of control flow keywords (`if`, `else if`, `elif`, `for`, `while`, `switch`, `case`, `catch`, `except`, `guard`, `when`, `unless`) plus `&&`/`||` operators
- Comment lines are excluded from branch counting

### Confidence bands

| Score range | Confidence |
|------------|------------|
| >= 15 | 0.8 |
| 8–15 | 0.6–0.8 (linear interpolation) |
| 6–8 | 0.5–0.6 (linear interpolation) |
| < 6 | Not emitted |

### Collector architecture

Independent collector running in parallel with all others. The hotspot cross-reference (complexity × churn) lives in the report layer, which reads both `complexity` and `gitlog` metrics post-scan.

### Language coverage (phase 1)

Go, Python, JS/TS, Java, Rust, Ruby — top 6 languages already supported by the patterns collector.

### Files affected

- New: `internal/collectors/complexity.go`, `internal/report/complexity.go`, `internal/report/hotspots.go`
- Modified: `internal/signal/signal.go`, `internal/config/config.go`, `internal/config/merge.go`, `cmd/stringer/collectors.go`

## Decision

Option B accepted. Regex-based function detection with composite scoring. Hotspot cross-reference in report layer.
