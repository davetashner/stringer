// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/davetashner/stringer/internal/signal"
)

// defaultRegistryTimeout bounds a single dependency-registry lookup. It was
// a fixed 30s before stringer-ds4; a slow registry (Maven Central answering
// in ~25s per artifact) then cost minutes per manifest, so the default is
// now 10s and configurable via collectors.dephealth.registry_timeout.
const defaultRegistryTimeout = 10 * time.Second

// defaultRegistryConcurrency is the number of registry lookups run at once
// within one ecosystem (collectors.dephealth.registry_concurrency).
const defaultRegistryConcurrency = 8

// maxRegistryChecks caps the number of registry lookups per ecosystem per
// scan. The cap counts started lookups, so a cancelled or timed-out lookup
// still consumes a slot.
const maxRegistryChecks = 50

// registryRun carries the lookup settings for one Collect call and counts
// lookups that hit the registry timeout across every ecosystem.
type registryRun struct {
	timeout     time.Duration
	concurrency int
	timedOut    atomic.Int64
}

// newRegistryRun builds the lookup settings from collector options, applying
// the defaults for zero values and a floor of one worker.
func newRegistryRun(opts signal.CollectorOpts) *registryRun {
	r := &registryRun{timeout: opts.RegistryTimeout, concurrency: opts.RegistryConcurrency}
	if r.timeout <= 0 {
		r.timeout = defaultRegistryTimeout
	}
	if r.concurrency < 1 {
		r.concurrency = defaultRegistryConcurrency
	}
	return r
}

// httpClient returns an HTTP client bounded by the run's registry timeout.
func (r *registryRun) httpClient() *http.Client {
	return &http.Client{Timeout: r.timeout}
}

// registryHTTPClient returns c, or a client with the default registry timeout
// when an ecosystem client was constructed without one.
func registryHTTPClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: defaultRegistryTimeout}
}

// isTimeoutErr reports whether a lookup error was caused by a deadline —
// either the per-lookup context deadline or the HTTP client timeout.
func isTimeoutErr(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// lookupFailed records a failed registry lookup. Timeouts are counted and
// logged at WARN so slow registries are visible without --verbose; every
// other failure (404, malformed response, cancellation) stays at DEBUG.
func (r *registryRun) lookupFailed(ecosystem, pkg string, err error) {
	if isTimeoutErr(err) {
		r.timedOut.Add(1)
		slog.Warn("dephealth: registry lookup timed out", "ecosystem", ecosystem, "package", pkg, "timeout", r.timeout)
		return
	}
	slog.Debug("dephealth: registry lookup failed", "ecosystem", ecosystem, "package", pkg, "error", err)
}

// lookupEach runs fn over the first maxRegistryChecks items with up to
// r.concurrency lookups in flight, and returns the signals in input order.
// Each lookup gets its own context deadline of r.timeout. Once ctx is
// cancelled no further lookups are started; in-flight ones are cancelled
// through their derived contexts and their results are discarded by fn.
func lookupEach[T any](ctx context.Context, r *registryRun, ecosystem string, items []T, fn func(context.Context, T) []signal.RawSignal) []signal.RawSignal {
	if len(items) > maxRegistryChecks {
		slog.Info("dephealth: reached registry check cap", "ecosystem", ecosystem, "cap", maxRegistryChecks, "total", len(items))
		items = items[:maxRegistryChecks]
	}

	results := make([][]signal.RawSignal, len(items))
	sem := make(chan struct{}, r.concurrency)
	var wg sync.WaitGroup

loop:
	for i, item := range items {
		if ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			// Stop starting lookups; in-flight ones observe the same ctx.
			break loop
		case sem <- struct{}{}:
			// A worker that observed cancellation frees its slot after the
			// cancel, so re-check before starting the next lookup.
			if ctx.Err() != nil {
				<-sem
				break loop
			}
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			lctx, cancel := context.WithTimeout(ctx, r.timeout)
			defer cancel()
			results[i] = fn(lctx, item)
		}()
	}
	wg.Wait()

	var signals []signal.RawSignal
	for _, s := range results {
		signals = append(signals, s...)
	}
	return signals
}
