// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/collector"
	_ "github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/output"
	"github.com/davetashner/stringer/internal/signal"
)

// --------------------------------------------------------------------------
// T2.1: End-to-end scan integration test
// --------------------------------------------------------------------------

// gitCmd runs a git command in the given directory.
func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Integration Test",
		"GIT_AUTHOR_EMAIL=test@stringer.dev",
		"GIT_COMMITTER_NAME=Integration Test",
		"GIT_COMMITTER_EMAIL=test@stringer.dev",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, out)
}

// initSyntheticRepo creates a git repository in t.TempDir() with:
//   - Go source files containing TODO and FIXME comments
//   - Multiple commits (including a revert) for gitlog analysis
//   - Source files with no test counterparts for patterns analysis
//
// Returns the repo directory path.
func initSyntheticRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Initialize git repo using CLI (avoids go-git race conditions).
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "Integration Test")
	gitCmd(t, dir, "config", "user.email", "test@stringer.dev")

	// --- Initial commit: source files with TODO comments ---
	files := map[string]string{
		"main.go": `package main

import "fmt"

func main() {
	// TODO: add proper CLI argument parsing
	fmt.Println("hello stringer")
}
`,
		"lib/handler.go": `package lib

// FIXME: this handler leaks goroutines on timeout
func Handle(req string) string {
	// HACK: temporary workaround for encoding issue
	return req
}
`,
		"lib/utils.go": `package lib

// BUG: race condition when called concurrently
func Process(data []byte) ([]byte, error) {
	return data, nil
}
`,
		"README.md": `# Test Project
This is a synthetic repo for integration tests.
`,
	}

	for relPath, content := range files {
		absPath := filepath.Join(dir, relPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o750))
		require.NoError(t, os.WriteFile(absPath, []byte(content), 0o600))
	}

	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "feat: initial project setup")

	// --- Second commit: add a feature ---
	featureFile := filepath.Join(dir, "lib", "feature.go")
	require.NoError(t, os.WriteFile(featureFile, []byte(`package lib

func NewFeature() string {
	// TODO: implement proper feature logic
	return "placeholder"
}
`), 0o600))

	gitCmd(t, dir, "add", "lib/feature.go")
	gitCmd(t, dir, "commit", "-m", "feat: add new feature module")

	// --- Third commit: then revert it ---
	require.NoError(t, os.Remove(featureFile))
	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", `Revert "feat: add new feature module"`)

	return dir
}

