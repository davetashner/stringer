# 026: Duplication Window Merging and Test-Only Clone Threshold

**Status:** Accepted — accepted 2026-09-18 with implementation in the same PR (owner authorisation)
**Date:** 2026-09-18
**Context:** stringer-nxx.4 — the September 2026 benchmark on v1.10.0 found the duplication collector reporting the same region repeatedly as the sliding window moved (gin 65/200, express 44/200, tokio 196/692 signals start within 6 lines of the previous signal in the same file), reporting adjacent lines of repetitive lists as clones of each other (flask `sansio/app.py:671` vs `:672`), and emitting 6-line test-only near-clones at floor confidence (express 195/200, gin 122/200 in test files). Duplication was 56–77% of all signals on every repo under 300 files.

## Problem

The collector hashes every window of six normalized lines and emits one clone group per hash. The previous `mergeAdjacentGroups` only joined groups with an identical path set, kept one location per path, and never recomputed the block length, so every signal said "6 lines" and a 40-line clone became 35 signals. Three distinct defects follow from that:

1. **Overlapping windows** — the same region is reported once per window offset.
2. **Adjacent-line artifacts** — in a repetitive list (`ref_a = export_a`, `ref_b = export_b`, ...) window *n* matches window *n+1* of the same file, so a file is reported as a clone of itself one line over.
3. **Test-only noise** — repeated six-line test setup is nearly always deliberate, yet it dominates output and consumes the 200-signal cap on small repos.

## Options

### Option A: Transitive region merge
Union any two groups with overlapping or adjacent locations in the same file; report the union range and the union of other locations.

**Pros:**
- Simplest possible rule; directly what the bead asked for.

**Cons:**
- Overlap is not transitive in intent. Prototyped on the stringer repo it snowballed boilerplate shared across test files into one signal claiming "267 lines, 1,170 locations" at 0.80 confidence — a worse false positive than the noise it removed.

### Option B: Extension + collapse + anchor-region merge (chosen)
Three ordered steps in `duplication_merge.go`:

1. **Extension.** Merge groups that are the *same set of files shifted by a few lines* (equal sorted path list, every location touching its counterpart). This is the window walking down one clone, so a 40-line clone becomes one 40-line group with correct per-location ranges. Exact (Type 1) and near (Type 2) windows of the same clone land in the same cluster; it is reported as exact when the exact members cover the same locations to within two lines (the renamed signature line that borders most exact blocks), otherwise as a near-clone over the union. `Lines` is the shortest location span — the block every copy shares — so a periodic region that collapsed into one long location does not inflate confidence.
2. **Collapse.** Within a group, locations in the same file whose ranges overlap or touch become one location. A group left with fewer than two locations is a list matching itself and is dropped.
3. **Anchor-region merge.** Groups whose *anchor* (first location in path order) ranges overlap or touch in the same file become one signal for that region, listing the union of their other locations. Only anchors chain, so shared boilerplate cannot snowball across the repo; `Lines` stays the longest member block and the kind is exact only if every member is exact.

**Pros:**
- One signal per duplicated region with an honest line count; 0 signals start within 6 lines of another in the same file on the stringer self-scan (was 834 of 2,212 uncapped).
- Bounded: a region signal can list many locations, but its line count and confidence reflect a real block.

**Cons:**
- ~200 lines of merge logic replacing ~150; three rules to understand instead of one.
- A near-clone that extends an exact clone by one or two lines is reported as the exact clone (the extension lines are dropped from the range).

### Option C: Suppress instead of merge
Keep single-window groups but drop any group whose every location overlaps an already-reported region.

**Pros:**
- Minimal code.

**Cons:**
- Still reports "6 lines" for every clone, so confidence never rises with clone size and the report cannot say how large the duplicated block is.

### Test-only threshold
Test-only clone groups were already tagged `test-only` and down-weighted by 0.15 (stringer-e0o); the benchmark shows they still crowd out production signals. Options were (a) drop them entirely, (b) raise the window size for tests, (c) require a larger merged block. (c) is chosen: `duplication_min_test_lines` (default 12) drops test-only groups whose merged block is shorter, keeping the existing down-weight for those that survive. Because merged blocks now have real lengths, a 12-line threshold means "two copies of a real helper", not "two overlapping windows". Non-test clones keep the 6-line window as their minimum. Suppressed groups are counted in `DuplicationMetrics.TestOnlySuppressed`.

### Signal cap
The 200-signal cap (`duplication_signal_cap`, or `max_issues` on the collector) applies per scan invocation, i.e. per workspace. A monorepo scanned workspace-by-workspace reports up to the cap for each workspace (tokio 692, react 3,807 in the benchmark). This is now stated in the collector description and in `docs/large-repos.md` rather than changed: a global cap would starve small workspaces of a large monorepo.

## Recommendation

Option B with the 12-line test-only threshold. It fixes all three defects with rules that each have a one-sentence justification, and the self-scan shows the reduction is fully explained by them.

## Decision

Accepted 2026-09-18 with implementation in the same PR (owner authorisation). Self-scan of the stringer repo, uncapped: 2,212 signals → 495 (−78%); 1,018 dropped were test-only blocks under 12 lines, 27 were single-file self-overlaps, and the remaining 1,162 were absorbed into merged regions. Mean block length rose from 6.0 to 12.2 lines and mean confidence from 0.25 to 0.37.
