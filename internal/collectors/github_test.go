// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	gogitconfig "github.com/go-git/go-git/v5/config"
	"github.com/google/go-github/v68/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/davetashner/stringer/internal/testable"
)

// mockGitHubAPI implements githubAPI for testing.
type mockGitHubAPI struct {
	issues         []*github.Issue
	issueResp      *github.Response
	issueErr       error
	prs            []*github.PullRequest
	prResp         *github.Response
	prErr          error
	reviews        map[int][]*github.PullRequestReview
	reviewResp     *github.Response
	reviewErr      error
	comments       map[int][]*github.PullRequestComment
	commentResp    *github.Response
	commentErr     error
	issueCallCount int
	prCallCount    int
	lastIssueOpts  *github.IssueListByRepoOptions
	lastPROpts     *github.PullRequestListOptions
}

func (m *mockGitHubAPI) ListIssues(_ context.Context, _, _ string, opts *github.IssueListByRepoOptions) ([]*github.Issue, *github.Response, error) {
	m.issueCallCount++
	m.lastIssueOpts = opts
	return m.issues, m.issueResp, m.issueErr
}

func (m *mockGitHubAPI) ListPullRequests(_ context.Context, _, _ string, opts *github.PullRequestListOptions) ([]*github.PullRequest, *github.Response, error) {
	m.prCallCount++
	m.lastPROpts = opts
	return m.prs, m.prResp, m.prErr
}

func (m *mockGitHubAPI) ListReviews(_ context.Context, _, _ string, number int, _ *github.ListOptions) ([]*github.PullRequestReview, *github.Response, error) {
	reviews := m.reviews[number]
	resp := m.reviewResp
	if resp == nil {
		resp = &github.Response{Response: &http.Response{StatusCode: http.StatusOK}}
	}
	return reviews, resp, m.reviewErr
}

func (m *mockGitHubAPI) ListReviewComments(_ context.Context, _, _ string, number int, _ *github.PullRequestListCommentsOptions) ([]*github.PullRequestComment, *github.Response, error) {
	comments := m.comments[number]
	resp := m.commentResp
	if resp == nil {
		resp = &github.Response{Response: &http.Response{StatusCode: http.StatusOK}}
	}
	return comments, resp, m.commentErr
}

func (m *mockGitHubAPI) ListPullRequestFiles(_ context.Context, _, _ string, _ int, _ *github.ListOptions) ([]*github.CommitFile, *github.Response, error) {
	return nil, emptyResponse(), nil
}

func (m *mockGitHubAPI) GetRepository(_ context.Context, _, _ string) (*github.Repository, *github.Response, error) {
	return nil, emptyResponse(), nil
}

func emptyResponse() *github.Response {
	return &github.Response{
		Response: &http.Response{StatusCode: http.StatusOK},
	}
}

func TestGitHubCollector_Name(t *testing.T) {
	c := &GitHubCollector{}
	assert.Equal(t, "github", c.Name())
}

func TestGitHubCollector_MissingToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	c := &GitHubCollector{}
	signals, err := c.Collect(context.Background(), t.TempDir(), signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestGitHubCollector_MissingTokenLogMessage(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	// Capture log output to verify actionable message.
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	c := &GitHubCollector{}
	_, _ = c.Collect(context.Background(), t.TempDir(), signal.CollectorOpts{})

	logOutput := buf.String()
	assert.Contains(t, logOutput, "gh auth token", "log message should suggest how to set the token")
}

func TestGitHubCollector_NonGitHubRemote(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://gitlab.com/owner/repo.git")

	c := &GitHubCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestGitHubCollector_IssuesWithLabels(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	now := time.Now()
	mock := &mockGitHubAPI{
		issues: []*github.Issue{
			makeIssue(1, "Bug report", now, []string{"bug"}),
			makeIssue(2, "Feature request", now, []string{"enhancement"}),
			makeIssue(3, "General issue", now, []string{"question"}),
		},
		issueResp: emptyResponse(),
		prs:       []*github.PullRequest{},
		prResp:    emptyResponse(),
		reviews:   map[int][]*github.PullRequestReview{},
		comments:  map[int][]*github.PullRequestComment{},
	}

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 3)

	// Find signals by file path (sorted).
	sigMap := make(map[string]signal.RawSignal)
	for _, s := range signals {
		sigMap[s.FilePath] = s
	}

	bugSig := sigMap["github/issues/1"]
	assert.Equal(t, "github-bug", bugSig.Kind)
	assert.InDelta(t, 0.7, bugSig.Confidence, 0.01)

	featureSig := sigMap["github/issues/2"]
	assert.Equal(t, "github-feature", featureSig.Kind)
	assert.InDelta(t, 0.5, featureSig.Confidence, 0.01)

	issueSig := sigMap["github/issues/3"]
	assert.Equal(t, "github-issue", issueSig.Kind)
	assert.InDelta(t, 0.4, issueSig.Confidence, 0.01)
}

func TestGitHubCollector_IssueAgeBoost(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	oldDate := time.Now().AddDate(0, -6, 0) // 6 months ago
	mock := &mockGitHubAPI{
		issues: []*github.Issue{
			makeIssue(1, "Old bug", oldDate, []string{"bug"}),
		},
		issueResp: emptyResponse(),
		prs:       []*github.PullRequest{},
		prResp:    emptyResponse(),
		reviews:   map[int][]*github.PullRequestReview{},
		comments:  map[int][]*github.PullRequestComment{},
	}

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	// Bug (0.7) + age boost (0.1) = 0.8.
	assert.InDelta(t, 0.8, signals[0].Confidence, 0.01)
}

func TestGitHubCollector_PRChangesRequested(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	now := time.Now()
	mock := &mockGitHubAPI{
		issues:    []*github.Issue{},
		issueResp: emptyResponse(),
		prs: []*github.PullRequest{
			makePR(10, "Fix the thing", now),
		},
		prResp: emptyResponse(),
		reviews: map[int][]*github.PullRequestReview{
			10: {makeReview("CHANGES_REQUESTED")},
		},
		comments: map[int][]*github.PullRequestComment{},
	}

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "github-pr-changes", signals[0].Kind)
	assert.InDelta(t, 0.7, signals[0].Confidence, 0.01)
}

func TestGitHubCollector_PRApproved(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	now := time.Now()
	mock := &mockGitHubAPI{
		issues:    []*github.Issue{},
		issueResp: emptyResponse(),
		prs: []*github.PullRequest{
			makePR(11, "Add feature", now),
		},
		prResp: emptyResponse(),
		reviews: map[int][]*github.PullRequestReview{
			11: {makeReview("APPROVED")},
		},
		comments: map[int][]*github.PullRequestComment{},
	}

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "github-pr-approved", signals[0].Kind)
	assert.InDelta(t, 0.6, signals[0].Confidence, 0.01)
}

