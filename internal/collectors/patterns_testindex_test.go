// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

// writeSourceFile creates relPath under dir with enough lines to pass the
// minimum-size gate for missing-test detection.
func writeSourceFile(t *testing.T, dir, relPath string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(relPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
	content := strings.Repeat("// line\n", minSourceLinesForTestCheck+5)
	require.NoError(t, os.WriteFile(full, []byte(content), 0o600))
}

// missingTestPaths runs the patterns collector and returns the FilePath of
// every missing-tests signal.
func missingTestPaths(t *testing.T, dir string, opts signal.CollectorOpts) []string {
	t.Helper()
	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, opts)
	require.NoError(t, err)
	var paths []string
	for _, s := range signals {
		if s.Kind == "missing-tests" {
			paths = append(paths, s.FilePath)
		}
	}
	return paths
}

// --- testSubjectStem: one case per naming convention ---

func TestTestSubjectStem(t *testing.T) {
	tests := []struct {
		base      string
		wantStem  string
		wantCamel bool
	}{
		// <Name>Test / <Name>Tests / <Name>Spec / <Name>Suite
		{"KafkaRaftLogTest.java", "KafkaRaftLog", true},
		{"SeriesResolverTests.cs", "SeriesResolver", true},
		{"DynamoDbStoreTest.php", "DynamoDbStore", true},
		{"ParserSpec.scala", "Parser", true},
		{"ParserSuite.scala", "Parser", true},
		{"FooTests.swift", "Foo", true},
		// Test<Name>
		{"TestKafkaRaftLog.java", "KafkaRaftLog", true},
		// <name>_test / <name>_spec / test_<name>
		{"handler_test.go", "handler", false},
		{"handler_spec.rb", "handler", false},
		{"test_handler.py", "handler", false},
		{"parser_test.exs", "parser", false},
		// <Name>.test / <Name>.spec
		{"app.test.ts", "app", false},
		{"app.spec.tsx", "app", false},
		{"widget.test.jsx", "widget", false},
		// Not test names.
		{"Testing.java", "", false},
		{"Tester.php", "", false},
		{"Contest.php", "", false},
		{"handler.go", "", false},
		{"Test.java", "", false},
		{"_test.go", "", false},
		{"test_.py", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.base, func(t *testing.T) {
			stem, camel := testSubjectStem(tt.base)
			assert.Equal(t, tt.wantStem, stem)
			assert.Equal(t, tt.wantCamel, camel)
		})
	}
}

func TestCamelTails(t *testing.T) {
	assert.Equal(t, []string{"DatabaseChannel", "Channel"}, camelTails("NotificationDatabaseChannel"))
	assert.Empty(t, camelTails("handler"))
	assert.Empty(t, camelTails("Handler"))
	assert.Equal(t, []string{"BC", "C"}, camelTails("ABC"))
}

func TestTestIndex_ExactMatch(t *testing.T) {
	idx := newTestIndex()
	idx.add("src/test/java/org/apache/kafka/raft/internals/KafkaRaftLogTest.java")
	idx.add("tests/Jellyfin.Naming.Tests/TV/SeriesResolverTests.cs")
	idx.add("tests/Integration/Cache/DynamoDbStoreTest.php")
	idx.add("tests/unit/test_handler.py")
	idx.add("src/__tests__/app.spec.tsx")
	idx.add("internal/handler_test.go")
	idx.add("src/Foo.php") // no affix: ignored

	assert.True(t, idx.covers("KafkaRaftLog.java"))
	assert.True(t, idx.covers("SeriesResolver.cs"))
	assert.True(t, idx.covers("DynamoDbStore.php"))
	assert.True(t, idx.covers("handler.py"))
	assert.True(t, idx.covers("app.tsx"))
	assert.True(t, idx.covers("handler.go"))
	assert.False(t, idx.covers("Foo.php"))
	assert.False(t, idx.covers("Other.java"))
	assert.False(t, idx.covers(""))
}

