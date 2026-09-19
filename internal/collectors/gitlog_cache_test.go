// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

// initMonorepoFixture builds a repository with two workspaces (pkg/a and
// pkg/b) whose history produces every gitlog signal kind: a high-churn file
// in pkg/a, a revert touching pkg/b, and a stale branch. It returns the
// repository root.
func initMonorepoFixture(t *testing.T) (*gogit.Repository, string) {
	t.Helper()
	repo, dir := initGoGitRepo(t, map[string]string{
		"pkg/a/a.go": "package a\n",
		"pkg/b/b.go": "package b\n",
	})

	// Stale branch at a 60-day-old commit.
	head, err := repo.Head()
	require.NoError(t, err)
	oldHash := addCommit(t, repo, dir, "pkg/b/old.go", "package b\n",
		"feat: old feature", time.Now().AddDate(0, 0, -60))
	require.NoError(t, repo.Storer.SetReference(
		plumbing.NewHashReference(plumbing.NewBranchReferenceName("old-feature"), oldHash)))
	require.NoError(t, repo.Storer.SetReference(
		plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), head.Hash())))
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("main"),
		Force:  true,
	}))

	// Churn in pkg/a.
	now := time.Now()
	for i := 0; i < 12; i++ {
		addCommitAs(t, repo, dir, "pkg/a/hot.go", fmt.Sprintf("package a\n// change %d\n", i),
			fmt.Sprintf("chore: tweak hot.go (%d)", i), now.Add(-time.Duration(i)*time.Hour),
			fmt.Sprintf("Author %d", i%3), "author@example.com")
	}

	// Revert in pkg/b.
	addCommit(t, repo, dir, "pkg/b/b.go", "package b\n\nfunc B() {}\n", "feat: add B", now)
	addCommit(t, repo, dir, "pkg/b/b.go", "package b\n", `Revert "feat: add B"`, now)

	return repo, dir
}

// collectWorkspace runs the gitlog collector the way the scan pipeline does
// for one workspace of a monorepo: repoPath is the workspace directory and
// GitRoot is the repository root.
func collectWorkspace(t *testing.T, c *GitlogCollector, root, rel string, opts signal.CollectorOpts) ([]signal.RawSignal, *GitlogMetrics) {
	t.Helper()
	opts.GitRoot = root
	signals, err := c.Collect(context.Background(), filepath.Join(root, rel), opts)
	require.NoError(t, err)
	m, ok := c.Metrics().(*GitlogMetrics)
	require.True(t, ok)
	return signals, m
}

func TestGitlogCollector_CachedWorkspaceOutputMatchesUncached(t *testing.T) {
	_, root := initMonorepoFixture(t)
	resetGitlogHistoryCache()
	t.Cleanup(resetGitlogHistoryCache)

	c := &GitlogCollector{}

	// First workspace walks the history and populates the cache.
	sigA, metricsA := collectWorkspace(t, c, root, "pkg/a", signal.CollectorOpts{})
	hits, misses := gitlogHistories.stats()
	assert.Equal(t, 0, hits)
	assert.Equal(t, 1, misses)

	// Second workspace of the same scan is served from the cache.
	sigBCached, metricsBCached := collectWorkspace(t, c, root, "pkg/b", signal.CollectorOpts{})
	hits, misses = gitlogHistories.stats()
	assert.Equal(t, 1, hits, "second workspace must reuse the first walk")
	assert.Equal(t, 1, misses)

	// Same workspace again with the cache dropped: a fresh, uncached walk.
	resetGitlogHistoryCache()
	sigBFresh, metricsBFresh := collectWorkspace(t, c, root, "pkg/b", signal.CollectorOpts{})
	_, misses = gitlogHistories.stats()
	assert.Equal(t, 1, misses, "reset must force a new walk")

	assert.Equal(t, sigBFresh, sigBCached, "cached workspace signals must equal the uncached walk")
	assert.Equal(t, metricsBFresh, metricsBCached, "cached workspace metrics must equal the uncached walk")

	// The shared history is repository-wide, so metrics agree across
	// workspaces, while each workspace keeps only its own files with
	// workspace-relative paths (stringer-nxx.17).
	assert.Equal(t, metricsA, metricsBCached)
	require.Len(t, filterByKind(sigA, "churn"), 1)
	assert.Equal(t, "hot.go", filterByKind(sigA, "churn")[0].FilePath)
	assert.Empty(t, filterByKind(sigA, "revert"), "pkg/a does not own the reverted file")

	assert.Empty(t, filterByKind(sigBCached, "churn"), "pkg/b does not own the churned file")
	require.Len(t, filterByKind(sigBCached, "revert"), 1)
	assert.Equal(t, "b.go", filterByKind(sigBCached, "revert")[0].FilePath)
	// Without a member list every workspace is repository-wide for stale branches.
	require.Len(t, filterByKind(sigBCached, "stale-branch"), 1)
	assert.Equal(t, "old-feature", filterByKind(sigBCached, "stale-branch")[0].FilePath)
	assert.Equal(t, 1, metricsBCached.RevertCount)
	assert.Equal(t, 1, metricsBCached.StaleBranchCount)
	assert.Len(t, metricsBCached.FileChurns, 2, "metrics cover the whole repository")
}

