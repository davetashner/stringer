package pipeline

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

// stubCollector implements collector.Collector for testing.
type stubCollector struct {
	name    string
	signals []signal.RawSignal
	err     error
	delay   time.Duration
}

func (s *stubCollector) Name() string { return s.name }

func (s *stubCollector) Collect(_ context.Context, _ string, _ signal.CollectorOpts) ([]signal.RawSignal, error) {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	return s.signals, s.err
}

// Compile-time interface check.
var _ collector.Collector = (*stubCollector)(nil)

func TestPipeline_SingleCollector(t *testing.T) {
	stub := &stubCollector{
		name: "test",
		signals: []signal.RawSignal{
			{Source: "test", Title: "Fix bug", FilePath: "main.go", Confidence: 0.9},
			{Source: "test", Title: "Add feature", FilePath: "lib.go", Confidence: 0.7},
		},
	}

	p := NewWithCollectors(signal.ScanConfig{RepoPath: "/tmp/repo"}, []collector.Collector{stub})
	result, err := p.Run(context.Background())

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Signals) != 2 {
		t.Errorf("expected 2 signals, got %d", len(result.Signals))
	}
	if len(result.Results) != 1 {
		t.Errorf("expected 1 collector result, got %d", len(result.Results))
	}
	if result.Results[0].Collector != "test" {
		t.Errorf("collector name = %q, want %q", result.Results[0].Collector, "test")
	}
	if result.Results[0].Err != nil {
		t.Errorf("collector error = %v, want nil", result.Results[0].Err)
	}
}

func TestPipeline_MultipleCollectors(t *testing.T) {
	stub1 := &stubCollector{
		name: "todos",
		signals: []signal.RawSignal{
			{Source: "todos", Title: "TODO found", FilePath: "a.go", Confidence: 0.8},
		},
	}
	stub2 := &stubCollector{
		name: "gitlog",
		signals: []signal.RawSignal{
			{Source: "gitlog", Title: "Revert detected", FilePath: "b.go", Confidence: 0.6},
			{Source: "gitlog", Title: "Churn detected", FilePath: "c.go", Confidence: 0.5},
		},
	}

	p := NewWithCollectors(signal.ScanConfig{RepoPath: "/tmp/repo"}, []collector.Collector{stub1, stub2})
	result, err := p.Run(context.Background())

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Signals) != 3 {
		t.Errorf("expected 3 signals, got %d", len(result.Signals))
	}
	if len(result.Results) != 2 {
		t.Errorf("expected 2 collector results, got %d", len(result.Results))
	}
}

func TestPipeline_CollectorError(t *testing.T) {
	errCollector := &stubCollector{
		name: "broken",
		err:  errors.New("collector failed"),
	}
	goodCollector := &stubCollector{
		name: "good",
		signals: []signal.RawSignal{
			{Source: "good", Title: "Valid signal", FilePath: "ok.go", Confidence: 0.9},
		},
	}

	p := NewWithCollectors(signal.ScanConfig{RepoPath: "/tmp/repo"}, []collector.Collector{errCollector, goodCollector})
	result, err := p.Run(context.Background())

	if err != nil {
		t.Fatalf("Run() should not return error when a collector fails, got %v", err)
	}

	// The broken collector's error is recorded in its result.
	if result.Results[0].Err == nil {
		t.Error("expected error in broken collector result")
	}
	if result.Results[0].Err.Error() != "collector failed" {
		t.Errorf("error = %q, want %q", result.Results[0].Err.Error(), "collector failed")
	}

	// The good collector's signals should still be present.
	if len(result.Signals) != 1 {
		t.Errorf("expected 1 valid signal from good collector, got %d", len(result.Signals))
	}
}

