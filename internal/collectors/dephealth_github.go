// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-github/v68/github"

	"github.com/davetashner/stringer/internal/signal"
)

// defaultStalenessThreshold is 2 years — repos with no push activity beyond
// this are flagged as stale.
const defaultStalenessThreshold = 2 * 365 * 24 * time.Hour

// dephealthGitHubAPI is a narrow interface for the GitHub API calls needed by
// the dephealth collector. It is a subset of the full githubAPI interface.
type dephealthGitHubAPI interface {
	GetRepository(ctx context.Context, owner, repo string) (*github.Repository, *github.Response, error)
}

// extractGitHubOwnerRepo extracts the GitHub owner and repo from a Go module
// path. Returns ok=false for non-GitHub modules.
// Examples:
//
//	"github.com/foo/bar"      → "foo", "bar", true
//	"github.com/foo/bar/v2"   → "foo", "bar", true
//	"github.com/foo/bar/pkg"  → "foo", "bar", true
//	"golang.org/x/mod"        → "", "", false
func extractGitHubOwnerRepo(modulePath string) (owner, repo string, ok bool) {
	if !strings.HasPrefix(modulePath, "github.com/") {
		return "", "", false
	}
	parts := strings.SplitN(modulePath, "/", 4) // ["github.com", owner, repo, ...]
	if len(parts) < 3 {
		return "", "", false
	}
	return parts[1], parts[2], true
}

// repoKey returns a dedup key for a GitHub repo.
func repoKey(owner, repo string) string {
	return owner + "/" + repo
}

// checkGitHubDeps queries the GitHub API for each unique GitHub-hosted
// dependency and emits signals for archived and stale repositories.
func (r *registryRun) checkGitHubDeps(ctx context.Context, api dephealthGitHubAPI, deps []ModuleDep, stalenessThreshold time.Duration) []signal.RawSignal {
	// Dedup GitHub-hosted modules by repo before capping, so the cap counts
	// unique repositories rather than module paths.
	seen := make(map[string]bool)
	var repos [][2]string
	for _, dep := range deps {
		owner, repo, ok := extractGitHubOwnerRepo(dep.Path)
		if !ok || seen[repoKey(owner, repo)] {
			continue
		}
		seen[repoKey(owner, repo)] = true
		repos = append(repos, [2]string{owner, repo})
	}

	return lookupEach(ctx, r, "github", repos, func(ctx context.Context, pair [2]string) []signal.RawSignal {
		owner, repo := pair[0], pair[1]
		ghRepo, _, err := api.GetRepository(ctx, owner, repo)
		if err != nil {
			r.lookupFailed("github", repoKey(owner, repo), err)
			return nil
		}

		if ghRepo.GetArchived() {
			// Archived repos skip the stale check (avoid double-flagging).
			return []signal.RawSignal{{
				Source:      "dephealth",
				Kind:        "archived-dependency",
				FilePath:    "go.mod",
				Title:       fmt.Sprintf("Archived dependency: %s/%s", owner, repo),
				Description: fmt.Sprintf("GitHub repository %s/%s is archived. Archived repos receive no updates, bug fixes, or security patches. Consider migrating to an actively maintained alternative.", owner, repo),
				Confidence:  0.9,
				Tags:        []string{"archived-dependency", "dephealth"},
			}}
		}

		pushedAt := ghRepo.GetPushedAt()
		if pushedAt.IsZero() || time.Since(pushedAt.Time) <= stalenessThreshold {
			return nil
		}
		return []signal.RawSignal{{
			Source:      "dephealth",
			Kind:        "stale-dependency",
			FilePath:    "go.mod",
			Title:       fmt.Sprintf("Stale dependency: %s/%s", owner, repo),
			Description: fmt.Sprintf("GitHub repository %s/%s has not been pushed to since %s (>%d months). The project may be unmaintained.", owner, repo, pushedAt.Format("2006-01-02"), int(stalenessThreshold.Hours()/24/30)),
			Confidence:  0.6,
			Tags:        []string{"stale-dependency", "dephealth"},
		}}
	})
}
