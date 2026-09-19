// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

// initMonorepoFixtureWithRoot extends initMonorepoFixture with a high-churn
// file at the repository root that belongs to no member workspace.
func initMonorepoFixtureWithRoot(t *testing.T) string {
	t.Helper()
	repo, root := initMonorepoFixture(t)
	now := time.Now()
	for i := 0; i < 11; i++ {
		addCommit(t, repo, root, "Makefile", fmt.Sprintf("all:\n\t@echo %d\n", i),
			fmt.Sprintf("build: tweak Makefile (%d)", i), now.Add(-time.Duration(i)*time.Minute))
	}
	return root
}

// scanWorkspaces runs the gitlog collector once per workspace the way the
// scan pipeline does, stamping each workspace's signal paths with its
// relative path afterwards (see cmd/stringer stampWorkspace), and returns
// the combined signals.
func scanWorkspaces(t *testing.T, root string, rels []string) []signal.RawSignal {
	t.Helper()
	var all []signal.RawSignal
	for _, rel := range rels {
		opts := signal.CollectorOpts{WorkspaceMembers: rels}
		repoPath := root
		if rel != "." {
			repoPath = filepath.Join(root, filepath.FromSlash(rel))
			opts.GitRoot = root
		}
		c := &GitlogCollector{}
		sigs, err := c.Collect(context.Background(), repoPath, opts)
		require.NoError(t, err, rel)
		for i := range sigs {
			sigs[i].Workspace = rel
			if rel != "." && sigs[i].Kind != "stale-branch" {
				sigs[i].FilePath = path.Join(rel, sigs[i].FilePath) // as stampWorkspace does
			}
		}
		all = append(all, sigs...)
	}
	return all
}

func signalPaths(signals []signal.RawSignal) []string {
	paths := make([]string, 0, len(signals))
	for _, s := range signals {
		paths = append(paths, s.FilePath)
	}
	sort.Strings(paths)
	return paths
}

func TestGitlogCollector_WorkspacesEmitEachSignalOnce(t *testing.T) {
	root := initMonorepoFixtureWithRoot(t)
	resetGitlogHistoryCache()
	t.Cleanup(resetGitlogHistoryCache)

	// Single-root run: the reference set of repository-relative paths.
	single, err := (&GitlogCollector{}).Collect(context.Background(), root, signal.CollectorOpts{})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"Makefile", "pkg/a/hot.go"}, signalPaths(filterByKind(single, "churn")))
	require.Equal(t, []string{"pkg/b/b.go"}, signalPaths(filterByKind(single, "revert")))
	require.Equal(t, []string{"old-feature"}, signalPaths(filterByKind(single, "stale-branch")))

	for _, order := range [][]string{
		{".", "pkg/a", "pkg/b"},
		{"pkg/a", "pkg/b", "."},
	} {
		t.Run(fmt.Sprintf("%v", order), func(t *testing.T) {
			combined := scanWorkspaces(t, root, order)

			// Every signal of the single-root run appears exactly once, with
			// the same repository-relative path, and nothing else appears.
			assert.Equal(t, signalPaths(single), signalPaths(combined))

			churn := filterByKind(combined, "churn")
			assert.Equal(t, []string{"Makefile", "pkg/a/hot.go"}, signalPaths(churn))
			for _, s := range churn {
				assert.Contains(t, s.Title, s.FilePath, "churn titles keep the repository-relative path")
			}
			byPath := map[string]string{}
			for _, s := range churn {
				byPath[s.FilePath] = s.Workspace
			}
			assert.Equal(t, ".", byPath["Makefile"], "root files belong to the root workspace")
			assert.Equal(t, "pkg/a", byPath["pkg/a/hot.go"])

			reverts := filterByKind(combined, "revert")
			require.Len(t, reverts, 1)
			assert.Equal(t, "pkg/b/b.go", reverts[0].FilePath)
			assert.Equal(t, "pkg/b", reverts[0].Workspace)

			stale := filterByKind(combined, "stale-branch")
			require.Len(t, stale, 1, "stale branches are emitted once per scan")
			assert.Equal(t, "old-feature", stale[0].FilePath)
			assert.Equal(t, order[0], stale[0].Workspace, "by the first scanned workspace")
		})
	}
}

func TestGitlogCollector_WorkspacesWithoutRootMember(t *testing.T) {
	root := initMonorepoFixtureWithRoot(t)
	resetGitlogHistoryCache()
	t.Cleanup(resetGitlogHistoryCache)

	// npm-style layouts list only packages: root-level files belong to no
	// scanned workspace and are not reported, and nothing is duplicated.
	combined := scanWorkspaces(t, root, []string{"pkg/b", "pkg/a"})
	assert.Equal(t, []string{"pkg/a/hot.go"}, signalPaths(filterByKind(combined, "churn")))
	assert.Equal(t, []string{"pkg/b/b.go"}, signalPaths(filterByKind(combined, "revert")))
	stale := filterByKind(combined, "stale-branch")
	require.Len(t, stale, 1)
	assert.Equal(t, "pkg/b", stale[0].Workspace)
}

func TestGitlogCollector_WorkspaceMetricsStayRepositoryWide(t *testing.T) {
	root := initMonorepoFixtureWithRoot(t)
	resetGitlogHistoryCache()
	t.Cleanup(resetGitlogHistoryCache)

	single := &GitlogCollector{}
	_, err := single.Collect(context.Background(), root, signal.CollectorOpts{})
	require.NoError(t, err)

	member := &GitlogCollector{}
	sigs, err := member.Collect(context.Background(), filepath.Join(root, "pkg", "b"), signal.CollectorOpts{
		GitRoot:          root,
		WorkspaceMembers: []string{"pkg/a", "pkg/b"},
	})
	require.NoError(t, err)
	assert.Empty(t, filterByKind(sigs, "stale-branch"), "pkg/b is not the first workspace")
	assert.Equal(t, single.Metrics(), member.Metrics(), "report metrics describe the whole repository")
}

func TestGitlogCollector_SubdirectoryScanScopesToSubdirectory(t *testing.T) {
	root := initMonorepoFixtureWithRoot(t)
	resetGitlogHistoryCache()
	t.Cleanup(resetGitlogHistoryCache)

	// A plain subdirectory scan (no workspace members) keeps the
	// subdirectory's own files, relative to it, and the repository-wide
	// stale branches.
	sigs, err := (&GitlogCollector{}).Collect(context.Background(), filepath.Join(root, "pkg", "a"),
		signal.CollectorOpts{GitRoot: root})
	require.NoError(t, err)
	assert.Equal(t, []string{"hot.go"}, signalPaths(filterByKind(sigs, "churn")))
	assert.Empty(t, filterByKind(sigs, "revert"))
	assert.Equal(t, []string{"old-feature"}, signalPaths(filterByKind(sigs, "stale-branch")))
}

func TestGitlogCollector_NonMonorepoOutputUnchanged(t *testing.T) {
	root := initMonorepoFixtureWithRoot(t)
	resetGitlogHistoryCache()
	t.Cleanup(resetGitlogHistoryCache)

	plain, err := (&GitlogCollector{}).Collect(context.Background(), root, signal.CollectorOpts{})
	require.NoError(t, err)
	withRoot, err := (&GitlogCollector{}).Collect(context.Background(), root, signal.CollectorOpts{GitRoot: root})
	require.NoError(t, err)
	assert.Equal(t, plain, withRoot)
	assert.Equal(t, []string{"Makefile", "pkg/a/hot.go"}, signalPaths(filterByKind(plain, "churn")))
}