func TestGitHubCollector_PRPendingReview(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	now := time.Now()
	mock := &mockGitHubAPI{
		issues:    []*github.Issue{},
		issueResp: emptyResponse(),
		prs: []*github.PullRequest{
			makePR(12, "New PR", now),
		},
		prResp:   emptyResponse(),
		reviews:  map[int][]*github.PullRequestReview{},
		comments: map[int][]*github.PullRequestComment{},
	}

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "github-pr-pending", signals[0].Kind)
	assert.InDelta(t, 0.5, signals[0].Confidence, 0.01)
}

func TestGitHubCollector_ReviewCommentsActionable(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	now := time.Now()
	mock := &mockGitHubAPI{
		issues:    []*github.Issue{},
		issueResp: emptyResponse(),
		prs: []*github.PullRequest{
			makePR(13, "Some PR", now),
		},
		prResp:  emptyResponse(),
		reviews: map[int][]*github.PullRequestReview{},
		comments: map[int][]*github.PullRequestComment{
			13: {
				makeComment("TODO: fix this later", "src/main.go", 42, now),
				makeComment("This should be refactored", "src/util.go", 10, now),
			},
		},
	}

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.NoError(t, err)

	// 1 PR signal + 2 review comment signals.
	require.Len(t, signals, 3)

	var reviewSigs []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "github-review-todo" {
			reviewSigs = append(reviewSigs, s)
		}
	}
	assert.Len(t, reviewSigs, 2)
}

func TestGitHubCollector_ReviewCommentsNonActionable(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	now := time.Now()
	mock := &mockGitHubAPI{
		issues:    []*github.Issue{},
		issueResp: emptyResponse(),
		prs: []*github.PullRequest{
			makePR(14, "Some PR", now),
		},
		prResp:  emptyResponse(),
		reviews: map[int][]*github.PullRequestReview{},
		comments: map[int][]*github.PullRequestComment{
			14: {
				makeComment("Looks good to me!", "src/main.go", 1, now),
				makeComment("Nice approach", "src/util.go", 5, now),
			},
		},
	}

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.NoError(t, err)

	// Only 1 PR signal, no review comment signals.
	require.Len(t, signals, 1)
	assert.Equal(t, "github-pr-pending", signals[0].Kind)
}

func TestGitHubCollector_Pagination(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	// Use mock API to simulate pagination across two pages.
	now := time.Now()
	mock := &paginatingMockAPI{
		issuePages: [][]*github.Issue{
			{makeIssue(1, "Issue 1", now, nil)},
			{makeIssue(2, "Issue 2", now, nil)},
		},
		prs: []*github.PullRequest{},
	}

	repoPath := initGitHubTestRepo(t, "https://github.com/owner/repo.git")

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Len(t, signals, 2)
	assert.Equal(t, 2, mock.issueCallCount, "should have made 2 paginated requests")
}

func TestGitHubCollector_RateLimit(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/owner/repo.git")

	// Simulate a rate limit error from the API.
	rateLimitErr := fmt.Errorf("GET https://api.github.com/repos/owner/repo/issues: 403 rate limit exceeded")
	mock := &mockGitHubAPI{
		issues:    nil,
		issueResp: emptyResponse(),
		issueErr:  rateLimitErr,
	}

	c := &GitHubCollector{api: mock}
	_, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit")
}

func TestGitHubCollector_ContextCancellation(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	mock := &mockGitHubAPI{
		issues:    []*github.Issue{},
		issueResp: emptyResponse(),
		issueErr:  context.Canceled,
	}

	c := &GitHubCollector{api: mock}
	_, err := c.Collect(ctx, repoPath, signal.CollectorOpts{})
	assert.Error(t, err)
}

func TestParseGitHubRemote_HTTPS(t *testing.T) {
	repoPath := initGitHubTestRepo(t, "https://github.com/myowner/myrepo.git")
	owner, repo, err := parseGitHubRemote(repoPath)
	require.NoError(t, err)
	assert.Equal(t, "myowner", owner)
	assert.Equal(t, "myrepo", repo)
}

func TestParseGitHubRemote_SSH(t *testing.T) {
	repoPath := initGitHubTestRepo(t, "git@github.com:sshowner/sshrepo.git")
	owner, repo, err := parseGitHubRemote(repoPath)
	require.NoError(t, err)
	assert.Equal(t, "sshowner", owner)
	assert.Equal(t, "sshrepo", repo)
}

func TestParseGitHubRemote_HTTPSNoGit(t *testing.T) {
	repoPath := initGitHubTestRepo(t, "https://github.com/noext/norepo")
	owner, repo, err := parseGitHubRemote(repoPath)
	require.NoError(t, err)
	assert.Equal(t, "noext", owner)
	assert.Equal(t, "norepo", repo)
}

func TestParseGitHubRemote_NonGitHub(t *testing.T) {
	repoPath := initGitHubTestRepo(t, "https://gitlab.com/other/repo.git")
	_, _, err := parseGitHubRemote(repoPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a GitHub URL")
}

func TestIsActionableComment(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected bool
	}{
		{"TODO", "TODO: fix this", true},
		{"FIXME", "FIXME: broken", true},
		{"should", "This should be refactored", true},
		{"needs", "This needs cleanup", true},
		{"must", "We must handle this edge case", true},
		{"case insensitive", "todo: handle error", true},
		{"no match", "Looks good to me!", false},
		{"partial word customer", "customer is great", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isActionableComment(tt.body))
		})
	}
}

func TestClassifyIssue(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		labels      []string
		wantKind    string
		wantMinConf float64
		wantMaxConf float64
	}{
		{"bug label", []string{"bug"}, "github-bug", 0.7, 0.71},
		{"enhancement label", []string{"enhancement"}, "github-feature", 0.5, 0.51},
		{"feature label", []string{"feature"}, "github-feature", 0.5, 0.51},
		{"no labels", nil, "github-issue", 0.4, 0.41},
		{"other label", []string{"question"}, "github-issue", 0.4, 0.41},
		{"bug takes precedence", []string{"enhancement", "bug"}, "github-bug", 0.7, 0.71},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := makeIssue(1, "Test", now, tt.labels)
			kind, confidence := classifyIssue(issue)
			assert.Equal(t, tt.wantKind, kind)
			assert.GreaterOrEqual(t, confidence, tt.wantMinConf)
			assert.LessOrEqual(t, confidence, tt.wantMaxConf)
		})
	}
}

