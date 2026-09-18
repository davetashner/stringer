// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dirRatios runs the patterns collector over dir and returns its directory
// ratios keyed by slash-separated path.
func dirRatios(t *testing.T, dir string, opts signal.CollectorOpts) (map[string]DirectoryTestRatio, []signal.RawSignal) {
	t.Helper()
	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, opts)
	require.NoError(t, err)
	m, ok := c.Metrics().(*PatternsMetrics)
	require.True(t, ok)
	out := make(map[string]DirectoryTestRatio, len(m.DirectoryTestRatios))
	for _, r := range m.DirectoryTestRatios {
		out[filepath.ToSlash(r.Path)] = r
	}
	return out, signals
}

// --- Directory ratios credit tests in mirrored trees (stringer-nxx.15) ---

func TestPatterns_RatioCreditsMavenMirror(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{
		"core/src/main/java/org/apache/kafka/raft/KafkaRaftLog.java",
		"core/src/main/java/org/apache/kafka/raft/QuorumState.java",
		"core/src/main/java/org/apache/kafka/raft/LeaderState.java",
		"core/src/test/java/org/apache/kafka/raft/KafkaRaftLogTest.java",
		"core/src/test/java/org/apache/kafka/raft/QuorumStateTest.java",
		"core/src/test/java/org/apache/kafka/raft/RaftTestUtil.java",
	} {
		writeSourceFile(t, dir, p)
	}

	ratios, signals := dirRatios(t, dir, signal.CollectorOpts{})

	main, ok := ratios["core/src/main/java/org/apache/kafka/raft"]
	require.True(t, ok, "main tree should appear in ratios: %v", ratios)
	assert.Equal(t, 3, main.SourceFiles)
	assert.Equal(t, 2, main.CoveredFiles)
	assert.Equal(t, 0, main.TestFiles, "no test files are colocated")
	assert.InDelta(t, 2.0/3.0, main.Ratio, 0.001)

	// The mirror is a test-only tree; it must not be listed as a source dir.
	_, listed := ratios["core/src/test/java/org/apache/kafka/raft"]
	assert.False(t, listed, "src/test tree must not appear as a source directory")

	assert.Empty(t, filterByKind(signals, "low-test-ratio"))
	missing := filterByKind(signals, "missing-tests")
	require.Len(t, missing, 1)
	assert.Equal(t, filepath.FromSlash("core/src/main/java/org/apache/kafka/raft/LeaderState.java"), missing[0].FilePath)
}

func TestPatterns_RatioCreditsPHPTestsMirror(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{
		"src/Illuminate/Cache/Repository.php",
		"src/Illuminate/Cache/ArrayStore.php",
		"src/Illuminate/Cache/Lock.php",
		"tests/Cache/CacheRepositoryTest.php", // prefixed class name covers Repository
		"tests/Cache/CacheArrayStoreTest.php",
		"tests/Cache/CacheTestCase.php", // support base class, not a test
	} {
		writeSourceFile(t, dir, p)
	}

	ratios, signals := dirRatios(t, dir, signal.CollectorOpts{})

	cache, ok := ratios["src/Illuminate/Cache"]
	require.True(t, ok)
	assert.Equal(t, 3, cache.SourceFiles)
	assert.Equal(t, 2, cache.CoveredFiles)
	assert.Equal(t, 0, cache.TestFiles)
	assert.InDelta(t, 2.0/3.0, cache.Ratio, 0.001)

	_, listed := ratios["tests/Cache"]
	assert.False(t, listed, "tests/ tree must not appear as a source directory")
	assert.Empty(t, filterByKind(signals, "low-test-ratio"))
}

func TestPatterns_RatioCreditsCSharpTestsProject(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{
		"Emby.Naming/Video/VideoResolver.cs",
		"Emby.Naming/Video/StubResolver.cs",
		"Emby.Naming/Video/Format3DParser.cs",
		"tests/Jellyfin.Naming.Tests/Video/VideoResolverTests.cs",
		"tests/Jellyfin.Naming.Tests/Video/StubTests.cs", // does not match StubResolver
		"tests/Jellyfin.Naming.Tests/Video/Format3DParserTests.cs",
	} {
		writeSourceFile(t, dir, p)
	}

	ratios, _ := dirRatios(t, dir, signal.CollectorOpts{})

	video, ok := ratios["Emby.Naming/Video"]
	require.True(t, ok)
	assert.Equal(t, 3, video.SourceFiles)
	assert.Equal(t, 2, video.CoveredFiles)
	assert.Equal(t, 0, video.TestFiles)
	assert.InDelta(t, 2.0/3.0, video.Ratio, 0.001)

	_, listed := ratios["tests/Jellyfin.Naming.Tests/Video"]
	assert.False(t, listed, "tests/<Project>.Tests tree must not appear as a source directory")
}

