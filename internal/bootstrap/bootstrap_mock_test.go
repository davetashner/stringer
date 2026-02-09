package bootstrap

import (
	"fmt"
	"os"
	"testing"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/testable"
)

// --- GenerateConfig mock tests ---

func TestGenerateConfig_WriteFileFailure(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		StatFn: func(_ string) (os.FileInfo, error) {
			return nil, os.ErrNotExist // config does not exist
		},
		WriteFileFn: func(_ string, _ []byte, _ os.FileMode) error {
			return fmt.Errorf("disk full")
		},
	}

	_, err := GenerateConfig("/fake/repo", true, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing .stringer.yaml")
	assert.Contains(t, err.Error(), "disk full")
}

func TestGenerateConfig_ForceSkipsStat(t *testing.T) {
	// When force=true, Stat should not be called — config is always regenerated.
	oldFS := FS
	defer func() { FS = oldFS }()

	statCalled := false
	dir := t.TempDir()
	FS = &testable.MockFileSystem{
		StatFn: func(_ string) (os.FileInfo, error) {
			statCalled = true
			return nil, nil
		},
	}

	action, err := GenerateConfig(dir, false, true)
	require.NoError(t, err)
	assert.False(t, statCalled, "Stat should not be called when force=true")
	assert.Equal(t, "created", action.Operation)
}

// --- AppendAgentSnippet mock tests ---

func TestAppendAgentSnippet_ReadFileError(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		ReadFileFn: func(_ string) ([]byte, error) {
			return nil, fmt.Errorf("permission denied")
		},
	}

	_, err := AppendAgentSnippet("/fake/repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading AGENTS.md")
	assert.Contains(t, err.Error(), "permission denied")
}

func TestAppendAgentSnippet_WriteFileFailure_Create(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		ReadFileFn: func(_ string) ([]byte, error) {
			return nil, os.ErrNotExist // file does not exist
		},
		WriteFileFn: func(_ string, _ []byte, _ os.FileMode) error {
			return fmt.Errorf("read-only filesystem")
		},
	}

	_, err := AppendAgentSnippet("/fake/repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating AGENTS.md")
	assert.Contains(t, err.Error(), "read-only filesystem")
}

func TestAppendAgentSnippet_WriteFileFailure_Update(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		ReadFileFn: func(_ string) ([]byte, error) {
			return []byte("# Existing AGENTS.md\n"), nil // file exists, no markers
		},
		WriteFileFn: func(_ string, _ []byte, _ os.FileMode) error {
			return fmt.Errorf("disk full")
		},
	}

	_, err := AppendAgentSnippet("/fake/repo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "updating AGENTS.md")
	assert.Contains(t, err.Error(), "disk full")
}

// --- DetectGitHubRemote mock tests ---

func TestDetectGitHubRemote_PlainOpenFailure(t *testing.T) {
	oldOpener := GitOpener
	defer func() { GitOpener = oldOpener }()

	GitOpener = &testable.MockGitOpener{
		OpenErr: git.ErrRepositoryNotExists,
	}

	remote := DetectGitHubRemote("/not/a/repo")
	assert.Nil(t, remote)
}

func TestDetectGitHubRemote_RemotesFailure(t *testing.T) {
	oldOpener := GitOpener
	defer func() { GitOpener = oldOpener }()

	GitOpener = &testable.MockGitOpener{
		Repo: &testable.MockGitRepository{
			RemotesErr: fmt.Errorf("corrupted remotes"),
		},
	}

	remote := DetectGitHubRemote("/some/repo")
	assert.Nil(t, remote)
}