func TestTestIndex_PrefixedCamelCaseName(t *testing.T) {
	idx := newTestIndex()
	idx.add("tests/Notifications/NotificationDatabaseChannelTest.php")

	// A test basename ending in <Name>Test covers <Name>.
	assert.True(t, idx.covers("DatabaseChannel.php"))
	assert.True(t, idx.covers("NotificationDatabaseChannel.php"))
	assert.True(t, idx.covers("Channel.php"))
	// Tails must start at a CamelCase boundary.
	assert.False(t, idx.covers("atabaseChannel.php"))
}

func TestTestIndex_SuffixRequiresMinimumLength(t *testing.T) {
	idx := newTestIndex()
	idx.add("tests/KafkaRaftLogTest.java")
	assert.True(t, idx.covers("RaftLog.java"))
	// "Log" is shorter than minSuffixSubjectLen and must not be covered.
	assert.False(t, idx.covers("Log.java"))
	assert.True(t, idx.covers("KafkaRaftLog.java"))
}

func TestTestIndex_SnakeCaseNamesAreExactOnly(t *testing.T) {
	idx := newTestIndex()
	idx.add("internal/collectors/vuln_cargo_test.go")
	assert.True(t, idx.covers("vuln_cargo.go"))
	// snake_case affixes never produce suffix matches: cargo.go stays uncovered.
	assert.False(t, idx.covers("cargo.go"))
}

// --- Collect-level: repo-wide lookup across trees ---

func TestPatterns_MavenTestInDifferentPackageCovers(t *testing.T) {
	dir := t.TempDir()
	writeSourceFile(t, dir, "raft/src/main/java/org/apache/kafka/raft/internals/KafkaRaftLog.java")
	writeSourceFile(t, dir, "raft/src/test/java/org/apache/kafka/raft/internals/KafkaRaftLogTest.java")
	// Test lives in an unrelated package: only the repo-wide index finds it.
	writeSourceFile(t, dir, "clients/src/main/java/org/apache/kafka/clients/Metadata.java")
	writeSourceFile(t, dir, "core/src/test/java/kafka/other/MetadataTest.java")
	writeSourceFile(t, dir, "clients/src/main/java/org/apache/kafka/clients/Untested.java")

	got := missingTestPaths(t, dir, signal.CollectorOpts{})
	assert.Equal(t, []string{filepath.FromSlash("clients/src/main/java/org/apache/kafka/clients/Untested.java")}, got)
}

func TestPatterns_CSharpSiblingTestProjectUnderTestsRoot(t *testing.T) {
	dir := t.TempDir()
	writeSourceFile(t, dir, "Emby.Naming/TV/SeriesResolver.cs")
	writeSourceFile(t, dir, "tests/Jellyfin.Naming.Tests/TV/SeriesResolverTests.cs")
	writeSourceFile(t, dir, "Emby.Naming/TV/EpisodeResolver.cs")

	got := missingTestPaths(t, dir, signal.CollectorOpts{})
	assert.Equal(t, []string{filepath.FromSlash("Emby.Naming/TV/EpisodeResolver.cs")}, got)
}

func TestPatterns_PHPExactNameInDifferentTree(t *testing.T) {
	dir := t.TempDir()
	writeSourceFile(t, dir, "src/Illuminate/Cache/DynamoDbStore.php")
	writeSourceFile(t, dir, "tests/Integration/Cache/DynamoDbStoreTest.php")
	writeSourceFile(t, dir, "src/Illuminate/Cache/RedisStore.php")

	got := missingTestPaths(t, dir, signal.CollectorOpts{})
	assert.Equal(t, []string{filepath.FromSlash("src/Illuminate/Cache/RedisStore.php")}, got)
}