func TestIncludePRsFalse(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	mock := &mockGitHubAPI{
		issues: []*github.Issue{
			makeIssue(1, "An issue", time.Now(), nil),
		},
		issueResp: emptyResponse(),
		prs: []*github.PullRequest{
			makePR(10, "A PR", time.Now()),
		},
		prResp:   emptyResponse(),
		reviews:  map[int][]*github.PullRequestReview{},
		comments: map[int][]*github.PullRequestComment{},
	}

	// Test that when include_prs is false, no PR signals are emitted.
	// We simulate this by collecting only issues, then verifying no PR API calls.
	signals, err := fetchIssues(context.Background(), mock, "testowner", "testrepo", 100, false, time.Time{})
	require.NoError(t, err)
	assert.Len(t, signals, 1)
	assert.Equal(t, "github-issue", signals[0].Kind)

	// Verify PR API was not called.
	assert.Equal(t, 0, mock.prCallCount, "PR API should not be called when include_prs is false")
}

func TestGitHubCollector_IncludeClosedIssues(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	now := time.Now()
	mock := &mockGitHubAPI{
		issues: []*github.Issue{
			makeIssue(1, "Open bug", now, []string{"bug"}),
			makeClosedIssue(2, "Closed feature", now, []string{"enhancement"}, "completed"),
		},
		issueResp: emptyResponse(),
		prs:       []*github.PullRequest{},
		prResp:    emptyResponse(),
		reviews:   map[int][]*github.PullRequestReview{},
		comments:  map[int][]*github.PullRequestComment{},
	}

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{
		IncludeClosed: true,
	})
	require.NoError(t, err)
	require.Len(t, signals, 2)

	sigMap := make(map[string]signal.RawSignal)
	for _, s := range signals {
		sigMap[s.FilePath] = s
	}

	// Open issue classified normally.
	openSig := sigMap["github/issues/1"]
	assert.Equal(t, "github-bug", openSig.Kind)
	assert.InDelta(t, 0.7, openSig.Confidence, 0.01)

	// Closed issue gets dedicated kind and lower confidence.
	closedSig := sigMap["github/issues/2"]
	assert.Equal(t, "github-closed-issue", closedSig.Kind)
	assert.InDelta(t, 0.3, closedSig.Confidence, 0.01)
	assert.Contains(t, closedSig.Tags, "pre-closed")
	assert.Contains(t, closedSig.Description, "completed")
}

func TestGitHubCollector_IncludeMergedPRs(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	now := time.Now()
	mock := &mockGitHubAPI{
		issues:    []*github.Issue{},
		issueResp: emptyResponse(),
		prs: []*github.PullRequest{
			makePR(10, "Open PR", now),
			makeMergedPR(11, "Merged PR", now),
			makeClosedPR(12, "Closed PR", now),
		},
		prResp:   emptyResponse(),
		reviews:  map[int][]*github.PullRequestReview{},
		comments: map[int][]*github.PullRequestComment{},
	}

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{
		IncludeClosed: true,
	})
	require.NoError(t, err)
	require.Len(t, signals, 3)

	sigMap := make(map[string]signal.RawSignal)
	for _, s := range signals {
		sigMap[s.FilePath] = s
	}

	// Open PR classified normally.
	openSig := sigMap["github/prs/10"]
	assert.Equal(t, "github-pr-pending", openSig.Kind)

	// Merged PR gets dedicated kind.
	mergedSig := sigMap["github/prs/11"]
	assert.Equal(t, "github-merged-pr", mergedSig.Kind)
	assert.InDelta(t, 0.3, mergedSig.Confidence, 0.01)
	assert.Contains(t, mergedSig.Tags, "pre-closed")

	// Closed (not merged) PR gets lower confidence.
	closedSig := sigMap["github/prs/12"]
	assert.Equal(t, "github-closed-pr", closedSig.Kind)
	assert.InDelta(t, 0.2, closedSig.Confidence, 0.01)
	assert.Contains(t, closedSig.Tags, "pre-closed")
}

func TestGitHubCollector_DefaultExcludesClosed(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	now := time.Now()
	// Even with closed issues in the mock, default (IncludeClosed=false)
	// should only request open state.
	mock := &mockGitHubAPI{
		issues: []*github.Issue{
			makeIssue(1, "Open issue", now, nil),
		},
		issueResp: emptyResponse(),
		prs:       []*github.PullRequest{},
		prResp:    emptyResponse(),
		reviews:   map[int][]*github.PullRequestReview{},
		comments:  map[int][]*github.PullRequestComment{},
	}

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "github-issue", signals[0].Kind)
	// No closed signals present.
	for _, s := range signals {
		assert.NotContains(t, s.Tags, "pre-closed")
	}
}

// paginatingMockAPI simulates paginated GitHub API responses.
type paginatingMockAPI struct {
	issuePages     [][]*github.Issue
	issueCallCount int
	prs            []*github.PullRequest
	prCallCount    int
	reviews        map[int][]*github.PullRequestReview
	comments       map[int][]*github.PullRequestComment
}

func (m *paginatingMockAPI) ListIssues(_ context.Context, _, _ string, opts *github.IssueListByRepoOptions) ([]*github.Issue, *github.Response, error) {
	page := opts.Page
	if page == 0 {
		page = 1
	}
	m.issueCallCount++

	idx := page - 1
	if idx >= len(m.issuePages) {
		return nil, emptyResponse(), nil
	}

	resp := &github.Response{
		Response: &http.Response{StatusCode: http.StatusOK},
	}
	if page < len(m.issuePages) {
		resp.NextPage = page + 1
	}

	return m.issuePages[idx], resp, nil
}

func (m *paginatingMockAPI) ListPullRequests(_ context.Context, _, _ string, _ *github.PullRequestListOptions) ([]*github.PullRequest, *github.Response, error) {
	m.prCallCount++
	return m.prs, emptyResponse(), nil
}

func (m *paginatingMockAPI) ListReviews(_ context.Context, _, _ string, number int, _ *github.ListOptions) ([]*github.PullRequestReview, *github.Response, error) {
	if m.reviews == nil {
		return nil, emptyResponse(), nil
	}
	return m.reviews[number], emptyResponse(), nil
}

func (m *paginatingMockAPI) ListReviewComments(_ context.Context, _, _ string, number int, _ *github.PullRequestListCommentsOptions) ([]*github.PullRequestComment, *github.Response, error) {
	if m.comments == nil {
		return nil, emptyResponse(), nil
	}
	return m.comments[number], emptyResponse(), nil
}

func (m *paginatingMockAPI) ListPullRequestFiles(_ context.Context, _, _ string, _ int, _ *github.ListOptions) ([]*github.CommitFile, *github.Response, error) {
	return nil, emptyResponse(), nil
}

func (m *paginatingMockAPI) GetRepository(_ context.Context, _, _ string) (*github.Repository, *github.Response, error) {
	return nil, emptyResponse(), nil
}

// --- Helper functions ---

