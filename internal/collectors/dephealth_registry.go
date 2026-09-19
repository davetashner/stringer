// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
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

// RegistryFailures counts registry lookups and their failures for one
// ecosystem (or, summed, for a whole Collect call). NotFound is not a
// failure: a 404 is normal for private or unpublished packages.
type RegistryFailures struct {
	Lookups     int // lookups started (after the per-ecosystem cap)
	TimedOut    int // hit collectors.dephealth.registry_timeout
	RateLimited int // HTTP 429 (after one retry)
	ServerError int // HTTP 5xx (after one retry)
	NotFound    int // HTTP 404
	Other       int // any other error: 4xx, malformed response, DNS, TLS
}

// Failed returns the number of lookups whose result is unknown, so findings
// for the ecosystem are incomplete.
func (f RegistryFailures) Failed() int {
	return f.TimedOut + f.RateLimited + f.ServerError + f.Other
}

func (f RegistryFailures) add(o RegistryFailures) RegistryFailures {
	return RegistryFailures{
		Lookups: f.Lookups + o.Lookups, TimedOut: f.TimedOut + o.TimedOut, RateLimited: f.RateLimited + o.RateLimited,
		ServerError: f.ServerError + o.ServerError, NotFound: f.NotFound + o.NotFound, Other: f.Other + o.Other,
	}
}

func (f RegistryFailures) sub(o RegistryFailures) RegistryFailures {
	return RegistryFailures{
		Lookups: f.Lookups - o.Lookups, TimedOut: f.TimedOut - o.TimedOut, RateLimited: f.RateLimited - o.RateLimited,
		ServerError: f.ServerError - o.ServerError, NotFound: f.NotFound - o.NotFound, Other: f.Other - o.Other,
	}
}

// attrs returns slog attributes for the non-zero failure classes, in the
// order they are most likely to explain missing findings.
func (f RegistryFailures) attrs() []any {
	var out []any
	for _, kv := range []struct {
		k string
		v int
	}{{lookupRateLimited, f.RateLimited}, {lookupTimedOut, f.TimedOut}, {lookupServerError, f.ServerError}, {lookupOther, f.Other}} {
		if kv.v > 0 {
			out = append(out, kv.k, kv.v)
		}
	}
	return out
}

// Lookup failure classes, as counted by lookupFailed and named in logs.
const (
	lookupTimedOut    = "timed_out"
	lookupRateLimited = "rate_limited"
	lookupServerError = "server_error"
	lookupNotFound    = "not_found"
	lookupOther       = "other"
	lookupCancelled   = "" // the scan was cancelled; not a registry failure
)

// registryRun carries the lookup settings for one Collect call and counts
// lookups and their failures per ecosystem.
type registryRun struct {
	timeout     time.Duration
	concurrency int

	mu       sync.Mutex
	failures map[string]RegistryFailures // by ecosystem
}

// count adds delta to an ecosystem's counters.
func (r *registryRun) count(ecosystem string, delta RegistryFailures) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failures == nil {
		r.failures = make(map[string]RegistryFailures)
	}
	r.failures[ecosystem] = r.failures[ecosystem].add(delta)
}

// snapshot returns an ecosystem's counters so far.
func (r *registryRun) snapshot(ecosystem string) RegistryFailures {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failures[ecosystem]
}

// byEcosystem returns a copy of every ecosystem's counters, or nil when no
// lookup ran.
func (r *registryRun) byEcosystem() map[string]RegistryFailures {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.failures) == 0 {
		return nil
	}
	out := make(map[string]RegistryFailures, len(r.failures))
	for k, v := range r.failures {
		out[k] = v
	}
	return out
}

// totals sums the counters across ecosystems.
func (r *registryRun) totals() RegistryFailures {
	var t RegistryFailures
	for _, f := range r.byEcosystem() {
		t = t.add(f)
	}
	return t
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

// classifyLookupErr maps a lookup error to its failure class.
func classifyLookupErr(err error) string {
	if isTimeoutErr(err) {
		return lookupTimedOut
	}
	if errors.Is(err, context.Canceled) {
		return lookupCancelled
	}
	status := registryStatus(err)
	if status == 0 {
		status = githubLookupStatus(err)
	}
	switch {
	case status == http.StatusTooManyRequests:
		return lookupRateLimited
	case status == http.StatusNotFound:
		return lookupNotFound
	case status >= 500:
		return lookupServerError
	}
	return lookupOther
}

// lookupFailed records a failed registry lookup under its class. Timeouts
// are also logged per package at WARN (the package name tells the reader
// what raising registry_timeout would recover); everything else stays at
// DEBUG here and is summarised per ecosystem by lookupEach, so a registry
// answering 429 to every lookup produces one WARN line, not fifty.
func (r *registryRun) lookupFailed(ecosystem, pkg string, err error) {
	var delta RegistryFailures
	switch classifyLookupErr(err) {
	case lookupTimedOut:
		delta.TimedOut = 1
		r.count(ecosystem, delta)
		slog.Warn("dephealth: registry lookup timed out", "ecosystem", ecosystem, "package", pkg, "timeout", r.timeout)
		return
	case lookupRateLimited:
		delta.RateLimited = 1
	case lookupServerError:
		delta.ServerError = 1
	case lookupNotFound:
		delta.NotFound = 1
	case lookupOther:
		delta.Other = 1
	}
	r.count(ecosystem, delta)
	slog.Debug("dephealth: registry lookup failed", "ecosystem", ecosystem, "package", pkg, "error", err)
}

// warnFailures logs one WARN summarising an ecosystem's failed lookups, e.g.
// "dephealth: maven lookups failed; dependency findings are incomplete
// failed=38 lookups=48 rate_limited=37 timed_out=1".
func warnFailures(ecosystem string, f RegistryFailures) {
	if f.Failed() == 0 {
		return
	}
	attrs := append([]any{"failed", f.Failed(), "lookups", f.Lookups}, f.attrs()...)
	slog.Warn(fmt.Sprintf("dephealth: %s lookups failed; dependency findings are incomplete", ecosystem), attrs...)
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
	before := r.snapshot(ecosystem)
	started := 0

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
		started++
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
	r.count(ecosystem, RegistryFailures{Lookups: started})
	warnFailures(ecosystem, r.snapshot(ecosystem).sub(before))

	var signals []signal.RawSignal
	for _, s := range results {
		signals = append(signals, s...)
	}
	return signals
}
