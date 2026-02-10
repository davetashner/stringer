package collectors

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v68/github"

	"github.com/davetashner/stringer/internal/signal"
)

// reviewParticipation tracks review activity in a directory.
type reviewParticipation struct {
	Reviewers map[string]int // reviewer login -> review count
	Authors   map[string]int // PR author login -> PR count
}

// fetchReviewParticipation fetches merged PRs and their reviews from GitHub,
// then maps review activity to directories based on changed files.
func fetchReviewParticipation(ctx context.Context, ghCtx *githubContext, ownership map[string]*dirOwnership, maxPRs int) (map[string]*reviewParticipation, error) {
	result := make(map[string]*reviewParticipation)

	// Fetch recently merged PRs.
	opts := &github.PullRequestListOptions{
		State:     "closed",
		Sort:      "updated",
		Direction: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	fetched := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		prs, resp, err := ghCtx.API.ListPullRequests(ctx, ghCtx.Owner, ghCtx.Repo, opts)
		if err != nil {
			return nil, fmt.Errorf("listing merged PRs for review analysis: %w", err)
		}

		for _, pr := range prs {
			if !pr.GetMerged() {
				continue
			}
			if fetched >= maxPRs {
				return result, nil
			}
			fetched++

			if err := ctx.Err(); err != nil {
				return nil, err
			}

			// Fetch reviews for this PR.
			reviews, reviewErr := fetchAllReviews(ctx, ghCtx.API, ghCtx.Owner, ghCtx.Repo, pr.GetNumber())
			if reviewErr != nil {
				continue // skip PRs with review fetch errors
			}

			// Fetch files changed in this PR.
			files, _, filesErr := ghCtx.API.ListPullRequestFiles(ctx, ghCtx.Owner, ghCtx.Repo, pr.GetNumber(), &github.ListOptions{PerPage: 100})
			if filesErr != nil {
				continue // skip PRs with file fetch errors
			}

			// Determine which directories this PR touches.
			touchedDirs := make(map[string]bool)
			for _, f := range files {
				dir := findOwningDir(f.GetFilename(), ownership)
				if dir != "" {
					touchedDirs[dir] = true
				}
			}

			// Attribute reviews and authorship to directories.
			for dir := range touchedDirs {
				if result[dir] == nil {
					result[dir] = &reviewParticipation{
						Reviewers: make(map[string]int),
						Authors:   make(map[string]int),
					}
				}

				result[dir].Authors[pr.GetUser().GetLogin()]++

				for _, review := range reviews {
					state := strings.ToUpper(review.GetState())
					if state == "APPROVED" || state == "CHANGES_REQUESTED" {
						result[dir].Reviewers[review.GetUser().GetLogin()]++
					}
				}
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return result, nil
}

// buildReviewConcentrationSignals produces signals for directories where a
// single reviewer handles more than 70% of all reviews.
// If anon is non-nil, reviewer names are anonymized.
func buildReviewConcentrationSignals(reviewData map[string]*reviewParticipation, anon *nameAnonymizer) []signal.RawSignal {
	var signals []signal.RawSignal

	for dir, rp := range reviewData {
		totalReviews := 0
		for _, count := range rp.Reviewers {
			totalReviews += count
		}
		if totalReviews < 3 {
			continue // not enough data to draw conclusions
		}

		for reviewer, count := range rp.Reviewers {
			fraction := float64(count) / float64(totalReviews)
			if fraction > reviewConcentrationThreshold {
				displayName := reviewer
				if anon != nil {
					displayName = anon.anonymize(reviewer)
				}

				signals = append(signals, signal.RawSignal{
					Source:      "lotteryrisk",
					Kind:        "review-concentration",
					FilePath:    dir,
					Line:        0,
					Title:       fmt.Sprintf("Review bottleneck: %s reviews %.0f%% of PRs in %s", displayName, fraction*100, dir),
					Description: fmt.Sprintf("Reviewer %s handled %d of %d reviews (%.0f%%) in %s. Consider distributing review responsibility to reduce knowledge silos.", displayName, count, totalReviews, fraction*100, dir),
					Confidence:  0.6,
					Tags:        []string{"review-concentration"},
				})
			}
		}
	}

	return signals
}