func TestIntegration_EndToEndScan(t *testing.T) {
	repoDir := initSyntheticRepo(t)

	// Run pipeline with the collectors that work on local repos.
	// We use todos, gitlog, and patterns. We skip github (needs token),
	// dephealth (needs go.mod with real deps), vuln (needs dep files),
	// and lotteryrisk (needs more git history).
	config := signal.ScanConfig{
		RepoPath:   repoDir,
		Collectors: []string{"todos", "gitlog", "patterns"},
		CollectorOpts: map[string]signal.CollectorOpts{
			"gitlog": {
				// Use warn mode so gitlog errors don't abort the test.
				ErrorMode: signal.ErrorModeWarn,
			},
		},
	}

	p, err := New(config)
	require.NoError(t, err)

	result, err := p.Run(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	// --- Verify collector results ---
	assert.Len(t, result.Results, 3, "should have results from 3 collectors")

	// Verify duration is tracked.
	assert.True(t, result.Duration > 0, "overall duration should be positive")

	// Check each collector ran.
	collectorNames := make(map[string]bool)
	for _, cr := range result.Results {
		collectorNames[cr.Collector] = true
		assert.True(t, cr.Duration >= 0, "collector %s duration should be non-negative", cr.Collector)
	}
	assert.True(t, collectorNames["todos"])
	assert.True(t, collectorNames["gitlog"])
	assert.True(t, collectorNames["patterns"])

	// --- Verify signals ---
	// We should have at least some TODO signals from the source files.
	require.NotEmpty(t, result.Signals, "should find at least some signals")

	// Check for TODO signals specifically.
	var todoSignals []signal.RawSignal
	for _, s := range result.Signals {
		if s.Source == "todos" {
			todoSignals = append(todoSignals, s)
		}
	}
	// We have 4 TODO-style comments: TODO (x2), FIXME (x1), HACK (x1), BUG (x1)
	// Note: one TODO is in a file that gets reverted (deleted), so it should be 4
	assert.GreaterOrEqual(t, len(todoSignals), 4, "should find at least 4 TODO-style signals")

	// Verify signal field completeness.
	for _, sig := range result.Signals {
		assert.NotEmpty(t, sig.Source, "signal Source must not be empty")
		assert.NotEmpty(t, sig.Title, "signal Title must not be empty")
		assert.True(t, sig.Confidence >= 0 && sig.Confidence <= 1.0,
			"signal Confidence must be 0.0-1.0, got %v", sig.Confidence)
		// FilePath should be relative (not absolute).
		if sig.FilePath != "" {
			assert.False(t, filepath.IsAbs(sig.FilePath),
				"signal FilePath must be relative, got %q", sig.FilePath)
		}
	}

	// Check for git-related signals (revert).
	var revertSignals []signal.RawSignal
	for _, s := range result.Signals {
		if s.Kind == "revert" {
			revertSignals = append(revertSignals, s)
		}
	}
	assert.GreaterOrEqual(t, len(revertSignals), 1, "should detect at least 1 revert")
}

func TestIntegration_BeadsJSONLOutput(t *testing.T) {
	repoDir := initSyntheticRepo(t)

	config := signal.ScanConfig{
		RepoPath:     repoDir,
		Collectors:   []string{"todos"},
		OutputFormat: "beads",
	}

	p, err := New(config)
	require.NoError(t, err)

	result, err := p.Run(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, result.Signals, "should find at least one signal")

	// Format as beads JSONL.
	formatter, err := output.GetFormatter("beads")
	require.NoError(t, err)

	var buf bytes.Buffer
	err = formatter.Format(result.Signals, &buf)
	require.NoError(t, err)

	outStr := buf.String()
	require.NotEmpty(t, outStr, "beads output should not be empty")

	// Each line should be valid JSON.
	lines := strings.Split(strings.TrimSpace(outStr), "\n")
	assert.GreaterOrEqual(t, len(lines), 1, "should have at least 1 JSONL line")

	for i, line := range lines {
		var record map[string]interface{}
		err := json.Unmarshal([]byte(line), &record)
		require.NoErrorf(t, err, "line %d should be valid JSON: %s", i, line)

		// Verify required beads fields.
		assert.NotEmpty(t, record["id"], "line %d: id must not be empty", i)
		assert.NotEmpty(t, record["title"], "line %d: title must not be empty", i)
		assert.NotEmpty(t, record["type"], "line %d: type must not be empty", i)
		assert.NotEmpty(t, record["status"], "line %d: status must not be empty", i)
		assert.NotEmpty(t, record["created_by"], "line %d: created_by must not be empty", i)

		// Priority should be 1-4.
		priority, ok := record["priority"].(float64)
		assert.True(t, ok, "line %d: priority should be a number", i)
		assert.True(t, priority >= 1 && priority <= 4,
			"line %d: priority should be 1-4, got %v", i, priority)

		// Labels should include stringer-generated.
		labels, ok := record["labels"].([]interface{})
		assert.True(t, ok, "line %d: labels should be an array", i)
		var labelStrs []string
		for _, l := range labels {
			if s, ok := l.(string); ok {
				labelStrs = append(labelStrs, s)
			}
		}
		assert.Contains(t, labelStrs, "stringer-generated",
			"line %d: labels should contain 'stringer-generated'", i)
	}
}

func TestIntegration_JSONOutput(t *testing.T) {
	repoDir := initSyntheticRepo(t)

	config := signal.ScanConfig{
		RepoPath:     repoDir,
		Collectors:   []string{"todos"},
		OutputFormat: "json",
	}

	p, err := New(config)
	require.NoError(t, err)

	result, err := p.Run(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, result.Signals)

	// Format as JSON.
	formatter, err := output.GetFormatter("json")
	require.NoError(t, err)

	var buf bytes.Buffer
	err = formatter.Format(result.Signals, &buf)
	require.NoError(t, err)

	// Parse the JSON envelope.
	var envelope output.JSONEnvelope
	err = json.Unmarshal(buf.Bytes(), &envelope)
	require.NoError(t, err, "output should be valid JSON envelope")

	assert.GreaterOrEqual(t, len(envelope.Signals), 1, "should have at least 1 signal")
	assert.Equal(t, len(envelope.Signals), envelope.Metadata.TotalCount,
		"total_count should match signals length")
	assert.NotEmpty(t, envelope.Metadata.Collectors, "collectors list should not be empty")
	assert.NotEmpty(t, envelope.Metadata.GeneratedAt, "generated_at should not be empty")

	// Verify signal data.
	for _, sig := range envelope.Signals {
		assert.NotEmpty(t, sig.Source, "signal Source must not be empty")
		assert.NotEmpty(t, sig.Title, "signal Title must not be empty")
		assert.True(t, sig.Confidence >= 0 && sig.Confidence <= 1.0,
			"confidence must be 0.0-1.0, got %v", sig.Confidence)
	}
}

func TestIntegration_MarkdownOutput(t *testing.T) {
	repoDir := initSyntheticRepo(t)

	config := signal.ScanConfig{
		RepoPath:     repoDir,
		Collectors:   []string{"todos"},
		OutputFormat: "markdown",
	}

	p, err := New(config)
	require.NoError(t, err)

	result, err := p.Run(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, result.Signals)

	// Format as markdown.
	formatter, err := output.GetFormatter("markdown")
	require.NoError(t, err)

	var buf bytes.Buffer
	err = formatter.Format(result.Signals, &buf)
	require.NoError(t, err)

	mdOutput := buf.String()
	require.NotEmpty(t, mdOutput)

	// Verify markdown structure.
	assert.Contains(t, mdOutput, "# Stringer Scan Results", "should have title heading")
	assert.Contains(t, mdOutput, "**Total signals:**", "should have summary line")
	assert.Contains(t, mdOutput, "| Priority | Count |", "should have priority table")
	assert.Contains(t, mdOutput, "## todos", "should have todos collector section")
}

func TestIntegration_AllFormatsProduceOutput(t *testing.T) {
	repoDir := initSyntheticRepo(t)

	// Run pipeline once and format in all three formats.
	config := signal.ScanConfig{
		RepoPath:   repoDir,
		Collectors: []string{"todos"},
	}

	p, err := New(config)
	require.NoError(t, err)

	result, err := p.Run(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, result.Signals)

	for _, fmtName := range []string{"beads", "json", "markdown"} {
		t.Run(fmtName, func(t *testing.T) {
			formatter, err := output.GetFormatter(fmtName)
			require.NoError(t, err)

			var buf bytes.Buffer
			err = formatter.Format(result.Signals, &buf)
			require.NoError(t, err)
			assert.NotEmpty(t, buf.String(), "format %q should produce non-empty output", fmtName)
		})
	}
}

func TestIntegration_MaxIssuesCap(t *testing.T) {
	repoDir := initSyntheticRepo(t)

	config := signal.ScanConfig{
		RepoPath:   repoDir,
		Collectors: []string{"todos"},
		MaxIssues:  2,
	}

	p, err := New(config)
	require.NoError(t, err)

	result, err := p.Run(context.Background())
	require.NoError(t, err)

	assert.LessOrEqual(t, len(result.Signals), 2,
		"MaxIssues=2 should cap signals to at most 2, got %d", len(result.Signals))
}

func TestIntegration_SignalDeduplication(t *testing.T) {
	repoDir := initSyntheticRepo(t)

	// Run with all local collectors to check dedup across collectors.
	config := signal.ScanConfig{
		RepoPath:   repoDir,
		Collectors: []string{"todos", "gitlog", "patterns"},
		CollectorOpts: map[string]signal.CollectorOpts{
			"gitlog": {ErrorMode: signal.ErrorModeWarn},
		},
	}

	p, err := New(config)
	require.NoError(t, err)

	result, err := p.Run(context.Background())
	require.NoError(t, err)

	// Verify no duplicate signals (same hash) in the output.
	seen := make(map[string]bool)
	for _, sig := range result.Signals {
		hash := SignalHash(sig)
		assert.False(t, seen[hash],
			"duplicate signal hash %s for signal: %s", hash, sig.Title)
		seen[hash] = true
	}
}

// --------------------------------------------------------------------------
// T2.5: Parallel collector execution correctness tests
// --------------------------------------------------------------------------

// racyCollector is a collector designed to exercise concurrent access patterns.
// It writes to shared state during collection to expose data races under -race.
type racyCollector struct {
	name    string
	delay   time.Duration
	numSigs int
	counter *atomic.Int64
}

func (r *racyCollector) Name() string { return r.name }

func (r *racyCollector) Collect(_ context.Context, _ string, _ signal.CollectorOpts) ([]signal.RawSignal, error) {
	// Simulate work with optional delay.
	if r.delay > 0 {
		time.Sleep(r.delay)
	}

	// Increment shared counter to prove concurrent execution.
	r.counter.Add(1)

	signals := make([]signal.RawSignal, r.numSigs)
	for i := 0; i < r.numSigs; i++ {
		signals[i] = signal.RawSignal{
			Source:     r.name,
			Kind:       "test",
			FilePath:   "file.go",
			Line:       i + 1,
			Title:      r.name + ": signal " + string(rune('A'+i)),
			Confidence: 0.5 + float64(i)*0.1,
			Tags:       []string{"test", r.name},
		}
	}
	return signals, nil
}

var _ collector.Collector = (*racyCollector)(nil)

func TestIntegration_ParallelCollectorsNoRace(t *testing.T) {
	t.Parallel()

	// Create many collectors to maximize concurrent goroutine count.
	const numCollectors = 10
	const sigsPerCollector = 5

	counter := &atomic.Int64{}
	collectors := make([]collector.Collector, numCollectors)
	for i := 0; i < numCollectors; i++ {
		collectors[i] = &racyCollector{
			name:    "racer-" + string(rune('A'+i)),
			delay:   time.Duration(rand.Intn(10)) * time.Millisecond, //nolint:gosec // test only
			numSigs: sigsPerCollector,
			counter: counter,
		}
	}

	config := signal.ScanConfig{RepoPath: "/tmp/fake"}
	p := NewWithCollectors(config, collectors)

	result, err := p.Run(context.Background())
	require.NoError(t, err)

	// All collectors should have run.
	assert.Equal(t, int64(numCollectors), counter.Load(),
		"all %d collectors should have executed", numCollectors)

	// Verify we get the expected total signals (after dedup).
	// Each collector produces unique signals (different Source + Line combos),
	// but all share the same FilePath, so different Sources prevent dedup.
	assert.Equal(t, numCollectors*sigsPerCollector, len(result.Signals),
		"should have %d total signals (no dedup across different Sources)",
		numCollectors*sigsPerCollector)

	// Verify result ordering matches collector input order.
	assert.Len(t, result.Results, numCollectors)
	for i, cr := range result.Results {
		expectedName := "racer-" + string(rune('A'+i))
		assert.Equal(t, expectedName, cr.Collector,
			"result[%d] should be %q", i, expectedName)
		assert.NoError(t, cr.Err)
		assert.Len(t, cr.Signals, sigsPerCollector)
	}
}

func TestIntegration_ParallelCollectorsWithErrors(t *testing.T) {
	t.Parallel()

	// Mix of working and failing collectors running in parallel.
	counter := &atomic.Int64{}

	good1 := &racyCollector{
		name: "good-1", numSigs: 3, counter: counter,
		delay: 5 * time.Millisecond,
	}
	good2 := &racyCollector{
		name: "good-2", numSigs: 2, counter: counter,
		delay: 5 * time.Millisecond,
	}
	bad := &stubCollector{
		name: "bad-collector",
		err:  assert.AnError,
	}

	config := signal.ScanConfig{
		RepoPath: "/tmp/fake",
		CollectorOpts: map[string]signal.CollectorOpts{
			"bad-collector": {ErrorMode: signal.ErrorModeWarn},
		},
	}

	p := NewWithCollectors(config, []collector.Collector{good1, bad, good2})
	result, err := p.Run(context.Background())
	require.NoError(t, err, "pipeline should not fail in warn mode")

	// Good collectors should produce signals despite the bad one.
	assert.Equal(t, 5, len(result.Signals), "should have 3+2=5 signals from good collectors")

	// Verify the bad collector's error is recorded.
	assert.Error(t, result.Results[1].Err, "bad collector should have an error")
	assert.NoError(t, result.Results[0].Err, "good-1 should succeed")
	assert.NoError(t, result.Results[2].Err, "good-2 should succeed")
}

func TestIntegration_ParallelDedupCorrectness(t *testing.T) {
	t.Parallel()

	// Two collectors produce identical signals (same Source+Kind+FilePath+Line+Title).
	// Dedup should collapse them, keeping the higher confidence.
	counter := &atomic.Int64{}

	c1 := &funcCollector{
		name: "dup-source",
		fn: func(_ context.Context) ([]signal.RawSignal, error) {
			counter.Add(1)
			return []signal.RawSignal{
				{Source: "dup-source", Kind: "todo", FilePath: "x.go", Line: 1, Title: "Fix it", Confidence: 0.5},
				{Source: "dup-source", Kind: "todo", FilePath: "x.go", Line: 2, Title: "Fix that", Confidence: 0.6},
			}, nil
		},
	}
	c2 := &funcCollector{
		name: "dup-source-2",
		fn: func(_ context.Context) ([]signal.RawSignal, error) {
			counter.Add(1)
			// Same signal content but with dup-source-2 as Source, so different hash.
			return []signal.RawSignal{
				{Source: "dup-source-2", Kind: "todo", FilePath: "x.go", Line: 1, Title: "Fix it", Confidence: 0.8},
			}, nil
		},
	}
	// A third collector producing an exact duplicate of c1's first signal.
	c3 := &funcCollector{
		name: "exact-dup",
		fn: func(_ context.Context) ([]signal.RawSignal, error) {
			counter.Add(1)
			return []signal.RawSignal{
				{Source: "dup-source", Kind: "todo", FilePath: "x.go", Line: 1, Title: "Fix it", Confidence: 0.9},
			}, nil
		},
	}

	config := signal.ScanConfig{RepoPath: "/tmp/fake"}
	p := NewWithCollectors(config, []collector.Collector{c1, c2, c3})

	result, err := p.Run(context.Background())
	require.NoError(t, err)

	// All 3 collectors should have run.
	assert.Equal(t, int64(3), counter.Load())

	// c1 produces 2 signals, c2 produces 1 (different Source), c3 produces 1 (exact dup of c1[0]).
	// After dedup: c1[0] (merged with c3[0], conf=0.9) + c1[1] + c2[0] = 3 unique signals.
	assert.Len(t, result.Signals, 3, "should have 3 unique signals after dedup")

	// The deduplicated signal should have the higher confidence (0.9 from c3).
	for _, sig := range result.Signals {
		if sig.Source == "dup-source" && sig.Line == 1 {
			assert.Equal(t, 0.9, sig.Confidence,
				"deduplicated signal should have highest confidence")
		}
	}
}

func TestIntegration_ParallelExecutionTiming(t *testing.T) {
	t.Parallel()

	// Verify that N slow collectors complete in roughly the time of 1 (parallel),
	// not N times (sequential).
	const delayPerCollector = 50 * time.Millisecond
	const numCollectors = 5

	counter := &atomic.Int64{}
	collectors := make([]collector.Collector, numCollectors)
	for i := 0; i < numCollectors; i++ {
		collectors[i] = &racyCollector{
			name:    "timed-" + string(rune('A'+i)),
			delay:   delayPerCollector,
			numSigs: 1,
			counter: counter,
		}
	}

	config := signal.ScanConfig{RepoPath: "/tmp/fake"}
	p := NewWithCollectors(config, collectors)

	start := time.Now()
	result, err := p.Run(context.Background())
	elapsed := time.Since(start)
	require.NoError(t, err)

	// All collectors should have run.
	assert.Equal(t, int64(numCollectors), counter.Load())
	assert.Len(t, result.Signals, numCollectors)

	// Parallel execution: total should be ~50ms, not ~250ms.
	// Use 3x single delay as a generous threshold.
	maxExpected := 3 * delayPerCollector
	assert.Less(t, elapsed, maxExpected,
		"parallel execution took %v, expected less than %v (sequential would be ~%v)",
		elapsed, maxExpected, time.Duration(numCollectors)*delayPerCollector)
}

func TestIntegration_ParallelWithRealCollectors(t *testing.T) {
	// Create a real git repo and run real collectors in parallel.
	// This exercises the actual concurrent code paths with real data.
	dir := t.TempDir()

	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "Test")
	gitCmd(t, dir, "config", "user.email", "t@t.com")

	// Create source files with TODO comments.
	files := map[string]string{
		"main.go": `package main

// TODO: implement main logic
func main() {}
`,
		"util.go": `package main

// FIXME: handle edge cases
func helper() {}
`,
	}

	for relPath, content := range files {
		absPath := filepath.Join(dir, relPath)
		require.NoError(t, os.WriteFile(absPath, []byte(content), 0o600))
	}

	gitCmd(t, dir, "add", "-A")
	gitCmd(t, dir, "commit", "-m", "initial")

	// Run todos and patterns in parallel on the real repo.
	config := signal.ScanConfig{
		RepoPath:   dir,
		Collectors: []string{"todos", "patterns"},
	}

	p, err := New(config)
	require.NoError(t, err)

	result, err := p.Run(context.Background())
	require.NoError(t, err)

	// Verify both collectors produced results.
	assert.Len(t, result.Results, 2)
	assert.NotEmpty(t, result.Signals, "should find signals from real collectors")

	// Verify no duplicates in output.
	hashes := make(map[string]bool)
	for _, sig := range result.Signals {
		h := SignalHash(sig)
		assert.False(t, hashes[h], "found duplicate signal: %s", sig.Title)
		hashes[h] = true
	}

	// Verify all signals pass validation.
	for _, sig := range result.Signals {
		errs := ValidateSignal(sig)
		assert.Emptyf(t, errs, "signal %q should be valid, got errors: %v", sig.Title, errs)
	}
}

