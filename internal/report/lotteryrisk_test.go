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

func TestLotteryRisk_Registered(t *testing.T) {
	s := Get("lottery-risk")
	require.NotNil(t, s)
	assert.Equal(t, "lottery-risk", s.Name())
	assert.NotEmpty(t, s.Description())
}

func TestLotteryRisk_Analyze_MissingMetrics(t *testing.T) {
	s := &lotteryRiskSection{}
	err := s.Analyze(&signal.ScanResult{Metrics: map[string]any{}})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestLotteryRisk_Analyze_NilMetrics(t *testing.T) {
	s := &lotteryRiskSection{}
	err := s.Analyze(&signal.ScanResult{
		Metrics: map[string]any{"lotteryrisk": (*collectors.LotteryRiskMetrics)(nil)},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestLotteryRisk_Analyze_WrongType(t *testing.T) {
	s := &lotteryRiskSection{}
	err := s.Analyze(&signal.ScanResult{
		Metrics: map[string]any{"lotteryrisk": "not-a-metrics-struct"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMetricsNotAvailable))
}

func TestLotteryRisk_AnalyzeAndRender(t *testing.T) {
	s := &lotteryRiskSection{}
	result := &signal.ScanResult{
		Metrics: map[string]any{
			"lotteryrisk": &collectors.LotteryRiskMetrics{
				Directories: []collectors.DirectoryOwnership{
					{
						Path:        "pkg/api",
						LotteryRisk: 3,
						Authors: []collectors.AuthorShare{
							{Name: "Alice", Ownership: 0.4},
							{Name: "Bob", Ownership: 0.35},
							{Name: "Carol", Ownership: 0.25},
						},
						TotalLines: 500,
					},
					{
						Path:        "pkg/core",
						LotteryRisk: 1,
						Authors: []collectors.AuthorShare{
							{Name: "Dave", Ownership: 0.95},
							{Name: "Eve", Ownership: 0.05},
						},
						TotalLines: 1200,
					},
					{
						Path:        "pkg/util",
						LotteryRisk: 2,
						Authors: []collectors.AuthorShare{
							{Name: "Frank", Ownership: 0.6},
							{Name: "Grace", Ownership: 0.4},
						},
						TotalLines: 300,
					},
				},
			},
		},
	}

	require.NoError(t, s.Analyze(result))

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))

	out := buf.String()
	assert.Contains(t, out, "Lottery Risk")

	// Should be sorted by risk ascending (worst first).
	coreIdx := bytes.Index(buf.Bytes(), []byte("pkg/core"))
	utilIdx := bytes.Index(buf.Bytes(), []byte("pkg/util"))
	apiIdx := bytes.Index(buf.Bytes(), []byte("pkg/api"))
	assert.True(t, coreIdx < utilIdx, "CRITICAL (risk=1) should come before WARNING (risk=2)")
	assert.True(t, utilIdx < apiIdx, "WARNING (risk=2) should come before ok (risk=3)")

	// Check levels.
	assert.Contains(t, out, "CRITICAL")
	assert.Contains(t, out, "WARNING")
	assert.Contains(t, out, "ok")

	// Check contributors.
	assert.Contains(t, out, "Dave (95%)")
	assert.Contains(t, out, "Alice (40%)")
}

func TestLotteryRisk_Render_Empty(t *testing.T) {
	s := &lotteryRiskSection{}
	s.dirs = nil

	var buf bytes.Buffer
	require.NoError(t, s.Render(&buf))
	assert.Contains(t, buf.String(), "No directory ownership data")
}

func TestLotteryRisk_TopContributors(t *testing.T) {
	tests := []struct {
		name    string
		authors []collectors.AuthorShare
		n       int
		want    string
	}{
		{"empty", nil, 3, "-"},
		{"one", []collectors.AuthorShare{{Name: "A", Ownership: 1.0}}, 3, "A (100%)"},
		{"capped at n", []collectors.AuthorShare{
			{Name: "A", Ownership: 0.5},
			{Name: "B", Ownership: 0.3},
			{Name: "C", Ownership: 0.15},
			{Name: "D", Ownership: 0.05},
		}, 3, "A (50%), B (30%), C (15%)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, topContributors(tt.authors, tt.n))
		})
	}
}

func TestLotteryRisk_RiskLevel(t *testing.T) {
	assert.Equal(t, "CRITICAL", riskLevel(0))
	assert.Equal(t, "CRITICAL", riskLevel(1))
	assert.Equal(t, "WARNING", riskLevel(2))
	assert.Equal(t, "ok", riskLevel(3))
	assert.Equal(t, "ok", riskLevel(10))
}

func TestLotteryRisk_Analyze_Reinitializes(t *testing.T) {
	s := &lotteryRiskSection{}
	s.dirs = []collectors.DirectoryOwnership{{Path: "old"}}

	result := &signal.ScanResult{
		Metrics: map[string]any{
			"lotteryrisk": &collectors.LotteryRiskMetrics{
				Directories: []collectors.DirectoryOwnership{{Path: "new", LotteryRisk: 5}},
			},
		},
	}
	require.NoError(t, s.Analyze(result))
	assert.Len(t, s.dirs, 1)
	assert.Equal(t, "new", s.dirs[0].Path)
}
