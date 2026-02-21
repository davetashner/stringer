// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package report

import (
	"bytes"
	"errors"
	"math"
	"testing"

	"github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHotspots_Registered(t *testing.T) {
	s := Get("hotspots")
	require.NotNil(t, s)
	assert.Equal(t, "hotspots", s.Name())
	assert.NotEmpty(t, s.Description())
}

func TestHotspots_Analyze_MissingComplexity(t *testing.T) {
	s := &hotspotsSection{}
	err := s.Analyze(&signal.ScanResult{
		Metrics: map[string]any{
			"gitlog": &collectors.GitlogMetrics{},
		},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestHotspots_Analyze_MissingGitlog(t *testing.T) {
	s := &hotspotsSection{}
	err := s.Analyze(&signal.ScanResult{
		Metrics: map[string]any{
			"complexity": &collectors.ComplexityMetrics{},
		},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestHotspots_Analyze_NilComplexity(t *testing.T) {
	s := &hotspotsSection{}
	err := s.Analyze(&signal.ScanResult{
		Metrics: map[string]any{
			"complexity": (*collectors.ComplexityMetrics)(nil),
			"gitlog":     &collectors.GitlogMetrics{},
		},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestHotspots_AnalyzeAndRender(t *testing.T) {
	s := &hotspotsSection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"complexity": &collectors.ComplexityMetrics{
				Functions: []collectors.FunctionComplexity{
					{FilePath: "hot.go", FuncName: "processAll", Lines: 200, Branches: 20, Score: 24.0},
					{FilePath: "stable.go", FuncName: "simple", Lines: 20, Branches: 1, Score: 1.4},
					{FilePath: "complex_only.go", FuncName: "bigFunc", Lines: 150, Branches: 12, Score: 15.0},
				},
			},
			"gitlog": &collectors.GitlogMetrics{
				FileChurns: []collectors.FileChurn{
					{Path: "hot.go", ChangeCount: 30, AuthorCount: 5},
					{Path: "stable.go", ChangeCount: 1, AuthorCount: 1},
					{Path: "churn_only.go", ChangeCount: 50, AuthorCount: 3},
				},
			},
		},
	}

	require.NoError(t, s.Analyze(result))

	// hot.go: score 24.0, 30 changes → hotspot score = 24.0 * log2(31)
	// stable.go: score 1.4, 1 change → excluded (score < 6)
	// complex_only.go: score 15.0, 0 changes → excluded (no churn)
	require.Len(t, s.hotspots, 1)
	assert.Equal(t, "hot.go", s.hotspots[0].FilePath)
	assert.Equal(t, "processAll", s.hotspots[0].FuncName)
	expectedScore := 24.0 * math.Log2(31)
	assert.InDelta(t, expectedScore, s.hotspots[0].HotspotScore, 0.1)

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))

	out := buf.String()
	assert.Contains(t, out, "Toxic Hotspots")
	assert.Contains(t, out, "processAll")
	assert.Contains(t, out, "hot.go")
	assert.NotContains(t, out, "stable.go")
	assert.NotContains(t, out, "complex_only.go")
}

func TestHotspots_Render_Empty(t *testing.T) {
	s := &hotspotsSection{}
	s.hotspots = nil

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))
	assert.Contains(t, buf.String(), "No toxic hotspots detected")
}

func TestHotspots_MinThresholds(t *testing.T) {
	s := &hotspotsSection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"complexity": &collectors.ComplexityMetrics{
				Functions: []collectors.FunctionComplexity{
					// Below complexity threshold (< 6.0).
					{FilePath: "a.go", FuncName: "low", Score: 5.9},
					// Above complexity but below churn threshold.
					{FilePath: "b.go", FuncName: "complex_no_churn", Score: 10.0},
				},
			},
			"gitlog": &collectors.GitlogMetrics{
				FileChurns: []collectors.FileChurn{
					{Path: "a.go", ChangeCount: 100},
					{Path: "b.go", ChangeCount: 4}, // Below minHotspotChurn (5).
				},
			},
		},
	}

	require.NoError(t, s.Analyze(result))
	assert.Empty(t, s.hotspots)
}

func TestHotspots_SortAndCap(t *testing.T) {
	funcs := make([]collectors.FunctionComplexity, 20)
	churns := make([]collectors.FileChurn, 20)
	for i := range funcs {
		path := "file" + string(rune('a'+i)) + ".go"
		funcs[i] = collectors.FunctionComplexity{
			FilePath: path,
			FuncName: "func" + string(rune('A'+i)),
			Score:    float64(10 + i),
		}
		churns[i] = collectors.FileChurn{
			Path:        path,
			ChangeCount: 10 + i,
		}
	}

	s := &hotspotsSection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"complexity": &collectors.ComplexityMetrics{Functions: funcs},
			"gitlog":     &collectors.GitlogMetrics{FileChurns: churns},
		},
	}

	require.NoError(t, s.Analyze(result))
	assert.Len(t, s.hotspots, hotspotsTopN)
	// First should have highest hotspot score.
	assert.True(t, s.hotspots[0].HotspotScore >= s.hotspots[1].HotspotScore)
}
