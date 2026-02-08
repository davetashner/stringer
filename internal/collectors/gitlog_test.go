package collectors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

// --- Revert detection tests ---

func TestGitlogCollector_Name(t *testing.T) {
	c := &GitlogCollector{}
	assert.Equal(t, "gitlog", c.Name())
}

func TestGitlogCollector_RevertDetected_SubjectPattern(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	// Make a normal commit.
	addCommit(t, repo, dir, "main.go", "package main\n\nfunc Foo() {}\n",
		"feat: add Foo function", time.Now())

	// Make a revert commit using the standard "Revert "..."" pattern.
	addCommit(t, repo, dir, "main.go", "package main\n",
		`Revert "feat: add Foo function"`, time.Now())

	c := &GitlogCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	reverts := filterByKind(signals, "revert")
	require.Len(t, reverts, 1)

	sig := reverts[0]
	assert.Equal(t, "gitlog", sig.Source)
	assert.Equal(t, "revert", sig.Kind)
	assert.Contains(t, sig.Title, "feat: add Foo function")
	assert.Equal(t, 0.7, sig.Confidence)
	assert.Contains(t, sig.Tags, "revert")
	assert.NotEmpty(t, sig.Author)
	assert.NotEmpty(t, sig.Description)
}

func TestGitlogCollector_RevertDetected_PrefixPattern(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	addCommit(t, repo, dir, "main.go", "package main\n\nfunc Bar() {}\n",
		"feat: add Bar", time.Now())

	addCommit(t, repo, dir, "main.go", "package main\n",
		"revert: add Bar", time.Now())

	c := &GitlogCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	reverts := filterByKind(signals, "revert")
	require.Len(t, reverts, 1)
	assert.Contains(t, reverts[0].Title, "add Bar")
}

func TestGitlogCollector_RevertDetected_BodyPattern(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	hash := addCommit(t, repo, dir, "main.go", "package main\n\nfunc Baz() {}\n",
		"feat: add Baz", time.Now())

	addCommit(t, repo, dir, "main.go", "package main\n",
		fmt.Sprintf("Revert \"feat: add Baz\"\n\nThis reverts commit %s.", hash.String()),
		time.Now())

	c := &GitlogCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	reverts := filterByKind(signals, "revert")
	require.Len(t, reverts, 1)
	assert.Contains(t, reverts[0].Description, hash.String()[:7])
}

func TestGitlogCollector_NoRevertForNormalCommit(t *testing.T) {
	_, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	c := &GitlogCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	reverts := filterByKind(signals, "revert")
	assert.Empty(t, reverts)
}

// --- Churn detection tests ---

func TestGitlogCollector_ChurnDetected(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"hot.go": "package main\n",
	})

	// Modify the same file 12 times (above churnThreshold of 10).
	now := time.Now()
	for i := 0; i < 12; i++ {
		content := fmt.Sprintf("package main\n// change %d\n", i)
		addCommit(t, repo, dir, "hot.go", content,
			fmt.Sprintf("chore: tweak hot.go (%d)", i),
			now.Add(-time.Duration(i)*24*time.Hour))
	}

	c := &GitlogCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	churn := filterByKind(signals, "churn")
	require.Len(t, churn, 1)

	sig := churn[0]
	assert.Equal(t, "gitlog", sig.Source)
	assert.Equal(t, "churn", sig.Kind)
	assert.Equal(t, "hot.go", sig.FilePath)
	assert.Contains(t, sig.Title, "hot.go")
	assert.Contains(t, sig.Title, "12 times")
	assert.GreaterOrEqual(t, sig.Confidence, 0.4)
	assert.LessOrEqual(t, sig.Confidence, 0.8)
	assert.Contains(t, sig.Tags, "churn")
	assert.Contains(t, sig.Description, "Test Author")
}

func TestGitlogCollector_ChurnNotDetected_FewChanges(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"stable.go": "package main\n",
	})

	// Only 3 modifications (well below threshold of 10).
	now := time.Now()
	for i := 0; i < 3; i++ {
		content := fmt.Sprintf("package main\n// change %d\n", i)
		addCommit(t, repo, dir, "stable.go", content,
			fmt.Sprintf("chore: tweak stable.go (%d)", i),
			now.Add(-time.Duration(i)*24*time.Hour))
	}

	c := &GitlogCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	churn := filterByKind(signals, "churn")
	assert.Empty(t, churn)
}

