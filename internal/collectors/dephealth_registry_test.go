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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-github/v68/github"
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

// shortRetryBackoff shrinks the 429/5xx retry wait for the test's duration.
func shortRetryBackoff(t *testing.T) {
	t.Helper()
	old := registryRetryBackoff
	registryRetryBackoff = 5 * time.Millisecond
	t.Cleanup(func() { registryRetryBackoff = old })
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
	assert.Equal(t, 0, r.totals().TimedOut)
	assert.Empty(t, buf.String(), "non-timeout failures stay at debug level")

	r.lookupFailed("maven", "org.apache:kafka", context.DeadlineExceeded)
	assert.Equal(t, 1, r.totals().TimedOut)
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
	assert.Equal(t, 2, r.totals().TimedOut)
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
			_, err := (&realMavenRegistryClient{httpClient: hc, baseURL: srv.URL, metaURL: srv.URL}).FetchArtifact(ctx, "g", "a")
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

// statusServer answers each request with the next status in seq (repeating
// the last one), sets the given headers, and counts hits.
type statusServer struct {
	*httptest.Server
	hits    atomic.Int32
	seq     []int
	headers http.Header
	body    string
}

func newStatusServer(t *testing.T, body string, seq ...int) *statusServer {
	t.Helper()
	s := &statusServer{seq: seq, headers: http.Header{}, body: body}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(s.hits.Add(1)) - 1
		status := s.seq[min(n, len(s.seq)-1)]
		for k, v := range s.headers {
			w.Header()[k] = v
		}
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(s.body))
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func getRegistry(t *testing.T, ctx context.Context, hc *http.Client, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := doRegistryRequest(hc, req, "test registry", "pkg")
	if resp != nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}
	return resp, err
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	assert.Equal(t, time.Duration(0), parseRetryAfter("", now))
	assert.Equal(t, 3*time.Second, parseRetryAfter(" 3 ", now))
	assert.Equal(t, time.Duration(0), parseRetryAfter("-5", now), "negative delay is treated as absent")
	assert.Equal(t, 90*time.Second, parseRetryAfter(now.Add(90*time.Second).Format(http.TimeFormat), now))
	assert.Equal(t, time.Duration(0), parseRetryAfter(now.Add(-time.Minute).Format(http.TimeFormat), now), "a past date means retry now")
	assert.Equal(t, time.Duration(0), parseRetryAfter("soon", now))
}

func TestDoRegistryRequest_RateLimitedRetryAfterHonoured(t *testing.T) {
	shortRetryBackoff(t)
	srv := newStatusServer(t, `{}`, http.StatusTooManyRequests, http.StatusOK)
	srv.headers.Set("Retry-After", "1")

	start := time.Now()
	resp, err := getRegistry(t, context.Background(), &http.Client{Timeout: 5 * time.Second}, srv.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), srv.hits.Load(), "one retry")
	assert.GreaterOrEqual(t, time.Since(start), 900*time.Millisecond, "Retry-After wins over the default backoff")
}

func TestDoRegistryRequest_RetryAfterCappedAtClientTimeout(t *testing.T) {
	srv := newStatusServer(t, `{}`, http.StatusTooManyRequests, http.StatusOK)
	srv.headers.Set("Retry-After", "3600")

	start := time.Now()
	resp, err := getRegistry(t, context.Background(), &http.Client{Timeout: 50 * time.Millisecond}, srv.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), srv.hits.Load())
	assert.Less(t, time.Since(start), 2*time.Second, "an hour-long Retry-After is capped at registry_timeout")

	// A zero client timeout (no ecosystem client timeout) caps at the default.
	wait, retry := registryRetryWait(&http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"3600"}}}, 0)
	assert.True(t, retry)
	assert.Equal(t, defaultRegistryTimeout, wait)
}

