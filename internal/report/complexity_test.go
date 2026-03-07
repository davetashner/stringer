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

func TestComplexity_AnalyzeAndRender_RegexOnly(t *testing.T) {
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
	assert.Contains(t, out, "Heuristic-based analysis")
	assert.NotContains(t, out, "AST-based analysis")
	assert.Contains(t, out, "processRequest")
	assert.Contains(t, out, "parse")
	assert.Contains(t, out, "simple")

	// Should be sorted by score descending.
	processIdx := bytes.Index(buf.Bytes(), []byte("processRequest"))
	parseIdx := bytes.Index(buf.Bytes(), []byte("parse"))
	assert.True(t, processIdx < parseIdx, "processRequest (24.0) before parse (12.0)")
}

func TestComplexity_AnalyzeAndRender_ASTBased(t *testing.T) {
	s := &complexitySection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"complexity": &collectors.ComplexityMetrics{
				Functions: []collectors.FunctionComplexity{
					{
						FilePath: "server.go", FuncName: "(*Server).Handle",
						StartLine: 10, EndLine: 50, Lines: 38,
						Cyclomatic: 12, Cognitive: 20, MaxNesting: 4,
						ASTBased: true, Score: 20,
					},
					{
						FilePath: "parser.go", FuncName: "parseExpr",
						StartLine: 5, EndLine: 30, Lines: 23,
						Cyclomatic: 8, Cognitive: 15, MaxNesting: 3,
						ASTBased: true, Score: 15,
					},
				},
				FilesAnalyzed:  2,
				FunctionsFound: 2,
			},
		},
	}

	require.NoError(t, s.Analyze(result))
	assert.True(t, s.hasAST)

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))

	out := buf.String()
	assert.Contains(t, out, "AST-based analysis (Go)")
	assert.NotContains(t, out, "Heuristic-based analysis")
	assert.Contains(t, out, "Cyclomatic")
	assert.Contains(t, out, "Cognitive")
	assert.Contains(t, out, "Nesting")
	assert.Contains(t, out, "(*Server).Handle")
	assert.Contains(t, out, "parseExpr")

	// Should be sorted by cognitive descending: Handle(20) before parseExpr(15).
	handleIdx := bytes.Index(buf.Bytes(), []byte("(*Server).Handle"))
	parseIdx := bytes.Index(buf.Bytes(), []byte("parseExpr"))
	assert.True(t, handleIdx < parseIdx,
		"Handle (cognitive 20) should appear before parseExpr (cognitive 15)")
}

func TestComplexity_AnalyzeAndRender_Mixed(t *testing.T) {
	s := &complexitySection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"complexity": &collectors.ComplexityMetrics{
				Functions: []collectors.FunctionComplexity{
					{
						FilePath: "handler.go", FuncName: "processRequest",
						StartLine: 10, EndLine: 50, Lines: 38,
						Cyclomatic: 12, Cognitive: 20, MaxNesting: 4,
						ASTBased: true, Score: 20,
					},
					{
						FilePath: "app.py", FuncName: "handle_request",
						StartLine: 5, Lines: 100, Branches: 15,
						Score: 17.0,
					},
				},
				FilesAnalyzed:  2,
				FunctionsFound: 2,
			},
		},
	}

	require.NoError(t, s.Analyze(result))

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))

	out := buf.String()
	assert.Contains(t, out, "AST-based analysis (Go)")
	assert.Contains(t, out, "Heuristic-based analysis")
	assert.Contains(t, out, "processRequest")
	assert.Contains(t, out, "handle_request")
}

func TestComplexity_SortByCognitive(t *testing.T) {
	s := &complexitySection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"complexity": &collectors.ComplexityMetrics{
				Functions: []collectors.FunctionComplexity{
					{
						FilePath: "a.go", FuncName: "lowCognitive",
						Cyclomatic: 15, Cognitive: 5, MaxNesting: 2,
						ASTBased: true, Score: 5,
					},
					{
						FilePath: "b.go", FuncName: "highCognitive",
						Cyclomatic: 8, Cognitive: 25, MaxNesting: 3,
						ASTBased: true, Score: 25,
					},
				},
			},
		},
	}

	require.NoError(t, s.Analyze(result))

	// highCognitive should be first despite lower cyclomatic.
	assert.Equal(t, "highCognitive", s.functions[0].FuncName)
	assert.Equal(t, "lowCognitive", s.functions[1].FuncName)
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
	high := ColorComplexity("20.0")
	assert.Contains(t, high, "20.0")

	med := ColorComplexity("10.0")
	assert.Contains(t, med, "10.0")

	low := ColorComplexity("5.0")
	assert.Equal(t, "5.0", low)

	assert.Equal(t, "n/a", ColorComplexity("n/a"))
}

func TestColorCyclomatic(t *testing.T) {
	high := ColorCyclomatic("25")
	assert.Contains(t, high, "25")

	med := ColorCyclomatic("12")
	assert.Contains(t, med, "12")

	low := ColorCyclomatic("5")
	assert.Equal(t, "5", low)

	assert.Equal(t, "n/a", ColorCyclomatic("n/a"))
}

func TestColorCognitive(t *testing.T) {
	high := ColorCognitive("30")
	assert.Contains(t, high, "30")

	med := ColorCognitive("18")
	assert.Contains(t, med, "18")

	low := ColorCognitive("5")
	assert.Equal(t, "5", low)

	assert.Equal(t, "n/a", ColorCognitive("n/a"))
}