func TestGitlogCollector_ChurnConfidenceScaling(t *testing.T) {
	// 10 changes -> 0.4 confidence
	assert.InDelta(t, 0.4, churnConfidence(10), 0.001)

	// 20 changes -> 0.6 confidence (midpoint)
	assert.InDelta(t, 0.6, churnConfidence(20), 0.001)

	// 30 changes -> 0.8 confidence (max)
	assert.InDelta(t, 0.8, churnConfidence(30), 0.001)

	// 50 changes -> still 0.8 (capped)
	assert.InDelta(t, 0.8, churnConfidence(50), 0.001)
}

func TestGitlogCollector_ChurnIgnoresOldCommits(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"old.go": "package main\n",
	})

	// 12 modifications, all older than 90 days.
	old := time.Now().AddDate(0, 0, -100)
	for i := 0; i < 12; i++ {
		content := fmt.Sprintf("package main\n// old change %d\n", i)
		addCommit(t, repo, dir, "old.go", content,
			fmt.Sprintf("chore: old tweak (%d)", i),
			old.Add(-time.Duration(i)*24*time.Hour))
	}

	c := &GitlogCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	churn := filterByKind(signals, "churn")
	assert.Empty(t, churn, "commits outside the 90-day window should not contribute to churn")
}

// --- Stale branch detection tests ---

func TestGitlogCollector_StaleBranchDetected(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	// Get HEAD hash for branching.
	head, err := repo.Head()
	require.NoError(t, err)

	// Create a branch pointing at a commit that is 60 days old.
	oldTime := time.Now().AddDate(0, 0, -60)
	oldHash := addCommit(t, repo, dir, "feature.go", "package main\n",
		"feat: old feature", oldTime)

	// Create a branch reference pointing at that commit.
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName("old-feature"), oldHash)
	require.NoError(t, repo.Storer.SetReference(ref))

	// Move HEAD back to main so the repo has a normal head.
	mainRef := plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), head.Hash())
	require.NoError(t, repo.Storer.SetReference(mainRef))

	// Reset the working tree to main.
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("main"),
		Force:  true,
	}))

	// Add a fresh commit on main to advance it.
	addCommit(t, repo, dir, "main.go", "package main\n// fresh\n",
		"chore: fresh commit", time.Now())

	c := &GitlogCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	stale := filterByKind(signals, "stale-branch")
	require.Len(t, stale, 1)

	sig := stale[0]
	assert.Equal(t, "gitlog", sig.Source)
	assert.Equal(t, "stale-branch", sig.Kind)
	assert.Equal(t, "old-feature", sig.FilePath)
	assert.Contains(t, sig.Title, "old-feature")
	assert.Contains(t, sig.Title, "days ago")
	assert.GreaterOrEqual(t, sig.Confidence, 0.3)
	assert.LessOrEqual(t, sig.Confidence, 0.6)
	assert.Contains(t, sig.Tags, "stale-branch")
	assert.NotEmpty(t, sig.Description)
}

func TestGitlogCollector_ProtectedBranchesExcluded(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	// The repo already has a "main" or "master" branch. Ensure it is not
	// flagged as stale even if the last commit is old.
	head, err := repo.Head()
	require.NoError(t, err)

	// Create branches named after protected branches pointing at an old commit.
	oldTime := time.Now().AddDate(0, 0, -120)
	oldHash := addCommit(t, repo, dir, "x.go", "package main\n",
		"old commit", oldTime)

	for _, name := range []string{"main", "master", "develop"} {
		ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(name), oldHash)
		require.NoError(t, repo.Storer.SetReference(ref))
	}

	// Restore HEAD.
	headRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))
	require.NoError(t, repo.Storer.SetReference(headRef))
	_ = head // keep the linter happy

	c := &GitlogCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	stale := filterByKind(signals, "stale-branch")
	for _, sig := range stale {
		assert.NotContains(t, []string{"main", "master", "develop"}, sig.FilePath,
			"protected branch %q should not appear in stale-branch signals", sig.FilePath)
	}
}

func TestGitlogCollector_StaleBranchConfidenceScaling(t *testing.T) {
	// 30 days -> 0.3 confidence
	assert.InDelta(t, 0.3, staleBranchConfidence(30), 0.001)

	// 60 days -> 0.45 confidence (midpoint)
	assert.InDelta(t, 0.45, staleBranchConfidence(60), 0.001)

	// 90 days -> 0.6 confidence (max)
	assert.InDelta(t, 0.6, staleBranchConfidence(90), 0.001)

	// 180 days -> still 0.6 (capped)
	assert.InDelta(t, 0.6, staleBranchConfidence(180), 0.001)
}

