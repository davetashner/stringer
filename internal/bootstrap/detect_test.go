// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package bootstrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectGitHubRemote_HTTPS(t *testing.T) {
	dir := initGitRepo(t, "https://github.com/octocat/hello-world.git")

	remote := DetectGitHubRemote(dir)
	require.NotNil(t, remote)
	assert.Equal(t, "octocat", remote.Owner)
	assert.Equal(t, "hello-world", remote.Repo)
}

func TestDetectGitHubRemote_SSH(t *testing.T) {
	dir := initGitRepo(t, "git@github.com:octocat/hello-world.git")

	remote := DetectGitHubRemote(dir)
	require.NotNil(t, remote)
	assert.Equal(t, "octocat", remote.Owner)
	assert.Equal(t, "hello-world", remote.Repo)
}

func TestDetectGitHubRemote_NonGitDir(t *testing.T) {
	dir := t.TempDir()
	remote := DetectGitHubRemote(dir)
	assert.Nil(t, remote)
}

func TestDetectGitHubRemote_NonGitHubRemote(t *testing.T) {
	dir := initGitRepo(t, "https://gitlab.com/owner/repo.git")

	remote := DetectGitHubRemote(dir)
	assert.Nil(t, remote)
}

func TestDetectGitHubRemote_NoOrigin(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	// No remote added.

	remote := DetectGitHubRemote(dir)
	assert.Nil(t, remote)
}

func TestParseGitHubURL_Variants(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		owner   string
		repo    string
		wantErr bool
	}{
		{"https", "https://github.com/foo/bar.git", "foo", "bar", false},
		{"https no .git", "https://github.com/foo/bar", "foo", "bar", false},
		{"ssh", "git@github.com:foo/bar.git", "foo", "bar", false},
		{"ssh no .git", "git@github.com:foo/bar", "foo", "bar", false},
		{"not github", "https://gitlab.com/foo/bar.git", "", "", true},
		{"malformed", "not-a-url://[invalid", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := parseGitHubURL(tt.url)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.owner, owner)
			assert.Equal(t, tt.repo, repo)
		})
	}
}

// initGitRepo creates a temporary git repo with an origin remote.
func initGitRepo(t *testing.T, remoteURL string) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "remote", "add", "origin", remoteURL)

	// Create an initial commit so the repo is valid.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o600))
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.name=Test", "-c", "user.email=test@test.com", "commit", "-m", "init")

	return dir
}

// runGit runs a git command in the given directory.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, out)
}
