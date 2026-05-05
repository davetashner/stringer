// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

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
			// go.mod read returns permission error; other manifests return not-found.
			if filepath.Base(name) == "go.mod" {
				return nil, os.ErrPermission
			}
			return nil, os.ErrNotExist
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

// --- npm registry tests ---

// mockNpmRegistryClient implements npmRegistryClient for testing.
type mockNpmRegistryClient struct {
	results map[string]*npmPackageInfo
	err     error
}

func (m *mockNpmRegistryClient) FetchPackage(_ context.Context, name string) (*npmPackageInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	info, ok := m.results[name]
	if !ok {
		return nil, fmt.Errorf("package %s not found", name)
	}
	return info, nil
}

func TestCheckNpmDeps_Deprecated(t *testing.T) {
	client := &mockNpmRegistryClient{
		results: map[string]*npmPackageInfo{
			"old-package": {Name: "old-package", Deprecated: "use new-package instead"},
		},
	}
	deps := []PackageQuery{{Ecosystem: "npm", Name: "old-package", Version: "1.0.0"}}

	signals := checkNpmDeps(context.Background(), client, deps, "package.json")
	require.Len(t, signals, 1)
	assert.Equal(t, "deprecated-dependency", signals[0].Kind)
	assert.Equal(t, 0.8, signals[0].Confidence)
	assert.Contains(t, signals[0].Title, "old-package")
	assert.Contains(t, signals[0].Description, "use new-package instead")
	assert.Contains(t, signals[0].Tags, "npm")
	assert.Equal(t, "package.json", signals[0].FilePath)
}

func TestCheckNpmDeps_NotDeprecated(t *testing.T) {
	client := &mockNpmRegistryClient{
		results: map[string]*npmPackageInfo{
			"good-package": {Name: "good-package"},
		},
	}
	deps := []PackageQuery{{Ecosystem: "npm", Name: "good-package", Version: "1.0.0"}}

	signals := checkNpmDeps(context.Background(), client, deps, "package.json")
	assert.Empty(t, signals)
}

func TestCheckNpmDeps_Error(t *testing.T) {
	client := &mockNpmRegistryClient{
		err: fmt.Errorf("network error"),
	}
	deps := []PackageQuery{{Ecosystem: "npm", Name: "some-package", Version: "1.0.0"}}

	signals := checkNpmDeps(context.Background(), client, deps, "package.json")
	assert.Empty(t, signals, "errors should be silently skipped")
}

func TestCheckNpmDeps_Multiple(t *testing.T) {
	client := &mockNpmRegistryClient{
		results: map[string]*npmPackageInfo{
			"old-a":  {Name: "old-a", Deprecated: "replaced"},
			"good-b": {Name: "good-b"},
			"old-c":  {Name: "old-c", Deprecated: "abandoned"},
		},
	}
	deps := []PackageQuery{
		{Ecosystem: "npm", Name: "old-a", Version: "1.0.0"},
		{Ecosystem: "npm", Name: "good-b", Version: "2.0.0"},
		{Ecosystem: "npm", Name: "old-c", Version: "3.0.0"},
	}

	signals := checkNpmDeps(context.Background(), client, deps, "package.json")
	require.Len(t, signals, 2)
}

// --- crates.io registry tests ---

// mockCratesRegistryClient implements cratesRegistryClient for testing.
type mockCratesRegistryClient struct {
	results map[string]*crateInfo
	err     error
}

func (m *mockCratesRegistryClient) FetchCrate(_ context.Context, name string) (*crateInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	info, ok := m.results[name]
	if !ok {
		return nil, fmt.Errorf("crate %s not found", name)
	}
	return info, nil
}

func TestCheckCratesDeps_Yanked(t *testing.T) {
	client := &mockCratesRegistryClient{
		results: map[string]*crateInfo{
			"bad-crate": {
				Versions: []crateVersion{
					{Num: "1.0.0", Yanked: true},
					{Num: "0.9.0", Yanked: false},
				},
			},
		},
	}
	deps := []PackageQuery{{Ecosystem: "crates.io", Name: "bad-crate", Version: "1.0.0"}}

	signals := checkCratesDeps(context.Background(), client, deps)
	require.Len(t, signals, 1)
	assert.Equal(t, "yanked-dependency", signals[0].Kind)
	assert.Equal(t, 0.9, signals[0].Confidence)
	assert.Contains(t, signals[0].Title, "bad-crate@1.0.0")
	assert.Contains(t, signals[0].Description, "yanked")
	assert.Contains(t, signals[0].Tags, "rust")
	assert.Equal(t, "Cargo.toml", signals[0].FilePath)
}

func TestCheckCratesDeps_NotYanked(t *testing.T) {
	client := &mockCratesRegistryClient{
		results: map[string]*crateInfo{
			"good-crate": {
				Versions: []crateVersion{
					{Num: "1.0.0", Yanked: false},
				},
			},
		},
	}
	deps := []PackageQuery{{Ecosystem: "crates.io", Name: "good-crate", Version: "1.0.0"}}

	signals := checkCratesDeps(context.Background(), client, deps)
	assert.Empty(t, signals)
}

