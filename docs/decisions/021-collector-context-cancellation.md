# 021: Collector Context Cancellation in Long-Running Loops

**Status:** Proposed
**Date:** 2026-04-21
**Context:** stringer-td9. Several collectors process large per-repo data sets in tight inner loops. `internal/collectors/duplication.go` (FNV-64a sliding window over every source file) and `internal/collectors/coupling_graph.go` (Tarjan SCC over the file-dependency graph) do not check `ctx.Done()` inside those loops. On a large repository (say 50K+ files), Ctrl+C hangs for several seconds before the collector surfaces the cancellation — bad UX on the CLI and actively harmful under CI timeouts.

## Problem

Collectors accept `ctx context.Context` and pass it to API clients, but CPU-bound inner loops don't re-check cancellation. We need a consistent approach that:

- Respects cancellation without measurable overhead in the happy path.
- Is easy to apply uniformly across existing and new collectors.
- Does not obscure the hot-loop logic with boilerplate.

## Options

### Option A: Inline `select` check at loop boundaries

Sprinkle the standard idiom:

```go
for _, f := range files {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }
    // …work…
}
```

at the outer loop boundary of each long-running collector.

**Pros:**
- Zero dependency overhead; idiomatic Go.
- Easy to reason about: one check per outer iteration.
- `default` branch keeps the happy path branch-predicted.

**Cons:**
- Repeated boilerplate across collectors.
- Easy to forget in new collectors (no compile-time guarantee).

### Option B: Cancellable iterator helper in `internal/collector`

Add a small helper `collector.Range(ctx, slice, func(item T) error)` that wraps the cancellation check internally. Collectors call `collector.Range(ctx, files, func(f file) error { … })`.

**Pros:**
- Enforces the check by construction.
- Shorter call sites.

**Cons:**
- Introduces an abstraction for something the stdlib already models.
- Closure indirection per iteration may matter in very tight loops (measurable on duplication.go at 100K files).
- Generics syntax makes the helper less greppable.

### Option C: Check cancellation via ticker at fixed intervals

Spawn a background goroutine that sets a sync/atomic flag when `ctx.Done()` fires. Hot loops read the flag every N iterations.

**Pros:**
- Minimal per-iteration overhead.
- Useful if the outer iteration is itself very short.

**Cons:**
- Adds a goroutine + atomic per collector.
- Complexity not justified for the cancellation latencies we care about (a few hundred ms at worst).

## Recommendation

**Option A.** Idiomatic Go, no abstraction cost, measurable latency improvement. Add a lint rule (or a simple grep in the existing doc-staleness workflow) that flags long-running collectors lacking a `ctx.Done()` check to mitigate Option A's main downside. Document the pattern in AGENTS.md under "Adding a new collector" so new collectors adopt it from day one.

## Decision

[To be filled in by a developer after review.]
