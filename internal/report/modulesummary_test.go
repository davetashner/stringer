// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package report

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModuleSummary_Registration(t *testing.T) {
	s := &moduleSummarySection{}
	assert.Equal(t, "module-summary", s.Name())
	assert.NotEmpty(t, s.Description())
}

func TestModuleSummary_GroupsByDirectory(t *testing.T) {
	s := &moduleSummarySection{}
	result := &signal.ScanResult{
		Signals: []signal.RawSignal{
			{FilePath: "internal/auth/handler.go", Kind: "todo", Confidence: 0.5},
			{FilePath: "internal/auth/middleware.go", Kind: "fixme", Confidence: 0.7},
			{FilePath: "cmd/stringer/scan.go", Kind: "todo", Confidence: 0.3},
		},
	}

	err := s.Analyze(result)
	require.NoError(t, err)
	assert.Len(t, s.modules, 2)

	// Find the auth module.
	var auth, cmd *moduleSummary
	for i := range s.modules {
		switch s.modules[i].Module {
		case "internal/auth":
			auth = &s.modules[i]
		case "cmd/stringer":
			cmd = &s.modules[i]
		}
	}
	require.NotNil(t, auth, "should have internal/auth module")
	require.NotNil(t, cmd, "should have cmd/stringer module")
	assert.Equal(t, 2, auth.Total)
	assert.Equal(t, 1, cmd.Total)
}

func TestModuleSummary_RootFiles(t *testing.T) {
	s := &moduleSummarySection{}
	result := &signal.ScanResult{
		Signals: []signal.RawSignal{
			{FilePath: "go.mod", Kind: "todo", Confidence: 0.5},
			{FilePath: "README.md", Kind: "fixme", Confidence: 0.5},
			{FilePath: "", Kind: "todo", Confidence: 0.5},
		},
	}

	err := s.Analyze(result)
	require.NoError(t, err)
	require.Len(t, s.modules, 1)
	assert.Equal(t, "(root)", s.modules[0].Module)
	assert.Equal(t, 3, s.modules[0].Total)
}

func TestModuleSummary_HealthScoring(t *testing.T) {
	p1 := 1
	p4 := 4
	s := &moduleSummarySection{}
	result := &signal.ScanResult{
		Signals: []signal.RawSignal{
			{FilePath: "pkg/critical/a.go", Kind: "todo", Confidence: 0.9, Priority: &p1},
			{FilePath: "pkg/minor/b.go", Kind: "todo", Confidence: 0.2, Priority: &p4},
		},
	}

	err := s.Analyze(result)
	require.NoError(t, err)
	require.Len(t, s.modules, 2)

	// P1 signal → health 4, P4 signal → health 1. Worst first.
	assert.Equal(t, "pkg/critical", s.modules[0].Module)
	assert.Equal(t, 4, s.modules[0].HealthScore)
	assert.Equal(t, "pkg/minor", s.modules[1].Module)
	assert.Equal(t, 1, s.modules[1].HealthScore)
}

func TestModuleSummary_SortByHealth(t *testing.T) {
	s := &moduleSummarySection{}
	result := &signal.ScanResult{
		Signals: []signal.RawSignal{
			// Low health module.
			{FilePath: "pkg/low/a.go", Kind: "todo", Confidence: 0.2},
			// High health module (more P1s).
			{FilePath: "pkg/high/a.go", Kind: "fixme", Confidence: 0.9},
			{FilePath: "pkg/high/b.go", Kind: "fixme", Confidence: 0.9},
			{FilePath: "pkg/high/c.go", Kind: "todo", Confidence: 0.85},
		},
	}

	err := s.Analyze(result)
	require.NoError(t, err)
	require.Len(t, s.modules, 2)
	// pkg/high should be first (higher health score = worse).
	assert.Equal(t, "pkg/high", s.modules[0].Module)
	assert.Equal(t, "pkg/low", s.modules[1].Module)
	assert.Greater(t, s.modules[0].HealthScore, s.modules[1].HealthScore)
}

func TestModuleSummary_TopKinds(t *testing.T) {
	s := &moduleSummarySection{}
	result := &signal.ScanResult{
		Signals: []signal.RawSignal{
			{FilePath: "pkg/foo/a.go", Kind: "todo", Confidence: 0.5},
			{FilePath: "pkg/foo/b.go", Kind: "todo", Confidence: 0.5},
			{FilePath: "pkg/foo/c.go", Kind: "fixme", Confidence: 0.5},
			{FilePath: "pkg/foo/d.go", Kind: "fixme", Confidence: 0.5},
			{FilePath: "pkg/foo/e.go", Kind: "churn", Confidence: 0.5},
			{FilePath: "pkg/foo/f.go", Kind: "revert", Confidence: 0.5},
		},
	}

	err := s.Analyze(result)
	require.NoError(t, err)
	require.Len(t, s.modules, 1)
	// Top 3 kinds: fixme and todo (tied at 2), then churn or revert (tied at 1, alphabetical).
	kinds := s.modules[0].TopKinds
	require.Len(t, kinds, 3)
	assert.Contains(t, kinds, "todo")
	assert.Contains(t, kinds, "fixme")
}