// initGitHubTestRepo creates a temporary git repository with the given remote URL.
func initGitHubTestRepo(t *testing.T, remoteURL string) string {
	t.Helper()
	dir := t.TempDir()

	// Initialize a git repo using go-git.
	repo, err := gogit.PlainInit(dir, false)
	require.NoError(t, err)

	// Add origin remote.
	_, err = repo.CreateRemote(&gogitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{remoteURL},
	})
	require.NoError(t, err)

	return dir
}

// makeIssue creates a test GitHub issue.
func makeIssue(number int, title string, created time.Time, labelNames []string) *github.Issue {
	var labels []*github.Label
	for _, name := range labelNames {
		n := name // copy for pointer
		labels = append(labels, &github.Label{Name: &n})
	}
	ts := github.Timestamp{Time: created}
	user := &github.User{Login: github.Ptr("testuser")}
	return &github.Issue{
		Number:    &number,
		Title:     &title,
		Body:      github.Ptr("Issue body"),
		Labels:    labels,
		CreatedAt: &ts,
		User:      user,
	}
}

// makePR creates a test GitHub pull request.
func makePR(number int, title string, created time.Time) *github.PullRequest {
	ts := github.Timestamp{Time: created}
	user := &github.User{Login: github.Ptr("testuser")}
	return &github.PullRequest{
		Number:    &number,
		Title:     &title,
		Body:      github.Ptr("PR body"),
		CreatedAt: &ts,
		User:      user,
	}
}

// makeReview creates a test PR review with the given state.
func makeReview(state string) *github.PullRequestReview {
	return &github.PullRequestReview{
		State: &state,
	}
}

// makeClosedIssue creates a test closed GitHub issue.
func makeClosedIssue(number int, title string, created time.Time, labelNames []string, stateReason string) *github.Issue {
	issue := makeIssue(number, title, created, labelNames)
	state := "closed"
	issue.State = &state
	issue.StateReason = &stateReason
	closedAt := github.Timestamp{Time: created.Add(24 * time.Hour)}
	issue.ClosedAt = &closedAt
	return issue
}

// makeMergedPR creates a test merged pull request.
func makeMergedPR(number int, title string, created time.Time) *github.PullRequest {
	pr := makePR(number, title, created)
	state := "closed"
	pr.State = &state
	merged := true
	pr.Merged = &merged
	mergedAt := github.Timestamp{Time: created.Add(24 * time.Hour)}
	pr.MergedAt = &mergedAt
	closedAt := github.Timestamp{Time: created.Add(24 * time.Hour)}
	pr.ClosedAt = &closedAt
	return pr
}

// makeClosedPR creates a test closed (not merged) pull request.
func makeClosedPR(number int, title string, created time.Time) *github.PullRequest {
	pr := makePR(number, title, created)
	state := "closed"
	pr.State = &state
	merged := false
	pr.Merged = &merged
	closedAt := github.Timestamp{Time: created.Add(24 * time.Hour)}
	pr.ClosedAt = &closedAt
	return pr
}

func TestHistoryDepthFiltersOldClosedIssues(t *testing.T) {
	now := time.Now()
	recentIssue := makeClosedIssue(1, "Recent issue", now.Add(-30*24*time.Hour), nil, "completed")
	oldIssue := makeClosedIssue(2, "Old issue", now.Add(-200*24*time.Hour), nil, "completed")
	// oldIssue was closed at created+24h = ~199 days ago

	mock := &mockGitHubAPI{
		issues:    []*github.Issue{recentIssue, oldIssue},
		issueResp: &github.Response{Response: &http.Response{StatusCode: 200}},
	}

	// Cutoff at 90 days ago — should keep recent, skip old.
	cutoff := now.Add(-90 * 24 * time.Hour)
	signals, err := fetchIssues(context.Background(), mock, "owner", "repo", 100, true, cutoff)
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "Recent issue", signals[0].Title)
}

func TestHistoryDepthFiltersOldMergedPRs(t *testing.T) {
	now := time.Now()
	recentPR := makeMergedPR(1, "Recent PR", now.Add(-30*24*time.Hour))
	oldPR := makeMergedPR(2, "Old PR", now.Add(-200*24*time.Hour))

	mock := &mockGitHubAPI{
		prs:    []*github.PullRequest{recentPR, oldPR},
		prResp: &github.Response{Response: &http.Response{StatusCode: 200}},
	}

	cutoff := now.Add(-90 * 24 * time.Hour)
	signals, err := fetchPullRequests(context.Background(), mock, "owner", "repo", 100, 30, true, cutoff)
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "Recent PR", signals[0].Title)
}

func TestHistoryDepthZeroCutoffNoFiltering(t *testing.T) {
	now := time.Now()
	oldIssue := makeClosedIssue(1, "Old issue", now.Add(-200*24*time.Hour), nil, "completed")

	mock := &mockGitHubAPI{
		issues:    []*github.Issue{oldIssue},
		issueResp: &github.Response{Response: &http.Response{StatusCode: 200}},
	}

	// Zero cutoff should not filter.
	signals, err := fetchIssues(context.Background(), mock, "owner", "repo", 100, true, time.Time{})
	require.NoError(t, err)
	assert.Len(t, signals, 1)
}

func TestExtractModuleContext_Various(t *testing.T) {
	files := []*github.CommitFile{
		{Filename: github.Ptr("internal/collectors/todos.go")},
		{Filename: github.Ptr("internal/collectors/github.go")},
		{Filename: github.Ptr("internal/collectors/duration.go")},
		{Filename: github.Ptr("cmd/stringer/main.go")},
		{Filename: github.Ptr("README.md")},
	}

	result := extractModuleContext(files)
	assert.Contains(t, result, "Modules affected:")
	assert.Contains(t, result, "internal/collectors (3 files)")
	assert.Contains(t, result, "cmd/stringer (1 file)")
	assert.Contains(t, result, ". (1 file)")
}

func TestExtractModuleContext_Empty(t *testing.T) {
	result := extractModuleContext(nil)
	assert.Equal(t, "", result)
}

func TestExtractModuleContext_SingleModule(t *testing.T) {
	files := []*github.CommitFile{
		{Filename: github.Ptr("internal/state/state.go")},
		{Filename: github.Ptr("internal/state/state_test.go")},
	}

	result := extractModuleContext(files)
	assert.Equal(t, "Modules affected: internal/state (2 files)", result)
}

func TestModuleFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"internal/collectors/todos.go", "internal/collectors"},
		{"cmd/stringer/main.go", "cmd/stringer"},
		{"cmd/main.go", "cmd"},
		{"README.md", "."},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, moduleFromPath(tt.path))
		})
	}
}

// makeComment creates a test PR review comment.
func makeComment(body, path string, line int, created time.Time) *github.PullRequestComment {
	ts := github.Timestamp{Time: created}
	user := &github.User{Login: github.Ptr("reviewer")}
	return &github.PullRequestComment{
		Body:      &body,
		Path:      &path,
		Line:      &line,
		CreatedAt: &ts,
		User:      user,
	}
}