func TestCheckCratesDeps_VersionNotFound(t *testing.T) {
	client := &mockCratesRegistryClient{
		results: map[string]*crateInfo{
			"some-crate": {
				Versions: []crateVersion{
					{Num: "2.0.0", Yanked: false},
				},
			},
		},
	}
	// Query for version 1.0.0 which doesn't exist in the response.
	deps := []PackageQuery{{Ecosystem: "crates.io", Name: "some-crate", Version: "1.0.0"}}

	signals := checkCratesDeps(context.Background(), client, deps)
	assert.Empty(t, signals, "version not found → no signal")
}

func TestCheckCratesDeps_Error(t *testing.T) {
	client := &mockCratesRegistryClient{
		err: fmt.Errorf("rate limited"),
	}
	deps := []PackageQuery{{Ecosystem: "crates.io", Name: "some-crate", Version: "1.0.0"}}

	signals := checkCratesDeps(context.Background(), client, deps)
	assert.Empty(t, signals, "errors should be silently skipped")
}

// --- Maven Central tests ---

// mockMavenRegistryClient implements mavenRegistryClient for testing.
type mockMavenRegistryClient struct {
	results map[string]*mavenArtifactInfo
	err     error
}

func (m *mockMavenRegistryClient) FetchArtifact(_ context.Context, groupID, artifactID string) (*mavenArtifactInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := groupID + ":" + artifactID
	info, ok := m.results[key]
	if !ok {
		return nil, fmt.Errorf("artifact %s not found", key)
	}
	return info, nil
}

func TestCheckMavenDeps_Stale(t *testing.T) {
	// Artifact last updated 5 years ago.
	staleTimestamp := time.Now().Add(-5 * 365 * 24 * time.Hour).UnixMilli()
	client := &mockMavenRegistryClient{
		results: map[string]*mavenArtifactInfo{
			"com.old:artifact": {
				Response: struct {
					NumFound int             `json:"numFound"`
					Docs     []mavenArtifact `json:"docs"`
				}{
					NumFound: 1,
					Docs: []mavenArtifact{
						{GroupID: "com.old", ArtifactID: "artifact", Version: "1.0.0", Timestamp: staleTimestamp},
					},
				},
			},
		},
	}
	deps := []PackageQuery{{Ecosystem: "Maven", Name: "com.old:artifact", Version: "1.0.0"}}

	signals := checkMavenDeps(context.Background(), client, deps, "pom.xml")
	require.Len(t, signals, 1)
	assert.Equal(t, "stale-dependency", signals[0].Kind)
	assert.Equal(t, 0.5, signals[0].Confidence)
	assert.Contains(t, signals[0].Title, "com.old:artifact")
	assert.Contains(t, signals[0].Tags, "maven")
	assert.Equal(t, "pom.xml", signals[0].FilePath)
}

func TestCheckMavenDeps_NotStale(t *testing.T) {
	recentTimestamp := time.Now().Add(-30 * 24 * time.Hour).UnixMilli()
	client := &mockMavenRegistryClient{
		results: map[string]*mavenArtifactInfo{
			"com.fresh:artifact": {
				Response: struct {
					NumFound int             `json:"numFound"`
					Docs     []mavenArtifact `json:"docs"`
				}{
					NumFound: 1,
					Docs: []mavenArtifact{
						{GroupID: "com.fresh", ArtifactID: "artifact", Version: "2.0.0", Timestamp: recentTimestamp},
					},
				},
			},
		},
	}
	deps := []PackageQuery{{Ecosystem: "Maven", Name: "com.fresh:artifact", Version: "2.0.0"}}

	signals := checkMavenDeps(context.Background(), client, deps, "pom.xml")
	assert.Empty(t, signals)
}

func TestCheckMavenDeps_NotFound(t *testing.T) {
	client := &mockMavenRegistryClient{
		results: map[string]*mavenArtifactInfo{
			"com.x:y": {
				Response: struct {
					NumFound int             `json:"numFound"`
					Docs     []mavenArtifact `json:"docs"`
				}{
					NumFound: 0,
					Docs:     nil,
				},
			},
		},
	}
	deps := []PackageQuery{{Ecosystem: "Maven", Name: "com.x:y", Version: "1.0.0"}}

	signals := checkMavenDeps(context.Background(), client, deps, "pom.xml")
	assert.Empty(t, signals, "not found → no signal")
}

func TestCheckMavenDeps_Error(t *testing.T) {
	client := &mockMavenRegistryClient{
		err: fmt.Errorf("network error"),
	}
	deps := []PackageQuery{{Ecosystem: "Maven", Name: "com.x:y", Version: "1.0.0"}}

	signals := checkMavenDeps(context.Background(), client, deps, "pom.xml")
	assert.Empty(t, signals, "errors should be silently skipped")
}

func TestCheckMavenDeps_InvalidName(t *testing.T) {
	client := &mockMavenRegistryClient{
		results: map[string]*mavenArtifactInfo{},
	}
	// Name without colon separator.
	deps := []PackageQuery{{Ecosystem: "Maven", Name: "no-colon", Version: "1.0.0"}}

	signals := checkMavenDeps(context.Background(), client, deps, "pom.xml")
	assert.Empty(t, signals, "invalid name → skipped")
}

