package collectors

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
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

// addCommitAs modifies a file in the worktree and creates a commit with the
// given message, timestamp, and custom author name/email. This is used for
// multi-author test scenarios.
func addCommitAs(t *testing.T, repo *gogit.Repository, dir string, file string, content string, msg string, when time.Time, authorName string, authorEmail string) plumbing.Hash {
	t.Helper()
	wt, err := repo.Worktree()
	require.NoError(t, err)

	absPath := filepath.Join(dir, file)
	require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o750))
	require.NoError(t, os.WriteFile(absPath, []byte(content), 0o600))
	_, err = wt.Add(file)
	require.NoError(t, err)

	hash, err := wt.Commit(msg, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  authorName,
			Email: authorEmail,
			When:  when,
		},
	})
	require.NoError(t, err)
	return hash
}

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