func TestPipeline_InvalidSignalsSkipped(t *testing.T) {
	stub := &stubCollector{
		name: "test",
		signals: []signal.RawSignal{
			{Source: "test", Title: "Valid", FilePath: "ok.go", Confidence: 0.5},
			{Source: "test", Title: "", FilePath: "bad.go", Confidence: 0.5},            // empty title
			{Source: "", Title: "No source", FilePath: "bad.go", Confidence: 0.5},       // empty source
			{Source: "test", Title: "Abs path", FilePath: "/abs/path", Confidence: 0.5}, // absolute path
			{Source: "test", Title: "Bad conf", FilePath: "ok.go", Confidence: 1.5},     // bad confidence
		},
	}

	p := NewWithCollectors(signal.ScanConfig{RepoPath: "/tmp/repo"}, []collector.Collector{stub})
	result, err := p.Run(context.Background())

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Signals) != 1 {
		t.Errorf("expected 1 valid signal (others should be skipped), got %d", len(result.Signals))
	}
	if result.Signals[0].Title != "Valid" {
		t.Errorf("surviving signal Title = %q, want %q", result.Signals[0].Title, "Valid")
	}

	// The collector result should still report all 5 signals.
	if len(result.Results[0].Signals) != 5 {
		t.Errorf("collector result should have all 5 signals, got %d", len(result.Results[0].Signals))
	}
}

func TestPipeline_MaxIssues(t *testing.T) {
	stub := &stubCollector{
		name: "test",
		signals: []signal.RawSignal{
			{Source: "test", Title: "Signal 1", FilePath: "a.go", Confidence: 0.8},
			{Source: "test", Title: "Signal 2", FilePath: "b.go", Confidence: 0.7},
			{Source: "test", Title: "Signal 3", FilePath: "c.go", Confidence: 0.6},
		},
	}

	p := NewWithCollectors(signal.ScanConfig{
		RepoPath:  "/tmp/repo",
		MaxIssues: 2,
	}, []collector.Collector{stub})
	result, err := p.Run(context.Background())

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Signals) != 2 {
		t.Errorf("expected 2 signals (capped by MaxIssues), got %d", len(result.Signals))
	}
}

func TestPipeline_MaxIssuesZeroMeansUnlimited(t *testing.T) {
	stub := &stubCollector{
		name: "test",
		signals: []signal.RawSignal{
			{Source: "test", Title: "Signal 1", FilePath: "a.go", Confidence: 0.8},
			{Source: "test", Title: "Signal 2", FilePath: "b.go", Confidence: 0.7},
		},
	}

	p := NewWithCollectors(signal.ScanConfig{
		RepoPath:  "/tmp/repo",
		MaxIssues: 0,
	}, []collector.Collector{stub})
	result, err := p.Run(context.Background())

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Signals) != 2 {
		t.Errorf("expected 2 signals (unlimited), got %d", len(result.Signals))
	}
}

func TestPipeline_TimingTracked(t *testing.T) {
	stub := &stubCollector{
		name:  "slow",
		delay: 50 * time.Millisecond,
		signals: []signal.RawSignal{
			{Source: "slow", Title: "Something", FilePath: "x.go", Confidence: 0.5},
		},
	}

	p := NewWithCollectors(signal.ScanConfig{RepoPath: "/tmp/repo"}, []collector.Collector{stub})
	result, err := p.Run(context.Background())

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Overall duration should be at least as long as the collector's delay.
	if result.Duration < 50*time.Millisecond {
		t.Errorf("total Duration = %v, expected at least 50ms", result.Duration)
	}

	// Per-collector duration should also reflect the delay.
	if result.Results[0].Duration < 50*time.Millisecond {
		t.Errorf("collector Duration = %v, expected at least 50ms", result.Results[0].Duration)
	}
}

func TestPipeline_NoCollectors(t *testing.T) {
	p := NewWithCollectors(signal.ScanConfig{RepoPath: "/tmp/repo"}, nil)
	result, err := p.Run(context.Background())

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Signals) != 0 {
		t.Errorf("expected 0 signals with no collectors, got %d", len(result.Signals))
	}
	if len(result.Results) != 0 {
		t.Errorf("expected 0 results with no collectors, got %d", len(result.Results))
	}
	if result.Duration < 0 {
		t.Errorf("Duration should be non-negative, got %v", result.Duration)
	}
}

func TestPipeline_CollectorOptsPassedThrough(t *testing.T) {
	wrapper := &optsRecordingCollector{
		name: "capture",
		signals: []signal.RawSignal{
			{Source: "capture", Title: "OK", FilePath: "f.go", Confidence: 0.5},
		},
	}

	config := signal.ScanConfig{
		RepoPath: "/tmp/repo",
		CollectorOpts: map[string]signal.CollectorOpts{
			"capture": {
				MinConfidence:   0.5,
				IncludePatterns: []string{"*.go"},
			},
		},
	}

	p := NewWithCollectors(config, []collector.Collector{wrapper})
	_, err := p.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !wrapper.captured {
		t.Fatal("Collect was not called")
	}
	if wrapper.receivedOpts.MinConfidence != 0.5 {
		t.Errorf("MinConfidence = %v, want 0.5", wrapper.receivedOpts.MinConfidence)
	}
	if len(wrapper.receivedOpts.IncludePatterns) != 1 || wrapper.receivedOpts.IncludePatterns[0] != "*.go" {
		t.Errorf("IncludePatterns = %v, want [*.go]", wrapper.receivedOpts.IncludePatterns)
	}
}