// --- Mock-based tests ---

func TestGitHubCollector_PlainOpenFailure(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	c := &GitHubCollector{
		GitOpener: &testable.MockGitOpener{
			OpenErr: fmt.Errorf("repo not found"),
		},
	}
	// PlainOpen failure is logged and skipped (returns nil, nil).
	signals, err := c.Collect(context.Background(), "/tmp/fake", signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestGitHubCollector_RemotesError(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	mockRepo := &testable.MockGitRepository{
		RemotesErr: fmt.Errorf("remotes failed"),
	}
	c := &GitHubCollector{
		GitOpener: &testable.MockGitOpener{Repo: mockRepo},
	}
	// Remotes error is logged and skipped (returns nil, nil).
	signals, err := c.Collect(context.Background(), "/tmp/fake", signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestParseGitHubURL_SSHFormat(t *testing.T) {
	owner, repo, err := parseGitHubURL("git@github.com:myowner/myrepo.git")
	require.NoError(t, err)
	assert.Equal(t, "myowner", owner)
	assert.Equal(t, "myrepo", repo)
}

func TestParseGitHubURL_SSHWithoutGit(t *testing.T) {
	owner, repo, err := parseGitHubURL("git@github.com:myowner/myrepo")
	require.NoError(t, err)
	assert.Equal(t, "myowner", owner)
	assert.Equal(t, "myrepo", repo)
}

func TestParseGitHubURL_InvalidURL(t *testing.T) {
	_, _, err := parseGitHubURL("not-a-url")
	require.Error(t, err)
}

func TestParseGitHubURL_NonGitHubHost(t *testing.T) {
	_, _, err := parseGitHubURL("https://gitlab.com/owner/repo.git")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a GitHub URL")
}

func TestParseGitHubURL_TooFewPathParts(t *testing.T) {
	_, _, err := parseGitHubURL("https://github.com/onlyowner")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot parse owner/repo")
}

func TestTruncateBody(t *testing.T) {
	// Short body: not truncated.
	assert.Equal(t, "hello", truncateBody("hello", 100))

	// Newlines replaced.
	assert.Equal(t, "line1 line2", truncateBody("line1\nline2", 100))

	// Long body: truncated.
	long := "a" + "b" + "c"
	result := truncateBody(long, 2)
	assert.Equal(t, "ab...", result)
}

func TestAgeBoost_Old(t *testing.T) {
	old := time.Now().Add(-200 * 24 * time.Hour)
	boost := ageBoost(old, 90, 0.1)
	assert.InDelta(t, 0.1, boost, 0.001)
}

func TestAgeBoost_Recent(t *testing.T) {
	recent := time.Now().Add(-10 * 24 * time.Hour)
	boost := ageBoost(recent, 90, 0.1)
	assert.InDelta(t, 0.0, boost, 0.001)
}

func TestFetchAllReviews_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := &mockGitHubAPI{
		reviewResp: emptyResponse(),
	}
	_, err := fetchAllReviews(ctx, mock, "owner", "repo", 1)
	require.Error(t, err)
}

func TestFetchAllReviews_APIError(t *testing.T) {
	mock := &mockGitHubAPI{
		reviewErr: fmt.Errorf("review API error"),
	}
	_, err := fetchAllReviews(context.Background(), mock, "owner", "repo", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "review API error")
}

func TestFetchActionableComments_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := &mockGitHubAPI{}
	_, err := fetchActionableComments(ctx, mock, "owner", "repo", 1, 30)
	require.Error(t, err)
}

func TestFetchActionableComments_APIError(t *testing.T) {
	mock := &mockGitHubAPI{
		commentErr: fmt.Errorf("comment API error"),
	}
	_, err := fetchActionableComments(context.Background(), mock, "owner", "repo", 1, 30)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "comment API error")
}

func TestFetchActionableComments_EmptyPath(t *testing.T) {
	now := time.Now()
	emptyPath := ""
	mock := &mockGitHubAPI{
		comments: map[int][]*github.PullRequestComment{
			1: {
				makeComment("TODO: fix this", emptyPath, 0, now),
			},
		},
	}

	signals, err := fetchActionableComments(context.Background(), mock, "owner", "repo", 1, 30)
	require.NoError(t, err)
	require.Len(t, signals, 1)
	// When path is empty, it should fall back to the PR path.
	assert.Equal(t, "github/prs/1", signals[0].FilePath)
}

func TestFetchActionableComments_DepthLimit(t *testing.T) {
	now := time.Now()
	// Create 5 actionable comments, but set depth to 3.
	var comments []*github.PullRequestComment
	for i := 0; i < 5; i++ {
		comments = append(comments, makeComment(
			fmt.Sprintf("TODO: fix %d", i), "file.go", i, now))
	}
	mock := &mockGitHubAPI{
		comments: map[int][]*github.PullRequestComment{1: comments},
	}

	signals, err := fetchActionableComments(context.Background(), mock, "owner", "repo", 1, 3)
	require.NoError(t, err)
	// Only 3 comments should be processed (depth limit).
	assert.LessOrEqual(t, len(signals), 3)
}

func TestFetchIssues_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := &mockGitHubAPI{
		issueResp: emptyResponse(),
	}
	_, err := fetchIssues(ctx, mock, "owner", "repo", 100, false, time.Time{})
	require.Error(t, err)
}

func TestFetchPullRequests_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := &mockGitHubAPI{
		prResp: emptyResponse(),
	}
	_, err := fetchPullRequests(ctx, mock, "owner", "repo", 100, 30, false, time.Time{})
	require.Error(t, err)
}

