// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

// captureWarnLogs swaps the default logger for one that records WARN and
// above into the returned buffer until the test ends.
func captureWarnLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// blockingNpmClient waits for ctx to expire, so every lookup ends in a
// timeout under a short registry_timeout.
type blockingNpmClient struct{}

func (blockingNpmClient) FetchPackage(ctx context.Context, _ string) (*npmPackageInfo, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestNewRegistryRun_Defaults(t *testing.T) {
	r := newRegistryRun(signal.CollectorOpts{})
	assert.Equal(t, defaultRegistryTimeout, r.timeout)
	assert.Equal(t, defaultRegistryConcurrency, r.concurrency)

	r = newRegistryRun(signal.CollectorOpts{RegistryTimeout: 5 * time.Second, RegistryConcurrency: 2})
	assert.Equal(t, 5*time.Second, r.timeout)
	assert.Equal(t, 2, r.concurrency)

	r = newRegistryRun(signal.CollectorOpts{RegistryTimeout: -1, RegistryConcurrency: -3})
	assert.Equal(t, defaultRegistryTimeout, r.timeout, "negative timeout falls back to the default")
	assert.Equal(t, defaultRegistryConcurrency, r.concurrency, "negative concurrency falls back to the default")

	assert.Equal(t, 5*time.Second, newRegistryRun(signal.CollectorOpts{RegistryTimeout: 5 * time.Second}).httpClient().Timeout)
}

func TestRegistryHTTPClient(t *testing.T) {
	assert.Equal(t, defaultRegistryTimeout, registryHTTPClient(nil).Timeout)
	custom := &http.Client{Timeout: time.Second}
	assert.Same(t, custom, registryHTTPClient(custom))
}

func TestIsTimeoutErr(t *testing.T) {
	assert.True(t, isTimeoutErr(context.DeadlineExceeded))
	assert.True(t, isTimeoutErr(fmt.Errorf("fetching: %w", context.DeadlineExceeded)))
	assert.True(t, isTimeoutErr(os.ErrDeadlineExceeded))
	assert.True(t, isTimeoutErr(&net.DNSError{IsTimeout: true}))
	assert.False(t, isTimeoutErr(&net.DNSError{IsTimeout: false}))
	assert.False(t, isTimeoutErr(context.Canceled))
	assert.False(t, isTimeoutErr(errors.New("404")))
	assert.False(t, isTimeoutErr(nil))
}

func TestLookupFailed_CountsAndWarnsOnlyForTimeouts(t *testing.T) {
	buf := captureWarnLogs(t)
	r := testRun()

	r.lookupFailed("npm", "left-pad", errors.New("registry returned 404"))
	assert.Equal(t, int64(0), r.timedOut.Load())
	assert.Empty(t, buf.String(), "non-timeout failures stay at debug level")

	r.lookupFailed("maven", "org.apache:kafka", context.DeadlineExceeded)
	assert.Equal(t, int64(1), r.timedOut.Load())
	out := buf.String()
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "timed out")
	assert.Contains(t, out, "ecosystem=maven")
	assert.Contains(t, out, "package=org.apache:kafka")
}

func TestLookupEach_PreservesOrderAndBoundsConcurrency(t *testing.T) {
	const workers = 3
	const n = 20
	r := newRegistryRun(signal.CollectorOpts{RegistryConcurrency: workers})

	started := make(chan struct{}, n) // one send per lookup that entered fn
	release := make(chan struct{})    // closed to let every lookup finish
	var inFlight, maxInFlight atomic.Int32

	items := make([]int, n)
	for i := range items {
		items[i] = i
	}

	done := make(chan []signal.RawSignal, 1)
	go func() {
		done <- lookupEach(context.Background(), r, "test", items, func(_ context.Context, i int) []signal.RawSignal {
			cur := inFlight.Add(1)
			for {
				prev := maxInFlight.Load()
				if cur <= prev || maxInFlight.CompareAndSwap(prev, cur) {
					break
				}
			}
			started <- struct{}{}
			<-release
			inFlight.Add(-1)
			return []signal.RawSignal{{Title: fmt.Sprintf("item-%d", i)}}
		})
	}()

	// Exactly `workers` lookups must be in flight before any is released:
	// the pool is parallel (three entered) and bounded (a fourth cannot).
	for range workers {
		<-started
	}
	assert.Equal(t, int32(workers), inFlight.Load())
	close(release)

	signals := <-done
	require.Len(t, signals, n)
	for i, s := range signals {
		assert.Equal(t, fmt.Sprintf("item-%d", i), s.Title, "results must follow input order")
	}
	assert.Equal(t, int32(workers), maxInFlight.Load(), "never more than registry_concurrency lookups at once")
}

func TestLookupEach_CapCountsStartedLookups(t *testing.T) {
	r := testRun()
	items := make([]int, maxRegistryChecks+25)
	for i := range items {
		items[i] = i
	}
	var calls atomic.Int32

	signals := lookupEach(context.Background(), r, "test", items, func(_ context.Context, i int) []signal.RawSignal {
		calls.Add(1)
		if i%2 == 0 {
			return nil // a failed or empty lookup still consumes a cap slot
		}
		return []signal.RawSignal{{Title: "hit"}}
	})

	assert.Equal(t, int32(maxRegistryChecks), calls.Load())
	assert.Len(t, signals, maxRegistryChecks/2)
}

