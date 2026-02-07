# 004: Confidence Scoring Semantics

**Status:** Accepted
**Date:** 2026-02-07
**Context:** C1.4 (stringer-vkt.4) implements confidence scoring for the TODO collector. The `--min-confidence` flag (CLI1.2) exposes this to users. This record defines what confidence values mean so that agents and humans can make informed filtering decisions without guessing.

## Problem

Stringer assigns a `Confidence` float (0.0-1.0) to every `RawSignal`. Users filter on this via `--min-confidence`. But the numbers are arbitrary without defined semantics — what's the difference between 0.5 and 0.7? An agent asked to "only show important stuff" has no way to map that intent to a threshold.

Each collector produces signals with different characteristics, so confidence scoring must be defined per-collector while maintaining consistent cross-collector semantics (a 0.8 from `todos` should mean roughly the same thing as a 0.8 from `gitlog`).

## Options

### Option A: Continuous heuristic score (per-collector formula)

Each collector computes a continuous 0.0-1.0 score from weighted heuristics. The formula varies by collector, but the output semantics are consistent:

**TODO collector heuristics:**
- Base score by keyword: `BUG` = 0.7, `FIXME` = 0.6, `HACK` = 0.55, `TODO` = 0.5, `XXX` = 0.5, `OPTIMIZE` = 0.4
- Age boost (via git blame): +0.1 if > 6 months old, +0.2 if > 1 year
- Author recency: +0.1 if author has recent commits (signal is "known debt", not abandoned code)
- Proximity boost: +0.05 if within 10 lines of a recently changed line (active area)
- Cap at 1.0

**Cross-collector semantic bands:**
| Range | Meaning | User guidance |
|-------|---------|---------------|
| 0.8-1.0 | Critical — almost certainly real, actionable work | "Show me the important stuff" |
| 0.6-0.79 | High — likely real work, may need triage | Default for most users |
| 0.4-0.59 | Moderate — plausible but may be noise | Use for comprehensive audits |
| 0.0-0.39 | Low — speculative, informational | Completionists only |

**Pros:**
- Fine-grained filtering; users can tune precisely
- Heuristics are transparent and documented
- Each collector can weight factors relevant to its signal type
- Deterministic (no LLM needed)

**Cons:**
- Formulas are arbitrary — weights need empirical tuning
- Cross-collector calibration is manual (how do you ensure a TODO 0.7 = a gitlog 0.7?)
- Users may over-trust precise-looking numbers

### Option B: Discrete tiers (low / medium / high / critical)

Replace the float with an enum. Each collector maps signals to one of four tiers using simple rules.

**TODO collector rules:**
- `critical`: `BUG` or `FIXME` keywords + > 6 months old
- `high`: `BUG`/`FIXME` (any age) or any keyword > 1 year old
- `medium`: `TODO`/`HACK`/`XXX` < 1 year old
- `low`: `OPTIMIZE` or any keyword in test/vendor files

**CLI interface:**
```
--min-confidence=high    # critical + high
--min-confidence=medium  # critical + high + medium (default)
```

**Pros:**
- Easier to explain and use ("give me high-confidence signals")
- No false precision — users don't agonize over 0.6 vs 0.65
- Simpler implementation
- CLI flag is more intuitive

**Cons:**
- Coarser filtering; can't say "give me the top 30"
- Tier boundaries are still arbitrary
- Harder to sort within a tier (for `--max-issues` cap)
- Breaks the existing `RawSignal.Confidence float64` interface

### Option C: Continuous score with named presets

Keep the float internally but expose named presets on the CLI. Users can use either.

```
--min-confidence=high       # maps to 0.7
--min-confidence=0.65       # exact value also accepted
```

**Preset mapping:**
| Preset | Value | Description |
|--------|-------|-------------|
| `critical` | 0.9 | Near-certain actionable work |
| `high` | 0.7 | Recommended for first scan |
| `medium` | 0.5 | Comprehensive triage |
| `low` | 0.3 | Everything including speculative |
| `all` | 0.0 | No filtering |

**Pros:**
- Best of both worlds: human-friendly names + machine-friendly precision
- Agents can use exact values; humans can use presets
- No interface change (`RawSignal.Confidence` stays float64)
- Presets documented in `--help` text
- Sorting and `--max-issues` work naturally on floats

**Cons:**
- Two ways to specify the same thing (minor complexity)
- Preset names need to stay stable across versions
- Still need to define per-collector heuristic formulas (same as Option A)

## Recommendation

**Option C: Continuous score with named presets.**

This gives agents exact control (`--min-confidence=0.65`) while giving humans intuitive handles (`--min-confidence=high`). The float stays in the data model, preserving sort order for `--max-issues`. The preset names solve the "what does 0.7 mean?" problem without sacrificing precision.

The per-collector heuristic formulas from Option A apply here — Option C is really "Option A with a better CLI surface."

For MVP, implement the TODO collector formula only. Other collectors get their formulas when implemented. The semantic bands (critical/high/medium/low) are documented in `--help` and README so users know what to expect at each level.

**Default:** `--min-confidence=0.0` (all signals). This keeps the MVP simple — users who want filtering opt in. The `--max-issues` flag (CLI1.8) provides a complementary safety valve.

## Decision

**Accepted: Option C — Continuous score with named presets.**

Implement per-collector heuristic formulas producing a 0.0-1.0 float. Expose named presets (`critical`, `high`, `medium`, `low`, `all`) on the `--min-confidence` flag alongside raw float values. Default to `0.0` (all signals) for MVP. Document semantic bands in `--help` output and README. Start with the TODO collector formula; other collectors define their own formulas when implemented.