// --- NuGet registry tests ---

// mockNuGetRegistryClient implements nugetRegistryClient for testing.
type mockNuGetRegistryClient struct {
	results map[string]*nugetRegistrationInfo
	err     error
}

func (m *mockNuGetRegistryClient) FetchRegistration(_ context.Context, id string) (*nugetRegistrationInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	info, ok := m.results[id]
	if !ok {
		return nil, fmt.Errorf("package %s not found", id)
	}
	return info, nil
}

func TestCheckNuGetDeps_Deprecated(t *testing.T) {
	client := &mockNuGetRegistryClient{
		results: map[string]*nugetRegistrationInfo{
			"OldPackage": {
				Items: []nugetRegistrationPage{
					{
						Items: []nugetRegistrationLeaf{
							{
								CatalogEntry: nugetCatalogEntry{
									ID:      "OldPackage",
									Version: "1.0.0",
									Deprecation: &nugetDeprecation{
										Reasons: []string{"Legacy"},
										Message: "Use NewPackage instead",
									},
								},
							},
						},
					},
				},
			},
		},
	}
	deps := []PackageQuery{{Ecosystem: "NuGet", Name: "OldPackage", Version: "1.0.0"}}

	signals := checkNuGetDeps(context.Background(), client, deps, "MyApp.csproj")
	require.Len(t, signals, 1)
	assert.Equal(t, "deprecated-dependency", signals[0].Kind)
	assert.Equal(t, 0.8, signals[0].Confidence)
	assert.Contains(t, signals[0].Title, "OldPackage")
	assert.Contains(t, signals[0].Tags, "nuget")
	assert.Equal(t, "MyApp.csproj", signals[0].FilePath)
}

func TestCheckNuGetDeps_NotDeprecated(t *testing.T) {
	client := &mockNuGetRegistryClient{
		results: map[string]*nugetRegistrationInfo{
			"GoodPackage": {
				Items: []nugetRegistrationPage{
					{
						Items: []nugetRegistrationLeaf{
							{
								CatalogEntry: nugetCatalogEntry{
									ID:      "GoodPackage",
									Version: "1.0.0",
									Listed:  true,
								},
							},
						},
					},
				},
			},
		},
	}
	deps := []PackageQuery{{Ecosystem: "NuGet", Name: "GoodPackage", Version: "1.0.0"}}

	signals := checkNuGetDeps(context.Background(), client, deps, "MyApp.csproj")
	assert.Empty(t, signals)
}