// --- Edge cases ---

func TestGitlogCollector_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)

	c := &GitlogCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals, "empty repo should produce no signals")
}

func TestGitlogCollector_NotAGitRepo(t *testing.T) {
	dir := t.TempDir()

	c := &GitlogCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	assert.Error(t, err, "non-git directory should return an error")
}

func TestGitlogCollector_ContextCancellation(t *testing.T) {
	_, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &GitlogCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{})
	assert.Error(t, err, "cancelled context should return an error")
}

// --- firstFileName edge case tests ---

func TestFirstFileName_EmptySlice(t *testing.T) {
	result := firstFileName(nil)
	assert.Equal(t, "", result, "empty/nil slice should return empty string")

	result = firstFileName([]string{})
	assert.Equal(t, "", result, "empty slice should return empty string")
}

func TestFirstFileName_SingleElement(t *testing.T) {
	result := firstFileName([]string{"main.go"})
	assert.Equal(t, "main.go", result)
}

func TestFirstFileName_MultipleElements(t *testing.T) {
	result := firstFileName([]string{"first.go", "second.go", "third.go"})
	assert.Equal(t, "first.go", result)
}

// --- shortHash edge case tests ---

func TestShortHash_NormalHash(t *testing.T) {
	result := shortHash("abc1234567890")
	assert.Equal(t, "abc1234", result, "should truncate to 7 chars")
}

func TestShortHash_ExactlySeven(t *testing.T) {
	result := shortHash("abc1234")
	assert.Equal(t, "abc1234", result, "7-char input should return unchanged")
}

func TestShortHash_ShorterThanSeven(t *testing.T) {
	result := shortHash("abc")
	assert.Equal(t, "abc", result, "short input should return unchanged")
}

func TestShortHash_EmptyString(t *testing.T) {
	result := shortHash("")
	assert.Equal(t, "", result, "empty input should return empty string")
}

func TestShortHash_FullSHA1(t *testing.T) {
	full := "da39a3ee5e6b4b0d3255bfef95601890afd80709"
	result := shortHash(full)
	assert.Equal(t, "da39a3e", result)
	assert.Len(t, result, 7)
}

// --- firstLine edge case tests ---

func TestFirstLine_NoNewline(t *testing.T) {
	result := firstLine("single line no newline")
	assert.Equal(t, "single line no newline", result)
}

func TestFirstLine_WithNewline(t *testing.T) {
	result := firstLine("first line\nsecond line\nthird line")
	assert.Equal(t, "first line", result)
}

func TestFirstLine_EmptyString(t *testing.T) {
	result := firstLine("")
	assert.Equal(t, "", result)
}

func TestFirstLine_OnlyNewline(t *testing.T) {
	result := firstLine("\n")
	assert.Equal(t, "", result)
}

// --- sortedKeys edge case tests ---

func TestSortedKeys_EmptyMap(t *testing.T) {
	result := sortedKeys(map[string]bool{})
	assert.Empty(t, result)
}

func TestSortedKeys_NilMap(t *testing.T) {
	result := sortedKeys(nil)
	assert.Empty(t, result)
}

func TestSortedKeys_SortOrder(t *testing.T) {
	m := map[string]bool{"charlie": true, "alice": true, "bob": true}
	result := sortedKeys(m)
	assert.Equal(t, []string{"alice", "bob", "charlie"}, result)
}

// --- buildChurnSignals edge case tests ---

func TestBuildChurnSignals_EmptyMaps(t *testing.T) {
	result := buildChurnSignals(map[string]int{}, map[string]map[string]bool{})
	assert.Empty(t, result, "empty maps should produce no signals")
}

func TestBuildChurnSignals_AllBelowThreshold(t *testing.T) {
	changes := map[string]int{"a.go": 5, "b.go": 3}
	authors := map[string]map[string]bool{
		"a.go": {"Alice": true},
		"b.go": {"Bob": true},
	}
	result := buildChurnSignals(changes, authors)
	assert.Empty(t, result, "changes below threshold should produce no signals")
}

func TestBuildChurnSignals_ExactThreshold(t *testing.T) {
	changes := map[string]int{"at-threshold.go": churnThreshold}
	authors := map[string]map[string]bool{
		"at-threshold.go": {"Alice": true},
	}
	result := buildChurnSignals(changes, authors)
	assert.Len(t, result, 1, "exactly at threshold should produce a signal")
	assert.Equal(t, "churn", result[0].Kind)
	assert.Equal(t, "at-threshold.go", result[0].FilePath)
	assert.Contains(t, result[0].Description, "Alice")
}