func TestDoRegistryRequest_RateLimitedWithoutRetryAfter(t *testing.T) {
	shortRetryBackoff(t)
	hc := &http.Client{Timeout: 5 * time.Second}

	t.Run("retry succeeds", func(t *testing.T) {
		srv := newStatusServer(t, `{}`, http.StatusTooManyRequests, http.StatusOK)
		resp, err := getRegistry(t, context.Background(), hc, srv.URL)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, int32(2), srv.hits.Load())
	})

	t.Run("retry fails", func(t *testing.T) {
		srv := newStatusServer(t, `{}`, http.StatusTooManyRequests)
		_, err := getRegistry(t, context.Background(), hc, srv.URL)
		require.Error(t, err)
		assert.Equal(t, http.StatusTooManyRequests, registryStatus(err))
		assert.EqualError(t, err, "test registry returned 429 for pkg")
		assert.Equal(t, int32(2), srv.hits.Load(), "exactly one retry")
	})

	t.Run("no retry when the lookup deadline is nearer than the wait", func(t *testing.T) {
		srv := newStatusServer(t, `{}`, http.StatusTooManyRequests, http.StatusOK)
		srv.headers.Set("Retry-After", "2")
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := getRegistry(t, ctx, hc, srv.URL)
		require.Error(t, err)
		assert.Equal(t, http.StatusTooManyRequests, registryStatus(err), "reported as rate limited, not as a timeout")
		assert.Equal(t, int32(1), srv.hits.Load())
		assert.Less(t, time.Since(start), 400*time.Millisecond, "gives up immediately instead of waiting out the deadline")
	})

	t.Run("cancelled during the wait", func(t *testing.T) {
		srv := newStatusServer(t, `{}`, http.StatusTooManyRequests, http.StatusOK)
		srv.headers.Set("Retry-After", "2")
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(20 * time.Millisecond); cancel() }()
		_, err := getRegistry(t, ctx, hc, srv.URL)
		assert.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, int32(1), srv.hits.Load())
	})
}

func TestDoRegistryRequest_ServerErrorRetriedOnce(t *testing.T) {
	shortRetryBackoff(t)
	hc := &http.Client{Timeout: 5 * time.Second}

	srv := newStatusServer(t, `{}`, http.StatusServiceUnavailable, http.StatusOK)
	resp, err := getRegistry(t, context.Background(), hc, srv.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), srv.hits.Load())

	srv = newStatusServer(t, `{}`, http.StatusBadGateway)
	_, err = getRegistry(t, context.Background(), hc, srv.URL)
	require.Error(t, err)
	assert.Equal(t, http.StatusBadGateway, registryStatus(err))
	assert.Equal(t, int32(2), srv.hits.Load(), "a persistent 5xx is retried once, never more")
}

func TestDoRegistryRequest_ClientErrorsNotRetried(t *testing.T) {
	shortRetryBackoff(t)
	hc := &http.Client{Timeout: 5 * time.Second}
	for _, status := range []int{http.StatusNotFound, http.StatusBadRequest, http.StatusForbidden} {
		srv := newStatusServer(t, `{}`, status, http.StatusOK)
		_, err := getRegistry(t, context.Background(), hc, srv.URL)
		require.Error(t, err)
		assert.Equal(t, status, registryStatus(err))
		assert.Equal(t, int32(1), srv.hits.Load(), "status %d must not be retried", status)
	}
	assert.Equal(t, 0, registryStatus(errors.New("plain")))
	assert.Equal(t, 0, registryStatus(nil))
}

func TestClassifyLookupErr(t *testing.T) {
	status := func(code int) error {
		return fmt.Errorf("wrapped: %w", &registryStatusError{Registry: "r", Status: code, Subject: "p"})
	}
	tests := map[string]struct {
		err  error
		want string
	}{
		"timeout":             {context.DeadlineExceeded, lookupTimedOut},
		"cancelled":           {context.Canceled, lookupCancelled},
		"429":                 {status(429), lookupRateLimited},
		"404":                 {status(404), lookupNotFound},
		"500":                 {status(500), lookupServerError},
		"503":                 {status(503), lookupServerError},
		"403":                 {status(403), lookupOther},
		"malformed":           {errors.New("decoding npm response: unexpected EOF"), lookupOther},
		"github rate limit":   {&github.RateLimitError{}, lookupRateLimited},
		"github abuse limit":  {&github.AbuseRateLimitError{}, lookupRateLimited},
		"github not found":    {&github.ErrorResponse{Response: &http.Response{StatusCode: 404}}, lookupNotFound},
		"github server error": {&github.ErrorResponse{Response: &http.Response{StatusCode: 502}}, lookupServerError},
		"github nil response": {&github.ErrorResponse{}, lookupOther},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tt.want, classifyLookupErr(tt.err))
		})
	}
}