func TestCheckNuGetDeps_VersionMismatch(t *testing.T) {
	client := &mockNuGetRegistryClient{
		results: map[string]*nugetRegistrationInfo{
			"SomePackage": {
				Items: []nugetRegistrationPage{
					{
						Items: []nugetRegistrationLeaf{
							{
								CatalogEntry: nugetCatalogEntry{
									ID:      "SomePackage",
									Version: "2.0.0",
									Deprecation: &nugetDeprecation{
										Reasons: []string{"Legacy"},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	// Query for version 1.0.0, but only 2.0.0 is deprecated.
	deps := []PackageQuery{{Ecosystem: "NuGet", Name: "SomePackage", Version: "1.0.0"}}

	signals := checkNuGetDeps(context.Background(), client, deps, "MyApp.csproj")
	assert.Empty(t, signals, "version mismatch → no signal")
}

func TestCheckNuGetDeps_Error(t *testing.T) {
	client := &mockNuGetRegistryClient{
		err: fmt.Errorf("API error"),
	}
	deps := []PackageQuery{{Ecosystem: "NuGet", Name: "SomePackage", Version: "1.0.0"}}

	signals := checkNuGetDeps(context.Background(), client, deps, "MyApp.csproj")
	assert.Empty(t, signals, "errors should be silently skipped")
}

func TestIsNuGetDeprecated(t *testing.T) {
	info := &nugetRegistrationInfo{
		Items: []nugetRegistrationPage{
			{
				Items: []nugetRegistrationLeaf{
					{CatalogEntry: nugetCatalogEntry{Version: "1.0.0"}},
					{CatalogEntry: nugetCatalogEntry{
						Version:     "2.0.0",
						Deprecation: &nugetDeprecation{Reasons: []string{"Legacy"}},
					}},
				},
			},
		},
	}

	assert.False(t, isNuGetDeprecated(info, "1.0.0"))
	assert.True(t, isNuGetDeprecated(info, "2.0.0"))
	assert.False(t, isNuGetDeprecated(info, "3.0.0"), "unknown version → not deprecated")
}

// --- PyPI registry tests ---

// mockPyPIRegistryClient implements pypiRegistryClient for testing.
type mockPyPIRegistryClient struct {
	results map[string]*pypiPackageInfo
	err     error
}

func (m *mockPyPIRegistryClient) FetchPackage(_ context.Context, name string) (*pypiPackageInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	info, ok := m.results[name]
	if !ok {
		return nil, fmt.Errorf("package %s not found", name)
	}
	return info, nil
}

func TestCheckPyPIDeps_Inactive(t *testing.T) {
	info := &pypiPackageInfo{}
	info.Info.Name = "old-lib"
	info.Info.Classifiers = []string{
		"Programming Language :: Python :: 3",
		"Development Status :: 7 - Inactive",
	}

	client := &mockPyPIRegistryClient{
		results: map[string]*pypiPackageInfo{
			"old-lib": info,
		},
	}
	deps := []PackageQuery{{Ecosystem: "PyPI", Name: "old-lib", Version: "1.0.0"}}

	signals := checkPyPIDeps(context.Background(), client, deps, "requirements.txt")
	require.Len(t, signals, 1)
	assert.Equal(t, "deprecated-dependency", signals[0].Kind)
	assert.Equal(t, 0.7, signals[0].Confidence)
	assert.Contains(t, signals[0].Title, "old-lib")
	assert.Contains(t, signals[0].Description, "inactive")
	assert.Contains(t, signals[0].Tags, "python")
	assert.Equal(t, "requirements.txt", signals[0].FilePath)
}

func TestCheckPyPIDeps_Active(t *testing.T) {
	info := &pypiPackageInfo{}
	info.Info.Name = "good-lib"
	info.Info.Classifiers = []string{
		"Development Status :: 5 - Production/Stable",
	}

	client := &mockPyPIRegistryClient{
		results: map[string]*pypiPackageInfo{
			"good-lib": info,
		},
	}
	deps := []PackageQuery{{Ecosystem: "PyPI", Name: "good-lib", Version: "2.0.0"}}

	signals := checkPyPIDeps(context.Background(), client, deps, "requirements.txt")
	assert.Empty(t, signals)
}

func TestCheckPyPIDeps_Error(t *testing.T) {
	client := &mockPyPIRegistryClient{
		err: fmt.Errorf("timeout"),
	}
	deps := []PackageQuery{{Ecosystem: "PyPI", Name: "some-lib", Version: "1.0.0"}}

	signals := checkPyPIDeps(context.Background(), client, deps, "requirements.txt")
	assert.Empty(t, signals, "errors should be silently skipped")
}

func TestPyPIDeprecationReason(t *testing.T) {
	tests := []struct {
		name        string
		classifiers []string
		wantReason  string
	}{
		{
			"inactive",
			[]string{"Development Status :: 7 - Inactive"},
			"inactive (Development Status :: 7 - Inactive)",
		},
		{
			"production stable",
			[]string{"Development Status :: 5 - Production/Stable"},
			"",
		},
		{
			"no classifiers",
			nil,
			"",
		},
		{
			"planning",
			[]string{"Development Status :: 1 - Planning"},
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &pypiPackageInfo{}
			info.Info.Classifiers = tt.classifiers
			assert.Equal(t, tt.wantReason, pypiDeprecationReason(info))
		})
	}
}

// --- Multi-ecosystem integration tests ---

func TestDepHealthCollector_NpmOnly(t *testing.T) {
	dir := t.TempDir()
	pkgJSON := `{
  "name": "my-app",
  "dependencies": {
    "old-pkg": "^1.0.0",
    "good-pkg": "^2.0.0"
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o600))

	npmClient := &mockNpmRegistryClient{
		results: map[string]*npmPackageInfo{
			"old-pkg":  {Name: "old-pkg", Deprecated: "use new-pkg"},
			"good-pkg": {Name: "good-pkg"},
		},
	}

	c := &DepHealthCollector{npmClient: npmClient}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "deprecated-dependency", signals[0].Kind)
	assert.Contains(t, signals[0].Title, "old-pkg")

	metrics := c.Metrics().(*DepHealthMetrics)
	assert.Contains(t, metrics.Ecosystems, "npm")
	assert.Len(t, metrics.Deprecated, 1)
}

func TestDepHealthCollector_CargoOnly(t *testing.T) {
	dir := t.TempDir()
	cargoTOML := `[package]
name = "my-crate"
version = "0.1.0"

[dependencies]
serde = "1.0.0"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargoTOML), 0o600))

	cratesClient := &mockCratesRegistryClient{
		results: map[string]*crateInfo{
			"serde": {
				Versions: []crateVersion{
					{Num: "1.0.0", Yanked: true},
				},
			},
		},
	}

	c := &DepHealthCollector{cratesClient: cratesClient}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "yanked-dependency", signals[0].Kind)
	assert.Contains(t, signals[0].Title, "serde@1.0.0")

	metrics := c.Metrics().(*DepHealthMetrics)
	assert.Contains(t, metrics.Ecosystems, "cargo")
	assert.Len(t, metrics.Yanked, 1)
}

func TestDepHealthCollector_MavenOnly(t *testing.T) {
	dir := t.TempDir()
	pomXML := `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>my-app</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>com.old</groupId>
      <artifactId>lib</artifactId>
      <version>1.0.0</version>
    </dependency>
  </dependencies>
</project>`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte(pomXML), 0o600))

	staleTimestamp := time.Now().Add(-5 * 365 * 24 * time.Hour).UnixMilli()
	mavenClient := &mockMavenRegistryClient{
		results: map[string]*mavenArtifactInfo{
			"com.old:lib": {
				Response: struct {
					NumFound int             `json:"numFound"`
					Docs     []mavenArtifact `json:"docs"`
				}{
					NumFound: 1,
					Docs: []mavenArtifact{
						{GroupID: "com.old", ArtifactID: "lib", Version: "1.0.0", Timestamp: staleTimestamp},
					},
				},
			},
		},
	}

	c := &DepHealthCollector{mavenClient: mavenClient}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "stale-dependency", signals[0].Kind)
	assert.Contains(t, signals[0].Title, "com.old:lib")

	metrics := c.Metrics().(*DepHealthMetrics)
	assert.Contains(t, metrics.Ecosystems, "maven")
	assert.Len(t, metrics.Stale, 1)
}

func TestDepHealthCollector_PythonOnly(t *testing.T) {
	dir := t.TempDir()
	requirements := `requests==2.28.0
old-lib==1.0.0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte(requirements), 0o600))

	inactiveInfo := &pypiPackageInfo{}
	inactiveInfo.Info.Name = "old-lib"
	inactiveInfo.Info.Classifiers = []string{"Development Status :: 7 - Inactive"}

	activeInfo := &pypiPackageInfo{}
	activeInfo.Info.Name = "requests"

	pypiClient := &mockPyPIRegistryClient{
		results: map[string]*pypiPackageInfo{
			"requests": activeInfo,
			"old-lib":  inactiveInfo,
		},
	}

	c := &DepHealthCollector{pypiClient: pypiClient}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "deprecated-dependency", signals[0].Kind)
	assert.Contains(t, signals[0].Title, "old-lib")

	metrics := c.Metrics().(*DepHealthMetrics)
	assert.Contains(t, metrics.Ecosystems, "python")
}

func TestDepHealthCollector_MultiEcosystem(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	dir := t.TempDir()

	// Go manifest.
	gomod := `module example.com/test
go 1.22
require github.com/x/y v1.0.0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	// npm manifest.
	pkgJSON := `{"dependencies": {"deprecated-npm": "^1.0.0"}}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkgJSON), 0o600))

	// Cargo manifest.
	cargoTOML := `[dependencies]
yanked-crate = "0.5.0"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(cargoTOML), 0o600))

	proxyClient := &noopProxyClient{}
	npmClient := &mockNpmRegistryClient{
		results: map[string]*npmPackageInfo{
			"deprecated-npm": {Name: "deprecated-npm", Deprecated: "old"},
		},
	}
	cratesClient := &mockCratesRegistryClient{
		results: map[string]*crateInfo{
			"yanked-crate": {
				Versions: []crateVersion{
					{Num: "0.5.0", Yanked: true},
				},
			},
		},
	}

	c := &DepHealthCollector{
		proxyClient:  proxyClient,
		npmClient:    npmClient,
		cratesClient: cratesClient,
	}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	// Expect: 1 npm deprecated + 1 cargo yanked = 2 signals (Go proxy returns errors, silently skipped).
	kinds := make(map[string]int)
	for _, s := range signals {
		kinds[s.Kind]++
	}
	assert.Equal(t, 1, kinds["deprecated-dependency"])
	assert.Equal(t, 1, kinds["yanked-dependency"])

	metrics := c.Metrics().(*DepHealthMetrics)
	assert.Contains(t, metrics.Ecosystems, "go")
	assert.Contains(t, metrics.Ecosystems, "npm")
	assert.Contains(t, metrics.Ecosystems, "cargo")
	assert.Len(t, metrics.Deprecated, 1)
	assert.Len(t, metrics.Yanked, 1)
}