func TestBuildChurnSignals_MultipleAuthors(t *testing.T) {
	changes := map[string]int{"hot.go": 15}
	authors := map[string]map[string]bool{
		"hot.go": {"Alice": true, "Bob": true, "Charlie": true},
	}
	result := buildChurnSignals(changes, authors)
	require.Len(t, result, 1)
	// Authors should be sorted.
	assert.Contains(t, result[0].Description, "Alice, Bob, Charlie")
}

func TestBuildChurnSignals_SortedByFilePath(t *testing.T) {
	changes := map[string]int{"z.go": 12, "a.go": 11, "m.go": 15}
	authors := map[string]map[string]bool{
		"z.go": {"Alice": true},
		"a.go": {"Bob": true},
		"m.go": {"Charlie": true},
	}
	result := buildChurnSignals(changes, authors)
	require.Len(t, result, 3)
	assert.Equal(t, "a.go", result[0].FilePath)
	assert.Equal(t, "m.go", result[1].FilePath)
	assert.Equal(t, "z.go", result[2].FilePath)
}

// --- changedFiles edge case tests ---

func TestChangedFiles_RootCommit(t *testing.T) {
	repo, _ := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	// Get the initial commit (which is a root commit).
	head, err := repo.Head()
	require.NoError(t, err)

	commit, err := repo.CommitObject(head.Hash())
	require.NoError(t, err)

	files, err := changedFiles(commit)
	require.NoError(t, err)
	assert.Empty(t, files, "root commit should return empty file list")
}

func TestChangedFiles_NormalCommit(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	addCommit(t, repo, dir, "new.go", "package main\n", "add new file", time.Now())

	head, err := repo.Head()
	require.NoError(t, err)

	commit, err := repo.CommitObject(head.Hash())
	require.NoError(t, err)

	files, err := changedFiles(commit)
	require.NoError(t, err)
	assert.Contains(t, files, "new.go")
}

// --- detectRevert edge case tests ---

func TestDetectRevert_NotARevert(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	hash := addCommit(t, repo, dir, "main.go", "package main\n// new\n",
		"feat: normal commit", time.Now())

	commit, err := repo.CommitObject(hash)
	require.NoError(t, err)

	_, ok := detectRevert(commit)
	assert.False(t, ok, "normal commit should not be detected as revert")
}

func TestDetectRevert_BodyOnlyPattern(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	// Create a commit with only the body pattern (no subject match).
	hash := addCommit(t, repo, dir, "main.go", "package main\n// changed\n",
		"Undo previous change\n\nThis reverts commit abc1234567890.", time.Now())

	commit, err := repo.CommitObject(hash)
	require.NoError(t, err)

	sig, ok := detectRevert(commit)
	assert.True(t, ok, "body-only revert pattern should be detected")
	assert.Contains(t, sig.Title, "abc1234")
}

// --- Gitlog Collector Collect edge cases ---

func TestGitlogCollector_RevertWithBodyHash_NoSubject(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	// A commit that has a revert body pattern but NOT a standard subject pattern.
	addCommit(t, repo, dir, "main.go", "package main\n// updated\n",
		"Undo changes\n\nThis reverts commit 1234567890abcdef.", time.Now())

	c := &GitlogCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	reverts := filterByKind(signals, "revert")
	require.Len(t, reverts, 1)
	// When there's no subject match, the title should contain shortHash.
	assert.Contains(t, reverts[0].Title, "1234567")
}

// --- staleBranchConfidence edge case tests ---

func TestStaleBranchConfidence_BelowMin(t *testing.T) {
	// 30 days is the minimum.
	conf := staleBranchConfidence(30)
	assert.InDelta(t, 0.3, conf, 0.001)
}

func TestStaleBranchConfidence_AboveMax(t *testing.T) {
	// 365 days should still cap at 0.6.
	conf := staleBranchConfidence(365)
	assert.InDelta(t, 0.6, conf, 0.001)
}

// --- churnConfidence edge case tests ---

func TestChurnConfidence_AtThreshold(t *testing.T) {
	assert.InDelta(t, 0.4, churnConfidence(churnThreshold), 0.001)
}

func TestChurnConfidence_WellAbove(t *testing.T) {
	assert.InDelta(t, 0.8, churnConfidence(100), 0.001)
}

// --- changedFiles: delete-only change (ch.To.Name empty, fallback to ch.From.Name) ---

