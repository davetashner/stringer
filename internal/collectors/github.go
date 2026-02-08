// Package collectors provides signal extraction modules for stringer.
package collectors

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/google/go-github/v68/github"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

// Default configuration values for the GitHub collector.
const (
	defaultCommentDepth          = 30
	defaultMaxIssuesPerCollector = 100
	defaultIncludePRs            = true
)

// actionablePattern matches comment text containing actionable language.
var actionablePattern = regexp.MustCompile(`(?i)\b(TODO|FIXME|should|needs|must)\b`)

// sshRemotePattern matches git@github.com:owner/repo.git SSH URLs.
var sshRemotePattern = regexp.MustCompile(`^git@github\.com:([^/]+)/([^/]+?)(?:\.git)?$`)

func init() {
	collector.Register(&GitHubCollector{})
}

// githubAPI abstracts the GitHub API for testing.
type githubAPI interface {
	ListIssues(ctx context.Context, owner, repo string, opts *github.IssueListByRepoOptions) ([]*github.Issue, *github.Response, error)
	ListPullRequests(ctx context.Context, owner, repo string, opts *github.PullRequestListOptions) ([]*github.PullRequest, *github.Response, error)
	ListReviews(ctx context.Context, owner, repo string, number int, opts *github.ListOptions) ([]*github.PullRequestReview, *github.Response, error)
	ListReviewComments(ctx context.Context, owner, repo string, number int, opts *github.PullRequestListCommentsOptions) ([]*github.PullRequestComment, *github.Response, error)
	ListPullRequestFiles(ctx context.Context, owner, repo string, number int, opts *github.ListOptions) ([]*github.CommitFile, *github.Response, error)
	GetRepository(ctx context.Context, owner, repo string) (*github.Repository, *github.Response, error)
}

// realGitHubAPI wraps the real go-github client to implement githubAPI.
type realGitHubAPI struct {
	client *github.Client
}

func (r *realGitHubAPI) ListIssues(ctx context.Context, owner, repo string, opts *github.IssueListByRepoOptions) ([]*github.Issue, *github.Response, error) {
	return r.client.Issues.ListByRepo(ctx, owner, repo, opts)
}

func (r *realGitHubAPI) ListPullRequests(ctx context.Context, owner, repo string, opts *github.PullRequestListOptions) ([]*github.PullRequest, *github.Response, error) {
	return r.client.PullRequests.List(ctx, owner, repo, opts)
}

func (r *realGitHubAPI) ListReviews(ctx context.Context, owner, repo string, number int, opts *github.ListOptions) ([]*github.PullRequestReview, *github.Response, error) {
	return r.client.PullRequests.ListReviews(ctx, owner, repo, number, opts)
}

func (r *realGitHubAPI) ListReviewComments(ctx context.Context, owner, repo string, number int, opts *github.PullRequestListCommentsOptions) ([]*github.PullRequestComment, *github.Response, error) {
	return r.client.PullRequests.ListComments(ctx, owner, repo, number, opts)
}

func (r *realGitHubAPI) ListPullRequestFiles(ctx context.Context, owner, repo string, number int, opts *github.ListOptions) ([]*github.CommitFile, *github.Response, error) {
	return r.client.PullRequests.ListFiles(ctx, owner, repo, number, opts)
}

func (r *realGitHubAPI) GetRepository(ctx context.Context, owner, repo string) (*github.Repository, *github.Response, error) {
	return r.client.Repositories.Get(ctx, owner, repo)
}

// GitHubCollector imports open issues, pull requests, and actionable review
// comments from GitHub.
type GitHubCollector struct {
	// api is the GitHub API client (nil means use real client).
	// Exported for testing only via the setAPI helper.
	api githubAPI
}

// Name returns the collector name used for registration and filtering.
func (c *GitHubCollector) Name() string { return "github" }

