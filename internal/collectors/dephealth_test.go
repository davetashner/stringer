package collectors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-github/v68/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/davetashner/stringer/internal/testable"
)

// mockDephealthGitHubAPI implements dephealthGitHubAPI for testing.
type mockDephealthGitHubAPI struct {
	repos map[string]*github.Repository
	err   error
}

func (m *mockDephealthGitHubAPI) GetRepository(_ context.Context, owner, repo string) (*github.Repository, *github.Response, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	key := owner + "/" + repo
	r, ok := m.repos[key]
	if !ok {
		return nil, nil, fmt.Errorf("repo %s not found", key)
	}
	return r, nil, nil
}

// mockModuleProxyClient implements moduleProxyClient for testing.
type mockModuleProxyClient struct {
	results map[string]*moduleInfo
	err     error
}

func (m *mockModuleProxyClient) FetchLatest(_ context.Context, modulePath string) (*moduleInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	info, ok := m.results[modulePath]
	if !ok {
		return nil, fmt.Errorf("module %s not found", modulePath)
	}
	return info, nil
}

// noopProxyClient is a proxy client that always returns not-found errors (no network).
type noopProxyClient struct{}

func (n *noopProxyClient) FetchLatest(_ context.Context, modulePath string) (*moduleInfo, error) {
	return nil, fmt.Errorf("proxy disabled in test")
}

func TestDepHealthCollector_Name(t *testing.T) {
	c := &DepHealthCollector{}
	assert.Equal(t, "dephealth", c.Name())
}

