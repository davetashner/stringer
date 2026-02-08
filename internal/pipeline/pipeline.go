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
	"github.com/davetashner/stringer/internal/signal"
)

// Pipeline orchestrates the execution of collectors and aggregates results.
type Pipeline struct {
	config     signal.ScanConfig
	collectors []collector.Collector
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
		mu      sync.Mutex
		results = make([]signal.CollectorResult, len(p.collectors))
	)

	g, gctx := errgroup.WithContext(ctx)

	for i, c := range p.collectors {
		i, c := i, c // capture loop variables
		g.Go(func() error {
			result := p.runCollector(gctx, c)

			mu.Lock()
			results[i] = result
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
					log.Printf("collector %q returned error: %v", result.Collector, result.Err)
				}
			}
			return nil
		})
	}

	// Wait for all collectors to finish.
	if err := g.Wait(); err != nil {
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
					p.collectors[i].Name(), s.Title, errs)
				continue
			}
			allSignals = append(allSignals, s)
		}
	}

	// Deduplicate signals based on content hash.
	allSignals = DeduplicateSignals(allSignals)

	// Apply MaxIssues cap if configured.
	if p.config.MaxIssues > 0 && len(allSignals) > p.config.MaxIssues {
		allSignals = allSignals[:p.config.MaxIssues]
	}

	return &signal.ScanResult{
		Signals:  allSignals,
		Results:  results,
		Duration: time.Since(start),
	}, nil
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

	start := time.Now()

	signals, err := c.Collect(ctx, p.config.RepoPath, opts)

	return signal.CollectorResult{
		Collector: c.Name(),
		Signals:   signals,
		Duration:  time.Since(start),
		Err:       err,
	}
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
