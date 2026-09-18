# 006: Lottery Risk Ownership Model

**Status:** Accepted
**Date:** 2026-02-07
**Context:** C5: Lottery Risk Analyzer (stringer-lmo) — flag directories with single-author ownership risk

## Problem

How should stringer model code ownership and compute lottery risk? We need to decide the unit of analysis (file vs. directory), the ownership signals (blame vs. commits vs. both), and how to weight recent contributions more heavily.

## Options

### Option A: File-level blame only

**Pros:**
- Simple: blame gives exact line-by-line ownership
- No time-based weighting needed

**Cons:**
- Too granular — a single file with one author is normal, not a risk signal
- Blame reflects last-touch, not sustained ownership

### Option B: Directory-level, blame + commits, exponential recency decay

**Pros:**
- Directory-level is the right granularity for organizational risk
- Combines two ownership signals: blame (current state) and commits (sustained involvement)
- Exponential decay (`e^(-ln2/half_life * days)`) naturally downweights old contributions
- Configurable depth, threshold, and file limits
- Well-established in academic lottery risk literature

**Cons:**
- More complex to implement
- Blame is expensive (must blame each file)
- Need to cap file count per directory for performance

### Option C: Commit-count only

**Pros:**
- Fast — no blame needed
- Simple to implement

**Cons:**
- Commit count doesn't reflect code volume — one 1-line fix == one 500-line feature
- Easy to game with formatting commits

## Recommendation

**Option B: Directory-level, blame + commits with exponential recency decay.**

Parameters:
- **Analysis unit:** Directory (up to configurable depth, default 2)
- **Ownership formula:** `ownership = blame_fraction * 0.6 + commit_weight_fraction * 0.4`
- **Recency decay:** `weight = e^(-ln2/180 * days_old)` (half-life 180 days)
- **Lottery risk:** Minimum number of authors whose combined ownership exceeds 50%
- **Signal threshold:** Emit signal when lottery risk <= configurable threshold (default 1)
- **Performance:** Cap blame at `max_blame_files` (default 50) per directory
- **Confidence mapping:** lottery risk 1 → 0.8, lottery risk 2 → 0.5, lottery risk 3+ → 0.3

## Decision

Accepted. Implement Option B with the parameters above. Defer review-based participation (C5.3) until the GitHub collector is available. Defer author anonymization (C5.6) to a future iteration.

## Amendment (2026-09-18, stringer-nxx.6)

The September 2026 benchmark (`--depth 100` clones) showed three accuracy problems, fixed without changing the core model:

1. **Shallow history over-reports, it does not under-report.** Shallow blame attributes every line older than the clone boundary to the boundary commit's author, so pallets/flask went from 1 signal (full clone) to 6 (shallow). The collector now probes `git rev-parse --is-shallow-repository`; when true, every low-lottery-risk signal is capped at confidence 0.5, tagged `shallow-history`, and its description explains the inflation and asks for a full clone. Short but complete histories are not capped: their blame is accurate.
2. **Minimum substance.** A directory is only flagged when it has at least 3 source files and 100 blamed lines and no path segment names a static/fixture/template directory (`fixtures`, `static`, `testdata`, `assets`, `templates`, `fonts`, `images`, `snapshots`, `golden`, `locales`, ...). Skipped directories still appear in metrics. This removes express `test/fixtures` (40%) and `test/support` (100%) style noise.
3. **Single-component renormalisation.** The commit-weight component was already scoped per directory (each changed file credits only its owning directory), so the identical "60%" seen across jellyfin directories was blame-only ownership (0.6 x 100%) in directories with no commits inside the walked window, and the "40%" static directories were commit-only ownership (0.4 x 100%) with zero blamed lines. `ownershipFraction` now gives the available component full weight when the other is absent, so a sole author reads as 100% and the majority test is applied to real fractions.
