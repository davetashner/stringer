package context

import (
	"os"
	"os/exec"
	"path/filepath"
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

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-02-05T12:00:00Z", "GIT_COMMITTER_DATE=2026-02-05T12:00:00Z")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command %s %v failed: %s", name, args, string(out))
}