// optsRecordingCollector captures the CollectorOpts passed to Collect.
type optsRecordingCollector struct {
	name         string
	signals      []signal.RawSignal
	receivedOpts signal.CollectorOpts
	captured     bool
}

func (o *optsRecordingCollector) Name() string { return o.name }

func (o *optsRecordingCollector) Collect(_ context.Context, _ string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	o.receivedOpts = opts
	o.captured = true
	return o.signals, nil
}

func TestPipeline_ContextCancelled(t *testing.T) {
	cancelCollector := &contextAwareCollector{
		name: "ctx-aware",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	p := NewWithCollectors(signal.ScanConfig{RepoPath: "/tmp/repo"}, []collector.Collector{cancelCollector})
	result, err := p.Run(ctx)

	if err != nil {
		t.Fatalf("Run() should not return error, got %v", err)
	}

	// The collector should have returned a context error.
	if result.Results[0].Err == nil {
		t.Error("expected error from context-aware collector when context is cancelled")
	}
}

// contextAwareCollector checks the context before proceeding.
type contextAwareCollector struct {
	name string
}

func (c *contextAwareCollector) Name() string { return c.name }

func (c *contextAwareCollector) Collect(ctx context.Context, _ string, _ signal.CollectorOpts) ([]signal.RawSignal, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return []signal.RawSignal{
			{Source: c.name, Title: "Should not appear", FilePath: "x.go", Confidence: 0.5},
		}, nil
	}
}

func TestNew_UnknownCollector(t *testing.T) {
	config := signal.ScanConfig{
		RepoPath:   "/tmp/repo",
		Collectors: []string{"nonexistent-collector"},
	}

	_, err := New(config)
	if err == nil {
		t.Fatal("expected error for unknown collector, got nil")
	}
}

// --- Parallel Execution Tests ---

func TestPipeline_ParallelExecution(t *testing.T) {
	// Two slow collectors should run in parallel, taking roughly the time
	// of one collector, not the sum.
	stub1 := &stubCollector{
		name:  "slow1",
		delay: 100 * time.Millisecond,
		signals: []signal.RawSignal{
			{Source: "slow1", Title: "Signal A", FilePath: "a.go", Confidence: 0.8},
		},
	}
	stub2 := &stubCollector{
		name:  "slow2",
		delay: 100 * time.Millisecond,
		signals: []signal.RawSignal{
			{Source: "slow2", Title: "Signal B", FilePath: "b.go", Confidence: 0.7},
		},
	}

	p := NewWithCollectors(signal.ScanConfig{RepoPath: "/tmp/repo"},
		[]collector.Collector{stub1, stub2})

	start := time.Now()
	result, err := p.Run(context.Background())
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Both collectors' signals should be present.
	if len(result.Signals) != 2 {
		t.Errorf("expected 2 signals, got %d", len(result.Signals))
	}

	// Parallel execution: total should be ~100ms, not ~200ms.
	// Use 180ms as threshold to be safe with scheduling jitter.
	if elapsed >= 180*time.Millisecond {
		t.Errorf("parallel execution took %v, expected less than 180ms (sequential would be ~200ms)", elapsed)
	}
}

