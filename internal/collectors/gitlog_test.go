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
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

// testAuthor returns a default git signature for test commits.
func testAuthor(when time.Time) *object.Signature {
	return &object.Signature{
		Name:  "Test Author",
		Email: "test@example.com",
		When:  when,
	}
}

// initGoGitRepo creates a new go-git repository in a temp directory with an
// initial commit containing the given files.
func initGoGitRepo(t *testing.T, files map[string]string) (*gogit.Repository, string) {
	t.Helper()
	dir := t.TempDir()

	repo, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)

	wt, err := repo.Worktree()
	require.NoError(t, err)

	for relPath, content := range files {
		absPath := filepath.Join(dir, relPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o750))
		require.NoError(t, os.WriteFile(absPath, []byte(content), 0o600))
		_, err := wt.Add(relPath)
		require.NoError(t, err)
	}

	_, err = wt.Commit("initial commit", &gogit.CommitOptions{
		Author: testAuthor(time.Now()),
	})
	require.NoError(t, err)

	return repo, dir
}

// addCommit modifies a file in the worktree and creates a commit with the
// given message and timestamp.
func addCommit(t *testing.T, repo *gogit.Repository, dir string, file string, content string, msg string, when time.Time) plumbing.Hash {
	t.Helper()
	wt, err := repo.Worktree()
	require.NoError(t, err)

	absPath := filepath.Join(dir, file)
	require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o750))
	require.NoError(t, os.WriteFile(absPath, []byte(content), 0o600))
	_, err = wt.Add(file)
	require.NoError(t, err)

	hash, err := wt.Commit(msg, &gogit.CommitOptions{
		Author: testAuthor(when),
	})
	require.NoError(t, err)
	return hash
}

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
	assert.Contains(t, sig.Tags, "stringer-generated")
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

// --- Helper functions ---

// filterByKind returns only signals of the given kind.
func filterByKind(signals []signal.RawSignal, kind string) []signal.RawSignal {
	var result []signal.RawSignal
	for _, s := range signals {
		if s.Kind == kind {
			result = append(result, s)
		}
	}
	return result
}
