// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestComputeTrends_NilHistory(t *testing.T) {
	result := ComputeTrends(nil, DefaultWindowSize)
	assert.Nil(t, result)
}

func TestComputeTrends_SingleEntry(t *testing.T) {
	h := &ScanHistory{
		Entries: []HistoryEntry{{TotalSignals: 10}},
	}
	result := ComputeTrends(h, DefaultWindowSize)
	assert.Nil(t, result)
}

func TestComputeTrends_Improving(t *testing.T) {
	h := &ScanHistory{
		Entries: []HistoryEntry{
			{TotalSignals: 20, CollectorCounts: map[string]int{"todos": 15}, KindCounts: map[string]int{"todo": 15}},
			{TotalSignals: 10, CollectorCounts: map[string]int{"todos": 7}, KindCounts: map[string]int{"todo": 7}},
		},
	}
	result := ComputeTrends(h, DefaultWindowSize)

	assert.NotNil(t, result)
	assert.Equal(t, Improving, result.TotalTrend.Direction)
	assert.Equal(t, 20, result.TotalTrend.Previous)
	assert.Equal(t, 10, result.TotalTrend.Current)
	assert.Equal(t, -10, result.TotalTrend.Delta)
	assert.Equal(t, 2, result.DataPoints)
}

func TestComputeTrends_Degrading(t *testing.T) {
	h := &ScanHistory{
		Entries: []HistoryEntry{
			{TotalSignals: 10, CollectorCounts: map[string]int{"todos": 5}, KindCounts: map[string]int{"todo": 5}},
			{TotalSignals: 20, CollectorCounts: map[string]int{"todos": 15}, KindCounts: map[string]int{"todo": 15}},
		},
	}
	result := ComputeTrends(h, DefaultWindowSize)

	assert.NotNil(t, result)
	assert.Equal(t, Degrading, result.TotalTrend.Direction)
	assert.Equal(t, 10, result.TotalTrend.Previous)
	assert.Equal(t, 20, result.TotalTrend.Current)
	assert.Equal(t, 10, result.TotalTrend.Delta)
}

func TestComputeTrends_Stable(t *testing.T) {
	h := &ScanHistory{
		Entries: []HistoryEntry{
			{TotalSignals: 100, CollectorCounts: map[string]int{"todos": 50}, KindCounts: map[string]int{"todo": 50}},
			{TotalSignals: 105, CollectorCounts: map[string]int{"todos": 52}, KindCounts: map[string]int{"todo": 52}},
		},
	}
	result := ComputeTrends(h, DefaultWindowSize)

	assert.NotNil(t, result)
	assert.Equal(t, Stable, result.TotalTrend.Direction)
}

func TestComputeTrends_DeadbandBoundary(t *testing.T) {
	// Exactly 10% change: 100→110 should be stable (boundary is <=10%).
	h := &ScanHistory{
		Entries: []HistoryEntry{
			{TotalSignals: 100},
			{TotalSignals: 110},
		},
	}
	result := ComputeTrends(h, DefaultWindowSize)
	assert.Equal(t, Stable, result.TotalTrend.Direction)

	// 11% change: should be degrading.
	h.Entries[1].TotalSignals = 111
	result = ComputeTrends(h, DefaultWindowSize)
	assert.Equal(t, Degrading, result.TotalTrend.Direction)
}

func TestComputeTrends_ZeroToNonZero(t *testing.T) {
	h := &ScanHistory{
		Entries: []HistoryEntry{
			{TotalSignals: 0},
			{TotalSignals: 5},
		},
	}
	result := ComputeTrends(h, DefaultWindowSize)
	assert.Equal(t, Degrading, result.TotalTrend.Direction)
}

func TestComputeTrends_NonZeroToZero(t *testing.T) {
	h := &ScanHistory{
		Entries: []HistoryEntry{
			{TotalSignals: 5},
			{TotalSignals: 0},
		},
	}
	result := ComputeTrends(h, DefaultWindowSize)
	assert.Equal(t, Improving, result.TotalTrend.Direction)
}

func TestComputeTrends_BothZero(t *testing.T) {
	h := &ScanHistory{
		Entries: []HistoryEntry{
			{TotalSignals: 0},
			{TotalSignals: 0},
		},
	}
	result := ComputeTrends(h, DefaultWindowSize)
	assert.Equal(t, Stable, result.TotalTrend.Direction)
}

