// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

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

// rootOwnership returns an ownership map holding only the repository root,
// which receives every changed file via findOwningDir.
func rootOwnership() map[string]*dirOwnership {
	return map[string]*dirOwnership{
		".": {Path: ".", Authors: make(map[string]*authorStats)},
	}
}

// assertSameWeights compares commit weights per author. Weights decay with
// the wall clock at walk time, so they are compared within a tolerance.
func assertSameWeights(t *testing.T, want, got *dirOwnership) {
	t.Helper()
	require.Len(t, got.Authors, len(want.Authors))
	for name, stats := range want.Authors {
		require.Contains(t, got.Authors, name)
		assert.InDelta(t, stats.CommitWeight, got.Authors[name].CommitWeight, 1e-6, name)
	}
}

func TestWalkCommitsForOwnership_SharedAcrossWorkspaces(t *testing.T) {
	repo, root := initGoGitRepo(t, map[string]string{
		"pkg/a/a.go": "package a\n",
		"pkg/b/b.go": "package b\n",
	})
	now := time.Now()
	addCommitAs(t, repo, root, "pkg/a/a.go", "package a\n// 1\n", "feat: a", now, "Alice", "alice@example.com")
	addCommitAs(t, repo, root, "pkg/b/b.go", "package b\n// 1\n", "feat: b", now, "Bob", "bob@example.com")

	resetNumstatHistoryCache()
	t.Cleanup(resetNumstatHistoryCache)
	ctx := context.Background()

	// First workspace runs git log --numstat.
	own1 := rootOwnership()
	require.NoError(t, walkCommitsForOwnership(ctx, root, own1, signal.CollectorOpts{}, workspaceScope{}))
	hits, misses := numstatHistories.stats()
	assert.Equal(t, 0, hits)
	assert.Equal(t, 1, misses)
	require.Contains(t, own1["."].Authors, "Alice")
	require.Contains(t, own1["."].Authors, "Bob")

	// Second workspace reuses the parsed commits.
	own2 := rootOwnership()
	require.NoError(t, walkCommitsForOwnership(ctx, root, own2, signal.CollectorOpts{}, workspaceScope{}))
	hits, misses = numstatHistories.stats()
	assert.Equal(t, 1, hits, "second workspace must reuse the numstat walk")
	assert.Equal(t, 1, misses)
	assertSameWeights(t, own1["."], own2["."])

	// Uncached walk after reset produces the same attribution.
	resetNumstatHistoryCache()
	own3 := rootOwnership()
	require.NoError(t, walkCommitsForOwnership(ctx, root, own3, signal.CollectorOpts{}, workspaceScope{}))
	_, misses = numstatHistories.stats()
	assert.Equal(t, 1, misses, "reset must force a new walk")
	assertSameWeights(t, own1["."], own3["."])

	// Depth is part of the key.
	require.NoError(t, walkCommitsForOwnership(ctx, root, rootOwnership(), signal.CollectorOpts{GitDepth: 1}, workspaceScope{}))
	_, misses = numstatHistories.stats()
	assert.Equal(t, 2, misses, "GitDepth must be part of the cache key")

	// A new commit moves HEAD and invalidates the entry.
	addCommitAs(t, repo, root, "pkg/b/b.go", "package b\n// 2\n", "feat: b again", now, "Carol", "carol@example.com")
	own4 := rootOwnership()
	require.NoError(t, walkCommitsForOwnership(ctx, root, own4, signal.CollectorOpts{}, workspaceScope{}))
	_, misses = numstatHistories.stats()
	assert.Equal(t, 3, misses, "HEAD must be part of the cache key")
	assert.Contains(t, own4["."].Authors, "Carol", "fresh walk must see the new commit")
}

func TestRepoHeadHash(t *testing.T) {
	assert.Equal(t, "", repoHeadHash(t.TempDir()), "not a repository")

	empty := t.TempDir()
	_, err := gogit.PlainInit(empty, false)
	require.NoError(t, err)
	assert.Equal(t, "", repoHeadHash(empty), "empty repository has no HEAD")

	repo, dir := initGoGitRepo(t, map[string]string{"main.go": "package main\n"})
	head, err := repo.Head()
	require.NoError(t, err)
	assert.Equal(t, head.Hash().String(), repoHeadHash(dir))
}