func TestChangedFiles_DeletedFile(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go":   "package main\n",
		"delete.go": "package main\n// will be deleted\n",
	})

	// Delete the file and commit.
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, "delete.go")))
	_, err = wt.Remove("delete.go")
	require.NoError(t, err)
	_, err = wt.Commit("chore: delete file", &gogit.CommitOptions{
		Author: testAuthor(time.Now()),
	})
	require.NoError(t, err)

	head, err := repo.Head()
	require.NoError(t, err)
	commit, err := repo.CommitObject(head.Hash())
	require.NoError(t, err)

	files, err := changedFiles(commit)
	require.NoError(t, err)
	assert.Contains(t, files, "delete.go", "deleted file should appear in changed files via From.Name")
}

// --- Collect: context cancelled between walkCommits and detectStaleBranches ---

func TestGitlogCollector_ContextCancelledBetweenWalkAndStaleBranch(t *testing.T) {
	// An empty repo's walkCommits returns nil,nil,nil (head not found, graceful).
	// Then ctx.Err() is checked before detectStaleBranches. If context is cancelled,
	// this should return the context error.
	dir := t.TempDir()
	_, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling Collect

	c := &GitlogCollector{}
	_, err = c.Collect(ctx, dir, signal.CollectorOpts{})
	assert.Error(t, err, "cancelled context should propagate after walkCommits succeeds for empty repo")
	assert.ErrorIs(t, err, context.Canceled)
}

// --- detectStaleBranches: context cancellation during refs iteration ---

func TestGitlogCollector_StaleBranch_ContextCancelledDuringRefs(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	// Create multiple stale branches.
	oldTime := time.Now().AddDate(0, 0, -60)
	for i := 0; i < 5; i++ {
		hash := addCommit(t, repo, dir, fmt.Sprintf("f%d.go", i), "package main\n",
			fmt.Sprintf("old-%d", i), oldTime)
		ref := plumbing.NewHashReference(
			plumbing.NewBranchReferenceName(fmt.Sprintf("stale-%d", i)), hash)
		require.NoError(t, repo.Storer.SetReference(ref))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	c := &GitlogCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{})
	assert.Error(t, err, "cancelled context should propagate as error")
}

// --- Gitlog Collect: walkCommits with context cancel between walks and stale branch ---

func TestGitlogCollector_ContextCancelledAfterWalkBeforeStaleBranch(t *testing.T) {
	// This test creates a scenario where walkCommits succeeds but
	// context is cancelled before detectStaleBranches.
	_, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &GitlogCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{})
	assert.Error(t, err)
}

// --- detectStaleBranches: multiple stale branches sorted ---

func TestGitlogCollector_MultipleStaleBranchesSorted(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	head, err := repo.Head()
	require.NoError(t, err)

	oldTime := time.Now().AddDate(0, 0, -60)

	// Create two stale branches with names that test sort order.
	for _, name := range []string{"z-branch", "a-branch"} {
		hash := addCommit(t, repo, dir, name+".go", "package main\n",
			"feat: "+name, oldTime)
		ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName(name), hash)
		require.NoError(t, repo.Storer.SetReference(ref))
	}

	// Reset to main.
	mainRef := plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), head.Hash())
	require.NoError(t, repo.Storer.SetReference(mainRef))
	wt, err := repo.Worktree()
	require.NoError(t, err)
	require.NoError(t, wt.Checkout(&gogit.CheckoutOptions{
		Branch: plumbing.NewBranchReferenceName("main"),
		Force:  true,
	}))
	addCommit(t, repo, dir, "main.go", "package main\n// fresh\n",
		"chore: fresh", time.Now())

	c := &GitlogCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	stale := filterByKind(signals, "stale-branch")
	require.Len(t, stale, 2, "expected 2 stale branches")

	// Verify sort order (a-branch before z-branch).
	assert.Equal(t, "a-branch", stale[0].FilePath)
	assert.Equal(t, "z-branch", stale[1].FilePath)
}

// --- detectStaleBranches: context cancelled during ref iteration ---

func TestDetectStaleBranches_ContextCancelledDuringIteration(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	// Create a non-protected branch so iteration enters the loop body.
	oldTime := time.Now().AddDate(0, 0, -60)
	hash := addCommit(t, repo, dir, "feat.go", "package main\n",
		"feat: old", oldTime)
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName("old-feat"), hash)
	require.NoError(t, repo.Storer.SetReference(ref))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &GitlogCollector{}
	_, err := c.detectStaleBranches(ctx, repo)
	assert.Error(t, err, "cancelled context should propagate from refs.ForEach")
}