func TestDetectGitHubRemote_NoOriginRemote(t *testing.T) {
	oldOpener := GitOpener
	defer func() { GitOpener = oldOpener }()

	// Create a remote named "upstream" instead of "origin".
	upstreamRemote := git.NewRemote(nil, &gitconfig.RemoteConfig{
		Name: "upstream",
		URLs: []string{"https://github.com/foo/bar.git"},
	})

	GitOpener = &testable.MockGitOpener{
		Repo: &testable.MockGitRepository{
			RemotesList: []*git.Remote{upstreamRemote},
		},
	}

	remote := DetectGitHubRemote("/some/repo")
	assert.Nil(t, remote)
}

func TestDetectGitHubRemote_NonGitHubOrigin(t *testing.T) {
	oldOpener := GitOpener
	defer func() { GitOpener = oldOpener }()

	originRemote := git.NewRemote(nil, &gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://gitlab.com/foo/bar.git"},
	})

	GitOpener = &testable.MockGitOpener{
		Repo: &testable.MockGitRepository{
			RemotesList: []*git.Remote{originRemote},
		},
	}

	remote := DetectGitHubRemote("/some/repo")
	assert.Nil(t, remote)
}

func TestDetectGitHubRemote_MockSuccessHTTPS(t *testing.T) {
	oldOpener := GitOpener
	defer func() { GitOpener = oldOpener }()

	originRemote := git.NewRemote(nil, &gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://github.com/testowner/testrepo.git"},
	})

	GitOpener = &testable.MockGitOpener{
		Repo: &testable.MockGitRepository{
			RemotesList: []*git.Remote{originRemote},
		},
	}

	remote := DetectGitHubRemote("/some/repo")
	require.NotNil(t, remote)
	assert.Equal(t, "testowner", remote.Owner)
	assert.Equal(t, "testrepo", remote.Repo)
}

func TestDetectGitHubRemote_MockSuccessSSH(t *testing.T) {
	oldOpener := GitOpener
	defer func() { GitOpener = oldOpener }()

	originRemote := git.NewRemote(nil, &gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{"git@github.com:sshowner/sshrepo.git"},
	})

	GitOpener = &testable.MockGitOpener{
		Repo: &testable.MockGitRepository{
			RemotesList: []*git.Remote{originRemote},
		},
	}

	remote := DetectGitHubRemote("/some/repo")
	require.NotNil(t, remote)
	assert.Equal(t, "sshowner", remote.Owner)
	assert.Equal(t, "sshrepo", remote.Repo)
}

func TestDetectGitHubRemote_EmptyURLs(t *testing.T) {
	oldOpener := GitOpener
	defer func() { GitOpener = oldOpener }()

	originRemote := git.NewRemote(nil, &gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{},
	})

	GitOpener = &testable.MockGitOpener{
		Repo: &testable.MockGitRepository{
			RemotesList: []*git.Remote{originRemote},
		},
	}

	remote := DetectGitHubRemote("/some/repo")
	assert.Nil(t, remote)
}

// --- Run mock tests ---

func TestRun_GenerateConfigFailure(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	// Set up FS to fail on WriteFile (which GenerateConfig calls).
	// Stat returns ErrNotExist so it tries to create.
	FS = &testable.MockFileSystem{
		StatFn: func(_ string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
		WriteFileFn: func(_ string, _ []byte, _ os.FileMode) error {
			return fmt.Errorf("write failed")
		},
	}

	// Use a real temp dir so docs.Analyze can scan it.
	dir := t.TempDir()

	_, err := Run(InitConfig{RepoPath: dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

func TestRun_AppendAgentSnippetFailure(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	dir := t.TempDir()

	callCount := 0
	FS = &testable.MockFileSystem{
		StatFn: func(_ string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
		WriteFileFn: func(_ string, _ []byte, _ os.FileMode) error {
			callCount++
			if callCount == 1 {
				return nil // GenerateConfig succeeds on first write
			}
			return fmt.Errorf("agents write failed") // AppendAgentSnippet fails
		},
		ReadFileFn: func(_ string) ([]byte, error) {
			return nil, os.ErrNotExist // AGENTS.md does not exist
		},
	}

	_, err := Run(InitConfig{RepoPath: dir})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agents write failed")
}
