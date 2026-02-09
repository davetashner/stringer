package context

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyzeHistory_NonGitRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := AnalyzeHistory(dir, 4)
	require.Error(t, err)
}

func TestAnalyzeHistory_EmptyRepo(t *testing.T) {
	dir := setupTestRepo(t)
	history, err := AnalyzeHistory(dir, 4)
	require.NoError(t, err)
	assert.Equal(t, 0, history.TotalCommits)
	assert.Empty(t, history.RecentWeeks)
	assert.Empty(t, history.TopAuthors)
}

func TestAnalyzeHistory_WithCommits(t *testing.T) {
	dir := setupTestRepo(t)

	// Create some commits.
	addCommit(t, dir, "initial commit", "alice")
	addCommit(t, dir, "second commit", "bob")
	addCommit(t, dir, "third commit", "alice")

	history, err := AnalyzeHistory(dir, 52) // large window to catch all
	require.NoError(t, err)

	assert.Equal(t, 3, history.TotalCommits)
	assert.NotEmpty(t, history.RecentWeeks)

	// Check author stats.
	require.NotEmpty(t, history.TopAuthors)
	// alice has 2 commits, bob has 1.
	found := false
	for _, a := range history.TopAuthors {
		if a.Name == "alice" {
			assert.Equal(t, 2, a.Commits)
			found = true
		}
	}
	assert.True(t, found, "alice should be in top authors")
}

func TestAnalyzeHistory_DefaultWeeks(t *testing.T) {
	dir := setupTestRepo(t)
	addCommit(t, dir, "test", "dev")

	// weeks=0 should default to 4.
	history, err := AnalyzeHistory(dir, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, history.TotalCommits)
}

func TestAnalyzeHistory_TopAuthorsLimit(t *testing.T) {
	dir := setupTestRepo(t)

	// Create 12 authors.
	for i := 0; i < 12; i++ {
		author := string(rune('a'+i)) + "-author"
		addCommit(t, dir, "commit", author)
	}

	history, err := AnalyzeHistory(dir, 52)
	require.NoError(t, err)

	assert.Equal(t, 12, history.TotalCommits)
	assert.LessOrEqual(t, len(history.TopAuthors), 10)
}

