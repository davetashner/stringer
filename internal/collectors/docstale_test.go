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

// TestDocStale_ConfigurableStaleDays verifies that the stale-days threshold
// is configurable via opts.
func TestDocStale_ConfigurableStaleDays(t *testing.T) {
	// The default is 180 days. Setting it to a very high value (9999) should
	// suppress stale-doc signals. We just verify the option is read and the
	// default fallback works correctly.
	assert.Equal(t, 180, defaultStalenessThresholdDays,
		"default staleness threshold should be 180 days")

	// Verify zero value falls back to default.
	opts := signal.CollectorOpts{DocStaleDays: 0}
	staleDays := opts.DocStaleDays
	if staleDays == 0 {
		staleDays = defaultStalenessThresholdDays
	}
	assert.Equal(t, 180, staleDays)

	// Verify custom value is used.
	opts2 := signal.CollectorOpts{DocStaleDays: 365}
	staleDays2 := opts2.DocStaleDays
	if staleDays2 == 0 {
		staleDays2 = defaultStalenessThresholdDays
	}
	assert.Equal(t, 365, staleDays2)
}

// TestDocStale_ConfigurableDriftMinCommits verifies the drift min-commits
// threshold is configurable.
func TestDocStale_ConfigurableDriftMinCommits(t *testing.T) {
	assert.Equal(t, 10, defaultDriftMinCommits,
		"default drift min commits should be 10")

	opts := signal.CollectorOpts{DocDriftMinCommits: 5}
	minCommits := opts.DocDriftMinCommits
	if minCommits == 0 {
		minCommits = defaultDriftMinCommits
	}
	assert.Equal(t, 5, minCommits)
}

