# 020: AGENTS.md Interface Drift Automation

**Status:** Proposed
**Date:** 2026-04-21
**Context:** stringer-td7. The Doc Staleness CI job guards against `Collector`, `Formatter`, and `Section` interface signatures drifting between AGENTS.md and the Go source. The current guard is grep-based: it extracts literal signature lines from the fenced code block and compares them to `grep` output over source files. This catches exact-string drift but fails on whitespace, formatting, or signature reorderings, and it does not notice new or removed methods.

## Problem

AGENTS.md is the onboarding entry point for agents building against stringer. The current guard is useful but brittle. Specifically:

- Changes to method comments in source but not in docs still pass.
- Adding a method to an interface in source goes undetected if the grep patterns don't cover it.
- Reordering methods is invisible.
- Reviewers sometimes tweak whitespace in docs to make the grep pass without actually keeping them in sync.

## Options

### Option A: AST parse both sides, structurally compare

Write a small Go tool (`tools/agentsmd-interfaces`) that:
1. Parses AGENTS.md, extracts fenced `go` blocks containing `type … interface { … }`.
2. Uses `go/parser` to parse each block into an `ast.InterfaceType`.
3. Loads the real Go package and extracts matching `InterfaceType` nodes.
4. Compares method sets (name, parameters, results) structurally.

**Pros:**
- Robust to whitespace, ordering, and comment differences.
- Detects added/removed/renamed methods.
- Output can be precise ("method `Collect` signature differs: docs say … source says …").

**Cons:**
- More code to maintain in `tools/`.
- Requires the docs block to be parseable Go — limits freedom in docs to use `...` ellipsis-style summaries.

### Option B: `go generate` the docs block from source

Add a `//go:generate` directive that dumps the real interface as a Markdown fenced block, and have CI diff the generated block against AGENTS.md.

**Pros:**
- Source is the single source of truth.
- Generation step catches drift by definition.

**Cons:**
- Surrounding prose in AGENTS.md must be outside the generated region, which means markers (`<!-- begin-generated -->` / `<!-- end-generated -->`).
- Reduces flexibility to hand-edit the docs example.
- Adds a code generation step to the build.

### Option C: Keep grep, broaden its surface

Expand the current grep patterns to cover additional signatures, document rules for adding new interfaces, and accept that this is a best-effort guard.

**Pros:**
- No new tooling.
- Low cost.

**Cons:**
- Doesn't solve the root cause.
- Silently rots again as interfaces evolve.

## Recommendation

**Option A.** Structural comparison is the only approach that actually validates the invariant. The constraint it imposes (docs block must be parseable Go) is a feature, not a bug: it forces the docs to stay honest. The tool is small — estimated ~150 lines.

## Decision

[To be filled in by a developer after review.]
