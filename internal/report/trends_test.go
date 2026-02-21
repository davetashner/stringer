// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package report

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/davetashner/stringer/internal/state"
)

func TestTrends_Registered(t *testing.T) {
	s := Get("trends")
	require.NotNil(t, s)
	assert.Equal(t, "trends", s.Name())
	assert.NotEmpty(t, s.Description())
}

func TestTrends_Analyze_MissingMetrics(t *testing.T) {
	s := &trendsSection{}
	err := s.Analyze(&signal.ScanResult{Metrics: map[string]any{}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestTrends_Analyze_NilHistory(t *testing.T) {
	s := &trendsSection{}
	err := s.Analyze(&signal.ScanResult{
		Metrics: map[string]any{"_history": (*state.ScanHistory)(nil)},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestTrends_Analyze_InsufficientData(t *testing.T) {
	s := &trendsSection{}
	h := &state.ScanHistory{
		Entries: []state.HistoryEntry{
			{TotalSignals: 10},
		},
	}
	err := s.Analyze(&signal.ScanResult{
		Metrics: map[string]any{"_history": h},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestTrends_AnalyzeAndRender(t *testing.T) {
	s := &trendsSection{}
	h := &state.ScanHistory{
		Version: "1",
		Entries: []state.HistoryEntry{
			{
				Timestamp:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
				TotalSignals:    20,
				CollectorCounts: map[string]int{"todos": 15, "gitlog": 5},
				KindCounts:      map[string]int{"todo": 15, "churn": 5},
			},
			{
				Timestamp:       time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
				TotalSignals:    12,
				CollectorCounts: map[string]int{"todos": 8, "gitlog": 4},
				KindCounts:      map[string]int{"todo": 8, "churn": 4},
			},
		},
	}

	result := &signal.ScanResult{
		Metrics: map[string]any{"_history": h},
	}

	require.NoError(t, s.Analyze(result))
	require.NotNil(t, s.trends)

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))

	out := buf.String()
	assert.Contains(t, out, "Health Trends")
	assert.Contains(t, out, "Total")
	assert.Contains(t, out, "todos")
	assert.Contains(t, out, "gitlog")
	assert.Contains(t, out, "todo")
	assert.Contains(t, out, "churn")
	assert.Contains(t, out, "improving")
}

func TestTrends_Render_Degrading(t *testing.T) {
	s := &trendsSection{}
	h := &state.ScanHistory{
		Entries: []state.HistoryEntry{
			{TotalSignals: 10, CollectorCounts: map[string]int{"todos": 10}, KindCounts: map[string]int{"todo": 10}},
			{TotalSignals: 25, CollectorCounts: map[string]int{"todos": 25}, KindCounts: map[string]int{"todo": 25}},
		},
	}

	require.NoError(t, s.Analyze(&signal.ScanResult{
		Metrics: map[string]any{"_history": h},
	}))

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))
	assert.Contains(t, buf.String(), "degrading")
}

func TestTrends_Render_Stable(t *testing.T) {
	s := &trendsSection{}
	h := &state.ScanHistory{
		Entries: []state.HistoryEntry{
			{TotalSignals: 100, CollectorCounts: map[string]int{"todos": 100}, KindCounts: map[string]int{"todo": 100}},
			{TotalSignals: 105, CollectorCounts: map[string]int{"todos": 105}, KindCounts: map[string]int{"todo": 105}},
		},
	}

	require.NoError(t, s.Analyze(&signal.ScanResult{
		Metrics: map[string]any{"_history": h},
	}))

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))
	assert.Contains(t, buf.String(), "stable")
}

func TestColorDirection(t *testing.T) {
	improving := ColorDirection("improving")
	assert.Contains(t, improving, "improving")

	degrading := ColorDirection("degrading")
	assert.Contains(t, degrading, "degrading")

	stable := ColorDirection("stable")
	assert.Equal(t, "stable", stable)

	unknown := ColorDirection("unknown")
	assert.Equal(t, "unknown", unknown)
}

func TestFormatDelta(t *testing.T) {
	assert.Equal(t, "+5", formatDelta(5))
	assert.Equal(t, "-3", formatDelta(-3))
	assert.Equal(t, "0", formatDelta(0))
}

func TestSortedTrendKeys(t *testing.T) {
	m := map[string]state.TrendLine{
		"charlie": {},
		"alpha":   {},
		"bravo":   {},
	}
	keys := SortedTrendKeys(m)
	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, keys)
}