func TestModuleSummary_EmptySignals(t *testing.T) {
	s := &moduleSummarySection{}
	result := &signal.ScanResult{
		Signals: nil,
	}

	err := s.Analyze(result)
	require.NoError(t, err)
	assert.Empty(t, s.modules)

	var buf bytes.Buffer
	err = s.Render(&buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "No signals to group.")
}

func TestModuleSummary_Render(t *testing.T) {
	s := &moduleSummarySection{}
	result := &signal.ScanResult{
		Signals: []signal.RawSignal{
			{FilePath: "internal/auth/handler.go", Kind: "todo", Confidence: 0.9},
			{FilePath: "internal/auth/middleware.go", Kind: "fixme", Confidence: 0.7},
			{FilePath: "cmd/stringer/scan.go", Kind: "todo", Confidence: 0.3},
		},
	}

	err := s.Analyze(result)
	require.NoError(t, err)

	var buf bytes.Buffer
	err = s.Render(&buf)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Module Health Summary")
	assert.Contains(t, out, "2 modules, 3 total signals")
	assert.Contains(t, out, "Module")
	assert.Contains(t, out, "Signals")
	assert.Contains(t, out, "Health")
	assert.Contains(t, out, "Top Kinds")
	assert.Contains(t, out, "internal/auth")
	assert.Contains(t, out, "cmd/stringer")
}

func TestModuleSummary_DepthExtraction(t *testing.T) {
	tests := []struct {
		path  string
		depth int
		want  string
	}{
		{"internal/auth/handler.go", 2, "internal/auth"},
		{"cmd/stringer/scan.go", 2, "cmd/stringer"},
		{"cmd/stringer/sub/deep.go", 2, "cmd/stringer"},
		{"pkg/a.go", 2, "pkg"},
		{"go.mod", 2, "(root)"},
		{"", 2, "(root)"},
		{"a/b/c/d.go", 1, "a"},
		{"a/b/c/d.go", 3, "a/b/c"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractModule(tt.path, tt.depth)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestModuleSummary_ConfidenceMapping(t *testing.T) {
	// Verify confidence→priority mapping without explicit Priority field.
	s := &moduleSummarySection{}
	result := &signal.ScanResult{
		Signals: []signal.RawSignal{
			{FilePath: "pkg/a/x.go", Kind: "todo", Confidence: 0.9},  // P1
			{FilePath: "pkg/a/y.go", Kind: "todo", Confidence: 0.65}, // P2
			{FilePath: "pkg/a/z.go", Kind: "todo", Confidence: 0.45}, // P3
			{FilePath: "pkg/a/w.go", Kind: "todo", Confidence: 0.2},  // P4
		},
	}

	err := s.Analyze(result)
	require.NoError(t, err)
	require.Len(t, s.modules, 1)

	m := s.modules[0]
	assert.Equal(t, 1, m.P1)
	assert.Equal(t, 1, m.P2)
	assert.Equal(t, 1, m.P3)
	assert.Equal(t, 1, m.P4)
	// Health = 1*4 + 1*3 + 1*2 + 1*1 = 10.
	assert.Equal(t, 10, m.HealthScore)
}

func TestTopKinds(t *testing.T) {
	counts := map[string]int{
		"todo":  5,
		"fixme": 3,
		"churn": 1,
		"hack":  1,
	}

	got := topKinds(counts, 3)
	assert.Equal(t, []string{"todo", "fixme", "churn"}, got)
}

func TestTopKinds_Empty(t *testing.T) {
	got := topKinds(nil, 3)
	assert.Nil(t, got)
}

func TestTopKinds_FewerThanN(t *testing.T) {
	counts := map[string]int{"todo": 1}
	got := topKinds(counts, 3)
	assert.Equal(t, []string{"todo"}, got)
}

func TestModuleSummary_Cap20(t *testing.T) {
	s := &moduleSummarySection{}
	signals := make([]signal.RawSignal, 0, 25)
	for i := 0; i < 25; i++ {
		signals = append(signals, signal.RawSignal{
			FilePath:   strings.Join([]string{"pkg", fmt.Sprintf("mod%02d", i), "a.go"}, "/"),
			Kind:       "todo",
			Confidence: 0.5,
		})
	}

	result := &signal.ScanResult{Signals: signals}
	err := s.Analyze(result)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(s.modules), 20)
}
