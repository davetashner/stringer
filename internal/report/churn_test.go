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

func TestChurn_Registered(t *testing.T) {
	s := Get("churn")
	require.NotNil(t, s)
	assert.Equal(t, "churn", s.Name())
	assert.NotEmpty(t, s.Description())
}

func TestChurn_Analyze_MissingMetrics(t *testing.T) {
	s := &churnSection{}
	err := s.Analyze(&signal.ScanResult{Metrics: map[string]any{}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestChurn_Analyze_NilMetrics(t *testing.T) {
	s := &churnSection{}
	err := s.Analyze(&signal.ScanResult{
		Metrics: map[string]any{"gitlog": (*collectors.GitlogMetrics)(nil)},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestChurn_AnalyzeAndRender(t *testing.T) {
	s := &churnSection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"gitlog": &collectors.GitlogMetrics{
				FileChurns: []collectors.FileChurn{
					{Path: "stable.go", ChangeCount: 2, AuthorCount: 1},
					{Path: "hot.go", ChangeCount: 25, AuthorCount: 5},
					{Path: "moderate.go", ChangeCount: 12, AuthorCount: 3},
				},
				RevertCount:      3,
				StaleBranchCount: 1,
			},
		},
	}

	require.NoError(t, s.Analyze(result))

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))

	out := buf.String()
	assert.Contains(t, out, "Code Churn")
	assert.Contains(t, out, "Reverts detected: 3")
	assert.Contains(t, out, "Stale branches:   1")

	// Should be sorted by change count descending.
	hotIdx := bytes.Index(buf.Bytes(), []byte("hot.go"))
	modIdx := bytes.Index(buf.Bytes(), []byte("moderate.go"))
	stableIdx := bytes.Index(buf.Bytes(), []byte("stable.go"))
	assert.True(t, hotIdx < modIdx, "hot.go (25 changes) before moderate.go (12)")
	assert.True(t, modIdx < stableIdx, "moderate.go (12) before stable.go (2)")

	// Check stability levels.
	assert.Contains(t, out, "unstable")
	assert.Contains(t, out, "moderate")
	assert.Contains(t, out, "stable")
}

func TestChurn_TopN_Cap(t *testing.T) {
	churns := make([]collectors.FileChurn, 30)
	for i := range churns {
		churns[i] = collectors.FileChurn{
			Path:        fmt.Sprintf("file%d.go", i),
			ChangeCount: 30 - i,
			AuthorCount: 1,
		}
	}

	s := &churnSection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"gitlog": &collectors.GitlogMetrics{FileChurns: churns},
		},
	}

	require.NoError(t, s.Analyze(result))
	assert.Len(t, s.churns, churnTopN)
	// First entry should be the highest churn.
	assert.Equal(t, 30, s.churns[0].ChangeCount)
}

func TestChurn_Render_EmptyChurns(t *testing.T) {
	s := &churnSection{}
	s.churns = nil

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))
	assert.Contains(t, buf.String(), "No file churn data available")
}

func TestChurn_Render_EmptyChurnsWithReverts(t *testing.T) {
	s := &churnSection{revertCount: 3}
	s.churns = nil

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))

	out := buf.String()
	assert.Contains(t, out, "File churn requires full git history")
	assert.Contains(t, out, "--depth")
	assert.NotContains(t, out, "No file churn data available")
}

func TestChurn_StabilityLevel(t *testing.T) {
	assert.Equal(t, "stable", stabilityLevel(0))
	assert.Equal(t, "stable", stabilityLevel(9))
	assert.Equal(t, "moderate", stabilityLevel(10))
	assert.Equal(t, "moderate", stabilityLevel(19))
	assert.Equal(t, "unstable", stabilityLevel(20))
	assert.Equal(t, "unstable", stabilityLevel(100))
}

func TestChurn_Analyze_Reinitializes(t *testing.T) {
	s := &churnSection{}
	s.revertCount = 99
	s.staleBranches = 88
	s.churns = []collectors.FileChurn{{Path: "old"}}

	result := &signal.ScanResult{
		Metrics: map[string]any{
			"gitlog": &collectors.GitlogMetrics{
				FileChurns:       []collectors.FileChurn{{Path: "new", ChangeCount: 5}},
				RevertCount:      1,
				StaleBranchCount: 2,
			},
		},
	}
	require.NoError(t, s.Analyze(result))
	assert.Equal(t, 1, s.revertCount)
	assert.Equal(t, 2, s.staleBranches)
	assert.Len(t, s.churns, 1)
	assert.Equal(t, "new", s.churns[0].Path)
}
