package pipeline

import (
	"context"
	"errors"
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
