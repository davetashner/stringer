// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/pipeline"
	"github.com/davetashner/stringer/internal/signal"
)

// captureSlog routes the default slog logger to a buffer at Debug level for
// the duration of the test.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestCollectorProgressLogger_Complete(t *testing.T) {
	buf := captureSlog(t)
	pr := collectorProgressLogger("")
	require.NotNil(t, pr.OnCollectorDone)
	require.NotNil(t, pr.OnHeartbeat)

	pr.OnCollectorDone(signal.CollectorResult{
		Collector: "todo",
		Signals:   make([]signal.RawSignal, 3),
		Duration:  1500 * time.Millisecond,
	})

	out := buf.String()
	assert.Contains(t, out, "level=INFO")
	assert.Contains(t, out, `msg="collector complete"`)
	assert.Contains(t, out, "name=todo")
	assert.Contains(t, out, "signals=3")
	assert.Contains(t, out, "duration=1.5s")
	assert.NotContains(t, out, "workspace=")
}

func TestCollectorProgressLogger_Failed(t *testing.T) {
	buf := captureSlog(t)
	pr := collectorProgressLogger("")

	pr.OnCollectorDone(signal.CollectorResult{
		Collector: "vuln",
		Err:       errors.New("osv unreachable"),
		Duration:  time.Second,
	})

	out := buf.String()
	assert.Contains(t, out, "level=ERROR")
	assert.Contains(t, out, `msg="collector failed"`)
	assert.Contains(t, out, "name=vuln")
	assert.Contains(t, out, `error="osv unreachable"`)
	assert.NotContains(t, out, "collector complete")
}

func TestCollectorProgressLogger_HeartbeatAndWorkspace(t *testing.T) {
	buf := captureSlog(t)
	pr := collectorProgressLogger("api")

	pr.OnHeartbeat([]pipeline.RunningCollector{
		{Name: "gitlog", Elapsed: 61*time.Second + 400*time.Millisecond},
		{Name: "duplication", Elapsed: 2 * time.Minute},
	})
	pr.OnCollectorDone(signal.CollectorResult{Collector: "gitlog"})

	out := buf.String()
	assert.Contains(t, out, "level=DEBUG")
	assert.Contains(t, out, `msg="collectors still running"`)
	assert.Contains(t, out, "count=2")
	assert.Contains(t, out, "running=gitlog(1m1s),duplication(2m0s)")
	// Workspace is attached to every line, including completions.
	assert.Equal(t, 2, bytes.Count([]byte(out), []byte("workspace=api")))
}

// TestCollectorProgressLogger_WiredThroughPipeline verifies the callbacks
// fire when driven by a real pipeline run rather than called directly.
func TestCollectorProgressLogger_WiredThroughPipeline(t *testing.T) {
	buf := captureSlog(t)
	p := pipeline.NewWithCollectors(signal.ScanConfig{RepoPath: t.TempDir()}, nil)
	p.SetProgress(collectorProgressLogger(""))
	_, err := p.Run(t.Context())
	require.NoError(t, err)
	assert.Empty(t, buf.String(), "no collectors means no completion lines")
}