func TestPipeline_ParallelResultOrdering(t *testing.T) {
	// Even though collectors run in parallel, results should be in the
	// same order as the input collectors slice.
	fast := &stubCollector{
		name:  "fast",
		delay: 0,
		signals: []signal.RawSignal{
			{Source: "fast", Title: "Fast signal", FilePath: "f.go", Confidence: 0.8},
		},
	}
	slow := &stubCollector{
		name:  "slow",
		delay: 50 * time.Millisecond,
		signals: []signal.RawSignal{
			{Source: "slow", Title: "Slow signal", FilePath: "s.go", Confidence: 0.7},
		},
	}

	// slow is first in the list, but finishes last.
	p := NewWithCollectors(signal.ScanConfig{RepoPath: "/tmp/repo"},
		[]collector.Collector{slow, fast})
	result, err := p.Run(context.Background())

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Results should preserve input order.
	if result.Results[0].Collector != "slow" {
		t.Errorf("results[0] = %q, want %q", result.Results[0].Collector, "slow")
	}
	if result.Results[1].Collector != "fast" {
		t.Errorf("results[1] = %q, want %q", result.Results[1].Collector, "fast")
	}

	// Signals should also follow input collector order (slow first, then fast).
	if len(result.Signals) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(result.Signals))
	}
	if result.Signals[0].Source != "slow" {
		t.Errorf("signals[0].Source = %q, want %q", result.Signals[0].Source, "slow")
	}
	if result.Signals[1].Source != "fast" {
		t.Errorf("signals[1].Source = %q, want %q", result.Signals[1].Source, "fast")
	}
}