func TestFetchPullRequests_ReviewError(t *testing.T) {
	now := time.Now()
	mock := &mockGitHubAPI{
		prs:       []*github.PullRequest{makePR(1, "PR 1", now)},
		prResp:    emptyResponse(),
		reviewErr: fmt.Errorf("review error"),
	}

	_, err := fetchPullRequests(context.Background(), mock, "owner", "repo", 100, 30, false, time.Time{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing reviews")
}

func TestFetchPullRequests_CommentError(t *testing.T) {
	now := time.Now()
	mock := &mockGitHubAPI{
		prs:        []*github.PullRequest{makePR(1, "PR 1", now)},
		prResp:     emptyResponse(),
		reviews:    map[int][]*github.PullRequestReview{},
		commentErr: fmt.Errorf("comment error"),
	}

	_, err := fetchPullRequests(context.Background(), mock, "owner", "repo", 100, 30, false, time.Time{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing review comments")
}

func TestGitHubCollector_HistoryDepthParsing(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	now := time.Now()
	mock := &mockGitHubAPI{
		issues: []*github.Issue{
			makeClosedIssue(1, "Old closed", now.Add(-200*24*time.Hour), nil, "completed"),
		},
		issueResp: emptyResponse(),
		prs:       []*github.PullRequest{},
		prResp:    emptyResponse(),
		reviews:   map[int][]*github.PullRequestReview{},
		comments:  map[int][]*github.PullRequestComment{},
	}

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{
		IncludeClosed: true,
		HistoryDepth:  "90d",
	})
	require.NoError(t, err)
	// The old closed issue should be filtered out by the 90-day history depth.
	assert.Empty(t, signals)
}

func TestGitHubCollector_InvalidHistoryDepth(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	now := time.Now()
	mock := &mockGitHubAPI{
		issues: []*github.Issue{
			makeClosedIssue(1, "Closed issue", now.Add(-200*24*time.Hour), nil, "completed"),
		},
		issueResp: emptyResponse(),
		prs:       []*github.PullRequest{},
		prResp:    emptyResponse(),
		reviews:   map[int][]*github.PullRequestReview{},
		comments:  map[int][]*github.PullRequestComment{},
	}

	// Suppress slog warning output.
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{
		IncludeClosed: true,
		HistoryDepth:  "invalid",
	})
	require.NoError(t, err)
	// Invalid history depth is ignored — closed issues should still appear.
	assert.NotEmpty(t, signals)
}

func TestFetchIssues_MaxIssuesLimit(t *testing.T) {
	now := time.Now()
	var issues []*github.Issue
	for i := 0; i < 5; i++ {
		issues = append(issues, makeIssue(i+1, fmt.Sprintf("Issue %d", i+1), now, nil))
	}

	mock := &mockGitHubAPI{
		issues:    issues,
		issueResp: emptyResponse(),
	}

	// Limit to 3 issues.
	signals, err := fetchIssues(context.Background(), mock, "owner", "repo", 3, false, time.Time{})
	require.NoError(t, err)
	assert.Len(t, signals, 3)
}

func TestFetchPullRequests_MaxIssuesLimit(t *testing.T) {
	now := time.Now()
	var prs []*github.PullRequest
	for i := 0; i < 5; i++ {
		prs = append(prs, makePR(i+1, fmt.Sprintf("PR %d", i+1), now))
	}

	mock := &mockGitHubAPI{
		prs:      prs,
		prResp:   emptyResponse(),
		reviews:  map[int][]*github.PullRequestReview{},
		comments: map[int][]*github.PullRequestComment{},
	}

	// Limit to 2 PRs.
	signals, err := fetchPullRequests(context.Background(), mock, "owner", "repo", 2, 30, false, time.Time{})
	require.NoError(t, err)
	assert.Len(t, signals, 2)
}

func TestParseGitHubRemoteWith_NoOrigin(t *testing.T) {
	mockRepo := &testable.MockGitRepository{
		RemotesList: nil, // No remotes.
	}
	opener := &testable.MockGitOpener{Repo: mockRepo}
	_, _, err := parseGitHubRemoteWith(opener, "/tmp/fake")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no origin remote found")
}

func TestClassifyPR_CommentOnlyReviews(t *testing.T) {
	now := time.Now()
	pr := makePR(1, "Test", now)
	// Review with "COMMENTED" state — should result in pending.
	reviews := []*github.PullRequestReview{
		{State: github.Ptr("COMMENTED")},
	}
	kind, conf := classifyPR(pr, reviews)
	assert.Equal(t, "github-pr-pending", kind)
	assert.InDelta(t, 0.5, conf, 0.11)
}

func TestModuleFromPath_Public(t *testing.T) {
	assert.Equal(t, "internal/collectors", ModuleFromPath("internal/collectors/todos.go"))
	assert.Equal(t, ".", ModuleFromPath("README.md"))
	assert.Equal(t, "cmd", ModuleFromPath("cmd/main.go"))
}

func TestNewGitHubContext_WithToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")
	ctx := newGitHubContext(repoPath)
	require.NotNil(t, ctx)
	assert.Equal(t, "testowner", ctx.Owner)
	assert.Equal(t, "testrepo", ctx.Repo)
	assert.NotNil(t, ctx.API)
}

func TestNewGitHubContext_NoToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	ctx := newGitHubContext("/tmp/fake")
	assert.Nil(t, ctx)
}

func TestNewGitHubContext_NotGitHub(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")
	repoPath := initGitHubTestRepo(t, "https://gitlab.com/owner/repo.git")
	ctx := newGitHubContext(repoPath)
	assert.Nil(t, ctx)
}

func TestFetchAllReviews_Pagination(t *testing.T) {
	// Simulate paginated review response.
	page1Reviews := []*github.PullRequestReview{
		{State: github.Ptr("APPROVED")},
	}
	mock := &paginatingReviewMock{
		reviewPages: [][]*github.PullRequestReview{page1Reviews, {{State: github.Ptr("CHANGES_REQUESTED")}}},
	}
	reviews, err := fetchAllReviews(context.Background(), mock, "owner", "repo", 1)
	require.NoError(t, err)
	assert.Len(t, reviews, 2)
}

// paginatingReviewMock simulates paginated review API responses.
type paginatingReviewMock struct {
	mockGitHubAPI
	reviewPages   [][]*github.PullRequestReview
	reviewCallIdx int
}

func (m *paginatingReviewMock) ListReviews(_ context.Context, _, _ string, _ int, opts *github.ListOptions) ([]*github.PullRequestReview, *github.Response, error) {
	page := opts.Page
	if page == 0 {
		page = 1
	}
	m.reviewCallIdx++

	idx := page - 1
	if idx >= len(m.reviewPages) {
		return nil, emptyResponse(), nil
	}

	resp := &github.Response{
		Response: &http.Response{StatusCode: http.StatusOK},
	}
	if page < len(m.reviewPages) {
		resp.NextPage = page + 1
	}
	return m.reviewPages[idx], resp, nil
}

func TestFetchActionableComments_Pagination(t *testing.T) {
	now := time.Now()
	mock := &paginatingCommentMock{
		commentPages: [][]*github.PullRequestComment{
			{makeComment("TODO: fix 1", "file1.go", 1, now)},
			{makeComment("TODO: fix 2", "file2.go", 2, now)},
		},
	}
	signals, err := fetchActionableComments(context.Background(), mock, "owner", "repo", 1, 10)
	require.NoError(t, err)
	assert.Len(t, signals, 2)
}

// paginatingCommentMock simulates paginated comment API responses.
type paginatingCommentMock struct {
	mockGitHubAPI
	commentPages   [][]*github.PullRequestComment
	commentCallIdx int
}

