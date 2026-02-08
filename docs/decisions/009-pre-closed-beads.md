# 009: Pre-closed Beads Design

**Status:** Accepted
**Date:** 2026-02-08
**Context:** A5: Pre-closed Beads (stringer-6kq) — emit closed beads from historical data so agents understand resolved work

## Problem

Stringer currently emits only `status: "open"` beads, even for signals from closed GitHub issues, merged PRs, and resolved TODOs. Agents working in a repo need context about what's already done — not just open work. We need to generate **closed beads** from historical data.

Three sources of closed signals exist:
1. Closed GitHub issues (already fetched via `--include-closed`)
2. Merged/closed PRs (already fetched via `--include-closed`)
3. Resolved TODOs (detected when a TODO disappears between delta scans)

The key decisions: how to detect resolved TODOs, and how to flow closed status through the beads formatter.

## Options for Resolved TODO Detection

### Option A: Git-history walking

Walk git history to find commits that removed TODO comments.

**Pros:**
- Discovers historical resolved TODOs even without prior scans
- Precise: knows exactly which commit resolved each TODO

**Cons:**
- Expensive: requires diffing every commit pair
- Duplicates work already done by the gitlog collector
- Complex: needs TODO-aware diff parsing

### Option B: Delta-based detection

Leverage existing delta scanning infrastructure. When `--delta` detects removed TODO signals (present in previous scan but absent in current), convert them to closed beads.

**Pros:**
- Simple: reuses existing `ComputeDiff()` and `SignalMeta` infrastructure
- Accurate for ongoing use: catches all TODOs that disappear between scans
- No additional git operations needed

**Cons:**
- Requires two scans (`--delta`) to detect resolved TODOs — first scan establishes baseline
- Cannot detect TODOs resolved before the first scan

## Recommendation

**Option B: Delta-based detection.** The simplicity and reuse of existing infrastructure outweigh the limitation of requiring a baseline scan. Users who run `--delta` regularly get accurate resolved-TODO tracking. The git-history approach (Option A) can be layered on later for one-time historical analysis.

## Design

### Closed Status Flow

1. `RawSignal.ClosedAt` (new field) carries the close timestamp
2. GitHub collector sets `ClosedAt` from API data on closed issues/merged PRs
3. Beads formatter checks for `"pre-closed"` tag → emits `status: "closed"`, populates `closed_at` and `close_reason`
4. Resolved TODOs (PR 2) build `RawSignal` with `"pre-closed"` tag and `ClosedAt` set to scan time

### History Depth

A `--history-depth` flag (default `6m`) filters ancient closed items to keep output focused on recent context.

## Decision

Accepted. Implement in 4 PRs:
1. Beads formatter emits closed status from existing pre-closed signals
2. Resolved TODO detection via delta scanning
3. Configurable `--history-depth` flag
4. Architectural context enrichment for closed items
