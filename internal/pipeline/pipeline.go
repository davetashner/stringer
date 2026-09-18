// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package pipeline

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/redact"
	"github.com/davetashner/stringer/internal/signal"
)

// HeartbeatInterval controls how often Run reports collectors that are still
// running via Progress.OnHeartbeat. It is a package-level variable so tests can
// shorten it; a value <= 0 disables the heartbeat.
var HeartbeatInterval = 60 * time.Second

// RunningCollector describes a collector that has not yet finished, as
// reported by Progress.OnHeartbeat.
type RunningCollector struct {
	// Name is the collector name.
	Name string
	// Elapsed is how long the collector has been running so far.
	Elapsed time.Duration
}

// Progress holds optional callbacks that Run invokes to report progress while
// collectors execute. The pipeline itself carries no logging policy; callers
// (e.g. the scan and report commands) decide how to surface these events.
//
// Callbacks are invoked serially — never concurrently with each other — and
// should return quickly, since they hold up result bookkeeping.
type Progress struct {
	// OnCollectorDone is invoked the moment a collector finishes, before the
	// remaining collectors complete. The result includes name, signals,
	// duration and any error.
	OnCollectorDone func(result signal.CollectorResult)

	// OnHeartbeat is invoked every HeartbeatInterval while at least one
	// collector is still running, listing the unfinished collectors and how
	// long each has been running.
	OnHeartbeat func(running []RunningCollector)
}

// Pipeline orchestrates the execution of collectors and aggregates results.
type Pipeline struct {
	config     signal.ScanConfig
	collectors []collector.Collector
	progress   Progress
}

// New creates a Pipeline from the given ScanConfig. It resolves collectors
// from the global registry. If config.Collectors is empty, all registered
// collectors are used (sorted by name for deterministic ordering).
// Returns an error if a requested collector is not found in the registry.
func New(config signal.ScanConfig) (*Pipeline, error) {
	collectors, err := resolveCollectors(config.Collectors)
	if err != nil {
		return nil, err
	}
	return &Pipeline{
		config:     config,
		collectors: collectors,
	}, nil
}

// NewWithCollectors creates a Pipeline with explicitly provided collectors,
// bypassing the global registry. This is primarily useful for testing.
func NewWithCollectors(config signal.ScanConfig, collectors []collector.Collector) *Pipeline {
	return &Pipeline{
		config:     config,
		collectors: collectors,
	}
}

// SetProgress installs progress callbacks that Run invokes as collectors
// finish and while long-running collectors are still in flight. Nil callbacks
// are ignored. Must be called before Run.
func (p *Pipeline) SetProgress(pr Progress) {
	p.progress = pr
}

// Run executes all configured collectors in parallel, validates their output,
// deduplicates signals, and returns the aggregated ScanResult. Each collector
// runs in its own goroutine using errgroup with context cancellation. Results
// are collected with proper synchronization and returned in deterministic order
// matching the input collector list.
//
// Error handling is controlled per-collector via ErrorMode in CollectorOpts:
//   - Skip: errors are silently ignored
//   - Warn: errors are logged, pipeline continues (default)
//   - Fail: first error aborts the entire scan
//
// Signals are deduplicated via content-based hashing (Source + Kind + FilePath +
// Line + Title). When duplicates are found, the first occurrence is kept and its
// confidence is updated if a later duplicate has a higher value.
//
// Invalid signals are logged and skipped.
func (p *Pipeline) Run(ctx context.Context) (*signal.ScanResult, error) {
	start := time.Now()

	if len(p.collectors) == 0 {
		return &signal.ScanResult{
			Signals:  nil,
			Results:  nil,
			Duration: time.Since(start),
		}, nil
	}

	var (
		mu       sync.Mutex
		results  = make([]signal.CollectorResult, len(p.collectors))
		started  = make([]time.Time, len(p.collectors))
		finished = make([]bool, len(p.collectors))
	)

	g, gctx := errgroup.WithContext(ctx)

	for i, c := range p.collectors {
		i, c := i, c // capture loop variables
		started[i] = time.Now()
		g.Go(func() error {
			result := p.runCollector(gctx, c)

			mu.Lock()
			results[i] = result
			finished[i] = true
			if p.progress.OnCollectorDone != nil {
				p.progress.OnCollectorDone(result)
			}
			mu.Unlock()

			if result.Err != nil {
				mode := p.errorMode(c.Name())
				switch mode {
				case signal.ErrorModeFail:
					return fmt.Errorf("collector %q failed: %w", c.Name(), result.Err)
				case signal.ErrorModeSkip:
					// Silently ignore.
				default:
					// ErrorModeWarn (default).
					log.Printf("collector %q returned error: %v", result.Collector, redact.String(result.Err.Error()))
				}
			}
			return nil
		})
	}

	// Report still-running collectors periodically until every goroutine
	// has finished, so a single slow collector is visible mid-run.
	stopHeartbeat := p.startHeartbeat(&mu, started, finished)

	// Wait for all collectors to finish.
	err := g.Wait()
	stopHeartbeat()
	if err != nil {
		return &signal.ScanResult{
			Results:  results,
			Duration: time.Since(start),
		}, err
	}

	// Collect valid signals from all results in deterministic order.
	var allSignals []signal.RawSignal
	for i, result := range results {
		if result.Err != nil {
			continue
		}
		for _, s := range result.Signals {
			errs := ValidateSignal(s)
			if len(errs) > 0 {
				log.Printf("skipping invalid signal from %q (title=%q): %v",
					p.collectors[i].Name(), redact.String(s.Title), errs)
				continue
			}
			allSignals = append(allSignals, s)
		}
	}

	// Deduplicate signals based on content hash.
	allSignals = DeduplicateSignals(allSignals)

	// Apply MaxIssues cap if configured.
	// Sort by priority first so the most actionable signals survive truncation.
	if p.config.MaxIssues > 0 && len(allSignals) > p.config.MaxIssues {
		sort.SliceStable(allSignals, func(i, j int) bool {
			pi := effectivePriority(allSignals[i])
			pj := effectivePriority(allSignals[j])
			if pi != pj {
				return pi < pj // P1 < P2 < P3 < P4
			}
			return allSignals[i].Confidence > allSignals[j].Confidence
		})
		allSignals = allSignals[:p.config.MaxIssues]
	}

	// Build aggregated metrics map from collector results.
	metrics := make(map[string]any)
	for _, result := range results {
		if result.Metrics != nil {
			metrics[result.Collector] = result.Metrics
		}
	}

	return &signal.ScanResult{
		Signals:  allSignals,
		Results:  results,
		Duration: time.Since(start),
		Metrics:  metrics,
	}, nil
}