func TestLookupFailed_CountsByClass(t *testing.T) {
	buf := captureWarnLogs(t)
	r := testRun()
	status := func(code int) error { return &registryStatusError{Registry: "r", Status: code, Subject: "p"} }

	r.lookupFailed("npm", "a", status(429))
	r.lookupFailed("npm", "b", status(429))
	r.lookupFailed("npm", "c", status(503))
	r.lookupFailed("npm", "d", status(404))
	r.lookupFailed("npm", "e", errors.New("decoding"))
	r.lookupFailed("npm", "f", context.Canceled)
	r.lookupFailed("maven", "g", status(429))

	assert.Equal(t, RegistryFailures{RateLimited: 2, ServerError: 1, NotFound: 1, Other: 1}, r.snapshot("npm"))
	assert.Equal(t, RegistryFailures{RateLimited: 1}, r.snapshot("maven"))
	assert.Equal(t, 4, r.snapshot("npm").Failed(), "404 and cancellation are not failures")
	assert.Equal(t, 5, r.totals().Failed())
	assert.Empty(t, buf.String(), "non-timeout failures are not logged per package")

	assert.Nil(t, testRun().byEcosystem())
	assert.Equal(t, []any{"rate_limited", 2, "server_error", 1, "other", 1}, r.snapshot("npm").attrs())
}

func TestLookupEach_SummaryWarnPerEcosystem(t *testing.T) {
	shortRetryBackoff(t)
	buf := captureWarnLogs(t)
	srv := newStatusServer(t, `{}`, http.StatusTooManyRequests)
	r := newRegistryRun(signal.CollectorOpts{RegistryTimeout: 5 * time.Second})
	client := &realNpmRegistryClient{httpClient: r.httpClient(), baseURL: srv.URL}
	deps := []PackageQuery{{Name: "a"}, {Name: "b"}, {Name: "c"}}

	assert.Empty(t, r.checkNpmDeps(context.Background(), client, deps, "package.json"))

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "level=WARN"), "one summary line, not one per package:\n%s", out)
	assert.Contains(t, out, `msg="dephealth: npm lookups failed; dependency findings are incomplete"`)
	assert.Contains(t, out, "failed=3 lookups=3 rate_limited=3")
	assert.Equal(t, int32(6), srv.hits.Load(), "each 429 retried once")
	assert.Equal(t, RegistryFailures{Lookups: 3, RateLimited: 3}, r.snapshot("npm"))

	// A healthy registry logs nothing at WARN and counts only lookups.
	buf.Reset()
	ok := newStatusServer(t, `{"name":"a"}`, http.StatusOK)
	client.baseURL = ok.URL
	assert.Empty(t, r.checkNpmDeps(context.Background(), client, deps[:1], "package.json"))
	assert.Empty(t, buf.String())
	assert.Equal(t, RegistryFailures{Lookups: 4, RateLimited: 3}, r.snapshot("npm"))

	// 404s stay silent.
	buf.Reset()
	nf := newStatusServer(t, ``, http.StatusNotFound)
	client.baseURL = nf.URL
	assert.Empty(t, r.checkNpmDeps(context.Background(), client, deps[:2], "package.json"))
	assert.Empty(t, buf.String(), "404 is normal for private packages")
	assert.Equal(t, RegistryFailures{Lookups: 6, RateLimited: 3, NotFound: 2}, r.snapshot("npm"))
}

func TestDepHealthCollector_FailedLookupMetrics(t *testing.T) {
	shortRetryBackoff(t)
	buf := captureWarnLogs(t)
	srv := newStatusServer(t, `{}`, http.StatusTooManyRequests)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"name":"app","dependencies":{"left-pad":"^1.0.0","is-odd":"^3.0.0"}}`), 0o600))

	c := &DepHealthCollector{npmClient: &realNpmRegistryClient{baseURL: srv.URL}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{RegistryTimeout: 5 * time.Second})
	require.NoError(t, err)
	assert.Empty(t, signals)

	m := c.Metrics().(*DepHealthMetrics)
	assert.Equal(t, 2, m.RateLimited)
	assert.Equal(t, 0, m.TimedOut)
	assert.Equal(t, 2, m.FailedLookups())
	assert.Equal(t, map[string]RegistryFailures{"npm": {Lookups: 2, RateLimited: 2}}, m.RegistryLookups)
	assert.Contains(t, buf.String(), "npm lookups failed")
	assert.NotContains(t, buf.String(), "timed out", "no timeout warning without timeouts")
}