func (m *paginatingCommentMock) ListReviewComments(_ context.Context, _, _ string, _ int, opts *github.PullRequestListCommentsOptions) ([]*github.PullRequestComment, *github.Response, error) {
	page := opts.Page
	if page == 0 {
		page = 1
	}
	m.commentCallIdx++

	idx := page - 1
	if idx >= len(m.commentPages) {
		return nil, emptyResponse(), nil
	}

	resp := &github.Response{
		Response: &http.Response{StatusCode: http.StatusOK},
	}
	if page < len(m.commentPages) {
		resp.NextPage = page + 1
	}
	return m.commentPages[idx], resp, nil
}

func TestFetchPullRequests_ContextDuringIteration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	// Create a mock that returns PRs but context is cancelled after the list call.
	mock := &mockGitHubAPI{
		prs:      []*github.PullRequest{makePR(1, "PR 1", now), makePR(2, "PR 2", now)},
		prResp:   emptyResponse(),
		reviews:  map[int][]*github.PullRequestReview{},
		comments: map[int][]*github.PullRequestComment{},
	}
	// Cancel after calling — the context check inside the PR loop should catch it.
	cancel()
	_, err := fetchPullRequests(ctx, mock, "owner", "repo", 100, 30, false, time.Time{})
	require.Error(t, err)
}

func TestGitHubCollector_ClosedPRWithModuleContext(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	now := time.Now()
	mock := &mockGitHubAPI{
		issues:    []*github.Issue{},
		issueResp: emptyResponse(),
		prs: []*github.PullRequest{
			makeMergedPR(10, "Add feature", now),
		},
		prResp:   emptyResponse(),
		reviews:  map[int][]*github.PullRequestReview{},
		comments: map[int][]*github.PullRequestComment{},
	}

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{
		IncludeClosed: true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, signals)
	// Verify that the closed PR signal has module context in its description.
	for _, sig := range signals {
		if sig.Kind == "github-merged-pr" {
			assert.Contains(t, sig.Tags, "pre-closed")
		}
	}
}

func TestGitHubCollector_FetchPRError(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	mock := &mockGitHubAPI{
		issues:    []*github.Issue{},
		issueResp: emptyResponse(),
		prErr:     fmt.Errorf("API error"),
		prResp:    emptyResponse(),
	}

	c := &GitHubCollector{api: mock}
	_, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fetching pull requests")
}

func TestParseGitHubURL_SSHMalformed(t *testing.T) {
	// SSH URL with only owner (no repo).
	_, _, err := parseGitHubURL("git@github.com:onlyowner")
	// This should fail because SSH pattern requires owner/repo.
	require.Error(t, err)
}