// startHeartbeat launches a goroutine that invokes Progress.OnHeartbeat every
// HeartbeatInterval with the collectors that have not yet finished. It returns
// a stop function that halts the goroutine and waits for it to exit. When no
// heartbeat callback is set (or the interval is disabled) it is a no-op.
//
// started must be fully populated before this is called; finished is read
// under mu, which is also held while invoking the callback so that heartbeat
// and completion callbacks never run concurrently.
func (p *Pipeline) startHeartbeat(mu *sync.Mutex, started []time.Time, finished []bool) func() {
	if p.progress.OnHeartbeat == nil || HeartbeatInterval <= 0 {
		return func() {}
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	ticker := time.NewTicker(HeartbeatInterval)

	go func() {
		defer close(done)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-ticker.C:
				mu.Lock()
				var running []RunningCollector
				for i, c := range p.collectors {
					if !finished[i] {
						running = append(running, RunningCollector{
							Name:    c.Name(),
							Elapsed: now.Sub(started[i]),
						})
					}
				}
				if len(running) > 0 {
					p.progress.OnHeartbeat(running)
				}
				mu.Unlock()
			}
		}
	}()

	return func() {
		close(stop)
		<-done
	}
}

// effectivePriority returns the signal's priority for sorting.
// Uses the LLM-inferred priority if set, otherwise maps confidence to P1-P4.
func effectivePriority(s signal.RawSignal) int {
	if s.Priority != nil {
		return *s.Priority
	}
	switch {
	case s.Confidence >= 0.8:
		return 1
	case s.Confidence >= 0.6:
		return 2
	case s.Confidence >= 0.4:
		return 3
	default:
		return 4
	}
}

// errorMode returns the ErrorMode for a given collector, defaulting to Warn.
func (p *Pipeline) errorMode(collectorName string) signal.ErrorMode {
	if opts, ok := p.config.CollectorOpts[collectorName]; ok && opts.ErrorMode != "" {
		return opts.ErrorMode
	}
	return signal.ErrorModeWarn
}

// runCollector executes a single collector and captures its result and timing.
func (p *Pipeline) runCollector(ctx context.Context, c collector.Collector) signal.CollectorResult {
	opts := p.config.CollectorOpts[c.Name()]

	// Prepend global exclude patterns so they apply to every collector.
	if len(p.config.ExcludePatterns) > 0 {
		opts.ExcludePatterns = append(p.config.ExcludePatterns, opts.ExcludePatterns...)
	}

	// Apply per-collector timeout if configured.
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	start := time.Now()

	signals, err := c.Collect(ctx, p.config.RepoPath, opts)

	result := signal.CollectorResult{
		Collector: c.Name(),
		Signals:   signals,
		Duration:  time.Since(start),
		Err:       err,
	}

	// If the collector provides metrics and collection succeeded, capture them.
	if err == nil {
		if mp, ok := c.(collector.MetricsProvider); ok {
			result.Metrics = mp.Metrics()
		}
	}

	return result
}

// resolveCollectors looks up collectors by name from the global registry.
// If names is empty, all registered collectors are returned in sorted order.
func resolveCollectors(names []string) ([]collector.Collector, error) {
	if len(names) == 0 {
		allNames := collector.List()
		sort.Strings(allNames)
		collectors := make([]collector.Collector, len(allNames))
		for i, name := range allNames {
			collectors[i] = collector.Get(name)
		}
		return collectors, nil
	}

	collectors := make([]collector.Collector, len(names))
	for i, name := range names {
		c := collector.Get(name)
		if c == nil {
			return nil, fmt.Errorf("unknown collector: %q", name)
		}
		collectors[i] = c
	}
	return collectors, nil
}
