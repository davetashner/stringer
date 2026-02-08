# 008: Report Data Model

**Status:** Accepted
**Date:** 2026-02-08
**Context:** stringer-itu (R1: Report Command Framework), stringer-rz4 (R2: Report Sections)

## Problem

The `stringer report` command needs to render rich report sections (lottery risk per directory, churn hotspots, TODO age distribution, test coverage gaps). These sections require structured metrics — not just the flat `[]RawSignal` that the pipeline currently produces.

For example, the lottery risk report section needs per-directory ownership data (author percentages, lottery risk scores), but the `LotteryRiskCollector` flattens this into human-readable `RawSignal.Description` strings before returning. The churn section needs per-file change counts, but `GitlogCollector` reduces these to individual signals for files exceeding a threshold.

The core question: **how should report sections access the structured data they need?**

## Options

### Option A: Parse metrics from existing signals

Report sections receive `[]RawSignal`, filter by `Source`/`Kind`, and parse structured data from `Title`/`Description` fields using string matching or regex.

**Pros:**
- Zero changes to pipeline, collectors, or domain types
- Works with existing scan output (JSON/JSONL files can be piped in)
- Report sections are decoupled from collector internals

**Cons:**
- Fragile — any change to signal description format silently breaks report parsing
- Lossy — collectors discard sub-threshold data (e.g., directories with lottery risk > threshold are not emitted as signals, but a report should still show them as "healthy")
- Encourages coupling to human-readable strings as a data contract
- Test coverage and TODO age require data not currently in signals (test file ratios, TODO timestamps)

### Option B: Add structured metrics to ScanResult

Extend `ScanResult` with a `Metrics map[string]any` field. Each collector optionally populates typed metric structs alongside its signals. Report sections type-assert the metrics they need.

```go
// In signal/signal.go
type ScanResult struct {
    Signals  []RawSignal
    Results  []CollectorResult
    Duration time.Duration
    Metrics  map[string]any // keyed by collector name
}

// In collectors/lotteryrisk.go — returned via CollectorResult
type LotteryRiskMetrics struct {
    Directories []DirectoryOwnership
}
type DirectoryOwnership struct {
    Path        string
    LotteryRisk int
    Authors     []AuthorShare // sorted by ownership desc
    TotalLines  int
}

// In a report section
func (s *LotteryRiskSection) Render(result *signal.ScanResult, w io.Writer) error {
    metrics, ok := result.Metrics["lotteryrisk"].(*LotteryRiskMetrics)
    if !ok {
        return fmt.Errorf("lottery risk metrics not available")
    }
    // render table from metrics.Directories
}
```

**Pros:**
- Clean, typed data contract between collectors and report sections
- Collectors already compute this data — just need to expose it instead of discarding it
- Full dataset available (not just above-threshold signals)
- Pipeline changes are additive (existing `Collector` interface unchanged)
- Metrics are available for any consumer, not just reports

**Cons:**
- Requires `CollectorResult` to carry a `Metrics any` field
- Type assertions at report section boundaries (mitigated by compile-time helper functions)
- Metrics are lost when scan output is serialized to JSONL/JSON (report must run its own scan or use a richer serialization)
- Slightly increases memory footprint per scan

### Option C: Report command runs independent analysis

The report command takes a repo path (not scan output) and runs its own specialized analyzers that are separate from the scan pipeline's collectors.

```go
// In internal/report/
type Analyzer interface {
    Name() string
    Analyze(ctx context.Context, repoPath string) (any, error)
}

// LotteryRiskAnalyzer, ChurnAnalyzer, etc. — independent of collectors
```

**Pros:**
- Complete decoupling — report analyzers can compute exactly what they need
- No changes to existing scan pipeline
- Report can include data that no collector produces (e.g., dependency graph metrics)

**Cons:**
- Duplicates significant logic from existing collectors (lottery risk computation, churn counting, TODO scanning)
- Two code paths computing the same metrics will drift over time
- Users must run `stringer report` separately from `stringer scan` with no shared work
- Substantially more code to write and maintain

## Recommendation

**Option B: Add structured metrics to ScanResult.**

It's the pragmatic middle ground. The collectors already compute the data — the lottery risk collector builds `dirOwnership` maps with per-author stats, the gitlog collector accumulates `fileChanges` counts. Today this data is discarded after being flattened into signals. Option B simply exposes it.

The key advantages over Option A: it's typed (not string parsing), complete (includes sub-threshold data), and testable. The key advantage over Option C: it reuses existing collector logic rather than duplicating it.

The "metrics lost on serialization" limitation is acceptable because `stringer report` will always operate on a live repository (it needs the repo for rendering context anyway). If future users need offline report generation from saved scan data, we can add a `--save-metrics` flag that serializes the metrics map to a JSON sidecar file.

Implementation approach:
1. Add `Metrics any` to `CollectorResult`
2. Add `Metrics map[string]any` to `ScanResult`, populated by pipeline from collector results
3. Define typed metric structs in each collector package (e.g., `LotteryRiskMetrics`)
4. Update collectors to populate metrics alongside signals
5. Report sections type-assert metrics by collector name

This is additive — the `Collector` interface is unchanged, existing scan behavior is unaffected, and the metrics field is ignored by current formatters.

## Decision

Option B accepted. Add `Metrics any` to `CollectorResult` and `Metrics map[string]any` to `ScanResult`. Collectors populate typed metric structs alongside signals; pipeline aggregates them. Report sections type-assert the metrics they need by collector name.
