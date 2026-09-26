// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package pipeline

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

// gateCollector blocks in Collect until release is closed (or the context is
// cancelled), so tests can control completion order deterministically without
// sleeping.
type gateCollector struct {
	name    string
	release chan struct{}
	signals []signal.RawSignal
	err     error
}

func (g *gateCollector) Name() string { return g.name }

func (g *gateCollector) Collect(ctx context.Context, _ string, _ signal.CollectorOpts) ([]signal.RawSignal, error) {
	if g.release != nil {
		select {
		case <-g.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return g.signals, g.err
}

// waitFor receives from ch or fails the test after a generous deadline. The
// deadline only guards against a hang; on the happy path it never elapses.
func waitFor[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

// setHeartbeatInterval overrides HeartbeatInterval for the test and restores
// it on cleanup.
func setHeartbeatInterval(t *testing.T, d time.Duration) {
	t.Helper()
	prev := HeartbeatInterval
	HeartbeatInterval = d
	t.Cleanup(func() { HeartbeatInterval = prev })
}

func TestPipeline_ProgressReportsCompletionAsItHappens(t *testing.T) {
	slowRelease := make(chan struct{})
	slow := &gateCollector{name: "slow", release: slowRelease, signals: []signal.RawSignal{
		{Source: "slow", Kind: "todo", Title: "one", Confidence: 0.5},
		{Source: "slow", Kind: "todo", Title: "two", Confidence: 0.5},
	}}
	fast := &gateCollector{name: "fast", signals: []signal.RawSignal{
		{Source: "fast", Kind: "todo", Title: "x", Confidence: 0.5},
	}}
	failing := &gateCollector{name: "failing", err: errors.New("boom")}

	done := make(chan signal.CollectorResult, 3)
	// Input order is slow, fast, failing; completion order must differ.
	p := NewWithCollectors(signal.ScanConfig{RepoPath: t.TempDir()}, []collector.Collector{slow, fast, failing})
	p.SetProgress(Progress{OnCollectorDone: func(r signal.CollectorResult) { done <- r }})

	runDone := make(chan struct{})
	var result *signal.ScanResult
	var runErr error
	go func() {
		defer close(runDone)
		result, runErr = p.Run(context.Background())
	}()

	// The two unblocked collectors must be reported while slow is still
	// running, i.e. before Run returns.
	first := waitFor(t, done, "first completion")
	second := waitFor(t, done, "second completion")
	select {
	case <-runDone:
		t.Fatal("Run returned before the gated collector was released")
	default:
	}
	names := map[string]signal.CollectorResult{first.Collector: first, second.Collector: second}
	require.Contains(t, names, "fast")
	require.Contains(t, names, "failing")
	assert.Len(t, names["fast"].Signals, 1)
	assert.NoError(t, names["fast"].Err)
	assert.EqualError(t, names["failing"].Err, "boom")

	close(slowRelease)
	third := waitFor(t, done, "third completion")
	assert.Equal(t, "slow", third.Collector)
	assert.Len(t, third.Signals, 2)
	// The monotonic clock on Windows is coarse enough that a collector
	// released immediately can measure 0s, so only require a non-negative
	// duration here.
	assert.GreaterOrEqual(t, third.Duration, time.Duration(0))

	waitFor(t, runDone, "Run to return")
	require.NoError(t, runErr)
	// Results stay in input order regardless of completion order.
	require.Len(t, result.Results, 3)
	assert.Equal(t, []string{"slow", "fast", "failing"},
		[]string{result.Results[0].Collector, result.Results[1].Collector, result.Results[2].Collector})
	assert.Len(t, result.Signals, 3)
}

func TestPipeline_HeartbeatListsOnlyRunningCollectors(t *testing.T) {
	setHeartbeatInterval(t, 2*time.Millisecond)

	slowRelease := make(chan struct{})
	slow := &gateCollector{name: "slow", release: slowRelease}
	fast := &gateCollector{name: "fast"}

	fastDone := make(chan struct{})
	heartbeats := make(chan []RunningCollector, 1)
	var once sync.Once

	p := NewWithCollectors(signal.ScanConfig{RepoPath: t.TempDir()}, []collector.Collector{slow, fast})
	p.SetProgress(Progress{
		OnCollectorDone: func(r signal.CollectorResult) {
			if r.Collector == "fast" {
				close(fastDone)
			}
		},
		OnHeartbeat: func(running []RunningCollector) {
			// Only capture heartbeats after fast has finished, so the
			// snapshot deterministically contains just the gated collector.
			select {
			case <-fastDone:
			default:
				return
			}
			once.Do(func() {
				snapshot := make([]RunningCollector, len(running))
				copy(snapshot, running)
				heartbeats <- snapshot
			})
		},
	})

	runDone := make(chan error, 1)
	go func() {
		_, err := p.Run(context.Background())
		runDone <- err
	}()

	waitFor(t, fastDone, "fast completion")
	hb := waitFor(t, heartbeats, "heartbeat")
	close(slowRelease)
	require.NoError(t, waitFor(t, runDone, "Run to return"))

	require.Len(t, hb, 1, "heartbeat should list only the unfinished collector")
	assert.Equal(t, "slow", hb[0].Name)
	assert.Greater(t, hb[0].Elapsed, time.Duration(0))
}

func TestPipeline_HeartbeatStopsWhenRunReturns(t *testing.T) {
	setHeartbeatInterval(t, 2*time.Millisecond)

	release := make(chan struct{})
	c := &gateCollector{name: "gated", release: release}

	var mu sync.Mutex
	beats := 0
	firstBeat := make(chan struct{})
	var once sync.Once

	p := NewWithCollectors(signal.ScanConfig{RepoPath: t.TempDir()}, []collector.Collector{c})
	p.SetProgress(Progress{OnHeartbeat: func(running []RunningCollector) {
		mu.Lock()
		beats++
		mu.Unlock()
		once.Do(func() { close(firstBeat) })
	}})

	runDone := make(chan error, 1)
	go func() {
		_, err := p.Run(context.Background())
		runDone <- err
	}()

	waitFor(t, firstBeat, "first heartbeat")
	close(release)
	require.NoError(t, waitFor(t, runDone, "Run to return"))

	// After Run returns the heartbeat goroutine has been stopped and joined,
	// so the count is stable: any further ticks would be a leak.
	mu.Lock()
	after := beats
	mu.Unlock()
	assert.GreaterOrEqual(t, after, 1)

	// Give the (stopped) ticker several intervals; the count must not move.
	// Run has already joined the goroutine, so this is a leak check rather
	// than a synchronization point.
	deadline := time.NewTimer(20 * time.Millisecond)
	defer deadline.Stop()
	<-deadline.C
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, after, beats, "heartbeat must not fire after Run returns")
}

func TestPipeline_HeartbeatDisabledByZeroInterval(t *testing.T) {
	setHeartbeatInterval(t, 0)

	c := &gateCollector{name: "quick"}
	called := false
	p := NewWithCollectors(signal.ScanConfig{RepoPath: t.TempDir()}, []collector.Collector{c})
	p.SetProgress(Progress{OnHeartbeat: func([]RunningCollector) { called = true }})

	_, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.False(t, called)
}

func TestPipeline_ProgressNilCallbacksIgnored(t *testing.T) {
	setHeartbeatInterval(t, time.Millisecond)

	c := &gateCollector{name: "quick", signals: []signal.RawSignal{
		{Source: "quick", Kind: "todo", Title: "x", Confidence: 0.5},
	}}
	p := NewWithCollectors(signal.ScanConfig{RepoPath: t.TempDir()}, []collector.Collector{c})
	p.SetProgress(Progress{})

	result, err := p.Run(context.Background())
	require.NoError(t, err)
	assert.Len(t, result.Signals, 1)
}

func TestPipeline_ProgressCompletionReportedOnFailMode(t *testing.T) {
	failing := &gateCollector{name: "failing", err: errors.New("fatal")}
	var reported []string
	p := NewWithCollectors(signal.ScanConfig{
		RepoPath:      t.TempDir(),
		CollectorOpts: map[string]signal.CollectorOpts{"failing": {ErrorMode: signal.ErrorModeFail}},
	}, []collector.Collector{failing})
	p.SetProgress(Progress{OnCollectorDone: func(r signal.CollectorResult) {
		reported = append(reported, r.Collector)
	}})

	_, err := p.Run(context.Background())
	require.Error(t, err)
	assert.Equal(t, []string{"failing"}, reported)
}