func TestPatterns_PHPPrefixedTestName(t *testing.T) {
	dir := t.TempDir()
	writeSourceFile(t, dir, "src/Illuminate/Notifications/Channels/DatabaseChannel.php")
	writeSourceFile(t, dir, "tests/Notifications/NotificationDatabaseChannelTest.php")

	got := missingTestPaths(t, dir, signal.CollectorOpts{})
	assert.Empty(t, got)
}

func TestPatterns_PythonTestPrefixAnywhere(t *testing.T) {
	dir := t.TempDir()
	writeSourceFile(t, dir, "src/pkg/sub/handler.py")
	writeSourceFile(t, dir, "tests/deep/nested/test_handler.py")
	writeSourceFile(t, dir, "src/pkg/sub/other.py")

	got := missingTestPaths(t, dir, signal.CollectorOpts{})
	assert.Equal(t, []string{filepath.FromSlash("src/pkg/sub/other.py")}, got)
}

func TestPatterns_JSSpecAnywhere(t *testing.T) {
	dir := t.TempDir()
	writeSourceFile(t, dir, "src/components/Widget.tsx")
	writeSourceFile(t, dir, "spec/unit/Widget.spec.tsx")
	writeSourceFile(t, dir, "src/util/format.js")
	writeSourceFile(t, dir, "test/format.test.js")

	got := missingTestPaths(t, dir, signal.CollectorOpts{})
	assert.Empty(t, got)
}

func TestPatterns_ScalaSpecAndJUnitPrefixAnywhere(t *testing.T) {
	dir := t.TempDir()
	writeSourceFile(t, dir, "core/src/main/scala/app/Parser.scala")
	writeSourceFile(t, dir, "other/src/test/scala/x/ParserSpec.scala")
	writeSourceFile(t, dir, "core/src/main/java/app/Tokenizer.java")
	writeSourceFile(t, dir, "other/src/test/java/x/TestTokenizer.java")

	got := missingTestPaths(t, dir, signal.CollectorOpts{})
	assert.Empty(t, got)
}

func TestIsUnderMavenTestRoot_MultiModule(t *testing.T) {
	assert.True(t, isUnderMavenTestRoot(filepath.FromSlash("src/test/java/a/FooTest.java")))
	assert.True(t, isUnderMavenTestRoot(filepath.FromSlash("raft/src/test/java/a/TestUtils.java")))
	assert.True(t, isUnderMavenTestRoot(filepath.FromSlash("libs/core/src/test/kotlin/a/Helpers.kt")))
	assert.False(t, isUnderMavenTestRoot(filepath.FromSlash("raft/src/main/java/a/Foo.java")))
	assert.False(t, isUnderMavenTestRoot(filepath.FromSlash("mysrc/test/java/a/Foo.java")))
}

func TestPatterns_MultiModuleMavenTestHelpersNotFlagged(t *testing.T) {
	dir := t.TempDir()
	writeSourceFile(t, dir, "raft/src/test/java/org/apache/kafka/raft/TestUtils.java")
	writeSourceFile(t, dir, "raft/src/main/java/org/apache/kafka/raft/Quorum.java")

	got := missingTestPaths(t, dir, signal.CollectorOpts{})
	assert.Equal(t, []string{filepath.FromSlash("raft/src/main/java/org/apache/kafka/raft/Quorum.java")}, got)
}

func TestPatterns_GoTestSameDirStillDetectedAndMissingReported(t *testing.T) {
	dir := t.TempDir()
	writeSourceFile(t, dir, "internal/a/handler.go")
	writeSourceFile(t, dir, "internal/a/handler_test.go")
	writeSourceFile(t, dir, "internal/b/server.go")

	got := missingTestPaths(t, dir, signal.CollectorOpts{})
	assert.Equal(t, []string{filepath.FromSlash("internal/b/server.go")}, got)
}

// --- Non-source exclusions ---

