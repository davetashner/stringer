// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package report

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComplexity_Registered(t *testing.T) {
	s := Get("complexity")
	require.NotNil(t, s)
	assert.Equal(t, "complexity", s.Name())
	assert.NotEmpty(t, s.Description())
}

func TestComplexity_Analyze_MissingMetrics(t *testing.T) {
	s := &complexitySection{}
	err := s.Analyze(&signal.ScanResult{Metrics: map[string]any{}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestComplexity_Analyze_NilMetrics(t *testing.T) {
	s := &complexitySection{}
	err := s.Analyze(&signal.ScanResult{
		Metrics: map[string]any{"complexity": (*collectors.ComplexityMetrics)(nil)},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestComplexity_AnalyzeAndRender(t *testing.T) {
	s := &complexitySection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"complexity": &collectors.ComplexityMetrics{
				Functions: []collectors.FunctionComplexity{
					{FilePath: "main.go", FuncName: "simple", StartLine: 1, Lines: 10, Branches: 1, Score: 1.2},
					{FilePath: "handler.go", FuncName: "processRequest", StartLine: 15, Lines: 200, Branches: 20, Score: 24.0},
					{FilePath: "parser.go", FuncName: "parse", StartLine: 30, Lines: 100, Branches: 10, Score: 12.0},
				},
				FilesAnalyzed:  3,
				FunctionsFound: 3,
			},
		},
	}

	require.NoError(t, s.Analyze(result))

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))

	out := buf.String()
	assert.Contains(t, out, "Function Complexity")
	assert.Contains(t, out, "processRequest")
	assert.Contains(t, out, "parse")
	assert.Contains(t, out, "simple")

	// Should be sorted by score descending.
	processIdx := bytes.Index(buf.Bytes(), []byte("processRequest"))
	parseIdx := bytes.Index(buf.Bytes(), []byte("parse"))
	assert.True(t, processIdx < parseIdx, "processRequest (24.0) before parse (12.0)")
}

func TestComplexity_TopN_Cap(t *testing.T) {
	funcs := make([]collectors.FunctionComplexity, 30)
	for i := range funcs {
		funcs[i] = collectors.FunctionComplexity{
			FilePath:  fmt.Sprintf("file%d.go", i),
			FuncName:  fmt.Sprintf("func%d", i),
			StartLine: i + 1,
			Lines:     100 + i,
			Branches:  20 - i,
			Score:     float64(30 - i),
		}
	}

	s := &complexitySection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"complexity": &collectors.ComplexityMetrics{Functions: funcs},
		},
	}

	require.NoError(t, s.Analyze(result))
	assert.Len(t, s.functions, complexityTopN)
	assert.Equal(t, float64(30), s.functions[0].Score)
}

func TestComplexity_Render_Empty(t *testing.T) {
	s := &complexitySection{}
	s.functions = nil

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))
	assert.Contains(t, buf.String(), "No complex functions detected")
}

func TestColorComplexity(t *testing.T) {
	// High score — should contain original text.
	high := ColorComplexity("20.0")
	assert.Contains(t, high, "20.0")

	// Medium score — should contain original text.
	med := ColorComplexity("10.0")
	assert.Contains(t, med, "10.0")

	// Low score returned as-is.
	low := ColorComplexity("5.0")
	assert.Equal(t, "5.0", low)

	// Non-numeric returned as-is.
	assert.Equal(t, "n/a", ColorComplexity("n/a"))
}