func TestComputeTrends_WindowClipping(t *testing.T) {
	h := &ScanHistory{
		Entries: []HistoryEntry{
			{Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), TotalSignals: 100},
			{Timestamp: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), TotalSignals: 90},
			{Timestamp: time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC), TotalSignals: 80},
			{Timestamp: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC), TotalSignals: 70},
			{Timestamp: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), TotalSignals: 60},
			{Timestamp: time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC), TotalSignals: 50},
			{Timestamp: time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC), TotalSignals: 40},
		},
	}

	// Window of 3: compares entry[4] vs entry[6] (60→40).
	result := ComputeTrends(h, 3)
	assert.Equal(t, 3, result.DataPoints)
	assert.Equal(t, 60, result.TotalTrend.Previous)
	assert.Equal(t, 40, result.TotalTrend.Current)
	assert.Equal(t, Improving, result.TotalTrend.Direction)
}

func TestComputeTrends_CollectorAndKindTrends(t *testing.T) {
	h := &ScanHistory{
		Entries: []HistoryEntry{
			{
				TotalSignals:    30,
				CollectorCounts: map[string]int{"todos": 20, "gitlog": 10},
				KindCounts:      map[string]int{"todo": 20, "churn": 10},
			},
			{
				TotalSignals:    25,
				CollectorCounts: map[string]int{"todos": 10, "gitlog": 15},
				KindCounts:      map[string]int{"todo": 10, "churn": 15},
			},
		},
	}
	result := ComputeTrends(h, DefaultWindowSize)

	// todos: 20→10 = improving
	assert.Equal(t, Improving, result.CollectorTrends["todos"].Direction)
	// gitlog: 10→15 = degrading (50% increase)
	assert.Equal(t, Degrading, result.CollectorTrends["gitlog"].Direction)
	// todo kind: 20→10 = improving
	assert.Equal(t, Improving, result.KindTrends["todo"].Direction)
	// churn kind: 10→15 = degrading
	assert.Equal(t, Degrading, result.KindTrends["churn"].Direction)
}

func TestComputeTrends_NewCollectorAppears(t *testing.T) {
	h := &ScanHistory{
		Entries: []HistoryEntry{
			{
				TotalSignals:    10,
				CollectorCounts: map[string]int{"todos": 10},
				KindCounts:      map[string]int{"todo": 10},
			},
			{
				TotalSignals:    15,
				CollectorCounts: map[string]int{"todos": 10, "vuln": 5},
				KindCounts:      map[string]int{"todo": 10, "vulnerability": 5},
			},
		},
	}
	result := ComputeTrends(h, DefaultWindowSize)

	// vuln appeared: 0→5 = degrading
	assert.Equal(t, Degrading, result.CollectorTrends["vuln"].Direction)
	assert.Equal(t, 0, result.CollectorTrends["vuln"].Previous)
	assert.Equal(t, 5, result.CollectorTrends["vuln"].Current)

	// todos unchanged: 10→10 = stable
	assert.Equal(t, Stable, result.CollectorTrends["todos"].Direction)
}

func TestComputeTrends_CollectorDisappears(t *testing.T) {
	h := &ScanHistory{
		Entries: []HistoryEntry{
			{
				TotalSignals:    15,
				CollectorCounts: map[string]int{"todos": 10, "vuln": 5},
			},
			{
				TotalSignals:    10,
				CollectorCounts: map[string]int{"todos": 10},
			},
		},
	}
	result := ComputeTrends(h, DefaultWindowSize)

	// vuln removed: 5→0 = improving
	assert.Equal(t, Improving, result.CollectorTrends["vuln"].Direction)
}

func TestClassifyDirection(t *testing.T) {
	tests := []struct {
		name     string
		oldVal   int
		newVal   int
		expected Direction
	}{
		{"both zero", 0, 0, Stable},
		{"no change", 50, 50, Stable},
		{"within deadband up", 100, 110, Stable},
		{"within deadband down", 100, 91, Stable},
		{"improving", 100, 50, Improving},
		{"degrading", 50, 100, Degrading},
		{"zero to non-zero", 0, 10, Degrading},
		{"non-zero to zero", 10, 0, Improving},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, classifyDirection(tt.oldVal, tt.newVal))
		})
	}
}