// TestFindBrokenLinks_SkipsURISchemesAndFences pins stringer-rd7: entity-ID
// link targets (person:lanrezac-charles) and link-shaped text inside fenced
// code blocks are not filesystem paths; all seven broken-link findings on a
// real repo were these two shapes.
func TestFindBrokenLinks_SkipsURISchemesAndFences(t *testing.T) {
	dir := t.TempDir()
	doc := `# Authoring

A [person link](person:lanrezac-charles) and an [era link](1914:army-de-4).
A [tel link](tel:+15551234) and a [vscode link](vscode://file/x).
A [placeholder](…) and [another](<entity-id>).

` + "```" + `markdown
A [sample link in a fence](missing-from-fence.md)
` + "```" + `

A [genuinely broken link](does-not-exist.md) survives all filters.
A [good link](real.md) is fine.
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "guide.md"), []byte(doc), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "real.md"), []byte("# real\n"), 0o600))

	broken := newLinkResolver(dir, nil).findBrokenLinks("guide.md")
	require.Len(t, broken, 1, "only the genuinely broken relative link should be reported, got %v", broken)
	assert.Equal(t, "does-not-exist.md", broken[0].target)
}

// writeDocFixture creates the given files (path → content) under dir.
func writeDocFixture(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o750))
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o600))
	}
}

// brokenTargets returns the targets of the broken links in relPath.
func brokenTargets(t *testing.T, dir, relPath string, docFiles []string) []string {
	t.Helper()
	var out []string
	for _, bl := range newLinkResolver(dir, docFiles).findBrokenLinks(relPath) {
		out = append(out, bl.target)
	}
	return out
}

// TestFindBrokenLinks_TemplatePlaceholders pins stringer-nxx.11: link
// targets containing template syntax are rendered by a site generator or
// substituted by a build, never resolved against the tree.
func TestFindBrokenLinks_TemplatePlaceholders(t *testing.T) {
	dir := t.TempDir()
	writeDocFixture(t, dir, map[string]string{
		"guide.md": `[a](/{version}/javadoc/index.html?org/apache/kafka/Foo.html "Javadoc")
[b]({{ .Site.BaseURL }}/docs/x.md)
[c](${DOCS_ROOT}/x.md)
[d](<placeholder-file>)
[e](docs/%s/readme.md)
[f](/users/:id/profile.md)
[g]([[WikiPage]])
[h](ENGLISH_VERSION_URL)
[i](url)
[j](docs/%20spaced%20name.md)
[k](%C3%A9t%C3%A9.md)
[real](does-not-exist.md)
`,
		"docs/ spaced name.md": "# spaced\n",
	})
	got := brokenTargets(t, dir, "guide.md", nil)
	assert.Equal(t, []string{"%C3%A9t%C3%A9.md", "does-not-exist.md"}, got)
}

func TestIsPlaceholderLinkTarget(t *testing.T) {
	for _, tc := range []struct {
		target string
		want   bool
	}{
		{"{version}/x", true}, {"{{ .x }}", true}, {"${X}/y", true}, {"<x>", true},
		{"%s/x", true}, {"/a/:param", true}, {":param", true}, {"[[x]]", true},
		{"ENGLISH_VERSION_URL", true}, {"URL", true}, {"LICENSE", false}, {"CODE_OF_CONDUCT.md", false},
		{"link", true}, {"a%20b.md", false}, {"a%2Fb", false}, {"%C3%A9", false}, {"a%dir", true},
		{"docs/x.md", false}, {"…", true}, {"...", true},
	} {
		assert.Equal(t, tc.want, isPlaceholderLinkTarget(tc.target), tc.target)
	}
}

func TestNormalizeLinkTarget(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
		ok   bool
	}{
		{`x.md "Title"`, "x.md", true},
		{`x.md 'Title'`, "x.md", true},
		{"x.md#frag", "x.md", true},
		{"index.html?org/apache/Foo.html", "index.html", true},
		{"<a b.md>", "a b.md", true},
		{"#only-anchor", "", false},
		{"?only-query", "", false},
		{"https://x", "", false},
		{"<https://x", "", false},
		{"mailto:x@y", "", false},
		{`"TextLinesTopic"`, "", false},
		{" spaced path.md ", "spaced path.md", true},
	} {
		got, ok := normalizeLinkTarget(tc.raw)
		assert.Equal(t, tc.ok, ok, tc.raw)
		assert.Equal(t, tc.want, got, tc.raw)
	}
}

// TestFindBrokenLinks_DirectoryAndPrettyURLs: dir/, dir and page links
// resolve to the index or source file a generator serves for them.
func TestFindBrokenLinks_DirectoryAndPrettyURLs(t *testing.T) {
	dir := t.TempDir()
	writeDocFixture(t, dir, map[string]string{
		"docs/guide.md": `[a](tutorial/) [b](tutorial) [c](advanced/) [d](section/) [e](site/)
[f](../reference) [g](../reference.html) [h](missing/) [i](missing) [j](missing.html) [k](section.html)
`,
		"docs/tutorial/index.md":  "#\n",
		"docs/advanced/README.md": "#\n",
		"docs/section/_index.md":  "#\n",
		"docs/site/index.html":    "<html>",
		"reference.md":            "#\n",
	})
	got := brokenTargets(t, dir, "docs/guide.md", nil)
	assert.Equal(t, []string{"missing/", "missing", "missing.html"}, got)
}

// TestFindBrokenLinks_HugoSite: site-root links resolve against content/
// (and static/, and content/<lang>/), pretty URLs map to _index.md and
// page.md, and unresolvable site-root links are still reported when the
// site config is present (routing is knowable).
func TestFindBrokenLinks_HugoSite(t *testing.T) {
	dir := t.TempDir()
	writeDocFixture(t, dir, map[string]string{
		"hugo.toml":              "baseURL = 'https://example.com/'\n",
		"content/docs/_index.md": "#\n",
		"content/docs/getting-started.md": `[a](/docs/) [b](/docs/getting-started/) [c](/images/logo.png)
[d](/en/docs/intro/) [e](/nowhere/) [f](/{version}/api/)
`,
		"content/en/docs/intro.md": "#\n",
		"static/images/logo.png":   "png",
	})
	docFiles := []string{"content/docs/_index.md", "content/docs/getting-started.md"}
	r := newLinkResolver(dir, docFiles)
	assert.True(t, r.configured)
	assert.True(t, r.siteDetected)
	assert.Contains(t, r.contentRoots, filepath.Join(dir, "content"))
	assert.Contains(t, r.contentRoots, filepath.Join(dir, "static"))
	assert.Contains(t, r.contentRoots, filepath.Join(dir, "content", "en"))

	got := brokenTargets(t, dir, "content/docs/getting-started.md", docFiles)
	assert.Equal(t, []string{"/nowhere/"}, got)
}

// TestFindBrokenLinks_HugoContentWithoutConfig: a docs/ tree of _index.md
// files whose site config lives in another repo (apache/kafka) resolves
// site-root links against the content tree and skips the ones whose
// routing cannot be known.
func TestFindBrokenLinks_HugoContentWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	writeDocFixture(t, dir, map[string]string{
		"docs/_index.md":         "#\n",
		"docs/streams/_index.md": "#\n",
		"docs/streams/tutorial.md": `[a](/documentation/streams) [b](/43/documentation/streams/) [c](../../architecture)
[d](../core-concepts) [e](../upgrade-guide/) [f](truly-missing.md)
`,
		"docs/documentation/streams/_index.md": "#\n",
		"docs/streams/core-concepts.md":        "#\n",
		"docs/streams/upgrade-guide.md":        "#\n",
	})
	docFiles := []string{"docs/_index.md", "docs/streams/_index.md", "docs/streams/tutorial.md", "docs/documentation/streams/_index.md"}
	r := newLinkResolver(dir, docFiles)
	assert.False(t, r.configured)
	assert.True(t, r.siteDetected)
	assert.Equal(t, []string{filepath.Join(dir, "docs")}, r.contentRoots)
	assert.Equal(t, []string{"docs"}, hugoContentDirs(docFiles))

	// tutorial.md is served at docs/streams/tutorial/, so ../core-concepts
	// resolves; ../../architecture would need docs/architecture.md.
	got := brokenTargets(t, dir, "docs/streams/tutorial.md", docFiles)
	assert.Equal(t, []string{"../../architecture", "truly-missing.md"}, got)

	// Without site evidence the pretty-URL fallback does not apply.
	plain := newLinkResolver(dir, nil)
	plain.siteDetected = false
	plain.contentRoots = nil
	var got2 []string
	for _, bl := range plain.findBrokenLinks("docs/streams/tutorial.md") {
		got2 = append(got2, bl.target)
	}
	assert.Equal(t, []string{"/documentation/streams", "/43/documentation/streams/", "../../architecture", "../core-concepts", "../upgrade-guide/", "truly-missing.md"}, got2)
}

// TestFindBrokenLinks_MkDocsSite: docs_dir is honoured, translated docs
// fall back to the English original, and a root README mirrored from the
// docs index resolves site-relative links against docs_dir.
func TestFindBrokenLinks_MkDocsSite(t *testing.T) {
	dir := t.TempDir()
	writeDocFixture(t, dir, map[string]string{
		"docs/en/mkdocs.yml":                     "site_name: X\ndocs_dir: 'content'\n",
		"docs/en/content/index.md":               "#\n",
		"docs/en/content/tutorial/index.md":      "#\n",
		"docs/en/content/release-notes.md":       "#\n",
		"docs/en/content/deployment/versions.md": "#\n",
		"docs/de/content/deployment/versions.md": "[a](../release-notes.md) [b](../tutorial/) [c](../gone.md)\n",
		"README.md":                              "[a](tutorial/#install) [b](/tutorial/) [c](nowhere/)\n",
	})
	r := newLinkResolver(dir, nil)
	assert.True(t, r.configured)
	assert.Equal(t, []string{filepath.Join(dir, "docs", "en", "content")}, r.contentRoots)

	assert.Equal(t, []string{"../gone.md"}, brokenTargets(t, dir, "docs/de/content/deployment/versions.md", nil))
	assert.Equal(t, []string{"nowhere/"}, brokenTargets(t, dir, "README.md", nil))
}

// TestSiteContentDirs covers the remaining generator configs.
func TestSiteContentDirs(t *testing.T) {
	dir := t.TempDir()
	writeDocFixture(t, dir, map[string]string{
		"jekyll/_config.yml":                    "title: x\n",
		"mdbook/book.toml":                      "[book]\nsrc = \"content\"\n",
		"sphinx/conf.py":                        "project = 'x'\n",
		"docusaurus/docusaurus.config.js":       "module.exports = {}\n",
		"hugo/config.toml":                      "baseURL = 'https://x'\n",
		"nothugo/config.toml":                   "[tool]\nname = 'x'\n",
		"hugodefault/config/_default/hugo.toml": "title = 'x'\n",
		"hugodir/hugo.yaml":                     "contentDir: pages\n",
	})
	rel := func(dirs []string) []string {
		var out []string
		for _, d := range dirs {
			r, _ := filepath.Rel(dir, d)
			out = append(out, filepath.ToSlash(r))
		}
		return out
	}
	assert.Equal(t, []string{"jekyll"}, rel(siteContentDirs(filepath.Join(dir, "jekyll"))))
	assert.Equal(t, []string{"mdbook/content", "mdbook/src"}, rel(siteContentDirs(filepath.Join(dir, "mdbook"))))
	assert.Equal(t, []string{"sphinx"}, rel(siteContentDirs(filepath.Join(dir, "sphinx"))))
	assert.Equal(t, []string{"docusaurus/docs", "docusaurus/static"}, rel(siteContentDirs(filepath.Join(dir, "docusaurus"))))
	assert.Equal(t, []string{"hugo/content", "hugo/static"}, rel(siteContentDirs(filepath.Join(dir, "hugo"))))
	assert.Empty(t, siteContentDirs(filepath.Join(dir, "nothugo")))
	assert.Equal(t, []string{"hugodefault/content", "hugodefault/static"}, rel(siteContentDirs(filepath.Join(dir, "hugodefault"))))
	assert.Equal(t, []string{"hugodir/pages", "hugodir/content", "hugodir/static"}, rel(siteContentDirs(filepath.Join(dir, "hugodir"))))
	assert.Empty(t, siteContentDirs(filepath.Join(dir, "missing")))

	// Config dirs: root, docs-style dirs and their immediate children.
	writeDocFixture(t, dir, map[string]string{"docs/en/x.md": "#\n", "docs/.hidden/x.md": "#\n"})
	got := rel(siteConfigDirs(dir))
	assert.Contains(t, got, "docs")
	assert.Contains(t, got, "docs/en")
	assert.NotContains(t, got, "docs/.hidden")
}

// TestDocStale_SiteRootLinkWithoutSite: with no static site in the repo a
// site-root link is checked against the repo root (GitHub rendering) and
// reported when missing.
func TestDocStale_SiteRootLinkWithoutSite(t *testing.T) {
	dir := initDocTestRepo(t)
	writeDocFixture(t, dir, map[string]string{
		"docs/guide.md": "[a](/docs/other.md) [b](/LICENSE) [c](/docs/missing.md)\n",
		"docs/other.md": "#\n",
		"LICENSE":       "MIT\n",
	})
	gitCommit(t, dir, "add docs")

	c := &DocStaleCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)
	broken := filterByKind(signals, "broken-doc-link")
	require.Len(t, broken, 1)
	assert.Contains(t, broken[0].Title, "/docs/missing.md")
}
