package report

import (
	"bytes"
	"testing"
	"time"

	"github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecommendations_Registered(t *testing.T) {
	s := Get("recommendations")
	require.NotNil(t, s)
	assert.Equal(t, "recommendations", s.Name())
	assert.NotEmpty(t, s.Description())
}

func TestRecommendations_Empty(t *testing.T) {
	s := &recommendationsSection{}
	require.NoError(t, s.Analyze(&signal.ScanResult{Metrics: map[string]any{}}))

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))
	assert.Contains(t, buf.String(), "No actionable recommendations")
}

func TestRecommendations_LotteryRisk(t *testing.T) {
	s := &recommendationsSection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"lotteryrisk": &collectors.LotteryRiskMetrics{
				Directories: []collectors.DirectoryOwnership{
					{Path: "pkg/core", LotteryRisk: 1},
					{Path: "pkg/api", LotteryRisk: 3}, // safe, no recommendation
				},
			},
		},
	}

	require.NoError(t, s.Analyze(result))

	assert.Len(t, s.recs, 1)
	assert.Equal(t, SeverityHigh, s.recs[0].Severity)
	assert.Contains(t, s.recs[0].Message, "pkg/core")
	assert.Contains(t, s.recs[0].Message, "single contributor")
}

func TestRecommendations_Churn(t *testing.T) {
	s := &recommendationsSection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"gitlog": &collectors.GitlogMetrics{
				RevertCount: 3,
				FileChurns: []collectors.FileChurn{
					{Path: "hot.go", ChangeCount: 25},
					{Path: "stable.go", ChangeCount: 5}, // below threshold
				},
			},
		},
	}

	require.NoError(t, s.Analyze(result))

	assert.Len(t, s.recs, 2)

	// Revert recommendation.
	assert.Equal(t, SeverityMedium, s.recs[0].Severity)
	assert.Contains(t, s.recs[0].Message, "3 revert(s)")

	// High churn recommendation.
	assert.Equal(t, SeverityMedium, s.recs[1].Severity)
	assert.Contains(t, s.recs[1].Message, "hot.go")
}

func TestRecommendations_StaleTodos(t *testing.T) {
	now := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	s := &recommendationsSection{now: func() time.Time { return now }}

	result := &signal.ScanResult{
		Metrics: map[string]any{},
		Signals: []signal.RawSignal{
			{Source: "todos", Timestamp: now.Add(-400 * 24 * time.Hour)}, // stale
			{Source: "todos", Timestamp: now.Add(-30 * 24 * time.Hour)},  // fresh
			{Source: "todos"}, // no timestamp
		},
	}

	require.NoError(t, s.Analyze(result))

	require.Len(t, s.recs, 1)
	assert.Equal(t, SeverityLow, s.recs[0].Severity)
	assert.Contains(t, s.recs[0].Message, "1 TODO(s)")
}

func TestRecommendations_Coverage(t *testing.T) {
	s := &recommendationsSection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"patterns": &collectors.PatternsMetrics{
				DirectoryTestRatios: []collectors.DirectoryTestRatio{
					{Path: "pkg/core", SourceFiles: 10, TestFiles: 0, Ratio: 0},    // no tests
					{Path: "pkg/util", SourceFiles: 10, TestFiles: 1, Ratio: 0.05}, // very low
					{Path: "pkg/api", SourceFiles: 10, TestFiles: 8, Ratio: 0.8},   // good
				},
			},
		},
	}

	require.NoError(t, s.Analyze(result))

	require.Len(t, s.recs, 2)

	// Low coverage recommendation (medium severity).
	var lowCov *Recommendation
	var noTests *Recommendation
	for i := range s.recs {
		if s.recs[i].Severity == SeverityMedium {
			lowCov = &s.recs[i]
		}
		if s.recs[i].Severity == SeverityHigh {
			noTests = &s.recs[i]
		}
	}

	require.NotNil(t, lowCov)
	assert.Contains(t, lowCov.Message, "pkg/util")

	require.NotNil(t, noTests)
	assert.Contains(t, noTests.Message, "1 directory(ies)")
}

func TestRecommendations_SortBySeverity(t *testing.T) {
	s := &recommendationsSection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"lotteryrisk": &collectors.LotteryRiskMetrics{
				Directories: []collectors.DirectoryOwnership{
					{Path: "pkg/core", LotteryRisk: 1}, // high
				},
			},
			"gitlog": &collectors.GitlogMetrics{
				RevertCount: 1, // medium
			},
		},
		Signals: []signal.RawSignal{},
	}

	require.NoError(t, s.Analyze(result))

	require.True(t, len(s.recs) >= 2)
	assert.Equal(t, SeverityHigh, s.recs[0].Severity)
	assert.Equal(t, SeverityMedium, s.recs[1].Severity)
}

func TestRecommendations_Render(t *testing.T) {
	s := &recommendationsSection{
		recs: []Recommendation{
			{Severity: SeverityHigh, Message: "Critical issue found."},
			{Severity: SeverityLow, Message: "Minor suggestion."},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))

	out := buf.String()
	assert.Contains(t, out, "Recommendations")
	assert.Contains(t, out, "Critical issue found.")
	assert.Contains(t, out, "Minor suggestion.")
}

func TestRecommendations_AnalyzeReinitializes(t *testing.T) {
	s := &recommendationsSection{
		recs: []Recommendation{{Severity: SeverityHigh, Message: "old"}},
	}

	require.NoError(t, s.Analyze(&signal.ScanResult{Metrics: map[string]any{}}))
	assert.Empty(t, s.recs)
}