func TestLookupEach_ContextCancelledStopsEarly(t *testing.T) {
	r := newRegistryRun(signal.CollectorOpts{RegistryConcurrency: 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	items := make([]int, 10)
	signals := lookupEach(ctx, r, "test", items, func(lctx context.Context, i int) []signal.RawSignal {
		calls.Add(1)
		cancel() // cancel mid-run: the parent is cancelled, so no further lookups start
		<-lctx.Done()
		assert.ErrorIs(t, lctx.Err(), context.Canceled)
		return []signal.RawSignal{{Title: fmt.Sprintf("item-%d", i)}}
	})

	assert.Equal(t, int32(1), calls.Load(), "no lookup starts after cancellation")
	assert.Len(t, signals, 1)

	// A context cancelled before the call starts nothing at all.
	pre, preCancel := context.WithCancel(context.Background())
	preCancel()
	calls.Store(0)
	assert.Empty(t, lookupEach(pre, r, "test", items, func(context.Context, int) []signal.RawSignal {
		calls.Add(1)
		return nil
	}))
	assert.Equal(t, int32(0), calls.Load())
}

func TestLookupEach_PerLookupDeadline(t *testing.T) {
	r := newRegistryRun(signal.CollectorOpts{RegistryTimeout: 50 * time.Millisecond})
	deadlines := make([]bool, 3)
	var mu sync.Mutex
	lookupEach(context.Background(), r, "test", []int{0, 1, 2}, func(lctx context.Context, i int) []signal.RawSignal {
		_, ok := lctx.Deadline()
		mu.Lock()
		deadlines[i] = ok
		mu.Unlock()
		return nil
	})
	assert.Equal(t, []bool{true, true, true}, deadlines, "every lookup gets its own deadline")
}

func TestCheckNpmDeps_TimeoutIsCountedAndWarned(t *testing.T) {
	buf := captureWarnLogs(t)
	r := newRegistryRun(signal.CollectorOpts{RegistryTimeout: 50 * time.Millisecond})
	deps := []PackageQuery{{Name: "slow-pkg", Version: "1.0.0"}, {Name: "slower-pkg", Version: "2.0.0"}}

	signals := r.checkNpmDeps(context.Background(), blockingNpmClient{}, deps, "package.json")

	assert.Empty(t, signals)
	assert.Equal(t, int64(2), r.timedOut.Load())
	assert.Contains(t, buf.String(), "package=slow-pkg")
	assert.Contains(t, buf.String(), "package=slower-pkg")
}

func TestDepHealthCollector_TimedOutMetricAndSummary(t *testing.T) {
	buf := captureWarnLogs(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"name":"app","dependencies":{"slow-pkg":"^1.0.0"}}`), 0o600))

	c := &DepHealthCollector{npmClient: blockingNpmClient{}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{RegistryTimeout: 50 * time.Millisecond})
	require.NoError(t, err)
	assert.Empty(t, signals)

	metrics := c.Metrics().(*DepHealthMetrics)
	assert.Equal(t, 1, metrics.TimedOut)
	assert.Contains(t, buf.String(), "timed_out=1")
	assert.Contains(t, buf.String(), "registry_timeout")
}

func TestRealClients_HonourRegistryTimeout(t *testing.T) {
	// The server never answers; each real client must give up at the
	// configured timeout rather than the old fixed 30s.
	hold := make(chan struct{})
	defer close(hold)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case <-hold:
		case <-req.Context().Done():
		}
	}))
	defer srv.Close()

	hc := &http.Client{Timeout: 30 * time.Millisecond}
	ctx := context.Background()
	clients := map[string]func() error{
		"maven": func() error {
			_, err := (&realMavenRegistryClient{httpClient: hc, baseURL: srv.URL}).FetchArtifact(ctx, "g", "a")
			return err
		},
		"proxy": func() error {
			_, err := (&realModuleProxyClient{httpClient: hc, baseURL: srv.URL}).FetchLatest(ctx, "example.com/m")
			return err
		},
		"npm": func() error {
			_, err := (&realNpmRegistryClient{httpClient: hc, baseURL: srv.URL}).FetchPackage(ctx, "p")
			return err
		},
		"crates": func() error {
			_, err := (&realCratesRegistryClient{httpClient: hc, baseURL: srv.URL}).FetchCrate(ctx, "c")
			return err
		},
		"nuget": func() error {
			_, err := (&realNuGetRegistryClient{httpClient: hc, baseURL: srv.URL}).FetchRegistration(ctx, "n")
			return err
		},
		"pypi": func() error {
			_, err := (&realPyPIRegistryClient{httpClient: hc, baseURL: srv.URL}).FetchPackage(ctx, "p")
			return err
		},
		"packagist": func() error {
			_, err := (&realPackagistRegistryClient{httpClient: hc, baseURL: srv.URL}).FetchPackage(ctx, "v/p")
			return err
		},
		"hex": func() error {
			_, err := (&realHexRegistryClient{httpClient: hc, baseURL: srv.URL}).FetchPackage(ctx, "h")
			return err
		},
	}
	for name, fetch := range clients {
		t.Run(name, func(t *testing.T) {
			start := time.Now()
			err := fetch()
			require.Error(t, err)
			assert.True(t, isTimeoutErr(err), "expected a timeout error, got %v", err)
			assert.Less(t, time.Since(start), 5*time.Second)
		})
	}
}
