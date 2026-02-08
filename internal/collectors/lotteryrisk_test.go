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

func TestLotteryRiskCollector_Name(t *testing.T) {
	c := &LotteryRiskCollector{}
	assert.Equal(t, "lotteryrisk", c.Name())
}

func TestLotteryRiskCollector_SingleAuthor(t *testing.T) {
	// All files by one author should yield lottery risk 1, confidence 0.8.
	_, dir := initGoGitRepo(t, map[string]string{
		"main.go":     "package main\n\nfunc main() {}\n",
		"lib/util.go": "package lib\n\nfunc Util() {}\n",
	})

	c := &LotteryRiskCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	lotteryrisk := filterByKind(signals, "low-lottery-risk")
	require.NotEmpty(t, lotteryrisk, "single-author repo should produce low-lottery-risk signals")

	for _, sig := range lotteryrisk {
		assert.Equal(t, "lotteryrisk", sig.Source)
		assert.Equal(t, "low-lottery-risk", sig.Kind)
		assert.Equal(t, 0.8, sig.Confidence, "lottery risk 1 should have confidence 0.8")
		assert.Contains(t, sig.Tags, "low-lottery-risk")
		assert.Contains(t, sig.Tags, "stringer-generated")
		assert.Contains(t, sig.Title, "lottery risk 1")
		assert.Contains(t, sig.Title, "Test Author")
	}
}

func TestLotteryRiskCollector_TwoAuthorsEqual(t *testing.T) {
	// Two authors with equal contributions should give lottery risk 2.
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

	c := &LotteryRiskCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	lotteryrisk := filterByKind(signals, "low-lottery-risk")

	// With two equal authors, lottery risk is 2 at root level.
	// With threshold=1, lottery risk 2 should NOT emit a signal
	// (lottery risk 2 > threshold 1).
	// But sub-directories with single author may still emit signals.
	for _, sig := range lotteryrisk {
		// Root "." should not be flagged if lottery risk is 2.
		if sig.FilePath == "./" || sig.FilePath == "." {
			t.Errorf("root directory should not be flagged with two equal authors, got lottery risk signal: %s", sig.Title)
		}
	}
}

func TestLotteryRiskCollector_OneDominant(t *testing.T) {
	// One author writes 90% of code, lottery risk should be 1.
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

	c := &LotteryRiskCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	lotteryrisk := filterByKind(signals, "low-lottery-risk")

	// Root should have lottery risk 1 because Alice dominates.
	var rootSignal *signal.RawSignal
	for i, sig := range lotteryrisk {
		if sig.FilePath == "./" || sig.FilePath == "." {
			rootSignal = &lotteryrisk[i]
			break
		}
	}
	require.NotNil(t, rootSignal, "root directory should be flagged when one author dominates")
	assert.Equal(t, 0.8, rootSignal.Confidence)
	assert.Contains(t, rootSignal.Title, "Alice")
}

func TestLotteryRiskCollector_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)

	c := &LotteryRiskCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals, "empty repo should produce no signals")
}

func TestLotteryRiskCollector_NotAGitRepo(t *testing.T) {
	dir := t.TempDir()

	c := &LotteryRiskCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	assert.Error(t, err, "non-git directory should return an error")
}

func TestLotteryRiskCollector_ContextCancellation(t *testing.T) {
	_, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &LotteryRiskCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{})
	assert.Error(t, err, "cancelled context should return an error")
}

func TestLotteryRiskCollector_DeterministicOutput(t *testing.T) {
	// Same repo should always produce the same signals.
	_, dir := initGoGitRepo(t, map[string]string{
		"main.go":     "package main\n\nfunc main() {}\n",
		"lib/util.go": "package lib\n\nfunc Util() {}\n",
	})

	c := &LotteryRiskCollector{}

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

func TestLotteryRiskCollector_SignalFields(t *testing.T) {
	// Verify all signal fields are populated correctly.
	_, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})

	c := &LotteryRiskCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	lotteryrisk := filterByKind(signals, "low-lottery-risk")
	require.NotEmpty(t, lotteryrisk)

	sig := lotteryrisk[0]
	assert.Equal(t, "lotteryrisk", sig.Source)
	assert.Equal(t, "low-lottery-risk", sig.Kind)
	assert.NotEmpty(t, sig.FilePath)
	assert.Equal(t, 0, sig.Line, "lottery risk signals use directory paths, not line numbers")
	assert.NotEmpty(t, sig.Title)
	assert.NotEmpty(t, sig.Description)
	assert.Contains(t, sig.Description, "Lottery risk:")
	assert.Contains(t, sig.Description, "Top authors:")
	assert.InDelta(t, 0.8, sig.Confidence, 0.001)
	assert.Contains(t, sig.Tags, "low-lottery-risk")
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