func TestDepHealthCollector_NoManifestsAtAll(t *testing.T) {
	dir := t.TempDir()
	c := &DepHealthCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	assert.NoError(t, err)
	assert.Nil(t, signals, "no manifests at all → nil signals")
	assert.Nil(t, c.Metrics(), "no manifests → nil metrics")
}

func TestDepHealthCollector_EcosystemsTracked(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/test
go 1.22
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	c := &DepHealthCollector{proxyClient: &noopProxyClient{}}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	metrics := c.Metrics().(*DepHealthMetrics)
	assert.Contains(t, metrics.Ecosystems, "go")
	assert.Len(t, metrics.Ecosystems, 1, "only go ecosystem should be tracked")
}

// --- Packagist registry tests ---

// mockPackagistRegistryClient implements packagistRegistryClient for testing.
type mockPackagistRegistryClient struct {
	results map[string]*packagistPackageInfo
	err     error
}

func (m *mockPackagistRegistryClient) FetchPackage(_ context.Context, name string) (*packagistPackageInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	info, ok := m.results[name]
	if !ok {
		return nil, fmt.Errorf("package %s not found", name)
	}
	return info, nil
}

func TestCheckPackagistDeps_Abandoned(t *testing.T) {
	client := &mockPackagistRegistryClient{
		results: map[string]*packagistPackageInfo{
			"vendor/old-pkg": {
				Packages: map[string][]packagistVersion{
					"vendor/old-pkg": {{Version: "1.0.0", Abandoned: "vendor/new-pkg"}},
				},
			},
		},
	}
	deps := []PackageQuery{{Ecosystem: "Packagist", Name: "vendor/old-pkg", Version: "1.0.0"}}

	signals := checkPackagistDeps(context.Background(), client, deps, "composer.json")
	require.Len(t, signals, 1)
	assert.Equal(t, "deprecated-dependency", signals[0].Kind)
	assert.Equal(t, 0.8, signals[0].Confidence)
	assert.Contains(t, signals[0].Title, "vendor/old-pkg")
	assert.Contains(t, signals[0].Description, "vendor/new-pkg")
	assert.Contains(t, signals[0].Tags, "php")
	assert.Equal(t, "composer.json", signals[0].FilePath)
}

