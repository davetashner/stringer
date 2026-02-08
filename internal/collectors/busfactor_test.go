package collectors

import (
	"context"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

func TestBusFactorCollector_Name(t *testing.T) {
	c := &BusFactorCollector{}
	assert.Equal(t, "busfactor", c.Name())
}

func TestBusFactorCollector_SingleAuthor(t *testing.T) {
	// All files by one author should yield bus factor 1, confidence 0.8.
	_, dir := initGoGitRepo(t, map[string]string{
		"main.go":     "package main\n\nfunc main() {}\n",
		"lib/util.go": "package lib\n\nfunc Util() {}\n",
	})

	c := &BusFactorCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	busfactor := filterByKind(signals, "low-bus-factor")
	require.NotEmpty(t, busfactor, "single-author repo should produce low-bus-factor signals")

	for _, sig := range busfactor {
		assert.Equal(t, "busfactor", sig.Source)
		assert.Equal(t, "low-bus-factor", sig.Kind)
		assert.Equal(t, 0.8, sig.Confidence, "bus factor 1 should have confidence 0.8")
		assert.Contains(t, sig.Tags, "low-bus-factor")
		assert.Contains(t, sig.Tags, "stringer-generated")
		assert.Contains(t, sig.Title, "bus factor 1")
		assert.Contains(t, sig.Title, "Test Author")
	}
}

func TestBusFactorCollector_TwoAuthorsEqual(t *testing.T) {
	// Two authors with equal contributions should give bus factor 2.
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	now := time.Now()

	// Author A writes file1.go
	addCommitAs(t, repo, dir, "file1.go",
		"package main\n\nfunc A1() {}\nfunc A2() {}\nfunc A3() {}\n",
		"feat: add file1", now, "Alice", "alice@example.com")

	// Author B writes file2.go
	addCommitAs(t, repo, dir, "file2.go",
		"package main\n\nfunc B1() {}\nfunc B2() {}\nfunc B3() {}\n",
		"feat: add file2", now, "Bob", "bob@example.com")

	c := &BusFactorCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	busfactor := filterByKind(signals, "low-bus-factor")

	// With two equal authors, bus factor is 2 at root level.
	// With threshold=1, bus factor 2 should NOT emit a signal
	// (bus factor 2 > threshold 1).
	// But sub-directories with single author may still emit signals.
	for _, sig := range busfactor {
		// Root "." should not be flagged if bus factor is 2.
		if sig.FilePath == "./" || sig.FilePath == "." {
			t.Errorf("root directory should not be flagged with two equal authors, got bus factor signal: %s", sig.Title)
		}
	}
}

func TestBusFactorCollector_OneDominant(t *testing.T) {
	// One author writes 90% of code, bus factor should be 1.
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	now := time.Now()

	// Author A writes many files.
	for i := 0; i < 9; i++ {
		addCommitAs(t, repo, dir, "a"+string(rune('1'+i))+".go",
			"package main\n\nfunc F() {}\nfunc G() {}\nfunc H() {}\n",
			"feat: add file", now, "Alice", "alice@example.com")
	}

	// Author B writes one file.
	addCommitAs(t, repo, dir, "b1.go",
		"package main\n\nfunc X() {}\n",
		"feat: add b1", now, "Bob", "bob@example.com")

	c := &BusFactorCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	busfactor := filterByKind(signals, "low-bus-factor")

	// Root should have bus factor 1 because Alice dominates.
	var rootSignal *signal.RawSignal
	for i, sig := range busfactor {
		if sig.FilePath == "./" || sig.FilePath == "." {
			rootSignal = &busfactor[i]
			break
		}
	}
	require.NotNil(t, rootSignal, "root directory should be flagged when one author dominates")
	assert.Equal(t, 0.8, rootSignal.Confidence)
	assert.Contains(t, rootSignal.Title, "Alice")
}

func TestBusFactorCollector_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)

	c := &BusFactorCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals, "empty repo should produce no signals")
}

func TestBusFactorCollector_NotAGitRepo(t *testing.T) {
	dir := t.TempDir()

	c := &BusFactorCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	assert.Error(t, err, "non-git directory should return an error")
}

func TestBusFactorCollector_ContextCancellation(t *testing.T) {
	_, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &BusFactorCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{})
	assert.Error(t, err, "cancelled context should return an error")
}

func TestBusFactorCollector_DeterministicOutput(t *testing.T) {
	// Same repo should always produce the same signals.
	_, dir := initGoGitRepo(t, map[string]string{
		"main.go":     "package main\n\nfunc main() {}\n",
		"lib/util.go": "package lib\n\nfunc Util() {}\n",
	})

	c := &BusFactorCollector{}

	signals1, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	signals2, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	require.Equal(t, len(signals1), len(signals2), "two scans should produce same number of signals")

	for i := range signals1 {
		assert.Equal(t, signals1[i].FilePath, signals2[i].FilePath, "signal %d FilePath mismatch", i)
		assert.Equal(t, signals1[i].Title, signals2[i].Title, "signal %d Title mismatch", i)
		assert.Equal(t, signals1[i].Kind, signals2[i].Kind, "signal %d Kind mismatch", i)
		assert.Equal(t, signals1[i].Confidence, signals2[i].Confidence, "signal %d Confidence mismatch", i)
	}
}