func TestIntegration_ParallelContextCancellationGraceful(t *testing.T) {
	t.Parallel()

	// Run multiple collectors with a short timeout context.
	// Verify the pipeline handles cancellation gracefully without panics.
	const numCollectors = 5
	counter := &atomic.Int64{}

	collectors := make([]collector.Collector, numCollectors)
	for i := 0; i < numCollectors; i++ {
		collectors[i] = &funcCollector{
			name: "ctx-" + string(rune('A'+i)),
			fn: func(ctx context.Context) ([]signal.RawSignal, error) {
				counter.Add(1)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(200 * time.Millisecond):
					return []signal.RawSignal{
						{Source: "ctx", Title: "Completed", FilePath: "f.go", Confidence: 0.5},
					}, nil
				}
			},
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	config := signal.ScanConfig{RepoPath: "/tmp/fake"}
	p := NewWithCollectors(config, collectors)

	// Should not panic.
	result, err := p.Run(ctx)
	require.NoError(t, err, "warn mode should not propagate errors")
	require.NotNil(t, result)

	// All collectors should have been launched.
	assert.Equal(t, int64(numCollectors), counter.Load(),
		"all collectors should have started")
}

func TestIntegration_ParallelSignalFieldIntegrity(t *testing.T) {
	t.Parallel()

	// Verify that signal fields are not corrupted during parallel collection.
	// Each collector writes signals with unique, predictable field values.
	const numCollectors = 8
	const sigsPerCollector = 10

	collectors := make([]collector.Collector, numCollectors)
	for i := 0; i < numCollectors; i++ {
		idx := i
		collectors[i] = &funcCollector{
			name: "integrity-" + string(rune('A'+idx)),
			fn: func(_ context.Context) ([]signal.RawSignal, error) {
				sigs := make([]signal.RawSignal, sigsPerCollector)
				for j := 0; j < sigsPerCollector; j++ {
					sigs[j] = signal.RawSignal{
						Source:     "integrity-" + string(rune('A'+idx)),
						Kind:       "test",
						FilePath:   "file-" + string(rune('A'+idx)) + ".go",
						Line:       j + 1,
						Title:      "Signal from collector " + string(rune('A'+idx)) + " #" + string(rune('0'+j)),
						Confidence: 0.5,
						Tags:       []string{"collector-" + string(rune('A'+idx))},
					}
				}
				return sigs, nil
			},
		}
	}

	config := signal.ScanConfig{RepoPath: "/tmp/fake"}
	p := NewWithCollectors(config, collectors)

	result, err := p.Run(context.Background())
	require.NoError(t, err)

	// Group signals by source to verify integrity.
	bySource := make(map[string][]signal.RawSignal)
	for _, sig := range result.Signals {
		bySource[sig.Source] = append(bySource[sig.Source], sig)
	}

	assert.Len(t, bySource, numCollectors,
		"should have signals from all %d collectors", numCollectors)

	for source, sigs := range bySource {
		assert.Len(t, sigs, sigsPerCollector,
			"collector %s should have %d signals", source, sigsPerCollector)

		for _, sig := range sigs {
			// Verify source matches the expected pattern.
			assert.True(t, strings.HasPrefix(sig.Source, "integrity-"),
				"signal source should start with 'integrity-', got %q", sig.Source)

			// Verify tags are not corrupted.
			require.Len(t, sig.Tags, 1, "signal should have exactly 1 tag")
			assert.True(t, strings.HasPrefix(sig.Tags[0], "collector-"),
				"tag should start with 'collector-', got %q", sig.Tags[0])

			// Verify the tag collector letter matches the source letter.
			sourceLetter := strings.TrimPrefix(sig.Source, "integrity-")
			tagLetter := strings.TrimPrefix(sig.Tags[0], "collector-")
			assert.Equal(t, sourceLetter, tagLetter,
				"signal source letter %q should match tag letter %q",
				sourceLetter, tagLetter)
		}
	}
}