func TestCheckPackagistDeps_NotAbandoned(t *testing.T) {
	client := &mockPackagistRegistryClient{
		results: map[string]*packagistPackageInfo{
			"vendor/good-pkg": {
				Packages: map[string][]packagistVersion{
					"vendor/good-pkg": {{Version: "2.0.0", Abandoned: false}},
				},
			},
		},
	}
	deps := []PackageQuery{{Ecosystem: "Packagist", Name: "vendor/good-pkg", Version: "2.0.0"}}

	signals := checkPackagistDeps(context.Background(), client, deps, "composer.json")
	assert.Empty(t, signals)
}

func TestCheckPackagistDeps_Error(t *testing.T) {
	client := &mockPackagistRegistryClient{
		err: fmt.Errorf("network error"),
	}
	deps := []PackageQuery{{Ecosystem: "Packagist", Name: "vendor/pkg", Version: "1.0.0"}}

	signals := checkPackagistDeps(context.Background(), client, deps, "composer.json")
	assert.Empty(t, signals, "errors should be silently skipped")
}

func TestCheckPackagistDeps_AbandonedBool(t *testing.T) {
	client := &mockPackagistRegistryClient{
		results: map[string]*packagistPackageInfo{
			"vendor/dead-pkg": {
				Packages: map[string][]packagistVersion{
					"vendor/dead-pkg": {{Version: "1.0.0", Abandoned: true}},
				},
			},
		},
	}
	deps := []PackageQuery{{Ecosystem: "Packagist", Name: "vendor/dead-pkg", Version: "1.0.0"}}

	signals := checkPackagistDeps(context.Background(), client, deps, "composer.json")
	require.Len(t, signals, 1)
	assert.Contains(t, signals[0].Description, "alternative")
}