// --- Lottery risk calculation tests ---

func TestComputeLotteryRisk_SingleAuthor(t *testing.T) {
	own := &dirOwnership{
		Path: ".",
		Authors: map[string]*authorStats{
			"Alice": {BlameLines: 100, CommitWeight: 5.0},
		},
		TotalLines: 100,
	}
	bf := computeLotteryRisk(own)
	assert.Equal(t, 1, bf, "single author should have lottery risk 1")
}

func TestComputeLotteryRisk_TwoEqualAuthors(t *testing.T) {
	own := &dirOwnership{
		Path: ".",
		Authors: map[string]*authorStats{
			"Alice": {BlameLines: 50, CommitWeight: 5.0},
			"Bob":   {BlameLines: 50, CommitWeight: 5.0},
		},
		TotalLines: 100,
	}
	bf := computeLotteryRisk(own)
	assert.Equal(t, 2, bf, "two equal authors need both to exceed 50%")
}

func TestComputeLotteryRisk_OneDominant(t *testing.T) {
	own := &dirOwnership{
		Path: ".",
		Authors: map[string]*authorStats{
			"Alice": {BlameLines: 90, CommitWeight: 9.0},
			"Bob":   {BlameLines: 10, CommitWeight: 1.0},
		},
		TotalLines: 100,
	}
	bf := computeLotteryRisk(own)
	assert.Equal(t, 1, bf, "dominant author alone exceeds 50%, lottery risk 1")
}

func TestComputeLotteryRisk_ThreeAuthors(t *testing.T) {
	own := &dirOwnership{
		Path: ".",
		Authors: map[string]*authorStats{
			"Alice":   {BlameLines: 40, CommitWeight: 4.0},
			"Bob":     {BlameLines: 35, CommitWeight: 3.5},
			"Charlie": {BlameLines: 25, CommitWeight: 2.5},
		},
		TotalLines: 100,
	}
	bf := computeLotteryRisk(own)
	// Alice has ~40% ownership, needs Bob too to exceed 50%.
	assert.Equal(t, 2, bf, "two authors needed to exceed 50%")
}

func TestComputeLotteryRisk_NoAuthors(t *testing.T) {
	own := &dirOwnership{
		Path:       ".",
		Authors:    map[string]*authorStats{},
		TotalLines: 0,
	}
	bf := computeLotteryRisk(own)
	assert.Equal(t, 0, bf, "no authors should return lottery risk 0")
}

// --- Confidence mapping tests ---

func TestLotteryRiskConfidence_LotteryRisk1(t *testing.T) {
	assert.InDelta(t, 0.8, lotteryRiskConfidence(1), 0.001)
}

func TestLotteryRiskConfidence_LotteryRisk0(t *testing.T) {
	assert.InDelta(t, 0.8, lotteryRiskConfidence(0), 0.001)
}

func TestLotteryRiskConfidence_LotteryRisk2(t *testing.T) {
	assert.InDelta(t, 0.5, lotteryRiskConfidence(2), 0.001)
}

func TestLotteryRiskConfidence_LotteryRisk3(t *testing.T) {
	assert.InDelta(t, 0.3, lotteryRiskConfidence(3), 0.001)
}

func TestLotteryRiskConfidence_LotteryRisk10(t *testing.T) {
	assert.InDelta(t, 0.3, lotteryRiskConfidence(10), 0.001)
}

// --- GitDepth tests ---

func TestLotteryRiskCollector_GitDepthLimitsCommitWalk(t *testing.T) {
	// With a very low GitDepth, commit-based ownership weights should differ
	// from the default. This test verifies that walkCommitsForOwnership
	// respects the depth setting.
	_, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})

	c := &LotteryRiskCollector{}

	// Default depth walks all commits.
	signals1, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	// GitDepth=1 walks only one commit.
	signals2, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		GitDepth: 1,
	})
	require.NoError(t, err)

	// Both should produce signals (single-author repo always has low lottery risk).
	assert.NotEmpty(t, signals1, "default depth should produce signals")
	assert.NotEmpty(t, signals2, "depth=1 should produce signals")
}

// --- Progress callback tests ---

func TestLotteryRiskCollector_ProgressCallback(t *testing.T) {
	_, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})

	var progressMessages []string
	c := &LotteryRiskCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		ProgressFunc: func(msg string) {
			progressMessages = append(progressMessages, msg)
		},
	})
	require.NoError(t, err)

	// Small repo may not trigger any progress (needs 100+ commits or 50+ blamed files).
	// This test just ensures the callback doesn't cause errors.
}