func TestPatterns_RatioColocatedGoPackageUnchanged(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{
		"pkg/scan.go", "pkg/scan_test.go",
		"pkg/walk.go", "pkg/walk_test.go",
		"pkg/emit.go", "pkg/emit_test.go",
		"pkg/util.go", // uncovered
	} {
		writeSourceFile(t, dir, p)
	}

	ratios, signals := dirRatios(t, dir, signal.CollectorOpts{})

	pkg, ok := ratios["pkg"]
	require.True(t, ok)
	assert.Equal(t, 4, pkg.SourceFiles)
	assert.Equal(t, 3, pkg.CoveredFiles)
	assert.Equal(t, 3, pkg.TestFiles, "colocated test files are still counted")
	assert.InDelta(t, 0.75, pkg.Ratio, 0.001)
	assert.Empty(t, filterByKind(signals, "low-test-ratio"))
}

func TestPatterns_RatioColocatedTestsMustMatchSources(t *testing.T) {
	dir := t.TempDir()
	// Three source files and one colocated test that matches none of them.
	for _, p := range []string{
		"pkg/alpha.go", "pkg/beta.go", "pkg/gamma.go", "pkg/integration_test.go",
	} {
		writeSourceFile(t, dir, p)
	}

	ratios, signals := dirRatios(t, dir, signal.CollectorOpts{})

	pkg, ok := ratios["pkg"]
	require.True(t, ok)
	assert.Equal(t, 3, pkg.SourceFiles)
	assert.Equal(t, 0, pkg.CoveredFiles)
	assert.Equal(t, 1, pkg.TestFiles)
	assert.InDelta(t, 0.0, pkg.Ratio, 0.001)

	low := filterByKind(signals, "low-test-ratio")
	require.Len(t, low, 1)
	assert.Equal(t, "pkg", low[0].FilePath)
	assert.Contains(t, low[0].Title, "0 of 3 source files have tests")
	assert.Contains(t, low[0].Description, "0.0%")
}

func TestPatterns_LowTestRatioNotReportedForMirroredTree(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{
		"src/main/java/com/ex/Alpha.java",
		"src/main/java/com/ex/Beta.java",
		"src/main/java/com/ex/Gamma.java",
		"src/main/java/com/ex/Delta.java",
		"src/test/java/com/ex/AlphaTest.java",
	} {
		writeSourceFile(t, dir, p)
	}

	_, signals := dirRatios(t, dir, signal.CollectorOpts{})
	// 1 of 4 covered = 25%, above the 10% default threshold.
	assert.Empty(t, filterByKind(signals, "low-test-ratio"))
}

func TestPatterns_TestOnlyDirsNotSourceDirs(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{
		"src/app.py",
		"tests/conftest.py",
		"tests/helpers.py",
		"tests/unit/test_app.py",
		"test/fixtures/data.py",
		"spec/support/helper.rb",
		"web/__tests__/setup.js",
		"web/__tests__/app.test.js",
		"src/test/java/com/ex/Fixture.java",
		"Sources/Lib/Tests/Helper.swift",
	} {
		writeSourceFile(t, dir, p)
	}

	ratios, signals := dirRatios(t, dir, signal.CollectorOpts{})

	require.Len(t, ratios, 1, "only src/ should be a source directory: %v", ratios)
	_, ok := ratios["src"]
	assert.True(t, ok)

	// Test support code is never flagged as missing tests either.
	for _, s := range filterByKind(signals, "missing-tests") {
		assert.Equal(t, filepath.FromSlash("src/app.py"), s.FilePath)
	}
}

func TestIsTestOnlyDir(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"tests/conftest.py", true},
		{"test/helper.go", true},
		{"spec/support/helper.rb", true},
		{"web/src/__tests__/setup.js", true},
		{"benches/bench.rs", true},
		{"Sources/Tests/Helper.swift", true},
		{"core/src/test/java/com/ex/Fixture.java", true},
		{"src/app.py", false},
		{"internal/testutil/helper.go", false},
		{"contest/entry.go", false},
		{"app.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, isTestOnlyDir(filepath.FromSlash(tt.path)))
		})
	}
}