func TestPipeline_ParallelContextCancellation(t *testing.T) {
	// When context is cancelled, all goroutines should respect it.
	var started atomic.Int32

	blockingCollector := &funcCollector{
		name: "blocking",
		fn: func(ctx context.Context) ([]signal.RawSignal, error) {
			started.Add(1)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	quickCollector := &funcCollector{
		name: "quick",
		fn: func(ctx context.Context) ([]signal.RawSignal, error) {
			started.Add(1)
			return []signal.RawSignal{
				{Source: "quick", Title: "Quick", FilePath: "q.go", Confidence: 0.5},
			}, nil
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	p := NewWithCollectors(signal.ScanConfig{RepoPath: "/tmp/repo"},
		[]collector.Collector{blockingCollector, quickCollector})

	_, err := p.Run(ctx)

	// The blocking collector gets a context error which is logged (warn),
	// so Run itself should not return an error.
	if err != nil {
		t.Fatalf("Run() should not return error in warn mode, got %v", err)
	}
}

// funcCollector is a collector that executes a function, useful for custom test behavior.
type funcCollector struct {
	name string
	fn   func(ctx context.Context) ([]signal.RawSignal, error)
}

func (f *funcCollector) Name() string { return f.name }

func (f *funcCollector) Collect(ctx context.Context, _ string, _ signal.CollectorOpts) ([]signal.RawSignal, error) {
	return f.fn(ctx)
}

// --- Error Mode Tests ---

func TestPipeline_ErrorModeWarn_Default(t *testing.T) {
	// Default behavior: errors are logged, pipeline continues.
	errCollector := &stubCollector{
		name: "warn-collector",
		err:  errors.New("something went wrong"),
	}
	goodCollector := &stubCollector{
		name: "good-collector",
		signals: []signal.RawSignal{
			{Source: "good-collector", Title: "Valid", FilePath: "v.go", Confidence: 0.8},
		},
	}

	p := NewWithCollectors(signal.ScanConfig{RepoPath: "/tmp/repo"},
		[]collector.Collector{errCollector, goodCollector})
	result, err := p.Run(context.Background())

	if err != nil {
		t.Fatalf("Run() should not return error in warn mode, got %v", err)
	}
	if len(result.Signals) != 1 {
		t.Errorf("expected 1 signal, got %d", len(result.Signals))
	}
	if result.Results[0].Err == nil {
		t.Error("expected error recorded in warn-collector result")
	}
}

func TestPipeline_ErrorModeSkip(t *testing.T) {
	// Skip mode: errors are silently ignored.
	errCollector := &stubCollector{
		name: "skip-collector",
		err:  errors.New("silently ignored"),
	}
	goodCollector := &stubCollector{
		name: "good-collector",
		signals: []signal.RawSignal{
			{Source: "good-collector", Title: "Valid", FilePath: "v.go", Confidence: 0.8},
		},
	}

	config := signal.ScanConfig{
		RepoPath: "/tmp/repo",
		CollectorOpts: map[string]signal.CollectorOpts{
			"skip-collector": {ErrorMode: signal.ErrorModeSkip},
		},
	}

	p := NewWithCollectors(config, []collector.Collector{errCollector, goodCollector})
	result, err := p.Run(context.Background())

	if err != nil {
		t.Fatalf("Run() should not return error in skip mode, got %v", err)
	}
	if len(result.Signals) != 1 {
		t.Errorf("expected 1 signal, got %d", len(result.Signals))
	}
	// The error is still recorded in the result.
	if result.Results[0].Err == nil {
		t.Error("expected error recorded in skip-collector result")
	}
}

func TestPipeline_ErrorModeFail(t *testing.T) {
	// Fail mode: first error aborts the entire scan.
	errCollector := &stubCollector{
		name:  "fail-collector",
		err:   errors.New("fatal error"),
		delay: 0,
	}
	goodCollector := &stubCollector{
		name:  "good-collector",
		delay: 10 * time.Millisecond, // slight delay so fail collector finishes first
		signals: []signal.RawSignal{
			{Source: "good-collector", Title: "Valid", FilePath: "v.go", Confidence: 0.8},
		},
	}

	config := signal.ScanConfig{
		RepoPath: "/tmp/repo",
		CollectorOpts: map[string]signal.CollectorOpts{
			"fail-collector": {ErrorMode: signal.ErrorModeFail},
		},
	}

	p := NewWithCollectors(config, []collector.Collector{errCollector, goodCollector})
	result, err := p.Run(context.Background())

	if err == nil {
		t.Fatal("Run() should return error in fail mode")
	}

	// The error message should mention the collector.
	if !errors.Is(err, errCollector.err) {
		// Check that the error wraps the original.
		if err.Error() != `collector "fail-collector" failed: fatal error` {
			t.Errorf("unexpected error message: %q", err.Error())
		}
	}

	// Result should still be returned (partial results).
	if result == nil {
		t.Fatal("expected non-nil result even on failure")
	}
}

func TestPipeline_ErrorModeFail_OnlyOneCollector(t *testing.T) {
	errCollector := &stubCollector{
		name: "sole-fail",
		err:  errors.New("only collector failed"),
	}

	config := signal.ScanConfig{
		RepoPath: "/tmp/repo",
		CollectorOpts: map[string]signal.CollectorOpts{
			"sole-fail": {ErrorMode: signal.ErrorModeFail},
		},
	}

	p := NewWithCollectors(config, []collector.Collector{errCollector})
	_, err := p.Run(context.Background())

	if err == nil {
		t.Fatal("Run() should return error when sole fail-mode collector fails")
	}
}

func TestPipeline_MixedErrorModes(t *testing.T) {
	// Test a mix: one fail-mode (success), one warn-mode (error), one skip-mode (error).
	successFail := &stubCollector{
		name: "success-fail",
		signals: []signal.RawSignal{
			{Source: "success-fail", Title: "Good", FilePath: "g.go", Confidence: 0.9},
		},
	}
	errorWarn := &stubCollector{
		name: "error-warn",
		err:  errors.New("warned"),
	}
	errorSkip := &stubCollector{
		name: "error-skip",
		err:  errors.New("skipped"),
	}

	config := signal.ScanConfig{
		RepoPath: "/tmp/repo",
		CollectorOpts: map[string]signal.CollectorOpts{
			"success-fail": {ErrorMode: signal.ErrorModeFail},
			"error-warn":   {ErrorMode: signal.ErrorModeWarn},
			"error-skip":   {ErrorMode: signal.ErrorModeSkip},
		},
	}

	p := NewWithCollectors(config, []collector.Collector{successFail, errorWarn, errorSkip})
	result, err := p.Run(context.Background())

	if err != nil {
		t.Fatalf("Run() should not return error (fail-mode collector succeeded), got %v", err)
	}

	if len(result.Signals) != 1 {
		t.Errorf("expected 1 signal, got %d", len(result.Signals))
	}

	// Check that errors are recorded for warn and skip collectors.
	if result.Results[1].Err == nil {
		t.Error("expected error recorded for error-warn")
	}
	if result.Results[2].Err == nil {
		t.Error("expected error recorded for error-skip")
	}
}

func TestPipeline_ErrorMode_DefaultIsWarn(t *testing.T) {
	// When no CollectorOpts are configured at all, error mode defaults to warn.
	errCollector := &stubCollector{
		name: "no-opts",
		err:  errors.New("unhandled"),
	}

	p := NewWithCollectors(signal.ScanConfig{RepoPath: "/tmp/repo"},
		[]collector.Collector{errCollector})
	_, err := p.Run(context.Background())

	if err != nil {
		t.Fatalf("Run() should not return error with default warn mode, got %v", err)
	}
}