// --- detectStaleBranches: active branch not flagged ---

func TestGitlogCollector_ActiveBranchNotFlagged(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	// Create a branch with a very recent commit.
	recentHash := addCommit(t, repo, dir, "new.go", "package main\n",
		"recent commit", time.Now())
	ref := plumbing.NewHashReference(plumbing.NewBranchReferenceName("active-feature"), recentHash)
	require.NoError(t, repo.Storer.SetReference(ref))

	c := &GitlogCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	stale := filterByKind(signals, "stale-branch")
	for _, s := range stale {
		assert.NotEqual(t, "active-feature", s.FilePath,
			"recently active branch should not be flagged as stale")
	}
}

// parseDuration tests have been moved to duration_test.go.

// --- GitDepth tests ---

func TestGitlogCollector_GitDepthLimitsCommits(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	// Create 15 commits modifying the same file (to trigger churn at default depth).
	now := time.Now()
	for i := 0; i < 15; i++ {
		content := fmt.Sprintf("package main\n// change %d\n", i)
		addCommit(t, repo, dir, "hot.go", content,
			fmt.Sprintf("chore: tweak hot.go (%d)", i),
			now.Add(-time.Duration(i)*24*time.Hour))
	}

	// With default depth (1000), all 15 commits are seen, triggering churn.
	c := &GitlogCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	churn := filterByKind(signals, "churn")
	require.Len(t, churn, 1, "all commits should be walked at default depth")

	// With GitDepth=5, only 5 commits are seen, not enough for churn threshold.
	signals, err = c.Collect(context.Background(), dir, signal.CollectorOpts{
		GitDepth: 5,
	})
	require.NoError(t, err)
	churn = filterByKind(signals, "churn")
	assert.Empty(t, churn, "with depth 5, only 5 commits seen, below churn threshold of 10")
}

// --- Progress callback tests ---

func TestGitlogCollector_ProgressCallback(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	// Create 250 commits to trigger progress at 100 mark.
	now := time.Now()
	for i := 0; i < 250; i++ {
		content := fmt.Sprintf("package main\n// change %d\n", i)
		addCommit(t, repo, dir, "file.go", content,
			fmt.Sprintf("chore: tweak (%d)", i),
			now.Add(-time.Duration(i)*time.Hour))
	}

	var progressMessages []string
	c := &GitlogCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		ProgressFunc: func(msg string) {
			progressMessages = append(progressMessages, msg)
		},
	})
	require.NoError(t, err)

	// With 250+ commits (plus initial), progress should fire at least twice (100, 200).
	assert.GreaterOrEqual(t, len(progressMessages), 2,
		"expected at least 2 progress messages for 250+ commits")
	for _, msg := range progressMessages {
		assert.Contains(t, msg, "gitlog: examined")
	}
}

// --- GitSince tests ---

func TestGitlogCollector_GitSinceFiltersOldCommits(t *testing.T) {
	repo, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})

	// Create 12 commits, all old (150+ days ago), which would normally trigger churn.
	old := time.Now().AddDate(0, 0, -200)
	for i := 0; i < 12; i++ {
		content := fmt.Sprintf("package main\n// old change %d\n", i)
		addCommit(t, repo, dir, "hot.go", content,
			fmt.Sprintf("chore: old tweak (%d)", i),
			old.Add(-time.Duration(i)*24*time.Hour))
	}

	// Without GitSince, old commits outside churn window don't trigger churn
	// (the churn window is 90 days), so this test verifies GitSince works at
	// the log level. Add recent commits too.
	now := time.Now()
	for i := 0; i < 12; i++ {
		content := fmt.Sprintf("package main\n// recent change %d\n", i)
		addCommit(t, repo, dir, "hot.go", content,
			fmt.Sprintf("chore: recent tweak (%d)", i),
			now.Add(-time.Duration(i)*24*time.Hour))
	}

	// With no git-since, churn is detected from recent commits.
	c := &GitlogCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	churn := filterByKind(signals, "churn")
	assert.NotEmpty(t, churn, "should detect churn from recent commits")

	// With git-since="1d", only very recent commits are considered.
	signals, err = c.Collect(context.Background(), dir, signal.CollectorOpts{
		GitSince: "1d",
	})
	require.NoError(t, err)
	churn = filterByKind(signals, "churn")
	assert.Empty(t, churn, "with git-since=1d, very few commits seen, below churn threshold")
}
