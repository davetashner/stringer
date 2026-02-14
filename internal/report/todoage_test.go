// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package report

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTodoAge_Registered(t *testing.T) {
	s := Get("todo-age")
	require.NotNil(t, s)
	assert.Equal(t, "todo-age", s.Name())
	assert.NotEmpty(t, s.Description())
}

func TestTodoAge_Analyze_MissingMetrics(t *testing.T) {
	s := &todoAgeSection{}
	err := s.Analyze(&signal.ScanResult{Metrics: map[string]any{}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestTodoAge_Analyze_NilMetrics(t *testing.T) {
	s := &todoAgeSection{}
	err := s.Analyze(&signal.ScanResult{
		Metrics: map[string]any{"todos": (*collectors.TodoMetrics)(nil)},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func fixedNow() time.Time {
	return time.Date(2026, 2, 8, 12, 0, 0, 0, time.UTC)
}

func TestTodoAge_AnalyzeAndRender(t *testing.T) {
	now := fixedNow()
	s := &todoAgeSection{now: func() time.Time { return now }}

	result := &signal.ScanResult{
		Metrics: map[string]any{
			"todos": &collectors.TodoMetrics{
				Total:         5,
				ByKind:        map[string]int{"TODO": 3, "FIXME": 2},
				WithTimestamp: 5,
			},
		},
		Signals: []signal.RawSignal{
			{Source: "todos", FilePath: "a.go", Line: 10, Title: "fresh", Timestamp: now.Add(-2 * 24 * time.Hour)},
			{Source: "todos", FilePath: "b.go", Line: 20, Title: "recent", Timestamp: now.Add(-14 * 24 * time.Hour)},
			{Source: "todos", FilePath: "c.go", Line: 30, Title: "months-old", Timestamp: now.Add(-60 * 24 * time.Hour)},
			{Source: "todos", FilePath: "d.go", Line: 40, Title: "half-year", Timestamp: now.Add(-200 * 24 * time.Hour)},
			{Source: "todos", FilePath: "e.go", Line: 50, Title: "ancient", Timestamp: now.Add(-400 * 24 * time.Hour)},
		},
	}

	require.NoError(t, s.Analyze(result))

	// Check bucket distribution.
	assert.Equal(t, 1, s.buckets[0].Count, "< 1 week")
	assert.Equal(t, 1, s.buckets[1].Count, "1-4 weeks")
	assert.Equal(t, 1, s.buckets[2].Count, "1-3 months")
	assert.Equal(t, 1, s.buckets[3].Count, "3-12 months")
	assert.Equal(t, 1, s.buckets[4].Count, "> 1 year")

	// Should have one stale TODO.
	require.Len(t, s.stale, 1)
	assert.Equal(t, "e.go", s.stale[0].File)
	assert.Equal(t, 50, s.stale[0].Line)
	assert.Equal(t, "ancient", s.stale[0].Title)

	// Render.
	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))

	out := buf.String()
	assert.Contains(t, out, "TODO Age Distribution")
	assert.Contains(t, out, "Total TODOs: 5")
	assert.Contains(t, out, "< 1 week")
	assert.Contains(t, out, "> 1 year")
	assert.Contains(t, out, "#")
	assert.Contains(t, out, "Stale TODOs")
	assert.Contains(t, out, "e.go:50")
	assert.Contains(t, out, "ancient")
	assert.Contains(t, out, "1 year")
}

func TestTodoAge_SkipsNonTodoSignals(t *testing.T) {
	now := fixedNow()
	s := &todoAgeSection{now: func() time.Time { return now }}

	result := &signal.ScanResult{
		Metrics: map[string]any{
			"todos": &collectors.TodoMetrics{Total: 1},
		},
		Signals: []signal.RawSignal{
			{Source: "gitlog", FilePath: "x.go", Line: 1, Timestamp: now.Add(-24 * time.Hour)},
			{Source: "todos", FilePath: "y.go", Line: 2, Timestamp: now.Add(-24 * time.Hour)},
		},
	}

	require.NoError(t, s.Analyze(result))

	total := 0
	for _, b := range s.buckets {
		total += b.Count
	}
	assert.Equal(t, 1, total, "should only count todos signals")
}

func TestTodoAge_SkipsZeroTimestamp(t *testing.T) {
	now := fixedNow()
	s := &todoAgeSection{now: func() time.Time { return now }}

	result := &signal.ScanResult{
		Metrics: map[string]any{
			"todos": &collectors.TodoMetrics{Total: 1},
		},
		Signals: []signal.RawSignal{
			{Source: "todos", FilePath: "z.go", Line: 1},
		},
	}

	require.NoError(t, s.Analyze(result))

	total := 0
	for _, b := range s.buckets {
		total += b.Count
	}
	assert.Equal(t, 0, total, "zero timestamp should be skipped")
}

func TestTodoAge_Render_NoStale(t *testing.T) {
	s := &todoAgeSection{
		total: 2,
		buckets: []ageBucket{
			{Label: "< 1 week", Count: 2},
			{Label: "1-4 weeks"},
			{Label: "1-3 months"},
			{Label: "3-12 months"},
			{Label: "> 1 year"},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))
	assert.NotContains(t, buf.String(), "Stale TODOs")
}

func TestTodoAge_FormatAge(t *testing.T) {
	tests := []struct {
		dur  time.Duration
		want string
	}{
		{0, "1 day"},
		{12 * time.Hour, "1 day"},
		{3 * 24 * time.Hour, "3 days"},
		{7 * 24 * time.Hour, "1 week"},
		{21 * 24 * time.Hour, "3 weeks"},
		{30 * 24 * time.Hour, "1 month"},
		{90 * 24 * time.Hour, "3 months"},
		{365 * 24 * time.Hour, "1 year"},
		{730 * 24 * time.Hour, "2 years"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, formatAge(tt.dur))
		})
	}
}

func TestTodoAge_Analyze_Reinitializes(t *testing.T) {
	now := fixedNow()
	s := &todoAgeSection{now: func() time.Time { return now }}
	s.total = 99
	s.stale = []staleTODO{{File: "old"}}

	result := &signal.ScanResult{
		Metrics: map[string]any{
			"todos": &collectors.TodoMetrics{Total: 1},
		},
		Signals: []signal.RawSignal{
			{Source: "todos", FilePath: "new.go", Line: 1, Timestamp: now.Add(-24 * time.Hour)},
		},
	}

	require.NoError(t, s.Analyze(result))
	assert.Equal(t, 1, s.total)
	assert.Empty(t, s.stale)
}

func TestTodoAge_Render_UnknownAge(t *testing.T) {
	// total=5 but only 3 are bucketed → 2 have unknown age.
	s := &todoAgeSection{
		total: 5,
		buckets: []ageBucket{
			{Label: "< 1 week", Count: 2},
			{Label: "1-4 weeks", Count: 1},
			{Label: "1-3 months"},
			{Label: "3-12 months"},
			{Label: "> 1 year"},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))

	out := buf.String()
	assert.Contains(t, out, "Unknown age")
	assert.Contains(t, out, "  2")
}

func TestTodoAge_Render_NoUnknownAge(t *testing.T) {
	// total matches bucket sum → no unknown-age line.
	s := &todoAgeSection{
		total: 3,
		buckets: []ageBucket{
			{Label: "< 1 week", Count: 2},
			{Label: "1-4 weeks", Count: 1},
			{Label: "1-3 months"},
			{Label: "3-12 months"},
			{Label: "> 1 year"},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))
	assert.NotContains(t, buf.String(), "Unknown age")
}