func TestStartOfWeek(t *testing.T) {
	tests := []struct {
		name  string
		input time.Time
		want  time.Time
	}{
		{
			name:  "monday",
			input: time.Date(2026, 2, 2, 15, 30, 0, 0, time.UTC), // Monday
			want:  time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "wednesday",
			input: time.Date(2026, 2, 4, 10, 0, 0, 0, time.UTC), // Wednesday
			want:  time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "sunday",
			input: time.Date(2026, 2, 8, 23, 59, 0, 0, time.UTC), // Sunday
			want:  time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "saturday",
			input: time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC), // Saturday
			want:  time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := startOfWeek(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFirstLine(t *testing.T) {
	assert.Equal(t, "first", firstLine("first\nsecond\nthird"))
	assert.Equal(t, "only", firstLine("only"))
	assert.Equal(t, "", firstLine(""))
	assert.Equal(t, "", firstLine("\nrest"))
}

func TestSemverTagPattern(t *testing.T) {
	tests := []struct {
		input string
		match bool
	}{
		{"v1.2.3", true},
		{"v0.1.0", true},
		{"1.0.0", true},
		{"v10.20.30", true},
		{"v1.2.3-rc1", true}, // prefix matches
		{"latest", false},
		{"release-1", false},
		{"v1.2", false},
		{"v1", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.match, semverTagPattern.MatchString(tt.input))
		})
	}
}

func TestAnalyzeHistory_WithTags(t *testing.T) {
	dir := setupTestRepo(t)

	addCommitAt(t, dir, "initial commit", "alice", "2026-02-04T12:00:00Z")
	run(t, dir, "git", "tag", "v1.0.0")

	addCommitAt(t, dir, "second commit", "bob", "2026-02-05T12:00:00Z")
	run(t, dir, "git", "tag", "v1.1.0")

	history, err := analyzeHistoryWithNow(dir, 52, time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	// Should have 2 milestones.
	require.Len(t, history.Milestones, 2)
	// Newest first.
	assert.Equal(t, "v1.1.0", history.Milestones[0].Name)
	assert.Equal(t, "v1.0.0", history.Milestones[1].Name)

	// Tagged commits should have Tag field set.
	require.NotEmpty(t, history.RecentWeeks)
	var taggedCommits []CommitSummary
	for _, week := range history.RecentWeeks {
		for _, c := range week.Commits {
			if c.Tag != "" {
				taggedCommits = append(taggedCommits, c)
			}
		}
	}
	assert.Len(t, taggedCommits, 2)
}

func TestAnalyzeHistory_WeekTags(t *testing.T) {
	dir := setupTestRepo(t)

	addCommit(t, dir, "initial commit", "alice")
	run(t, dir, "git", "tag", "v1.0.0")

	history, err := analyzeHistoryWithNow(dir, 52, time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	// The week containing the tagged commit should have Tags populated.
	require.NotEmpty(t, history.RecentWeeks)
	found := false
	for _, week := range history.RecentWeeks {
		if len(week.Tags) > 0 {
			assert.Equal(t, "v1.0.0", week.Tags[0].Name)
			found = true
		}
	}
	assert.True(t, found, "expected a week with tags")
}

func TestAnalyzeHistory_NonSemverTagsIgnored(t *testing.T) {
	dir := setupTestRepo(t)

	addCommit(t, dir, "initial commit", "alice")
	run(t, dir, "git", "tag", "latest")
	run(t, dir, "git", "tag", "release-candidate")

	history, err := analyzeHistoryWithNow(dir, 52, time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	assert.Empty(t, history.Milestones)
}

func TestAnalyzeHistory_MergeCommit(t *testing.T) {
	dir := setupTestRepo(t)

	// Create a commit on main.
	addCommit(t, dir, "initial on main", "alice")

	// Detect the default branch name (master or main depending on git config).
	defaultBranch := getDefaultBranch(t, dir)

	// Create a branch and add a commit.
	run(t, dir, "git", "checkout", "-b", "feature")
	addCommit(t, dir, "feature work", "bob")

	// Switch back and create a merge commit.
	run(t, dir, "git", "checkout", defaultBranch)
	addCommit(t, dir, "main work", "alice")
	run(t, dir, "git", "merge", "feature", "--no-ff", "-m", "Merge feature branch")

	history, err := analyzeHistoryWithNow(dir, 52, time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	// Find the merge commit.
	var mergeFound bool
	for _, week := range history.RecentWeeks {
		for _, c := range week.Commits {
			if c.Message == "Merge feature branch" {
				assert.True(t, c.IsMerge, "merge commit should have IsMerge=true")
				mergeFound = true
			}
		}
	}
	assert.True(t, mergeFound, "should have found merge commit")

	// Non-merge commits should have IsMerge=false.
	for _, week := range history.RecentWeeks {
		for _, c := range week.Commits {
			if c.Message == "initial on main" {
				assert.False(t, c.IsMerge, "regular commit should have IsMerge=false")
			}
		}
	}
}

func TestCollectTags_TruncatesAt10(t *testing.T) {
	dir := setupTestRepo(t)

	// Create 12 tagged commits.
	for i := 0; i < 12; i++ {
		dateISO := fmt.Sprintf("2026-01-%02dT12:00:00Z", i+1)
		addCommitAt(t, dir, fmt.Sprintf("commit-%d", i), "alice", dateISO)
		run(t, dir, "git", "tag", fmt.Sprintf("v1.%d.0", i))
	}

	history, err := analyzeHistoryWithNow(dir, 52, time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	assert.LessOrEqual(t, len(history.Milestones), 10, "milestones should be capped at 10")
	// Newest first: v1.11.0 should be first.
	assert.Equal(t, "v1.11.0", history.Milestones[0].Name)
}

func TestCollectTags_AnnotatedTag(t *testing.T) {
	dir := setupTestRepo(t)

	addCommitAt(t, dir, "initial commit", "alice", "2026-02-04T12:00:00Z")

	// Create an annotated tag.
	runAt(t, dir, "2026-02-04T12:00:00Z", "git",
		"-c", "user.name=alice",
		"-c", "user.email=alice@test.com",
		"tag", "-a", "v2.0.0", "-m", "Release v2.0.0")

	history, err := analyzeHistoryWithNow(dir, 52, time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	require.Len(t, history.Milestones, 1)
	assert.Equal(t, "v2.0.0", history.Milestones[0].Name)
}

func TestAnalyzeHistory_CommitsOutsideWindow(t *testing.T) {
	dir := setupTestRepo(t)

	// Create an old commit outside the 1-week window.
	addCommitAt(t, dir, "old commit", "alice", "2025-01-01T12:00:00Z")
	// Create a recent commit inside the window.
	addCommitAt(t, dir, "recent commit", "bob", "2026-02-04T12:00:00Z")

	// Use a 1-week window from 2026-02-05.
	history, err := analyzeHistoryWithNow(dir, 1, time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	// Both commits should be counted in total.
	assert.Equal(t, 2, history.TotalCommits)

	// Only the recent commit should appear in weeks.
	recentCount := 0
	for _, week := range history.RecentWeeks {
		recentCount += len(week.Commits)
	}
	assert.Equal(t, 1, recentCount, "only 1 commit should be in the recent weeks window")
}

// --- Test Helpers ---

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "test")
	return dir
}

func addCommit(t *testing.T, dir, message, author string) {
	t.Helper()
	// Create a unique file for each commit.
	f := filepath.Join(dir, message+"-"+author+".txt")
	require.NoError(t, os.WriteFile(f, []byte(message), 0o600))
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git",
		"-c", "user.name="+author,
		"-c", "user.email="+author+"@test.com",
		"commit", "-m", message)
}

func addCommitAt(t *testing.T, dir, message, author, dateISO string) {
	t.Helper()
	f := filepath.Join(dir, message+"-"+author+".txt")
	require.NoError(t, os.WriteFile(f, []byte(message), 0o600))
	runAt(t, dir, dateISO, "git", "add", "-A")
	runAt(t, dir, dateISO, "git",
		"-c", "user.name="+author,
		"-c", "user.email="+author+"@test.com",
		"commit", "-m", message)
}

func runAt(t *testing.T, dir, dateISO string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+dateISO, "GIT_COMMITTER_DATE="+dateISO)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command %s %v failed: %s", name, args, string(out))
}

func getDefaultBranch(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git branch failed: %s", string(out))
	return strings.TrimSpace(string(out))
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-02-05T12:00:00Z", "GIT_COMMITTER_DATE=2026-02-05T12:00:00Z")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command %s %v failed: %s", name, args, string(out))
}
