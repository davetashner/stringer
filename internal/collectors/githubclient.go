package collectors

import (
	"log/slog"
	"os"

	"github.com/google/go-github/v68/github"
)

// githubContext holds a GitHub API client and the parsed owner/repo.
// It is shared between the GitHub collector and the lottery risk collector.
type githubContext struct {
	Owner string
	Repo  string
	API   githubAPI
}

// newGitHubContext creates a githubContext for the given repo path.
// Returns nil if GITHUB_TOKEN is not set or the remote is not a GitHub URL.
func newGitHubContext(repoPath string) *githubContext {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil
	}

	owner, repo, err := parseGitHubRemote(repoPath)
	if err != nil {
		slog.Debug("cannot determine GitHub remote for lottery risk review analysis", "error", err)
		return nil
	}

	client := github.NewClient(nil).WithAuthToken(token)
	return &githubContext{
		Owner: owner,
		Repo:  repo,
		API:   &realGitHubAPI{client: client},
	}
}
