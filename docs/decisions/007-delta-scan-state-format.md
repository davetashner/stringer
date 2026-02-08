# 007: Delta Scan State Format

**Status:** Accepted
**Date:** 2026-02-07
**Context:** A1: Delta Scanning (stringer-7ws) — track scan state so `--delta` reports only new signals

## Problem

Every stringer scan re-scans the full repo. Users who run stringer periodically get duplicate signals on each import. We need a way to remember what was already reported and only output new signals on subsequent runs.

The key question: what state format should we persist, and how should we use it to filter signals?

## Options

### Option A: Git-diff based (only scan changed files)

**Pros:**
- Minimal state: just store the last git HEAD
- Fast: only processes files touched since last scan

**Cons:**
- Collectors like `gitlog` and `lotteryrisk` analyze repo-wide history, not individual files
- A file untouched since last scan could still produce new signals (e.g., a branch became stale)
- Complex to implement per-collector diffing logic

### Option B: Signal hash set (full scan, filter output)

**Pros:**
- Simple: run the full pipeline, compare output hashes against previous run
- Works for all collectors equally — no per-collector diffing logic
- Reuses existing `SignalHash()` function from dedup
- Accurate: catches signals from any source, even non-file-based collectors

**Cons:**
- Full scan cost on every run (no speedup)
- State file grows with signal count (but 1000 signals × 8-char hashes = ~10KB)

### Option C: Per-collector state with pluggable diff strategies

**Pros:**
- Each collector could optimize (e.g., TODO collector only scans changed files)
- Potentially faster for large repos

**Cons:**
- High complexity: new interface method on every collector
- Breaks the "collectors are independent and composable" principle
- Each collector needs its own state schema and migration story

## Recommendation

**Option B: Signal hash set.** Run a full scan, filter output to new-only signals using the hash set from the previous run, then save the full hash set for next time.

Key design points:
- State stored in `<repo>/.stringer/last-scan.json` (gitignored)
- Schema version field for forward compatibility
- Save **all** signal hashes (pre-filter), not just new ones — otherwise previously-output signals reappear as "new"
- `git_head` stored for informational purposes only (not used for filtering)
- Collector list stored for mismatch detection

## Decision

Accepted. Implement Option B with the following schema:

```json
{
  "version": "1",
  "scan_timestamp": "2026-02-07T20:30:00Z",
  "git_head": "abc123def456...",
  "collectors": ["todos", "gitlog", "patterns"],
  "signal_hashes": ["a1b2c3d4", "e5f6g7h8"],
  "signal_count": 42
}
```

The simplicity of hash-based filtering outweighs the lack of scan speedup. Per-collector optimization (Option C) can be layered on later if needed, without changing the state format.