// Collect fetches open issues, PRs, and review comments from GitHub and
// returns them as raw signals.
func (c *GitHubCollector) Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	// Check for GITHUB_TOKEN.
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		slog.Info("GITHUB_TOKEN not set, skipping GitHub collector (set via: export GITHUB_TOKEN=$(gh auth token))")
		return nil, nil
	}

	// Parse owner/repo from git remote.
	owner, repo, err := parseGitHubRemote(repoPath)
	if err != nil {
		slog.Info("cannot determine GitHub remote, skipping GitHub collector", "error", err)
		return nil, nil
	}

	// Create API client.
	api := c.api
	if api == nil {
		client := github.NewClient(nil).WithAuthToken(token)
		api = &realGitHubAPI{client: client}
	}

	// Read config values with defaults.
	maxIssues := defaultMaxIssuesPerCollector
	commentDepth := defaultCommentDepth
	includePRs := defaultIncludePRs

	// Note: config values are read from opts.IncludePatterns as a workaround.
	// The actual config fields are on the CollectorConfig struct.
	// For now, use defaults. Config integration happens via the pipeline.

	includeClosed := opts.IncludeClosed

	// Compute history depth cutoff for closed items.
	var historyCutoff time.Time
	if includeClosed && opts.HistoryDepth != "" {
		if d, parseErr := ParseDuration(opts.HistoryDepth); parseErr == nil {
			historyCutoff = time.Now().Add(-d)
			slog.Info("history depth filter", "cutoff", historyCutoff.Format(time.RFC3339))
		} else {
			slog.Warn("invalid history-depth, ignoring", "value", opts.HistoryDepth, "error", parseErr)
		}
	}

	var signals []signal.RawSignal

	// Fetch issues.
	issueSigs, err := fetchIssues(ctx, api, owner, repo, maxIssues, includeClosed, historyCutoff)
	if err != nil {
		return nil, fmt.Errorf("fetching issues: %w", err)
	}
	signals = append(signals, issueSigs...)

	// Fetch PRs.
	if includePRs {
		prSigs, prErr := fetchPullRequests(ctx, api, owner, repo, maxIssues, commentDepth, includeClosed, historyCutoff)
		if prErr != nil {
			return nil, fmt.Errorf("fetching pull requests: %w", prErr)
		}
		signals = append(signals, prSigs...)
	}

	// Sort by FilePath for deterministic output.
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].FilePath < signals[j].FilePath
	})

	return signals, nil
}

// parseGitHubRemote extracts the owner and repo name from the git remote
// origin URL. Supports both HTTPS and SSH formats.
func parseGitHubRemote(repoPath string) (owner, repo string, err error) {
	gitRepo, err := git.PlainOpen(repoPath)
	if err != nil {
		return "", "", fmt.Errorf("opening repo: %w", err)
	}

	remotes, err := gitRepo.Remotes()
	if err != nil {
		return "", "", fmt.Errorf("listing remotes: %w", err)
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
		return "", "", fmt.Errorf("no origin remote found")
	}

	rawURL := originURLs[0]
	return parseGitHubURL(rawURL)
}

