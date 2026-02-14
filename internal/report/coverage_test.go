// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package report

import (
	"bytes"
	"errors"
	"testing"

	"github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoverage_Registered(t *testing.T) {
	s := Get("coverage")
	require.NotNil(t, s)
	assert.Equal(t, "coverage", s.Name())
	assert.NotEmpty(t, s.Description())
}

func TestCoverage_Analyze_MissingMetrics(t *testing.T) {
	s := &coverageSection{}
	err := s.Analyze(&signal.ScanResult{Metrics: map[string]any{}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestCoverage_Analyze_NilMetrics(t *testing.T) {
	s := &coverageSection{}
	err := s.Analyze(&signal.ScanResult{
		Metrics: map[string]any{"patterns": (*collectors.PatternsMetrics)(nil)},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestCoverage_AnalyzeAndRender(t *testing.T) {
	s := &coverageSection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"patterns": &collectors.PatternsMetrics{
				LargeFiles: 2,
				DirectoryTestRatios: []collectors.DirectoryTestRatio{
					{Path: "pkg/good", SourceFiles: 10, TestFiles: 8, Ratio: 0.8},
					{Path: "pkg/none", SourceFiles: 5, TestFiles: 0, Ratio: 0.0},
					{Path: "pkg/critical", SourceFiles: 20, TestFiles: 1, Ratio: 0.05},
					{Path: "pkg/low", SourceFiles: 15, TestFiles: 3, Ratio: 0.2},
					{Path: "pkg/moderate", SourceFiles: 12, TestFiles: 5, Ratio: 0.42},
				},
			},
		},
	}

	require.NoError(t, s.Analyze(result))

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))

	out := buf.String()
	assert.Contains(t, out, "Test Coverage Gaps")

	// Should be sorted by ratio ascending (worst first).
	noneIdx := bytes.Index(buf.Bytes(), []byte("pkg/none"))
	critIdx := bytes.Index(buf.Bytes(), []byte("pkg/critical"))
	lowIdx := bytes.Index(buf.Bytes(), []byte("pkg/low"))
	modIdx := bytes.Index(buf.Bytes(), []byte("pkg/moderate"))
	goodIdx := bytes.Index(buf.Bytes(), []byte("pkg/good"))
	assert.True(t, noneIdx < critIdx, "NO TESTS before CRITICAL")
	assert.True(t, critIdx < lowIdx, "CRITICAL before LOW")
	assert.True(t, lowIdx < modIdx, "LOW before MODERATE")
	assert.True(t, modIdx < goodIdx, "MODERATE before GOOD")

	// Check assessments.
	assert.Contains(t, out, "NO TESTS")
	assert.Contains(t, out, "CRITICAL")
	assert.Contains(t, out, "LOW")
	assert.Contains(t, out, "MODERATE")
	assert.Contains(t, out, "GOOD")
}

func TestCoverage_Render_Empty(t *testing.T) {
	s := &coverageSection{}
	s.dirs = nil

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))
	assert.Contains(t, buf.String(), "No test coverage data available")
}

func TestCoverage_Assessment(t *testing.T) {
	tests := []struct {
		name string
		dir  collectors.DirectoryTestRatio
		want string
	}{
		{"no tests", collectors.DirectoryTestRatio{TestFiles: 0, Ratio: 0}, "NO TESTS"},
		{"critical", collectors.DirectoryTestRatio{TestFiles: 1, Ratio: 0.05}, "CRITICAL"},
		{"low", collectors.DirectoryTestRatio{TestFiles: 2, Ratio: 0.2}, "LOW"},
		{"moderate", collectors.DirectoryTestRatio{TestFiles: 5, Ratio: 0.42}, "MODERATE"},
		{"good", collectors.DirectoryTestRatio{TestFiles: 8, Ratio: 0.8}, "GOOD"},
		{"boundary 0.1", collectors.DirectoryTestRatio{TestFiles: 1, Ratio: 0.1}, "LOW"},
		{"boundary 0.3", collectors.DirectoryTestRatio{TestFiles: 3, Ratio: 0.3}, "MODERATE"},
		{"boundary 0.5", collectors.DirectoryTestRatio{TestFiles: 5, Ratio: 0.5}, "GOOD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, coverageAssessment(tt.dir))
		})
	}
}

func TestCoverage_Analyze_Reinitializes(t *testing.T) {
	s := &coverageSection{}
	s.dirs = []collectors.DirectoryTestRatio{{Path: "old"}}

	result := &signal.ScanResult{
		Metrics: map[string]any{
			"patterns": &collectors.PatternsMetrics{
				DirectoryTestRatios: []collectors.DirectoryTestRatio{
					{Path: "new", SourceFiles: 5, TestFiles: 3, Ratio: 0.6},
				},
			},
		},
	}
	require.NoError(t, s.Analyze(result))
	assert.Len(t, s.dirs, 1)
	assert.Equal(t, "new", s.dirs[0].Path)
}