func TestIsConfigPath(t *testing.T) {
	yes := []string{
		"config/app.php",
		"src/config/database.js",
		"Config/Routes.cs",
		"configs/dev.py",
		"app/settings/base.py",
		"webpack.config.js",
		"packages/ui/jest.config.ts",
		"vite.Config.ts",
		".eslintrc.js",
		"proj/settings.py",
		"docs/conf.py",
		"setup.py",
	}
	for _, p := range yes {
		assert.True(t, isConfigPath(filepath.FromSlash(p)), p)
	}
	no := []string{
		"src/configuration.go",
		"src/ConfigLoader.java",
		"app/models.py",
		"config-stubs/app.php",
	}
	for _, p := range no {
		assert.False(t, isConfigPath(filepath.FromSlash(p)), p)
	}
}

func TestIsDataClassPath(t *testing.T) {
	yes := []string{
		"src/Illuminate/Auth/Events/Login.php",
		"src/Illuminate/Contracts/Cache/Store.php",
		"src/Illuminate/Database/Exceptions/QueryException.php",
		"Jellyfin.Api/Models/PlaybackDtos/DeviceInfo.cs",
		"src/main/java/com/acme/dto/UserDto.java",
		"src/main/java/com/acme/DTOs/Order.java",
		"src/main/kotlin/com/acme/entities/User.kt",
		"src/main/scala/acme/interfaces/Repo.scala",
		"src/Auth/AuthenticationException.php",
		"src/Auth/UserDto.php",
		"src/Auth/UserDTO.java",
		"src/Auth/TokenRepositoryInterface.php",
	}
	for _, p := range yes {
		assert.True(t, isDataClassPath(filepath.FromSlash(p)), p)
	}
	no := []string{
		"src/Illuminate/Cache/DynamoDbStore.php",
		// Only class-per-file languages get the cheap directory check.
		"app/models/user.py",
		"src/models/user.ts",
		"internal/models/user.go",
		// Bare suffix words are not class names.
		"src/Exception.php",
		"src/Dto.cs",
	}
	for _, p := range no {
		assert.False(t, isDataClassPath(filepath.FromSlash(p)), p)
	}
}

func TestIsDocOrDemoTree(t *testing.T) {
	yes := []string{
		"docs_src/tutorial/main.py",
		"docs/contributors/gen.py",
		"doc/build.py",
		"extras/profiling/bench.py",
		"examples/basic/main.go",
		"samples/Sample1/Program.cs",
		"tutorial01/step1.py",
		"Tutorials/intro/app.js",
		"pkg/sub/examples/demo.go",
	}
	for _, p := range yes {
		assert.True(t, isDocOrDemoTree(filepath.FromSlash(p)), p)
	}
	no := []string{
		"src/main.py",
		"internal/docstale.go",
		"src/example.go",
	}
	for _, p := range no {
		assert.False(t, isDocOrDemoTree(filepath.FromSlash(p)), p)
	}
}

func TestIsNonSourceForTests_DemoGate(t *testing.T) {
	demo := filepath.FromSlash("docs_src/tutorial/main.py")
	assert.True(t, isNonSourceForTests(demo, false))
	assert.False(t, isNonSourceForTests(demo, true))

	// Config and data-class exclusions are not gated by IncludeDemoPaths.
	cfg := filepath.FromSlash("config/app.php")
	assert.True(t, isNonSourceForTests(cfg, true))
	dto := filepath.FromSlash("src/Events/Login.php")
	assert.True(t, isNonSourceForTests(dto, true))

	assert.False(t, isNonSourceForTests(filepath.FromSlash("src/Cache/Store.php"), false))
}