// parseGitHubURL parses a GitHub URL (HTTPS or SSH) into owner and repo.
func parseGitHubURL(rawURL string) (owner, repo string, err error) {
	// Try SSH format: git@github.com:owner/repo.git
	if m := sshRemotePattern.FindStringSubmatch(rawURL); m != nil {
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

// fetchIssues fetches issues (excluding PRs) from GitHub. When includeClosed
// is true, it fetches all issues (open and closed) and classifies closed ones
// with dedicated kinds and lower confidence. If historyCutoff is non-zero,
// closed items with ClosedAt before the cutoff are skipped.
func fetchIssues(ctx context.Context, api githubAPI, owner, repo string, maxIssues int, includeClosed bool, historyCutoff time.Time) ([]signal.RawSignal, error) {
	var signals []signal.RawSignal
	state := "open"
	if includeClosed {
		state = "all"
	}
	opts := &github.IssueListByRepoOptions{
		State:     state,
		Sort:      "created",
		Direction: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		issues, resp, err := api.ListIssues(ctx, owner, repo, opts)
		if err != nil {
			return nil, fmt.Errorf("listing issues: %w", err)
		}

		for _, issue := range issues {
			// Skip pull requests (GitHub API returns PRs as issues).
			if issue.IsPullRequest() {
				continue
			}

			// Skip closed issues older than the history depth cutoff.
			if issue.GetState() == "closed" && !historyCutoff.IsZero() && issue.ClosedAt != nil && issue.ClosedAt.Before(historyCutoff) {
				continue
			}

			var kind string
			var confidence float64
			var tags []string

			if issue.GetState() == "closed" {
				kind = "github-closed-issue"
				confidence = 0.3
				tags = []string{kind, "pre-closed"}
			} else {
				kind, confidence = classifyIssue(issue)
				tags = []string{kind}
			}

			desc := truncateBody(issue.GetBody(), 500)
			if issue.GetState() == "closed" {
				closedAt := ""
				if issue.ClosedAt != nil {
					closedAt = issue.ClosedAt.Format(time.RFC3339)
				}
				desc = fmt.Sprintf("Closed at: %s, Reason: %s\n%s", closedAt, issue.GetStateReason(), desc)
			}

			sig := signal.RawSignal{
				Source:      "github",
				Kind:        kind,
				FilePath:    fmt.Sprintf("github/issues/%d", issue.GetNumber()),
				Line:        0,
				Title:       issue.GetTitle(),
				Description: desc,
				Author:      issue.GetUser().GetLogin(),
				Timestamp:   issue.GetCreatedAt().Time,
				Confidence:  confidence,
				Tags:        tags,
			}
			if issue.GetState() == "closed" && issue.ClosedAt != nil {
				sig.ClosedAt = issue.ClosedAt.Time
			}
			signals = append(signals, sig)

			if len(signals) >= maxIssues {
				return signals, nil
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return signals, nil
}

// fetchPullRequests fetches PRs with review state and actionable review
// comments. When includeClosed is true, it also fetches merged and
// closed-not-merged PRs with dedicated kinds and lower confidence.
// If historyCutoff is non-zero, closed PRs before the cutoff are skipped.
func fetchPullRequests(ctx context.Context, api githubAPI, owner, repo string, maxIssues, commentDepth int, includeClosed bool, historyCutoff time.Time) ([]signal.RawSignal, error) {
	var signals []signal.RawSignal
	state := "open"
	if includeClosed {
		state = "all"
	}
	opts := &github.PullRequestListOptions{
		State:     state,
		Sort:      "created",
		Direction: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		prs, resp, err := api.ListPullRequests(ctx, owner, repo, opts)
		if err != nil {
			return nil, fmt.Errorf("listing pull requests: %w", err)
		}

		for _, pr := range prs {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			// Skip closed PRs older than the history depth cutoff.
			if pr.GetState() == "closed" && !historyCutoff.IsZero() {
				closedTime := pr.ClosedAt
				if pr.GetMerged() && pr.MergedAt != nil {
					closedTime = pr.MergedAt
				}
				if closedTime != nil && closedTime.Before(historyCutoff) {
					continue
				}
			}

			var kind string
			var confidence float64
			var tags []string
			desc := truncateBody(pr.GetBody(), 500)

			if pr.GetState() == "closed" {
				if pr.GetMerged() {
					kind = "github-merged-pr"
					confidence = 0.3
					tags = []string{kind, "pre-closed"}
				} else {
					kind = "github-closed-pr"
					confidence = 0.2
					tags = []string{kind, "pre-closed"}
				}
				mergedAt := ""
				if pr.MergedAt != nil {
					mergedAt = pr.MergedAt.GetTime().Format(time.RFC3339)
				}
				closedAt := ""
				if pr.ClosedAt != nil {
					closedAt = pr.ClosedAt.GetTime().Format(time.RFC3339)
				}
				desc = fmt.Sprintf("Merged at: %s, Closed at: %s\n%s", mergedAt, closedAt, desc)

				// Enrich with module context from changed files.
				files, _, filesErr := api.ListPullRequestFiles(ctx, owner, repo, pr.GetNumber(), &github.ListOptions{PerPage: 100})
				if filesErr == nil {
					if moduleCtx := extractModuleContext(files); moduleCtx != "" {
						desc = moduleCtx + "\n" + desc
					}
				}
			} else {
				// Open PR: fetch reviews and classify.
				reviews, reviewErr := fetchAllReviews(ctx, api, owner, repo, pr.GetNumber())
				if reviewErr != nil {
					return nil, fmt.Errorf("listing reviews for PR #%d: %w", pr.GetNumber(), reviewErr)
				}
				kind, confidence = classifyPR(pr, reviews)
				tags = []string{kind}

				// Fetch actionable review comments for open PRs only.
				commentSigs, commentErr := fetchActionableComments(ctx, api, owner, repo, pr.GetNumber(), commentDepth)
				if commentErr != nil {
					return nil, fmt.Errorf("listing review comments for PR #%d: %w", pr.GetNumber(), commentErr)
				}
				signals = append(signals, commentSigs...)
			}

			sig := signal.RawSignal{
				Source:      "github",
				Kind:        kind,
				FilePath:    fmt.Sprintf("github/prs/%d", pr.GetNumber()),
				Line:        0,
				Title:       pr.GetTitle(),
				Description: desc,
				Author:      pr.GetUser().GetLogin(),
				Timestamp:   pr.GetCreatedAt().Time,
				Confidence:  confidence,
				Tags:        tags,
			}
			if pr.GetState() == "closed" {
				if pr.GetMerged() && pr.MergedAt != nil {
					sig.ClosedAt = pr.MergedAt.Time
				} else if pr.ClosedAt != nil {
					sig.ClosedAt = pr.ClosedAt.Time
				}
			}
			signals = append(signals, sig)

			if len(signals) >= maxIssues {
				return signals, nil
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return signals, nil
}

// fetchAllReviews fetches all reviews for a PR with pagination.
func fetchAllReviews(ctx context.Context, api githubAPI, owner, repo string, prNumber int) ([]*github.PullRequestReview, error) {
	var allReviews []*github.PullRequestReview
	opts := &github.ListOptions{
		PerPage: 100,
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		reviews, resp, err := api.ListReviews(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, err
		}
		allReviews = append(allReviews, reviews...)

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allReviews, nil
}

// fetchActionableComments fetches review comments that contain actionable
// language (TODO, FIXME, should, needs, must).
func fetchActionableComments(ctx context.Context, api githubAPI, owner, repo string, prNumber, commentDepth int) ([]signal.RawSignal, error) {
	var signals []signal.RawSignal
	opts := &github.PullRequestListCommentsOptions{
		Sort:      "created",
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

		comments, resp, err := api.ListReviewComments(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, err
		}

		for _, comment := range comments {
			if fetched >= commentDepth {
				return signals, nil
			}
			fetched++

			if !isActionableComment(comment.GetBody()) {
				continue
			}

			filePath := comment.GetPath()
			if filePath == "" {
				filePath = fmt.Sprintf("github/prs/%d", prNumber)
			}

			confidence := 0.6 + ageBoost(comment.GetCreatedAt().Time, 30, 0.1)
			confidence = math.Min(confidence, 1.0)

			signals = append(signals, signal.RawSignal{
				Source:      "github",
				Kind:        "github-review-todo",
				FilePath:    filePath,
				Line:        comment.GetLine(),
				Title:       fmt.Sprintf("Review comment on PR #%d: %s", prNumber, truncateBody(comment.GetBody(), 100)),
				Description: comment.GetBody(),
				Author:      comment.GetUser().GetLogin(),
				Timestamp:   comment.GetCreatedAt().Time,
				Confidence:  confidence,
				Tags:        []string{"github-review-todo"},
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return signals, nil
}

// classifyIssue determines the signal kind and confidence for an issue based
// on its labels.
func classifyIssue(issue *github.Issue) (kind string, confidence float64) {
	labels := issue.Labels
	for _, label := range labels {
		name := strings.ToLower(label.GetName())
		if name == "bug" {
			confidence = 0.7 + ageBoost(issue.GetCreatedAt().Time, 90, 0.1)
			return "github-bug", math.Min(confidence, 1.0)
		}
	}
	for _, label := range labels {
		name := strings.ToLower(label.GetName())
		if name == "enhancement" || name == "feature" {
			confidence = 0.5 + ageBoost(issue.GetCreatedAt().Time, 90, 0.1)
			return "github-feature", math.Min(confidence, 1.0)
		}
	}

	// Default: generic issue.
	confidence = 0.4 + ageBoost(issue.GetCreatedAt().Time, 90, 0.1)
	return "github-issue", math.Min(confidence, 1.0)
}

// classifyPR determines the signal kind and confidence for a PR based on its
// review state.
func classifyPR(pr *github.PullRequest, reviews []*github.PullRequestReview) (kind string, confidence float64) {
	hasChangesRequested := false
	hasApproved := false

	for _, review := range reviews {
		state := strings.ToUpper(review.GetState())
		switch state {
		case "CHANGES_REQUESTED":
			hasChangesRequested = true
		case "APPROVED":
			hasApproved = true
		}
	}

	if hasChangesRequested {
		confidence = 0.7 + ageBoost(pr.GetCreatedAt().Time, 30, 0.1)
		return "github-pr-changes", math.Min(confidence, 1.0)
	}
	if hasApproved {
		confidence = 0.6 + ageBoost(pr.GetCreatedAt().Time, 14, 0.1)
		return "github-pr-approved", math.Min(confidence, 1.0)
	}

	// Pending review (no reviews or only comments).
	confidence = 0.5 + ageBoost(pr.GetCreatedAt().Time, 14, 0.05)
	return "github-pr-pending", math.Min(confidence, 1.0)
}

// isActionableComment returns true if the comment body contains actionable
// language such as TODO, FIXME, should, needs, or must.
func isActionableComment(body string) bool {
	return actionablePattern.MatchString(body)
}

// ageBoost returns the boost value if the created time is older than the
// threshold in days, otherwise 0.
func ageBoost(created time.Time, thresholdDays int, boost float64) float64 {
	age := time.Since(created)
	threshold := time.Duration(thresholdDays) * 24 * time.Hour
	if age > threshold {
		return boost
	}
	return 0
}

// truncateBody truncates a string to maxLen characters, appending "..." if
// truncated. Newlines are replaced with spaces for single-line display.
func truncateBody(body string, maxLen int) string {
	body = strings.ReplaceAll(body, "\n", " ")
	body = strings.ReplaceAll(body, "\r", "")
	body = strings.TrimSpace(body)
	if len(body) > maxLen {
		return body[:maxLen] + "..."
	}
	return body
}

// extractModuleContext groups changed files by top-level directory and returns
// a summary string like "Modules affected: internal/collectors (3 files), cmd (1 file)".
func extractModuleContext(files []*github.CommitFile) string {
	if len(files) == 0 {
		return ""
	}

	counts := make(map[string]int)
	for _, f := range files {
		path := f.GetFilename()
		module := moduleFromPath(path)
		counts[module]++
	}

	// Sort for deterministic output.
	modules := make([]string, 0, len(counts))
	for m := range counts {
		modules = append(modules, m)
	}
	sort.Strings(modules)

	parts := make([]string, 0, len(modules))
	for _, m := range modules {
		n := counts[m]
		if n == 1 {
			parts = append(parts, fmt.Sprintf("%s (1 file)", m))
		} else {
			parts = append(parts, fmt.Sprintf("%s (%d files)", m, n))
		}
	}
	return "Modules affected: " + strings.Join(parts, ", ")
}

// ModuleFromPath derives a module name from a file path by taking the
// first two path segments (e.g., "internal/collectors") or the directory
// name if only one level deep. Root-level files return ".".
func ModuleFromPath(path string) string {
	return moduleFromPath(path)
}

func moduleFromPath(path string) string {
	parts := strings.Split(path, "/")
	switch {
	case len(parts) >= 3:
		return parts[0] + "/" + parts[1]
	case len(parts) == 2:
		return parts[0]
	default:
		return "."
	}
}

// Compile-time interface check.
var _ collector.Collector = (*GitHubCollector)(nil)