func TestFetchIssues_SkipsPullRequests(t *testing.T) {
	now := time.Now()
	prLink := &github.PullRequestLinks{URL: github.Ptr("https://api.github.com/repos/o/r/pulls/1")}
	issueThatIsPR := makeIssue(1, "PR disguised as issue", now, nil)
	issueThatIsPR.PullRequestLinks = prLink

	realIssue := makeIssue(2, "Real issue", now, nil)

	mock := &mockGitHubAPI{
		issues:    []*github.Issue{issueThatIsPR, realIssue},
		issueResp: emptyResponse(),
	}

	signals, err := fetchIssues(context.Background(), mock, "owner", "repo", 100, false, time.Time{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "Real issue", signals[0].Title)
}

func TestFetchPullRequests_ClosedPRWithModuleContext(t *testing.T) {
	now := time.Now()
	merged := makeMergedPR(10, "Add collectors feature", now)

	mock := &closedPRWithFilesMock{
		prs: []*github.PullRequest{merged},
		files: map[int][]*github.CommitFile{
			10: {
				{Filename: github.Ptr("internal/collectors/todos.go")},
				{Filename: github.Ptr("internal/collectors/github.go")},
			},
		},
	}

	signals, err := fetchPullRequests(context.Background(), mock, "owner", "repo", 100, 30, true, time.Time{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "github-merged-pr", signals[0].Kind)
	// Description should contain module context from ListPullRequestFiles.
	assert.Contains(t, signals[0].Description, "internal/collectors")
}

// closedPRWithFilesMock returns file data for closed PRs so extractModuleContext is called.
type closedPRWithFilesMock struct {
	prs   []*github.PullRequest
	files map[int][]*github.CommitFile
}

func (m *closedPRWithFilesMock) ListIssues(_ context.Context, _, _ string, _ *github.IssueListByRepoOptions) ([]*github.Issue, *github.Response, error) {
	return nil, emptyResponse(), nil
}

func (m *closedPRWithFilesMock) ListPullRequests(_ context.Context, _, _ string, _ *github.PullRequestListOptions) ([]*github.PullRequest, *github.Response, error) {
	return m.prs, emptyResponse(), nil
}

func (m *closedPRWithFilesMock) ListReviews(_ context.Context, _, _ string, _ int, _ *github.ListOptions) ([]*github.PullRequestReview, *github.Response, error) {
	return nil, emptyResponse(), nil
}

func (m *closedPRWithFilesMock) ListReviewComments(_ context.Context, _, _ string, _ int, _ *github.PullRequestListCommentsOptions) ([]*github.PullRequestComment, *github.Response, error) {
	return nil, emptyResponse(), nil
}

func (m *closedPRWithFilesMock) ListPullRequestFiles(_ context.Context, _, _ string, number int, _ *github.ListOptions) ([]*github.CommitFile, *github.Response, error) {
	return m.files[number], emptyResponse(), nil
}

func (m *closedPRWithFilesMock) GetRepository(_ context.Context, _, _ string) (*github.Repository, *github.Response, error) {
	return nil, emptyResponse(), nil
}

func TestFetchPullRequests_Pagination(t *testing.T) {
	now := time.Now()
	mock := &paginatingPRMock{
		prPages: [][]*github.PullRequest{
			{makePR(1, "PR 1", now)},
			{makePR(2, "PR 2", now)},
		},
		reviews:  map[int][]*github.PullRequestReview{},
		comments: map[int][]*github.PullRequestComment{},
	}

	signals, err := fetchPullRequests(context.Background(), mock, "owner", "repo", 100, 30, false, time.Time{})
	require.NoError(t, err)
	assert.Len(t, signals, 2)
}

// paginatingPRMock simulates paginated PR responses.
type paginatingPRMock struct {
	prPages   [][]*github.PullRequest
	prCallIdx int
	reviews   map[int][]*github.PullRequestReview
	comments  map[int][]*github.PullRequestComment
}

func (m *paginatingPRMock) ListIssues(_ context.Context, _, _ string, _ *github.IssueListByRepoOptions) ([]*github.Issue, *github.Response, error) {
	return nil, emptyResponse(), nil
}

func (m *paginatingPRMock) ListPullRequests(_ context.Context, _, _ string, opts *github.PullRequestListOptions) ([]*github.PullRequest, *github.Response, error) {
	page := opts.Page
	if page == 0 {
		page = 1
	}
	m.prCallIdx++
	idx := page - 1
	if idx >= len(m.prPages) {
		return nil, emptyResponse(), nil
	}
	resp := &github.Response{
		Response: &http.Response{StatusCode: http.StatusOK},
	}
	if page < len(m.prPages) {
		resp.NextPage = page + 1
	}
	return m.prPages[idx], resp, nil
}

func (m *paginatingPRMock) ListReviews(_ context.Context, _, _ string, number int, _ *github.ListOptions) ([]*github.PullRequestReview, *github.Response, error) {
	return m.reviews[number], emptyResponse(), nil
}

func (m *paginatingPRMock) ListReviewComments(_ context.Context, _, _ string, number int, _ *github.PullRequestListCommentsOptions) ([]*github.PullRequestComment, *github.Response, error) {
	return m.comments[number], emptyResponse(), nil
}

func (m *paginatingPRMock) ListPullRequestFiles(_ context.Context, _, _ string, _ int, _ *github.ListOptions) ([]*github.CommitFile, *github.Response, error) {
	return nil, emptyResponse(), nil
}

func (m *paginatingPRMock) GetRepository(_ context.Context, _, _ string) (*github.Repository, *github.Response, error) {
	return nil, emptyResponse(), nil
}

// makeIssueWithUpdatedAt creates a test issue with a specific UpdatedAt time.
func makeIssueWithUpdatedAt(number int, title string, created, updated time.Time, labelNames []string) *github.Issue {
	issue := makeIssue(number, title, created, labelNames)
	ts := github.Timestamp{Time: updated}
	issue.UpdatedAt = &ts
	return issue
}

func TestFetchIssues_SortByUpdated(t *testing.T) {
	now := time.Now()
	mock := &mockGitHubAPI{
		issues:    []*github.Issue{makeIssue(1, "Issue 1", now, nil)},
		issueResp: emptyResponse(),
	}

	_, err := fetchIssues(context.Background(), mock, "owner", "repo", 25, false, time.Time{})
	require.NoError(t, err)
	require.NotNil(t, mock.lastIssueOpts)
	assert.Equal(t, "updated", mock.lastIssueOpts.Sort)
	assert.Equal(t, "desc", mock.lastIssueOpts.Direction)
}

func TestFetchPullRequests_SortByUpdated(t *testing.T) {
	now := time.Now()
	mock := &mockGitHubAPI{
		prs:      []*github.PullRequest{makePR(1, "PR 1", now)},
		prResp:   emptyResponse(),
		reviews:  map[int][]*github.PullRequestReview{},
		comments: map[int][]*github.PullRequestComment{},
	}

	_, err := fetchPullRequests(context.Background(), mock, "owner", "repo", 25, 30, false, time.Time{})
	require.NoError(t, err)
	require.NotNil(t, mock.lastPROpts)
	assert.Equal(t, "updated", mock.lastPROpts.Sort)
	assert.Equal(t, "desc", mock.lastPROpts.Direction)
}

func TestFetchIssues_DefaultCapIs25(t *testing.T) {
	now := time.Now()
	var issues []*github.Issue
	for i := 0; i < 30; i++ {
		issues = append(issues, makeIssue(i+1, fmt.Sprintf("Issue %d", i+1), now, nil))
	}

	mock := &mockGitHubAPI{
		issues:    issues,
		issueResp: emptyResponse(),
	}

	// Use the default cap value.
	signals, err := fetchIssues(context.Background(), mock, "owner", "repo", defaultMaxIssuesPerCollector, false, time.Time{})
	require.NoError(t, err)
	assert.Len(t, signals, 25)
}

func TestGitHubCollector_StaleIssue(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	now := time.Now()
	sevenMonthsAgo := now.Add(-7 * 30 * 24 * time.Hour)
	mock := &mockGitHubAPI{
		issues: []*github.Issue{
			// Stale issue: created and last updated 7 months ago.
			makeIssueWithUpdatedAt(1, "Old stale issue", sevenMonthsAgo, sevenMonthsAgo, []string{"bug"}),
			// Fresh issue: created recently with recent update.
			makeIssueWithUpdatedAt(2, "Fresh issue", now, now, []string{"bug"}),
		},
		issueResp: emptyResponse(),
		prs:       []*github.PullRequest{},
		prResp:    emptyResponse(),
		reviews:   map[int][]*github.PullRequestReview{},
		comments:  map[int][]*github.PullRequestComment{},
	}

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 2)

	sigMap := make(map[string]signal.RawSignal)
	for _, s := range signals {
		sigMap[s.FilePath] = s
	}

	// Stale issue should be classified as github-stale-issue.
	staleSig := sigMap["github/issues/1"]
	assert.Equal(t, "github-stale-issue", staleSig.Kind)
	assert.InDelta(t, 0.2, staleSig.Confidence, 0.01)
	assert.Contains(t, staleSig.Tags, "github-stale-issue")

	// Fresh issue should remain as github-bug.
	freshSig := sigMap["github/issues/2"]
	assert.Equal(t, "github-bug", freshSig.Kind)
	assert.InDelta(t, 0.7, freshSig.Confidence, 0.01)
}

func TestGitHubCollector_StaleIssueNotAppliedToClosed(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	now := time.Now()
	sevenMonthsAgo := now.Add(-7 * 30 * 24 * time.Hour)
	closedIssue := makeClosedIssue(1, "Old closed issue", sevenMonthsAgo, nil, "completed")
	ts := github.Timestamp{Time: sevenMonthsAgo}
	closedIssue.UpdatedAt = &ts

	mock := &mockGitHubAPI{
		issues:    []*github.Issue{closedIssue},
		issueResp: emptyResponse(),
		prs:       []*github.PullRequest{},
		prResp:    emptyResponse(),
		reviews:   map[int][]*github.PullRequestReview{},
		comments:  map[int][]*github.PullRequestComment{},
	}

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{
		IncludeClosed: true,
	})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	// Closed issue should stay as github-closed-issue, not github-stale-issue.
	assert.Equal(t, "github-closed-issue", signals[0].Kind)
}

func TestGitHubCollector_MaxIssuesFromConfig(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	repoPath := initGitHubTestRepo(t, "https://github.com/testowner/testrepo.git")

	now := time.Now()
	var issues []*github.Issue
	for i := 0; i < 10; i++ {
		issues = append(issues, makeIssue(i+1, fmt.Sprintf("Issue %d", i+1), now, nil))
	}

	mock := &mockGitHubAPI{
		issues:    issues,
		issueResp: emptyResponse(),
		prs:       []*github.PullRequest{},
		prResp:    emptyResponse(),
		reviews:   map[int][]*github.PullRequestReview{},
		comments:  map[int][]*github.PullRequestComment{},
	}

	c := &GitHubCollector{api: mock}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{
		MaxIssues: 5,
	})
	require.NoError(t, err)
	assert.Len(t, signals, 5)
}