func TestPatterns_ConfigAndDataClassesNotFlagged(t *testing.T) {
	dir := t.TempDir()
	writeSourceFile(t, dir, "config/app.php")
	writeSourceFile(t, dir, "config/cache.php")
	writeSourceFile(t, dir, "webpack.config.js")
	writeSourceFile(t, dir, "proj/settings.py")
	writeSourceFile(t, dir, "src/Illuminate/Auth/Events/Login.php")
	writeSourceFile(t, dir, "src/Illuminate/Contracts/Cache/Store.php")
	writeSourceFile(t, dir, "src/Illuminate/Cache/CacheException.php")
	writeSourceFile(t, dir, "src/Illuminate/Cache/RedisStore.php")

	got := missingTestPaths(t, dir, signal.CollectorOpts{IncludeDemoPaths: true})
	assert.Equal(t, []string{filepath.FromSlash("src/Illuminate/Cache/RedisStore.php")}, got)
}

func TestPatterns_DocsSrcAndTutorialNotFlaggedByDefault(t *testing.T) {
	dir := t.TempDir()
	writeSourceFile(t, dir, "docs_src/tutorial/main.py")
	writeSourceFile(t, dir, "tutorial01/app.py")
	writeSourceFile(t, dir, "extras/scripts/build.py")
	writeSourceFile(t, dir, "docs/contributors/gen.py")
	writeSourceFile(t, dir, "src/app.py")

	got := missingTestPaths(t, dir, signal.CollectorOpts{})
	assert.Equal(t, []string{filepath.FromSlash("src/app.py")}, got)

	// --include-demo-paths brings docs_src/ and tutorial*/ back.
	got = missingTestPaths(t, dir, signal.CollectorOpts{IncludeDemoPaths: true})
	assert.Contains(t, got, filepath.FromSlash("docs_src/tutorial/main.py"))
	assert.Contains(t, got, filepath.FromSlash("tutorial01/app.py"))
}

// --- Directory ratios exclude the same non-source directories (stringer-89b) ---

func TestPatterns_DirectoryRatiosExcludeNonSourceDirs(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{
		"docs/contributors/gen.py",
		"docs/packaging/brew/update.py",
		"extras/profiling/bench.py",
		"extras/scripts/tool.py",
		"docs_src/tutorial/main.py",
		"config/app.php",
		"src/Events/Login.php",
		"src/core/engine.py",
		"src/core/test_engine.py",
	} {
		writeSourceFile(t, dir, p)
	}

	c := &PatternsCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	m, ok := c.Metrics().(*PatternsMetrics)
	require.True(t, ok)
	require.Len(t, m.DirectoryTestRatios, 1)
	assert.Equal(t, filepath.FromSlash("src/core"), m.DirectoryTestRatios[0].Path)
	assert.Equal(t, 1, m.DirectoryTestRatios[0].SourceFiles)
	assert.Equal(t, 1, m.DirectoryTestRatios[0].TestFiles)
}

func TestPatterns_DirectoryRatiosIncludeDemoOptIn(t *testing.T) {
	dir := t.TempDir()
	writeSourceFile(t, dir, "docs_src/tutorial/main.py")
	writeSourceFile(t, dir, "config/app.php")

	c := &PatternsCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{IncludeDemoPaths: true})
	require.NoError(t, err)

	m, ok := c.Metrics().(*PatternsMetrics)
	require.True(t, ok)
	// docs_src/ is opted back in; config/ stays excluded.
	require.Len(t, m.DirectoryTestRatios, 1)
	assert.Equal(t, filepath.FromSlash("docs_src/tutorial"), m.DirectoryTestRatios[0].Path)
}

func TestPatterns_LowTestRatioSkipsConfigDirs(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"config/a.php", "config/b.php", "config/c.php", "config/d.php"} {
		writeSourceFile(t, dir, p)
	}

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	for _, s := range signals {
		assert.NotEqual(t, "low-test-ratio", s.Kind)
		assert.NotEqual(t, "missing-tests", s.Kind)
	}
}

func TestPatterns_ContextCancelledDuringMissingTestLookup(t *testing.T) {
	dir := t.TempDir()
	writeSourceFile(t, dir, "src/a.go")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &PatternsCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{})
	require.Error(t, err)
}