func TestDepHealthCollector_PackagistOnly(t *testing.T) {
	dir := t.TempDir()
	composerJSON := `{
  "require": {
    "vendor/old-pkg": "^1.0",
    "vendor/good-pkg": "^2.0"
  }
}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composerJSON), 0o600))

	client := &mockPackagistRegistryClient{
		results: map[string]*packagistPackageInfo{
			"vendor/old-pkg": {
				Packages: map[string][]packagistVersion{
					"vendor/old-pkg": {{Version: "1.0.0", Abandoned: "vendor/new-pkg"}},
				},
			},
			"vendor/good-pkg": {
				Packages: map[string][]packagistVersion{
					"vendor/good-pkg": {{Version: "2.0.0", Abandoned: false}},
				},
			},
		},
	}

	c := &DepHealthCollector{packagistClient: client}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "deprecated-dependency", signals[0].Kind)
	assert.Contains(t, signals[0].Title, "vendor/old-pkg")

	metrics := c.Metrics().(*DepHealthMetrics)
	assert.Contains(t, metrics.Ecosystems, "packagist")
	assert.Len(t, metrics.Deprecated, 1)
}

// --- Hex.pm registry tests ---

// mockHexRegistryClient implements hexRegistryClient for testing.
type mockHexRegistryClient struct {
	results map[string]*hexPackageInfo
	err     error
}

func (m *mockHexRegistryClient) FetchPackage(_ context.Context, name string) (*hexPackageInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	info, ok := m.results[name]
	if !ok {
		return nil, fmt.Errorf("package %s not found", name)
	}
	return info, nil
}

func TestCheckHexDeps_Retired(t *testing.T) {
	client := &mockHexRegistryClient{
		results: map[string]*hexPackageInfo{
			"old_lib": {
				Name: "old_lib",
				Retirements: map[string]hexRetirement{
					"1.0.0": {Reason: "security", Message: "critical vulnerability found"},
				},
			},
		},
	}
	deps := []PackageQuery{{Ecosystem: "Hex", Name: "old_lib", Version: "1.0.0"}}

	signals := checkHexDeps(context.Background(), client, deps, "mix.exs")
	require.Len(t, signals, 1)
	assert.Equal(t, "deprecated-dependency", signals[0].Kind)
	assert.Equal(t, 0.8, signals[0].Confidence)
	assert.Contains(t, signals[0].Title, "old_lib@1.0.0")
	assert.Contains(t, signals[0].Description, "security")
	assert.Contains(t, signals[0].Description, "critical vulnerability found")
	assert.Contains(t, signals[0].Tags, "elixir")
	assert.Equal(t, "mix.exs", signals[0].FilePath)
}

func TestCheckHexDeps_NotRetired(t *testing.T) {
	client := &mockHexRegistryClient{
		results: map[string]*hexPackageInfo{
			"good_lib": {
				Name:        "good_lib",
				Retirements: map[string]hexRetirement{},
			},
		},
	}
	deps := []PackageQuery{{Ecosystem: "Hex", Name: "good_lib", Version: "1.0.0"}}

	signals := checkHexDeps(context.Background(), client, deps, "mix.exs")
	assert.Empty(t, signals)
}

func TestCheckHexDeps_Error(t *testing.T) {
	client := &mockHexRegistryClient{
		err: fmt.Errorf("network error"),
	}
	deps := []PackageQuery{{Ecosystem: "Hex", Name: "some_lib", Version: "1.0.0"}}

	signals := checkHexDeps(context.Background(), client, deps, "mix.exs")
	assert.Empty(t, signals, "errors should be silently skipped")
}

func TestCheckHexDeps_DifferentVersionRetired(t *testing.T) {
	client := &mockHexRegistryClient{
		results: map[string]*hexPackageInfo{
			"my_lib": {
				Name: "my_lib",
				Retirements: map[string]hexRetirement{
					"0.9.0": {Reason: "deprecated"},
				},
			},
		},
	}
	deps := []PackageQuery{{Ecosystem: "Hex", Name: "my_lib", Version: "1.0.0"}}

	signals := checkHexDeps(context.Background(), client, deps, "mix.exs")
	assert.Empty(t, signals, "only the exact version should trigger retirement signal")
}

func TestDepHealthCollector_HexOnly(t *testing.T) {
	dir := t.TempDir()
	mixExs := `defmodule MyApp.MixProject do
  use Mix.Project

  defp deps do
    [
      {:phoenix, "~> 1.7.0"},
      {:retired_lib, "~> 1.0.0"},
    ]
  end
end`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mix.exs"), []byte(mixExs), 0o600))

	hexClient := &mockHexRegistryClient{
		results: map[string]*hexPackageInfo{
			"phoenix": {
				Name:        "phoenix",
				Retirements: map[string]hexRetirement{},
			},
			"retired_lib": {
				Name: "retired_lib",
				Retirements: map[string]hexRetirement{
					"1.0.0": {Reason: "renamed", Message: "use new_lib instead"},
				},
			},
		},
	}

	c := &DepHealthCollector{hexClient: hexClient}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "deprecated-dependency", signals[0].Kind)
	assert.Contains(t, signals[0].Title, "retired_lib")

	metrics := c.Metrics().(*DepHealthMetrics)
	assert.Contains(t, metrics.Ecosystems, "hex")
	assert.Len(t, metrics.Deprecated, 1)
}

// --- Scala/sbt tests ---

func TestDepHealthCollector_SbtOnly(t *testing.T) {
	dir := t.TempDir()
	buildSbt := `name := "my-project"
version := "0.1.0"
scalaVersion := "2.13.12"

libraryDependencies += "org.typelevel" %% "cats-core" % "2.9.0"
libraryDependencies += "com.old" % "stale-lib" % "1.0.0"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.sbt"), []byte(buildSbt), 0o600))

	staleTimestamp := time.Now().Add(-5 * 365 * 24 * time.Hour).UnixMilli()
	mavenClient := &mockMavenRegistryClient{
		results: map[string]*mavenArtifactInfo{
			"com.old:stale-lib": {
				Response: struct {
					NumFound int             `json:"numFound"`
					Docs     []mavenArtifact `json:"docs"`
				}{
					NumFound: 1,
					Docs: []mavenArtifact{
						{GroupID: "com.old", ArtifactID: "stale-lib", Version: "1.0.0", Timestamp: staleTimestamp},
					},
				},
			},
		},
	}

	c := &DepHealthCollector{mavenClient: mavenClient}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "stale-dependency", signals[0].Kind)
	assert.Contains(t, signals[0].Title, "com.old:stale-lib")

	metrics := c.Metrics().(*DepHealthMetrics)
	assert.Contains(t, metrics.Ecosystems, "sbt")
	assert.Len(t, metrics.Stale, 1)
}

// --- Parser unit tests ---

func TestParseComposerDeps(t *testing.T) {
	data := []byte(`{
  "require": {
    "php": ">=8.0",
    "vendor/pkg-a": "^1.0",
    "vendor/pkg-b": "~2.3.0",
    "ext-json": "*"
  },
  "require-dev": {
    "phpunit/phpunit": "^10.0"
  }
}`)
	queries, err := parseComposerDeps(data)
	require.NoError(t, err)

	names := make(map[string]string)
	for _, q := range queries {
		names[q.Name] = q.Version
		assert.Equal(t, "Packagist", q.Ecosystem)
	}

	assert.Contains(t, names, "vendor/pkg-a")
	assert.Contains(t, names, "vendor/pkg-b")
	assert.Contains(t, names, "phpunit/phpunit")
	assert.NotContains(t, names, "php", "platform req should be skipped")
	assert.NotContains(t, names, "ext-json", "extension req should be skipped")
}

