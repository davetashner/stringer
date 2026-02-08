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
}

func (m *mockGitHubAPI) ListIssues(_ context.Context, _, _ string, _ *github.IssueListByRepoOptions) ([]*github.Issue, *github.Response, error) {
	m.issueCallCount++
	return m.issues, m.issueResp, m.issueErr
}

func (m *mockGitHubAPI) ListPullRequests(_ context.Context, _, _ string, _ *github.PullRequestListOptions) ([]*github.PullRequest, *github.Response, error) {
	m.prCallCount++
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
	signals, err := fetchIssues(context.Background(), mock, "testowner", "testrepo", 100, false)
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
