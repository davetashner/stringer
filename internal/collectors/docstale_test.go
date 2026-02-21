// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

func TestDocStale_Registration(t *testing.T) {
	c := collector.Get("docstale")
	require.NotNil(t, c)
	assert.Equal(t, "docstale", c.Name())
}

func TestDocStale_StaleDoc(t *testing.T) {
	dir := initDocTestRepo(t)

	// Create source dir and commit it.
	srcDir := filepath.Join(dir, "internal", "auth")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "auth.go"), []byte("package auth\n"), 0o600))
	gitCommit(t, dir, "add auth source")

	// Create docs dir and doc file, commit it.
	docsDir := filepath.Join(dir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "auth.md"), []byte("# Auth\n"), 0o600))
	gitCommit(t, dir, "add auth doc")

	// Backdate the doc commit so it appears old.
	backdateLastCommit(t, dir, time.Now().AddDate(0, -8, 0))

	// Update source file with a recent commit.
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "auth.go"), []byte("package auth\n\nfunc Login() {}\n"), 0o600))
	gitCommit(t, dir, "update auth source")

	c := &DocStaleCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	stale := filterByKind(signals, "stale-doc")
	assert.NotEmpty(t, stale, "expected stale-doc signal")
	if len(stale) > 0 {
		assert.Contains(t, stale[0].Title, "auth.md")
		assert.Equal(t, "docstale", stale[0].Source)
		assert.GreaterOrEqual(t, stale[0].Confidence, 0.3)
	}
}

func TestDocStale_NotStale(t *testing.T) {
	dir := initDocTestRepo(t)

	// Create source and doc at the same time.
	srcDir := filepath.Join(dir, "internal", "auth")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "auth.go"), []byte("package auth\n"), 0o600))

	docsDir := filepath.Join(dir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "auth.md"), []byte("# Auth\n"), 0o600))
	gitCommit(t, dir, "add auth source and doc together")

	c := &DocStaleCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	stale := filterByKind(signals, "stale-doc")
	assert.Empty(t, stale, "expected no stale-doc signal when doc and source are same age")
}

func TestDocStale_DocCodeDrift(t *testing.T) {
	// Create repo with doc+source in the initial commit, then only touch source.
	// Since the doc creation commit also touches source (both in same commit),
	// its doc file is counted as a doc commit. To get docCommits==0 in the
	// log window, we backdate the initial commit before the --since window
	// and use a recent --since.
	dir := t.TempDir()
	runDocGit(t, dir, "init")

	// Create source dir and docs in the initial commit.
	srcDir := filepath.Join(dir, "internal", "auth")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "auth.go"), []byte("package auth\n"), 0o600))
	docsDir := filepath.Join(dir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "auth.md"), []byte("# Auth\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0o600))
	gitCommit(t, dir, "init with doc and source")

	// Backdate initial commit to 3 years ago so --since=1y excludes it.
	backdateLastCommit(t, dir, time.Now().AddDate(-3, 0, 0))

	// Make 12 source-only commits (within the --since window).
	for i := 0; i < 12; i++ {
		content := []byte("package auth\n\n// " + string(rune('a'+i)) + "\n")
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, "auth.go"), content, 0o600))
		gitCommit(t, dir, "update auth source")
	}

	c := &DocStaleCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		GitRoot:  dir,
		GitSince: "1y",
		GitDepth: 5000,
	})
	require.NoError(t, err)

	drift := filterByKind(signals, "doc-code-drift")
	assert.NotEmpty(t, drift, "expected doc-code-drift signal")

	// Find the auth.md drift signal specifically (README.md may also appear).
	var foundAuth bool
	for _, s := range drift {
		if strings.Contains(s.Title, "auth.md") {
			foundAuth = true
			assert.Equal(t, 0.3, s.Confidence)
			break
		}
	}
	assert.True(t, foundAuth, "expected doc-code-drift signal for auth.md")
}

func TestDocStale_BrokenLink(t *testing.T) {
	dir := initDocTestRepo(t)

	docsDir := filepath.Join(dir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o750))
	content := "# Guide\n\nSee [the code](nonexistent.go) for details.\n"
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "guide.md"), []byte(content), 0o600))
	gitCommit(t, dir, "add guide with broken link")

	c := &DocStaleCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	broken := filterByKind(signals, "broken-doc-link")
	assert.Len(t, broken, 1)
	if len(broken) > 0 {
		assert.Contains(t, broken[0].Title, "nonexistent.go")
		assert.Equal(t, 0.6, broken[0].Confidence)
		assert.Equal(t, 3, broken[0].Line)
	}
}

func TestDocStale_ValidLink(t *testing.T) {
	dir := initDocTestRepo(t)

	docsDir := filepath.Join(dir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "other.md"), []byte("# Other\n"), 0o600))
	content := "# Guide\n\nSee [other doc](other.md) for details.\n"
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "guide.md"), []byte(content), 0o600))
	gitCommit(t, dir, "add guide with valid link")

	c := &DocStaleCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	broken := filterByKind(signals, "broken-doc-link")
	assert.Empty(t, broken, "expected no broken-doc-link for valid links")
}

func TestDocStale_ExternalLinkSkipped(t *testing.T) {
	dir := initDocTestRepo(t)

	docsDir := filepath.Join(dir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o750))
	content := "# Guide\n\nSee [example](https://example.com) and [http](http://example.com).\n"
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "guide.md"), []byte(content), 0o600))
	gitCommit(t, dir, "add guide with external links")

	c := &DocStaleCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	broken := filterByKind(signals, "broken-doc-link")
	assert.Empty(t, broken, "expected no broken-doc-link for external URLs")
}

