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

func TestGitHygiene_Registered(t *testing.T) {
	s := Get("git-hygiene")
	require.NotNil(t, s)
	assert.Equal(t, "git-hygiene", s.Name())
	assert.NotEmpty(t, s.Description())
}

func TestGitHygiene_Analyze_MissingMetrics(t *testing.T) {
	s := &gitHygieneSection{}
	err := s.Analyze(&signal.ScanResult{Metrics: map[string]any{}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestGitHygiene_Analyze_NilMetrics(t *testing.T) {
	s := &gitHygieneSection{}
	err := s.Analyze(&signal.ScanResult{
		Metrics: map[string]any{"githygiene": (*collectors.GitHygieneMetrics)(nil)},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestGitHygiene_AnalyzeAndRender(t *testing.T) {
	s := &gitHygieneSection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"githygiene": &collectors.GitHygieneMetrics{
				FilesScanned:         100,
				LargeBinaries:        2,
				MergeConflictMarkers: 1,
				CommittedSecrets:     3,
				MixedLineEndings:     0,
			},
		},
	}

	require.NoError(t, s.Analyze(result))

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))

	out := buf.String()
	assert.Contains(t, out, "Git Hygiene")
	assert.Contains(t, out, "Large binaries")
	assert.Contains(t, out, "Merge conflict markers")
	assert.Contains(t, out, "Committed secrets")
	assert.Contains(t, out, "Mixed line endings")
	assert.Contains(t, out, "6 issues found")
}

func TestGitHygiene_Render_NoIssues(t *testing.T) {
	s := &gitHygieneSection{
		metrics: &collectors.GitHygieneMetrics{
			FilesScanned: 50,
		},
	}

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))
	assert.Contains(t, buf.String(), "No git hygiene issues detected")
	assert.Contains(t, buf.String(), "50 files scanned")
}

func TestGitHygiene_Render_NilMetrics(t *testing.T) {
	s := &gitHygieneSection{}

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))
	assert.Contains(t, buf.String(), "No git hygiene data available")
}

func TestColorHygieneCount(t *testing.T) {
	zero := colorHygieneCount("0")
	assert.Contains(t, zero, "0")

	nonzero := colorHygieneCount("5")
	assert.Contains(t, nonzero, "5")
}