func TestDepHealthCollector_NoGoMod(t *testing.T) {
	dir := t.TempDir()
	c := &DepHealthCollector{proxyClient: &noopProxyClient{}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	assert.NoError(t, err)
	assert.Nil(t, signals)
}

func TestDepHealthCollector_BasicParse(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/mymod

go 1.22

require (
	github.com/foo/bar v1.2.3
	github.com/baz/qux v0.1.0 // indirect
)
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	c := &DepHealthCollector{proxyClient: &noopProxyClient{}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals) // no replaces or retracts → no signals

	m := c.Metrics()
	require.NotNil(t, m)
	metrics, ok := m.(*DepHealthMetrics)
	require.True(t, ok)

	assert.Equal(t, "example.com/mymod", metrics.ModulePath)
	assert.Equal(t, "1.22", metrics.GoVersion)
	require.Len(t, metrics.Dependencies, 2)

	assert.Equal(t, "github.com/foo/bar", metrics.Dependencies[0].Path)
	assert.Equal(t, "v1.2.3", metrics.Dependencies[0].Version)
	assert.False(t, metrics.Dependencies[0].Indirect)

	assert.Equal(t, "github.com/baz/qux", metrics.Dependencies[1].Path)
	assert.Equal(t, "v0.1.0", metrics.Dependencies[1].Version)
	assert.True(t, metrics.Dependencies[1].Indirect)
}

func TestDepHealthCollector_LocalReplace(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/mymod

go 1.22

require github.com/foo/bar v1.2.3

replace github.com/foo/bar => ../local-bar
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	c := &DepHealthCollector{proxyClient: &noopProxyClient{}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	sig := signals[0]
	assert.Equal(t, "dephealth", sig.Source)
	assert.Equal(t, "local-replace", sig.Kind)
	assert.Equal(t, "go.mod", sig.FilePath)
	assert.Contains(t, sig.Title, "github.com/foo/bar")
	assert.Contains(t, sig.Title, "../local-bar")
	assert.Contains(t, sig.Description, "non-portable")
	assert.Equal(t, 0.5, sig.Confidence)
	assert.Contains(t, sig.Tags, "local-replace")
	assert.Greater(t, sig.Line, 0)

	// Metrics should reflect IsLocal.
	metrics := c.Metrics().(*DepHealthMetrics)
	require.Len(t, metrics.Replaces, 1)
	assert.True(t, metrics.Replaces[0].IsLocal)
}

func TestDepHealthCollector_RemoteReplace(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/mymod

go 1.22

require github.com/foo/bar v1.2.3

replace github.com/foo/bar => github.com/fork/bar v1.2.4
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	c := &DepHealthCollector{proxyClient: &noopProxyClient{}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals) // remote replace → no signal

	metrics := c.Metrics().(*DepHealthMetrics)
	require.Len(t, metrics.Replaces, 1)
	assert.False(t, metrics.Replaces[0].IsLocal)
	assert.Equal(t, "github.com/fork/bar", metrics.Replaces[0].NewPath)
	assert.Equal(t, "v1.2.4", metrics.Replaces[0].NewVersion)
}

func TestDepHealthCollector_RetractDirective(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/mymod

go 1.22

retract v1.0.0 // security vulnerability
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	c := &DepHealthCollector{proxyClient: &noopProxyClient{}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	sig := signals[0]
	assert.Equal(t, "dephealth", sig.Source)
	assert.Equal(t, "retracted-version", sig.Kind)
	assert.Equal(t, "go.mod", sig.FilePath)
	assert.Contains(t, sig.Title, "v1.0.0")
	assert.Contains(t, sig.Description, "security vulnerability")
	assert.Equal(t, 0.3, sig.Confidence)
	assert.Contains(t, sig.Tags, "retracted-version")
	assert.Greater(t, sig.Line, 0)

	metrics := c.Metrics().(*DepHealthMetrics)
	require.Len(t, metrics.Retracts, 1)
	assert.Equal(t, "v1.0.0", metrics.Retracts[0].Low)
	assert.Equal(t, "v1.0.0", metrics.Retracts[0].High)
	assert.Equal(t, "security vulnerability", metrics.Retracts[0].Rationale)
}

func TestDepHealthCollector_RetractRange(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/mymod

go 1.22

retract [v1.0.0, v1.2.0] // broken API
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	c := &DepHealthCollector{proxyClient: &noopProxyClient{}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	sig := signals[0]
	assert.Contains(t, sig.Title, "[v1.0.0, v1.2.0]")
	assert.Contains(t, sig.Description, "[v1.0.0, v1.2.0]")

	metrics := c.Metrics().(*DepHealthMetrics)
	require.Len(t, metrics.Retracts, 1)
	assert.Equal(t, "v1.0.0", metrics.Retracts[0].Low)
	assert.Equal(t, "v1.2.0", metrics.Retracts[0].High)
}

func TestDepHealthCollector_MultipleSignals(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/mymod

go 1.22

require (
	github.com/foo/bar v1.2.3
	github.com/baz/qux v0.1.0
)

replace github.com/foo/bar => ./local-foo

replace github.com/baz/qux => ../local-qux

retract v0.9.0 // broken
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	c := &DepHealthCollector{proxyClient: &noopProxyClient{}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 3) // 2 local replaces + 1 retract

	kinds := make(map[string]int)
	for _, s := range signals {
		kinds[s.Kind]++
	}
	assert.Equal(t, 2, kinds["local-replace"])
	assert.Equal(t, 1, kinds["retracted-version"])
}

func TestDepHealthCollector_MalformedGoMod(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("this is not valid go.mod syntax!!!"), 0o600))

	c := &DepHealthCollector{proxyClient: &noopProxyClient{}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	assert.Error(t, err)
	assert.Nil(t, signals)
	assert.Contains(t, err.Error(), "parsing go.mod")
}

func TestDepHealthCollector_Metrics(t *testing.T) {
	c := &DepHealthCollector{proxyClient: &noopProxyClient{}}

	// Before Collect, Metrics returns nil.
	assert.Nil(t, c.Metrics())

	// After Collect with a valid go.mod, Metrics is populated.
	dir := t.TempDir()
	gomod := `module example.com/test

go 1.22

require github.com/x/y v1.0.0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	m := c.Metrics()
	require.NotNil(t, m)
	metrics, ok := m.(*DepHealthMetrics)
	require.True(t, ok)
	assert.Equal(t, "example.com/test", metrics.ModulePath)
	assert.Len(t, metrics.Dependencies, 1)
}

func TestDepHealthCollector_ReadFileError(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		ReadFileFn: func(name string) ([]byte, error) {
			return nil, os.ErrPermission
		},
	}

	c := &DepHealthCollector{}
	signals, err := c.Collect(context.Background(), "/fake", signal.CollectorOpts{})
	assert.Error(t, err)
	assert.Nil(t, signals)
	assert.Contains(t, err.Error(), "reading go.mod")
}

func TestIsLocalPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"./foo", true},
		{"../bar", true},
		{"/absolute/path", true},
		{"github.com/x/y", false},
		{"example.com/mod", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, isLocalPath(tt.path))
		})
	}
}

// --- C6.2/C6.4: GitHub archived + stale tests ---

func TestExtractGitHubOwnerRepo(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{"standard", "github.com/foo/bar", "foo", "bar", true},
		{"versioned", "github.com/foo/bar/v2", "foo", "bar", true},
		{"subpackage", "github.com/foo/bar/pkg/sub", "foo", "bar", true},
		{"non-github", "golang.org/x/mod", "", "", false},
		{"too-short", "github.com/foo", "", "", false},
		{"empty", "", "", "", false},
		{"other-host", "gitlab.com/foo/bar", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, ok := extractGitHubOwnerRepo(tt.path)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantOwner, owner)
			assert.Equal(t, tt.wantRepo, repo)
		})
	}
}

func TestCheckGitHubDeps_Archived(t *testing.T) {
	api := &mockDephealthGitHubAPI{
		repos: map[string]*github.Repository{
			"foo/bar": {Archived: github.Ptr(true)},
		},
	}
	deps := []ModuleDep{{Path: "github.com/foo/bar", Version: "v1.0.0"}}

	signals := checkGitHubDeps(context.Background(), api, deps, defaultStalenessThreshold)
	require.Len(t, signals, 1)
	assert.Equal(t, "archived-dependency", signals[0].Kind)
	assert.Equal(t, 0.9, signals[0].Confidence)
	assert.Contains(t, signals[0].Title, "foo/bar")
	assert.Contains(t, signals[0].Description, "archived")
	assert.Contains(t, signals[0].Tags, "archived-dependency")
}

func TestCheckGitHubDeps_Stale(t *testing.T) {
	staleTime := time.Now().Add(-3 * 365 * 24 * time.Hour) // 3 years ago
	api := &mockDephealthGitHubAPI{
		repos: map[string]*github.Repository{
			"foo/bar": {
				Archived: github.Ptr(false),
				PushedAt: &github.Timestamp{Time: staleTime},
			},
		},
	}
	deps := []ModuleDep{{Path: "github.com/foo/bar", Version: "v1.0.0"}}

	signals := checkGitHubDeps(context.Background(), api, deps, defaultStalenessThreshold)
	require.Len(t, signals, 1)
	assert.Equal(t, "stale-dependency", signals[0].Kind)
	assert.Equal(t, 0.6, signals[0].Confidence)
	assert.Contains(t, signals[0].Description, "not been pushed")
}

func TestCheckGitHubDeps_ArchivedNotDoubleStale(t *testing.T) {
	staleTime := time.Now().Add(-3 * 365 * 24 * time.Hour)
	api := &mockDephealthGitHubAPI{
		repos: map[string]*github.Repository{
			"foo/bar": {
				Archived: github.Ptr(true),
				PushedAt: &github.Timestamp{Time: staleTime},
			},
		},
	}
	deps := []ModuleDep{{Path: "github.com/foo/bar", Version: "v1.0.0"}}

	signals := checkGitHubDeps(context.Background(), api, deps, defaultStalenessThreshold)
	require.Len(t, signals, 1)
	assert.Equal(t, "archived-dependency", signals[0].Kind, "should only emit archived, not stale")
}

func TestCheckGitHubDeps_Healthy(t *testing.T) {
	recentTime := time.Now().Add(-30 * 24 * time.Hour) // 30 days ago
	api := &mockDephealthGitHubAPI{
		repos: map[string]*github.Repository{
			"foo/bar": {
				Archived: github.Ptr(false),
				PushedAt: &github.Timestamp{Time: recentTime},
			},
		},
	}
	deps := []ModuleDep{{Path: "github.com/foo/bar", Version: "v1.0.0"}}

	signals := checkGitHubDeps(context.Background(), api, deps, defaultStalenessThreshold)
	assert.Empty(t, signals)
}

func TestCheckGitHubDeps_NonGitHub(t *testing.T) {
	api := &mockDephealthGitHubAPI{repos: map[string]*github.Repository{}}
	deps := []ModuleDep{
		{Path: "golang.org/x/mod", Version: "v0.17.0"},
		{Path: "gopkg.in/yaml.v3", Version: "v3.0.1"},
	}

	signals := checkGitHubDeps(context.Background(), api, deps, defaultStalenessThreshold)
	assert.Empty(t, signals, "non-GitHub deps should be silently skipped")
}

func TestCheckGitHubDeps_Dedup(t *testing.T) {
	recentTime := time.Now().Add(-30 * 24 * time.Hour)
	callCount := 0
	api := &countingGitHubAPI{
		inner: &mockDephealthGitHubAPI{
			repos: map[string]*github.Repository{
				"foo/bar": {
					Archived: github.Ptr(false),
					PushedAt: &github.Timestamp{Time: recentTime},
				},
			},
		},
		count: &callCount,
	}
	deps := []ModuleDep{
		{Path: "github.com/foo/bar", Version: "v1.0.0"},
		{Path: "github.com/foo/bar/v2", Version: "v2.0.0"},
		{Path: "github.com/foo/bar/pkg/sub", Version: "v1.1.0"},
	}

	signals := checkGitHubDeps(context.Background(), api, deps, defaultStalenessThreshold)
	assert.Empty(t, signals) // healthy repo
	assert.Equal(t, 1, callCount, "should only make one API call for foo/bar")
}

// countingGitHubAPI wraps a mock and counts API calls.
type countingGitHubAPI struct {
	inner dephealthGitHubAPI
	count *int
}

func (c *countingGitHubAPI) GetRepository(ctx context.Context, owner, repo string) (*github.Repository, *github.Response, error) {
	*c.count++
	return c.inner.GetRepository(ctx, owner, repo)
}

func TestCheckGitHubDeps_APIError(t *testing.T) {
	api := &mockDephealthGitHubAPI{
		err: fmt.Errorf("rate limited"),
	}
	deps := []ModuleDep{{Path: "github.com/foo/bar", Version: "v1.0.0"}}

	signals := checkGitHubDeps(context.Background(), api, deps, defaultStalenessThreshold)
	assert.Empty(t, signals, "API errors should be silently skipped")
}

// --- C6.3: Deprecated module tests ---

func TestCheckDeprecatedDeps_Deprecated(t *testing.T) {
	client := &mockModuleProxyClient{
		results: map[string]*moduleInfo{
			"github.com/old/thing": {
				Version:    "v1.0.0",
				Deprecated: "use github.com/new/thing instead",
			},
		},
	}
	deps := []ModuleDep{{Path: "github.com/old/thing", Version: "v1.0.0"}}

	signals := checkDeprecatedDeps(context.Background(), client, deps)
	require.Len(t, signals, 1)
	assert.Equal(t, "deprecated-dependency", signals[0].Kind)
	assert.Equal(t, 0.8, signals[0].Confidence)
	assert.Contains(t, signals[0].Title, "github.com/old/thing")
	assert.Contains(t, signals[0].Description, "use github.com/new/thing instead")
	assert.Contains(t, signals[0].Tags, "deprecated-dependency")
}

func TestCheckDeprecatedDeps_NotDeprecated(t *testing.T) {
	client := &mockModuleProxyClient{
		results: map[string]*moduleInfo{
			"github.com/good/thing": {Version: "v2.0.0"},
		},
	}
	deps := []ModuleDep{{Path: "github.com/good/thing", Version: "v2.0.0"}}

	signals := checkDeprecatedDeps(context.Background(), client, deps)
	assert.Empty(t, signals)
}

func TestCheckDeprecatedDeps_ProxyError(t *testing.T) {
	client := &mockModuleProxyClient{
		err: fmt.Errorf("proxy returned 404"),
	}
	deps := []ModuleDep{{Path: "github.com/private/thing", Version: "v1.0.0"}}

	signals := checkDeprecatedDeps(context.Background(), client, deps)
	assert.Empty(t, signals, "proxy errors should be silently skipped")
}

func TestCheckDeprecatedDeps_MultipleDeps(t *testing.T) {
	client := &mockModuleProxyClient{
		results: map[string]*moduleInfo{
			"github.com/old/a":  {Version: "v1.0.0", Deprecated: "replaced by b"},
			"github.com/good/c": {Version: "v1.0.0"},
		},
	}
	deps := []ModuleDep{
		{Path: "github.com/old/a", Version: "v1.0.0"},
		{Path: "github.com/good/c", Version: "v1.0.0"},
		{Path: "github.com/missing/d", Version: "v1.0.0"}, // not in mock → error → skipped
	}

	signals := checkDeprecatedDeps(context.Background(), client, deps)
	require.Len(t, signals, 1)
	assert.Equal(t, "deprecated-dependency", signals[0].Kind)
	assert.Contains(t, signals[0].Title, "github.com/old/a")
}

// --- Integration tests ---

func TestDepHealthCollector_NoToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	dir := t.TempDir()
	gomod := `module example.com/test

go 1.22

require github.com/foo/bar v1.0.0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	proxy := &mockModuleProxyClient{
		results: map[string]*moduleInfo{
			"github.com/foo/bar": {Version: "v1.0.0", Deprecated: "use baz"},
		},
	}
	c := &DepHealthCollector{proxyClient: proxy}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	// GitHub checks skipped (no token), but proxy still finds deprecated.
	require.Len(t, signals, 1)
	assert.Equal(t, "deprecated-dependency", signals[0].Kind)
}

func TestDepHealthCollector_Integration(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "fake-token")

	dir := t.TempDir()
	gomod := `module example.com/test

go 1.22

require (
	github.com/archived/repo v1.0.0
	github.com/stale/repo v1.0.0
	github.com/deprecated/mod v1.0.0
	github.com/healthy/repo v1.0.0
	golang.org/x/text v0.14.0
)

replace github.com/local/thing => ../local
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	staleTime := time.Now().Add(-3 * 365 * 24 * time.Hour)
	recentTime := time.Now().Add(-30 * 24 * time.Hour)

	ghAPI := &mockDephealthGitHubAPI{
		repos: map[string]*github.Repository{
			"archived/repo": {Archived: github.Ptr(true)},
			"stale/repo": {
				Archived: github.Ptr(false),
				PushedAt: &github.Timestamp{Time: staleTime},
			},
			"deprecated/mod": {
				Archived: github.Ptr(false),
				PushedAt: &github.Timestamp{Time: recentTime},
			},
			"healthy/repo": {
				Archived: github.Ptr(false),
				PushedAt: &github.Timestamp{Time: recentTime},
			},
		},
	}

	proxy := &mockModuleProxyClient{
		results: map[string]*moduleInfo{
			"github.com/archived/repo":  {Version: "v1.0.0"},
			"github.com/stale/repo":     {Version: "v1.0.0"},
			"github.com/deprecated/mod": {Version: "v1.0.0", Deprecated: "use github.com/new/mod instead"},
			"github.com/healthy/repo":   {Version: "v1.0.0"},
			"golang.org/x/text":         {Version: "v0.14.0"},
		},
	}

	c := &DepHealthCollector{ghAPI: ghAPI, proxyClient: proxy}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	// Expect: archived(1) + stale(1) + deprecated(1) = 3 signals.
	kinds := make(map[string]int)
	for _, s := range signals {
		kinds[s.Kind]++
		assert.Equal(t, "dephealth", s.Source)
		assert.Equal(t, "go.mod", s.FilePath)
	}
	assert.Equal(t, 1, kinds["archived-dependency"])
	assert.Equal(t, 1, kinds["stale-dependency"])
	assert.Equal(t, 1, kinds["deprecated-dependency"])

	// Metrics should be populated.
	metrics := c.Metrics().(*DepHealthMetrics)
	assert.Len(t, metrics.Archived, 1)
	assert.Len(t, metrics.Stale, 1)
	assert.Len(t, metrics.Deprecated, 1)
	assert.Len(t, metrics.Dependencies, 5)
}

func TestDepHealthCollector_StalenessThresholdOpt(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "fake-token")

	dir := t.TempDir()
	gomod := `module example.com/test

go 1.22

require github.com/foo/bar v1.0.0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	// Repo was pushed 6 months ago — stale with 3m threshold, not stale with default 2y.
	sixMonthsAgo := time.Now().Add(-6 * 30 * 24 * time.Hour)
	ghAPI := &mockDephealthGitHubAPI{
		repos: map[string]*github.Repository{
			"foo/bar": {
				Archived: github.Ptr(false),
				PushedAt: &github.Timestamp{Time: sixMonthsAgo},
			},
		},
	}

	// With default threshold (2y), should not be stale.
	c := &DepHealthCollector{ghAPI: ghAPI, proxyClient: &noopProxyClient{}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals)

	// With 3-month threshold, should be stale.
	c2 := &DepHealthCollector{ghAPI: ghAPI, proxyClient: &noopProxyClient{}}
	signals2, err := c2.Collect(context.Background(), dir, signal.CollectorOpts{
		StalenessThreshold: "3m",
	})
	require.NoError(t, err)
	require.Len(t, signals2, 1)
	assert.Equal(t, "stale-dependency", signals2[0].Kind)
}
