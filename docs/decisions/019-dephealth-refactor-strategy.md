# 019: Dephealth Collector Refactor Strategy

**Status:** Proposed
**Date:** 2026-04-21
**Context:** stringer-td5. `internal/collectors/dephealth.go` has grown to ~13.4K lines with nine `collect*Health()` methods covering Go, npm, Cargo, Maven, NuGet, and PyPI. Client setup, rate-limit handling, and signal emission are tangled across ecosystems. A failure in one ecosystem (e.g. PyPI timeout) currently surfaces as a partial result across the whole collector.

## Problem

The monolithic collector makes three things hard:

1. **Isolated testing.** Ecosystem-specific parsing is exercised through the full collector entry point; regressions in one ecosystem are easy to miss.
2. **Independent failure handling.** A single rate-limited API affects the whole collector's timing and logs.
3. **Onboarding new ecosystems.** Adding a language (PHP Composer, Swift Package Manager) means editing a 13K-line file rather than creating a new small one — which scales badly alongside the `stringer-043` L1 language expansion epic.

## Options

### Option A: One sub-collector per ecosystem, registered independently

Each ecosystem becomes its own `Collector` implementation (`dephealth_go`, `dephealth_npm`, etc.) registered separately. A shared helper package hosts common HTTP client setup, retry logic, and signal construction.

**Pros:**
- Matches stringer's existing collector registry idiom.
- Independent enable/disable per ecosystem via config.
- Failures localized by design; easier to test in isolation.
- New ecosystems drop in without touching existing code.

**Cons:**
- Signal source label changes (e.g. `dephealth` → `dephealth_npm`) — mild back-compat concern for existing beads IDs (signal IDs hash Source+Kind+FilePath+Line+Title, so they shift).
- More files in `internal/collectors/`.
- Report-level rollup needs a small aggregation change.

### Option B: Keep one collector, split internal implementations via an ecosystem interface

A single registered `dephealth` Collector delegates to an internal `ecosystemAnalyzer` interface with per-ecosystem implementations in separate files.

**Pros:**
- No external contract changes; signal IDs stable.
- Parallel execution across ecosystems is easy (fan out internally).
- Existing users' config and report consumers unaffected.

**Cons:**
- Still a single entry point — one panic from one ecosystem still risks the whole collector unless we're strict about error walls.
- Less flexibility to enable/disable per ecosystem from stringer config.
- Leaves the coupling problem partly unsolved.

### Option C: Plugin-style registry with per-ecosystem modules

Similar to Option B but each ecosystem registers itself via `init()`, mirroring how collectors register today. Dephealth becomes a thin orchestrator.

**Pros:**
- Symmetric with existing patterns.
- Allows third-party ecosystems in theory.
- Testable modules, single collector entry point.

**Cons:**
- Two tiers of registry (collector registry + dephealth ecosystem registry) — mild cognitive overhead.
- Plugin surface is overkill if we only have in-repo ecosystems.

## Recommendation

**Option A**, with a deliberate one-time signal ID migration. Independent sub-collectors match stringer's existing pattern, localize failures, and scale cleanly with the L1 language expansion. The signal ID shift can be handled by seeding stable ID aliases in `internal/output/signalid.go` (see stringer-td8 for the stability contract work).

If the alias plumbing is unacceptable churn, fall back to Option B — still a substantial improvement over the current monolith — and revisit A later.

## Decision

[To be filled in by a developer after review. State the chosen option and any conditions.]
