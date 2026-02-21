// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package state

import "math"

// DefaultWindowSize is the default number of entries to compare for trends.
const DefaultWindowSize = 5

// deadbandPct is the percentage change threshold below which a trend is "stable".
const deadbandPct = 0.10

// Direction describes whether a metric is improving, stable, or degrading.
type Direction string

const (
	Improving Direction = "improving"
	Stable    Direction = "stable"
	Degrading Direction = "degrading"
)

// TrendLine captures the directional change for a single metric.
type TrendLine struct {
	Current   int       `json:"current"`
	Previous  int       `json:"previous"`
	Delta     int       `json:"delta"`
	Direction Direction `json:"direction"`
}

// TrendResult holds computed trends across all metrics.
type TrendResult struct {
	TotalTrend      TrendLine            `json:"total_trend"`
	CollectorTrends map[string]TrendLine `json:"collector_trends"`
	KindTrends      map[string]TrendLine `json:"kind_trends"`
	WindowSize      int                  `json:"window_size"`
	DataPoints      int                  `json:"data_points"`
}

// ComputeTrends analyzes a scan history and produces directional trends.
// It compares the oldest and newest entries within the window.
// Returns nil if fewer than 2 data points are available.
func ComputeTrends(h *ScanHistory, windowSize int) *TrendResult {
	if h == nil || len(h.Entries) < 2 {
		return nil
	}

	entries := h.Entries
	if len(entries) > windowSize {
		entries = entries[len(entries)-windowSize:]
	}

	oldest := entries[0]
	newest := entries[len(entries)-1]

	result := &TrendResult{
		TotalTrend:      computeTrendLine(oldest.TotalSignals, newest.TotalSignals),
		CollectorTrends: make(map[string]TrendLine),
		KindTrends:      make(map[string]TrendLine),
		WindowSize:      windowSize,
		DataPoints:      len(entries),
	}

	// Merge all collector keys from both oldest and newest.
	collectorKeys := mergeKeys(oldest.CollectorCounts, newest.CollectorCounts)
	for _, k := range collectorKeys {
		result.CollectorTrends[k] = computeTrendLine(
			oldest.CollectorCounts[k],
			newest.CollectorCounts[k],
		)
	}

	// Merge all kind keys from both oldest and newest.
	kindKeys := mergeKeys(oldest.KindCounts, newest.KindCounts)
	for _, k := range kindKeys {
		result.KindTrends[k] = computeTrendLine(
			oldest.KindCounts[k],
			newest.KindCounts[k],
		)
	}

	return result
}

// computeTrendLine determines direction from old→new using a 10% deadband.
// For signal counts, fewer signals = improving (work items resolved).
func computeTrendLine(oldVal, newVal int) TrendLine {
	delta := newVal - oldVal
	dir := classifyDirection(oldVal, newVal)
	return TrendLine{
		Current:   newVal,
		Previous:  oldVal,
		Delta:     delta,
		Direction: dir,
	}
}

// classifyDirection applies the deadband threshold to determine direction.
// Signal counts going down means the codebase is improving (fewer issues).
func classifyDirection(oldVal, newVal int) Direction {
	if oldVal == 0 && newVal == 0 {
		return Stable
	}

	// Use the larger value as the denominator to avoid division by zero.
	base := oldVal
	if base == 0 {
		base = newVal
	}

	pctChange := math.Abs(float64(newVal-oldVal)) / float64(base)
	if pctChange <= deadbandPct {
		return Stable
	}

	if newVal < oldVal {
		return Improving // fewer signals = better
	}
	return Degrading // more signals = worse
}

// mergeKeys returns the sorted union of keys from two maps.
func mergeKeys(a, b map[string]int) []string {
	seen := make(map[string]int, len(a)+len(b))
	for k := range a {
		seen[k] = 0
	}
	for k := range b {
		seen[k] = 0
	}
	return SortedKeys(seen)
}
