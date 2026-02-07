package pipeline

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

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

// Run executes all configured collectors sequentially, validates their output,
// and returns the aggregated ScanResult. Collectors that return errors are
// recorded in their CollectorResult but do not abort the pipeline. Invalid
// signals are logged and skipped.
func (p *Pipeline) Run(ctx context.Context) (*signal.ScanResult, error) {
	start := time.Now()

	var allSignals []signal.RawSignal
	var results []signal.CollectorResult

	for _, c := range p.collectors {
		result := p.runCollector(ctx, c)
		results = append(results, result)

		if result.Err != nil {
			log.Printf("collector %q returned error: %v", result.Collector, result.Err)
			continue
		}

		// Validate each signal, keeping only valid ones.
		for _, s := range result.Signals {
			errs := ValidateSignal(s)
			if len(errs) > 0 {
				log.Printf("skipping invalid signal from %q (title=%q): %v", c.Name(), s.Title, errs)
				continue
			}
			allSignals = append(allSignals, s)
		}
	}

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

// runCollector executes a single collector and captures its result and timing.
func (p *Pipeline) runCollector(ctx context.Context, c collector.Collector) signal.CollectorResult {
	opts := p.config.CollectorOpts[c.Name()]
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
