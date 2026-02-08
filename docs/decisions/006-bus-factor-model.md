# 006: Bus Factor Ownership Model

**Status:** Accepted
**Date:** 2026-02-07
**Context:** C5: Bus Factor Analyzer (stringer-lmo) — flag directories with single-author ownership risk

## Problem

How should stringer model code ownership and compute bus factor? We need to decide the unit of analysis (file vs. directory), the ownership signals (blame vs. commits vs. both), and how to weight recent contributions more heavily.

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
- Well-established in academic bus factor literature

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
- **Bus factor:** Minimum number of authors whose combined ownership exceeds 50%
- **Signal threshold:** Emit signal when bus factor <= configurable threshold (default 1)
- **Performance:** Cap blame at `max_blame_files` (default 50) per directory
- **Confidence mapping:** bus factor 1 → 0.8, bus factor 2 → 0.5, bus factor 3+ → 0.3

## Decision

Accepted. Implement Option B with the parameters above. Defer review-based participation (C5.3) until the GitHub collector is available. Defer author anonymization (C5.6) to a future iteration.