func TestDocStale_AnchorOnlyLinkSkipped(t *testing.T) {
	dir := initDocTestRepo(t)

	docsDir := filepath.Join(dir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o750))
	content := "# Guide\n\nSee [section](#overview) for details.\n"
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "guide.md"), []byte(content), 0o600))
	gitCommit(t, dir, "add guide with anchor link")

	c := &DocStaleCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	broken := filterByKind(signals, "broken-doc-link")
	assert.Empty(t, broken, "expected no broken-doc-link for pure anchor links")
}

func TestDocStale_Metrics(t *testing.T) {
	dir := initDocTestRepo(t)

	docsDir := filepath.Join(dir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "guide.md"), []byte("# Guide\n"), 0o600))
	gitCommit(t, dir, "add guide")

	c := &DocStaleCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	m := c.Metrics()
	require.NotNil(t, m)
	metrics, ok := m.(*DocStaleMetrics)
	require.True(t, ok)
	// initDocTestRepo creates README.md + we add docs/guide.md = 2 docs.
	assert.Equal(t, 2, metrics.DocsScanned)
}

func TestDocStale_MinConfidenceFilter(t *testing.T) {
	dir := initDocTestRepo(t)

	docsDir := filepath.Join(dir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o750))
	content := "# Guide\n\n[broken](missing.txt)\n"
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "guide.md"), []byte(content), 0o600))
	gitCommit(t, dir, "add guide with broken link")

	c := &DocStaleCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		GitRoot:       dir,
		MinConfidence: 0.9,
	})
	require.NoError(t, err)

	broken := filterByKind(signals, "broken-doc-link")
	assert.Empty(t, broken, "broken-doc-link (conf 0.6) should be filtered at min 0.9")
}

func TestDocStale_RootReadme(t *testing.T) {
	dir := initDocTestRepo(t)

	// The README.md created by initDocTestRepo is a root doc.
	c := &DocStaleCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	m := c.Metrics().(*DocStaleMetrics)
	assert.GreaterOrEqual(t, m.DocsScanned, 1, "root README.md should be scanned")
}

func TestDocStale_ContextCancellation(t *testing.T) {
	dir := initDocTestRepo(t)

	docsDir := filepath.Join(dir, "docs")
	require.NoError(t, os.MkdirAll(docsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(docsDir, "guide.md"), []byte("# Guide\n"), 0o600))
	gitCommit(t, dir, "add guide")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &DocStaleCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{GitRoot: dir})
	assert.Error(t, err)
}

// isDocFile unit tests.

func TestIsDocFile_DocsDir(t *testing.T) {
	assert.True(t, isDocFile("docs/guide.md"))
	assert.True(t, isDocFile("docs/api/reference.rst"))
	assert.True(t, isDocFile("doc/notes.txt"))
}

func TestIsDocFile_RootDocs(t *testing.T) {
	assert.True(t, isDocFile("README.md"))
	assert.True(t, isDocFile("CONTRIBUTING.md"))
	assert.True(t, isDocFile("CHANGELOG.rst"))
}

func TestIsDocFile_NonDoc(t *testing.T) {
	assert.False(t, isDocFile("internal/auth/auth.go"))
	assert.False(t, isDocFile("src/main.py"))
	assert.False(t, isDocFile("random.md")) // not root doc, not in docs dir
}

func TestInferSourceDir(t *testing.T) {
	dir := t.TempDir()

	// Create internal/auth directory.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "auth"), 0o750))

	got := inferSourceDir(dir, "docs/auth.md")
	assert.Equal(t, "internal/auth", got)

	// Root doc → ".".
	got = inferSourceDir(dir, "README.md")
	assert.Equal(t, ".", got)

	// No matching source dir.
	got = inferSourceDir(dir, "docs/nonexistent.md")
	assert.Equal(t, "", got)
}

func TestStaleConfidence(t *testing.T) {
	assert.Equal(t, 0.3, staleConfidence(180))
	assert.Equal(t, 0.3, staleConfidence(364))
	assert.Equal(t, 0.5, staleConfidence(365))
	assert.Equal(t, 0.5, staleConfidence(729))
	assert.Equal(t, 0.7, staleConfidence(730))
	assert.Equal(t, 0.7, staleConfidence(1000))
}

// Test helpers.

// initDocTestRepo creates a temporary git repo with an initial commit.
func initDocTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runDocGit(t, dir, "init")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0o600))
	runDocGit(t, dir, "add", ".")
	runDocGit(t, dir, "-c", "user.name=Test", "-c", "user.email=test@test.com", "commit", "-m", "init")
	return dir
}

// gitCommit stages all changes and commits in the test repo.
func gitCommit(t *testing.T, dir, msg string) {
	t.Helper()
	runDocGit(t, dir, "add", ".")
	runDocGit(t, dir, "-c", "user.name=Test", "-c", "user.email=test@test.com", "commit", "-m", msg)
}

// backdateLastCommit rewrites the last commit's author and committer dates.
func backdateLastCommit(t *testing.T, dir string, when time.Time) {
	t.Helper()
	dateStr := when.Format(time.RFC3339)
	env := []string{
		"GIT_AUTHOR_DATE=" + dateStr,
		"GIT_COMMITTER_DATE=" + dateStr,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	}
	cmd := exec.Command("git", "-c", "user.name=Test", "-c", "user.email=test@test.com",
		"commit", "--amend", "--no-edit", "--date="+dateStr)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "backdate commit: %s", string(out))
}

// runDocGit runs a git command in the given directory.
func runDocGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, string(out))
}