func TestGitlogCollector_CacheKeyCoversWalkOptions(t *testing.T) {
	repo, root := initMonorepoFixture(t)
	resetGitlogHistoryCache()
	t.Cleanup(resetGitlogHistoryCache)

	c := &GitlogCollector{}
	collectWorkspace(t, c, root, "pkg/a", signal.CollectorOpts{})

	// A different depth is a different walk.
	sigs, _ := collectWorkspace(t, c, root, "pkg/b", signal.CollectorOpts{GitDepth: 5})
	_, misses := gitlogHistories.stats()
	assert.Equal(t, 2, misses, "GitDepth must be part of the cache key")
	assert.Empty(t, filterByKind(sigs, "churn"), "depth 5 sees too few commits for churn")

	// A different since window is a different walk.
	collectWorkspace(t, c, root, "pkg/b", signal.CollectorOpts{GitSince: "1d"})
	_, misses = gitlogHistories.stats()
	assert.Equal(t, 3, misses, "GitSince must be part of the cache key")

	// Back to the defaults: the single-entry cache was evicted, so walk again.
	collectWorkspace(t, c, root, "pkg/b", signal.CollectorOpts{})
	_, misses = gitlogHistories.stats()
	assert.Equal(t, 4, misses)

	// A new commit moves HEAD and invalidates the entry.
	addCommit(t, repo, root, "pkg/b/b.go", "package b\n// again\n", `Revert "feat: add B"`, time.Now())
	sigs, _ = collectWorkspace(t, c, root, "pkg/b", signal.CollectorOpts{})
	hits, misses := gitlogHistories.stats()
	assert.Equal(t, 0, hits)
	assert.Equal(t, 5, misses, "HEAD must be part of the cache key")
	assert.Len(t, filterByKind(sigs, "revert"), 2, "fresh walk must see the new commit")
}

func TestGitlogCollector_CacheHitHonoursCancelledContext(t *testing.T) {
	_, root := initMonorepoFixture(t)
	resetGitlogHistoryCache()
	t.Cleanup(resetGitlogHistoryCache)

	c := &GitlogCollector{}
	collectWorkspace(t, c, root, "pkg/a", signal.CollectorOpts{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Collect(ctx, filepath.Join(root, "pkg/b"), signal.CollectorOpts{GitRoot: root})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestGitlogCollector_ConcurrentWorkspacesShareOneWalk(t *testing.T) {
	_, root := initMonorepoFixture(t)
	resetGitlogHistoryCache()
	t.Cleanup(resetGitlogHistoryCache)

	const n = 8
	results := make([][]signal.RawSignal, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c := &GitlogCollector{}
			rel := "pkg/a"
			if i%2 == 1 {
				rel = "pkg/b"
			}
			results[i], errs[i] = c.Collect(context.Background(), filepath.Join(root, rel),
				signal.CollectorOpts{GitRoot: root})
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
		assert.Equal(t, results[i%2], results[i], "every run of a workspace must see the same history")
	}
	hits, misses := gitlogHistories.stats()
	assert.Equal(t, 1, misses, "concurrent collectors must share a single walk")
	assert.Equal(t, n-1, hits)
}

func TestGitlogCollector_CachedSignalsAreIndependentCopies(t *testing.T) {
	_, root := initMonorepoFixture(t)
	resetGitlogHistoryCache()
	t.Cleanup(resetGitlogHistoryCache)

	c := &GitlogCollector{}
	first, _ := collectWorkspace(t, c, root, "pkg/b", signal.CollectorOpts{})
	reverts := filterByKind(first, "revert")
	require.Len(t, reverts, 1)
	reverts[0].Tags[0] = "mutated"
	reverts[0].FilePath = "mutated"

	second, _ := collectWorkspace(t, c, root, "pkg/b", signal.CollectorOpts{})
	got := filterByKind(second, "revert")
	require.Len(t, got, 1)
	assert.Equal(t, "b.go", got[0].FilePath)
	assert.Equal(t, []string{"revert", "historical-path"}, got[0].Tags)
}

func TestCloneSignals_Nil(t *testing.T) {
	assert.Nil(t, cloneSignals(nil))
	assert.Empty(t, cloneSignals([]signal.RawSignal{}))
}
