// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package report

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

func TestRenderJSON_EmptyResult(t *testing.T) {
	result := &signal.ScanResult{
		Duration: 100 * time.Millisecond,
	}

	var buf bytes.Buffer
	err := RenderJSON(result, "/test/repo", nil, nil, &buf)
	require.NoError(t, err)

	var parsed ReportJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))
	assert.Equal(t, "/test/repo", parsed.Repository)
	assert.Equal(t, 0, parsed.Signals.Total)
}

func TestRenderJSON_WithSignals(t *testing.T) {
	result := &signal.ScanResult{
		Signals: []signal.RawSignal{
			{Source: "todos", Kind: "todo", Title: "Fix this"},
			{Source: "todos", Kind: "fixme", Title: "Fix that"},
		},
		Results: []signal.CollectorResult{
			{
				Collector: "todos",
				Signals:   []signal.RawSignal{{Kind: "todo"}, {Kind: "fixme"}},
				Duration:  50 * time.Millisecond,
			},
		},
		Duration: 100 * time.Millisecond,
	}

	var buf bytes.Buffer
	err := RenderJSON(result, "/test/repo", []string{"todos"}, nil, &buf)
	require.NoError(t, err)

	var parsed ReportJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))
	assert.Equal(t, 2, parsed.Signals.Total)
	assert.Equal(t, 1, parsed.Signals.ByKind["todo"])
	assert.Equal(t, 1, parsed.Signals.ByKind["fixme"])
	assert.Len(t, parsed.Collectors, 1)
	assert.Equal(t, "todos", parsed.Collectors[0].Name)
}

func TestResolveSections_EmptyFilter(t *testing.T) {
	resetForTesting()
	defer resetForTesting()

	Register(&renderStubSection{name: "alpha"})
	Register(&renderStubSection{name: "beta"})

	result := ResolveSections(nil)
	assert.Equal(t, []string{"alpha", "beta"}, result)
}

func TestResolveSections_FilterKnown(t *testing.T) {
	resetForTesting()
	defer resetForTesting()

	Register(&renderStubSection{name: "alpha"})
	Register(&renderStubSection{name: "beta"})

	result := ResolveSections([]string{"beta"})
	assert.Equal(t, []string{"beta"}, result)
}

func TestResolveSections_FilterUnknown(t *testing.T) {
	resetForTesting()
	defer resetForTesting()

	Register(&renderStubSection{name: "alpha"})

	result := ResolveSections([]string{"unknown"})
	assert.Empty(t, result)
}

// renderStubSection is a minimal Section implementation for testing.
type renderStubSection struct {
	name string
}

func (s *renderStubSection) Name() string                       { return s.name }
func (s *renderStubSection) Description() string                { return "test section" }
func (s *renderStubSection) Analyze(_ *signal.ScanResult) error { return nil }
func (s *renderStubSection) Render(_ io.Writer) error           { return nil }