func TestParseComposerDeps_Empty(t *testing.T) {
	data := []byte(`{}`)
	queries, err := parseComposerDeps(data)
	require.NoError(t, err)
	assert.Empty(t, queries)
}

func TestParseComposerDeps_Invalid(t *testing.T) {
	_, err := parseComposerDeps([]byte(`not json`))
	assert.Error(t, err)
}

func TestExtractComposerVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"^1.0", "1.0"},
		{"~2.3.0", "2.3.0"},
		{">=1.0,<2.0", "1.0"},
		{"1.0.0", "1.0.0"},
		{"*", ""},
		{"dev-main", ""},
		{"", ""},
		{"v1.2.3", "1.2.3"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, extractComposerVersion(tt.input))
		})
	}
}

func TestParseSwiftPackageDeps(t *testing.T) {
	data := []byte(`
// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "MyApp",
    dependencies: [
        .package(url: "https://github.com/apple/swift-argument-parser", from: "1.2.0"),
        .package(url: "https://github.com/vapor/vapor.git", .upToNextMajor(from: "4.0.0")),
        .package(url: "https://github.com/swift-server/async-http-client.git", exact: "1.19.0"),
    ],
    targets: []
)`)
	queries := parseSwiftPackageDeps(data)
	require.Len(t, queries, 3)

	names := make(map[string]string)
	for _, q := range queries {
		names[q.Name] = q.Version
		assert.Equal(t, "SwiftURL", q.Ecosystem)
	}

	assert.Equal(t, "1.2.0", names["https://github.com/apple/swift-argument-parser"])
	assert.Equal(t, "4.0.0", names["https://github.com/vapor/vapor"])
	assert.Equal(t, "1.19.0", names["https://github.com/swift-server/async-http-client"])
}

func TestParseSwiftPackageDeps_Empty(t *testing.T) {
	data := []byte(`let package = Package(name: "Empty", targets: [])`)
	queries := parseSwiftPackageDeps(data)
	assert.Empty(t, queries)
}

func TestParseSbtDeps(t *testing.T) {
	data := []byte(`
name := "my-project"
version := "0.1.0"
scalaVersion := "2.13.12"

libraryDependencies += "org.typelevel" %% "cats-core" % "2.9.0"
libraryDependencies += "com.google.guava" % "guava" % "31.1-jre"
libraryDependencies ++= Seq(
  "com.typesafe.akka" %% "akka-actor" % "2.8.0",
  "org.scalatest" %% "scalatest" % "3.2.15" % Test
)
`)
	queries := parseSbtDeps(data)
	require.NotEmpty(t, queries)

	names := make(map[string]string)
	for _, q := range queries {
		names[q.Name] = q.Version
		assert.Equal(t, "Maven", q.Ecosystem)
	}

	// %% deps get _2.13 suffix
	assert.Contains(t, names, "org.typelevel:cats-core_2.13")
	assert.Equal(t, "2.9.0", names["org.typelevel:cats-core_2.13"])

	// % deps (Java) don't get suffix
	assert.Contains(t, names, "com.google.guava:guava")
	assert.Equal(t, "31.1-jre", names["com.google.guava:guava"])
}

func TestParseSbtDeps_Empty(t *testing.T) {
	data := []byte(`name := "empty-project"`)
	queries := parseSbtDeps(data)
	assert.Empty(t, queries)
}

func TestParseMixDeps(t *testing.T) {
	data := []byte(`
defmodule MyApp.MixProject do
  use Mix.Project

  defp deps do
    [
      {:phoenix, "~> 1.7.0"},
      {:ecto, "~> 3.10"},
      {:jason, "~> 1.4"},
      {:local_dep, path: "../local_dep"},
    ]
  end
end`)
	queries := parseMixDeps(data)

	names := make(map[string]string)
	for _, q := range queries {
		names[q.Name] = q.Version
		assert.Equal(t, "Hex", q.Ecosystem)
	}

	assert.Contains(t, names, "phoenix")
	assert.Equal(t, "1.7.0", names["phoenix"])
	assert.Contains(t, names, "ecto")
	assert.Contains(t, names, "jason")
	assert.NotContains(t, names, "local_dep", "path deps should be skipped")
}

func TestParseMixDeps_Empty(t *testing.T) {
	data := []byte(`defmodule Empty.MixProject do end`)
	queries := parseMixDeps(data)
	assert.Empty(t, queries)
}

func TestExtractMixVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"~> 1.7.0", "1.7.0"},
		{">= 1.0.0", "1.0.0"},
		{"== 2.0.0", "2.0.0"},
		{"1.0.0", "1.0.0"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, extractMixVersion(tt.input))
		})
	}
}

func TestExtractGitHubPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://github.com/apple/swift-argument-parser", "github.com/apple/swift-argument-parser"},
		{"https://github.com/vapor/vapor.git", "github.com/vapor/vapor"},
		{"https://github.com/owner/repo/tree/main", "github.com/owner/repo"},
		{"https://gitlab.com/owner/repo", ""},
		{"not-a-url", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.want, extractGitHubPath(tt.input))
		})
	}
}
