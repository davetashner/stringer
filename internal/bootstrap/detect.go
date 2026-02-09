// Package bootstrap implements the `stringer init` command, which detects
// repository characteristics and generates a starter configuration.
package bootstrap

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5"
)

// GitHubRemote holds the parsed owner/repo from a GitHub remote URL.
type GitHubRemote struct {
	Owner string
	Repo  string
}

// sshPattern matches git@github.com:owner/repo.git SSH URLs.
var sshPattern = regexp.MustCompile(`^git@github\.com:([^/]+)/([^/]+?)(?:\.git)?$`)

// DetectGitHubRemote opens the git repository at repoPath and checks whether
// the origin remote points to GitHub. Returns nil (not an error) when the
// directory is not a git repo or the remote is not GitHub.
func DetectGitHubRemote(repoPath string) *GitHubRemote {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil
	}

	remotes, err := repo.Remotes()
	if err != nil {
		return nil
	}

	// Find origin remote.
	var originURLs []string
	for _, r := range remotes {
		if r.Config().Name == "origin" {
			originURLs = r.Config().URLs
			break
		}
	}
	if len(originURLs) == 0 {
		return nil
	}

	owner, repoName, err := parseGitHubURL(originURLs[0])
	if err != nil {
		return nil
	}

	return &GitHubRemote{Owner: owner, Repo: repoName}
}

// parseGitHubURL parses a GitHub URL (HTTPS or SSH) into owner and repo.
func parseGitHubURL(rawURL string) (owner, repo string, err error) {
	// Try SSH format: git@github.com:owner/repo.git
	if m := sshPattern.FindStringSubmatch(rawURL); m != nil {
		return m[1], m[2], nil
	}

	// Try HTTPS format: https://github.com/owner/repo.git
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("parsing URL %q: %w", rawURL, err)
	}

	if parsed.Host != "github.com" {
		return "", "", fmt.Errorf("remote %q is not a GitHub URL", rawURL)
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("cannot parse owner/repo from %q", rawURL)
	}

	owner = parts[0]
	repo = strings.TrimSuffix(parts[1], ".git")
	return owner, repo, nil
}