func TestBusFactorCollector_SignalFields(t *testing.T) {
	// Verify all signal fields are populated correctly.
	_, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})

	c := &BusFactorCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	busfactor := filterByKind(signals, "low-bus-factor")
	require.NotEmpty(t, busfactor)

	sig := busfactor[0]
	assert.Equal(t, "busfactor", sig.Source)
	assert.Equal(t, "low-bus-factor", sig.Kind)
	assert.NotEmpty(t, sig.FilePath)
	assert.Equal(t, 0, sig.Line, "bus factor signals use directory paths, not line numbers")
	assert.NotEmpty(t, sig.Title)
	assert.NotEmpty(t, sig.Description)
	assert.Contains(t, sig.Description, "Bus factor:")
	assert.Contains(t, sig.Description, "Top authors:")
	assert.InDelta(t, 0.8, sig.Confidence, 0.001)
	assert.Contains(t, sig.Tags, "low-bus-factor")
	assert.Contains(t, sig.Tags, "stringer-generated")
}

// --- Recency decay function tests ---

func TestRecencyDecay_Today(t *testing.T) {
	// A commit from today should have weight ~1.0.
	weight := recencyDecay(0)
	assert.InDelta(t, 1.0, weight, 0.001)
}

func TestRecencyDecay_HalfLife(t *testing.T) {
	// At exactly the half-life (180 days), weight should be 0.5.
	weight := recencyDecay(float64(decayHalfLifeDays))
	assert.InDelta(t, 0.5, weight, 0.001)
}

func TestRecencyDecay_DoubleHalfLife(t *testing.T) {
	// At 2x half-life (360 days), weight should be 0.25.
	weight := recencyDecay(float64(2 * decayHalfLifeDays))
	assert.InDelta(t, 0.25, weight, 0.001)
}

func TestRecencyDecay_VeryOld(t *testing.T) {
	// Very old commits should have near-zero weight.
	weight := recencyDecay(3650) // ~10 years
	assert.Less(t, weight, 0.001)
}

func TestRecencyDecay_Negative(t *testing.T) {
	// Negative days should be treated as 0 (weight 1.0).
	weight := recencyDecay(-10)
	assert.InDelta(t, 1.0, weight, 0.001)
}

// --- Bus factor calculation tests ---

func TestComputeBusFactor_SingleAuthor(t *testing.T) {
	own := &dirOwnership{
		Path: ".",
		Authors: map[string]*authorStats{
			"Alice": {BlameLines: 100, CommitWeight: 5.0},
		},
		TotalLines: 100,
	}
	bf := computeBusFactor(own)
	assert.Equal(t, 1, bf, "single author should have bus factor 1")
}

func TestComputeBusFactor_TwoEqualAuthors(t *testing.T) {
	own := &dirOwnership{
		Path: ".",
		Authors: map[string]*authorStats{
			"Alice": {BlameLines: 50, CommitWeight: 5.0},
			"Bob":   {BlameLines: 50, CommitWeight: 5.0},
		},
		TotalLines: 100,
	}
	bf := computeBusFactor(own)
	assert.Equal(t, 2, bf, "two equal authors need both to exceed 50%")
}

func TestComputeBusFactor_OneDominant(t *testing.T) {
	own := &dirOwnership{
		Path: ".",
		Authors: map[string]*authorStats{
			"Alice": {BlameLines: 90, CommitWeight: 9.0},
			"Bob":   {BlameLines: 10, CommitWeight: 1.0},
		},
		TotalLines: 100,
	}
	bf := computeBusFactor(own)
	assert.Equal(t, 1, bf, "dominant author alone exceeds 50%, bus factor 1")
}

func TestComputeBusFactor_ThreeAuthors(t *testing.T) {
	own := &dirOwnership{
		Path: ".",
		Authors: map[string]*authorStats{
			"Alice":   {BlameLines: 40, CommitWeight: 4.0},
			"Bob":     {BlameLines: 35, CommitWeight: 3.5},
			"Charlie": {BlameLines: 25, CommitWeight: 2.5},
		},
		TotalLines: 100,
	}
	bf := computeBusFactor(own)
	// Alice has ~40% ownership, needs Bob too to exceed 50%.
	assert.Equal(t, 2, bf, "two authors needed to exceed 50%")
}

func TestComputeBusFactor_NoAuthors(t *testing.T) {
	own := &dirOwnership{
		Path:       ".",
		Authors:    map[string]*authorStats{},
		TotalLines: 0,
	}
	bf := computeBusFactor(own)
	assert.Equal(t, 0, bf, "no authors should return bus factor 0")
}

// --- Confidence mapping tests ---

func TestBusFactorConfidence_BusFactor1(t *testing.T) {
	assert.InDelta(t, 0.8, busFactorConfidence(1), 0.001)
}

func TestBusFactorConfidence_BusFactor0(t *testing.T) {
	assert.InDelta(t, 0.8, busFactorConfidence(0), 0.001)
}

func TestBusFactorConfidence_BusFactor2(t *testing.T) {
	assert.InDelta(t, 0.5, busFactorConfidence(2), 0.001)
}

func TestBusFactorConfidence_BusFactor3(t *testing.T) {
	assert.InDelta(t, 0.3, busFactorConfidence(3), 0.001)
}

func TestBusFactorConfidence_BusFactor10(t *testing.T) {
	assert.InDelta(t, 0.3, busFactorConfidence(10), 0.001)
}
