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

// =============================================================================
// Rust ecosystem tests
// =============================================================================

// --- isTestFile: Rust patterns ---

func TestIsTestFile_Rust(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "rs_source", path: "src/lib.rs", want: false},
		{name: "rs_source_main", path: "src/main.rs", want: false},
		{name: "rs_in_tests_dir", path: "tests/integration.rs", want: true},
		{name: "rs_in_tests_subdir", path: "tests/api/health.rs", want: true},
		{name: "rs_in_benches_dir", path: "benches/bench_sort.rs", want: true},
		{name: "rs_in_benches_subdir", path: "benches/crypto/aes.rs", want: true},
		{name: "rs_test_suffix", path: "src/parser_test.rs", want: true},
		{name: "rs_normal_source", path: "src/parser.rs", want: false},
		{name: "rs_mod_in_tests", path: "tests/common/mod.rs", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTestFile(tt.path)
			assert.Equal(t, tt.want, got, "isTestFile(%q)", tt.path)
		})
	}
}

// --- hasInlineTests ---

func TestHasInlineTests(t *testing.T) {
	dir := t.TempDir()

	t.Run("file_with_inline_tests", func(t *testing.T) {
		content := `use std::collections::HashMap;

pub fn add(a: i32, b: i32) -> i32 {
    a + b
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_add() {
        assert_eq!(add(2, 3), 5);
    }
}
`
		path := filepath.Join(dir, "with_tests.rs")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		assert.True(t, hasInlineTests(path), "should detect #[cfg(test)]")
	})

	t.Run("file_without_inline_tests", func(t *testing.T) {
		content := `pub fn add(a: i32, b: i32) -> i32 {
    a + b
}
`
		path := filepath.Join(dir, "without_tests.rs")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		assert.False(t, hasInlineTests(path), "should not detect inline tests")
	})

	t.Run("non_rs_file", func(t *testing.T) {
		path := filepath.Join(dir, "main.go")
		require.NoError(t, os.WriteFile(path, []byte("package main\n"), 0o600))
		assert.False(t, hasInlineTests(path), "non-.rs file should return false")
	})

	t.Run("nonexistent_file", func(t *testing.T) {
		assert.False(t, hasInlineTests("/nonexistent/path.rs"), "nonexistent file should return false")
	})

	t.Run("cfg_test_at_bottom_of_large_file", func(t *testing.T) {
		// Simulate a large file where #[cfg(test)] is at the bottom (line 300+).
		var lines []string
		for i := 0; i < 300; i++ {
			lines = append(lines, "// filler line")
		}
		lines = append(lines, "#[cfg(test)]")
		lines = append(lines, "mod tests {}")
		content := strings.Join(lines, "\n")
		path := filepath.Join(dir, "large_file.rs")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		assert.True(t, hasInlineTests(path), "should detect #[cfg(test)] even at bottom of file")
	})

	t.Run("cfg_test_with_indentation", func(t *testing.T) {
		content := `pub fn foo() {}

    #[cfg(test)]
    mod tests {}
`
		path := filepath.Join(dir, "indented.rs")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		assert.True(t, hasInlineTests(path), "should detect indented #[cfg(test)]")
	})
}

// --- hasTestCounterpart: Rust patterns ---

func TestHasTestCounterpart_RustInlineTests(t *testing.T) {
	dir := t.TempDir()

	// Create a Rust source file with inline tests.
	srcDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))

	content := `pub fn add(a: i32, b: i32) -> i32 { a + b }

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn test_add() { assert_eq!(add(1, 2), 3); }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "lib.rs"), []byte(content), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(srcDir, "lib.rs"),
		"src/lib.rs",
		dir,
		nil,
	), "Rust file with #[cfg(test)] inline tests should have test counterpart")
}

func TestHasTestCounterpart_RustNoInlineTests(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))

	content := "pub fn add(a: i32, b: i32) -> i32 { a + b }\n"
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "lib.rs"), []byte(content), 0o600))

	assert.False(t, hasTestCounterpart(
		filepath.Join(srcDir, "lib.rs"),
		"src/lib.rs",
		dir,
		nil,
	), "Rust file without inline tests or test files should not have test counterpart")
}

func TestHasTestCounterpart_RustTestFileInSameDir(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))

	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "parser.rs"), []byte("pub fn parse() {}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "parser_test.rs"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(srcDir, "parser.rs"),
		"src/parser.rs",
		dir,
		nil,
	), "Rust file with foo_test.rs in same dir should have test counterpart")
}

func TestHasTestCounterpart_RustIntegrationTestsDir(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "api.rs"), []byte("pub fn api() {}\n"), 0o600))

	// Create tests/api.rs at repo root.
	testsDir := filepath.Join(dir, "tests")
	require.NoError(t, os.MkdirAll(testsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(testsDir, "api.rs"), []byte("// integration test\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(srcDir, "api.rs"),
		"src/api.rs",
		dir,
		nil,
	), "Rust file with tests/api.rs at repo root should have test counterpart")
}

func TestHasTestCounterpart_RustIntegrationTestsModDir(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "api.rs"), []byte("pub fn api() {}\n"), 0o600))

	// Create tests/api/mod.rs at repo root (multi-file integration test).
	testsDir := filepath.Join(dir, "tests", "api")
	require.NoError(t, os.MkdirAll(testsDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(testsDir, "mod.rs"), []byte("// mod test\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(srcDir, "api.rs"),
		"src/api.rs",
		dir,
		nil,
	), "Rust file with tests/api/mod.rs should have test counterpart")
}

// --- Collect integration: Rust ---

func TestPatterns_RustInlineTestsSuppressMissingTestSignal(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))

	// Create a Rust file with enough lines and inline tests.
	filler := strings.Repeat("// line\n", 25)
	content := filler + "\n#[cfg(test)]\nmod tests {\n    #[test]\n    fn it_works() {}\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "lib.rs"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "lib.rs") {
			t.Error("Rust file with #[cfg(test)] inline tests should not produce missing-tests signal")
		}
	}
}

func TestPatterns_RustMissingTestsDetected(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))

	// Create a Rust file without inline tests and no test counterpart.
	content := strings.Repeat("pub fn foo() {}\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "handler.rs"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	var missingTests []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "handler.rs") {
			missingTests = append(missingTests, s)
		}
	}
	require.Len(t, missingTests, 1, "Rust file without tests should produce missing-tests signal")
}

func TestPatterns_RustBenchFilesNotFlaggedMissingTests(t *testing.T) {
	dir := t.TempDir()

	benchDir := filepath.Join(dir, "benches")
	require.NoError(t, os.MkdirAll(benchDir, 0o750))

	content := strings.Repeat("// bench code\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(benchDir, "sorting.rs"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "sorting.rs") {
			t.Error("bench files should not produce missing-tests signal")
		}
	}
}

func TestPatterns_RustTestsInTestsDirAreTestFiles(t *testing.T) {
	dir := t.TempDir()

	testsDir := filepath.Join(dir, "tests")
	require.NoError(t, os.MkdirAll(testsDir, 0o750))

	content := strings.Repeat("// test code\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(testsDir, "integration.rs"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "integration.rs") {
			t.Error("files in tests/ directory should be recognized as test files")
		}
	}
}

// --- detectTestRoots: benches directory ---

func TestDetectTestRoots_BenchesDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "benches"), 0o750))

	roots := detectTestRoots(dir)

	assert.Contains(t, roots, "benches")
}

// =============================================================================
// Java / Kotlin ecosystem tests
// =============================================================================

func TestIsTestFile_JavaTests(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "java_test_singular", path: "FooTest.java", want: true},
		{name: "java_test_plural", path: "FooTests.java", want: true},
		{name: "java_spec", path: "FooSpec.java", want: true},
		{name: "java_source", path: "Foo.java", want: false},
		{name: "kotlin_test_singular", path: "BarTest.kt", want: true},
		{name: "kotlin_test_plural", path: "BarTests.kt", want: true},
		{name: "kotlin_spec", path: "BarSpec.kt", want: true},
		{name: "kotlin_source", path: "Bar.kt", want: false},
		{name: "java_tests_in_maven_test_dir", path: "src/test/java/com/example/FooTests.java", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTestFile(tt.path)
			assert.Equal(t, tt.want, got, "isTestFile(%q)", tt.path)
		})
	}
}

func TestHasTestCounterpart_MavenJavaTestsPlural(t *testing.T) {
	dir := t.TempDir()

	// src/main/java/com/example/Foo.java
	srcDir := filepath.Join(dir, "src", "main", "java", "com", "example")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Foo.java"), []byte("// java\n"), 0o600))

	// src/test/java/com/example/FooTests.java (plural Tests)
	testDir := filepath.Join(dir, "src", "test", "java", "com", "example")
	require.NoError(t, os.MkdirAll(testDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "FooTests.java"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(srcDir, "Foo.java"),
		"src/main/java/com/example/Foo.java",
		dir,
		nil,
	), "should find FooTests.java (plural) as counterpart via Maven convention")
}

func TestPatterns_JavaTestsPluralNotFlaggedMissingTests(t *testing.T) {
	dir := t.TempDir()

	// Create a Java source file with enough lines.
	srcDir := filepath.Join(dir, "src", "main", "java", "com", "example")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	content := strings.Repeat("// java source\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Foo.java"), []byte(content), 0o600))

	// Create FooTests.java (plural) in Maven test tree.
	testDir := filepath.Join(dir, "src", "test", "java", "com", "example")
	require.NoError(t, os.MkdirAll(testDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "FooTests.java"), []byte("// test\n"), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.HasSuffix(s.FilePath, "Foo.java") {
			t.Error("Java file with FooTests.java counterpart should not produce missing-tests signal")
		}
	}
}

// =============================================================================
// C# / .NET ecosystem tests
// =============================================================================

// --- isTestFile: C# patterns ---

func TestIsTestFile_CSharp(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "cs_source", path: "MyApp/Foo.cs", want: false},
		{name: "cs_tests_suffix", path: "MyApp.Tests/FooTests.cs", want: true},
		{name: "cs_test_suffix", path: "MyApp.Tests/FooTest.cs", want: true},
		{name: "cs_just_test_name", path: "Test.cs", want: true},
		{name: "cs_just_tests_name", path: "Tests.cs", want: true},
		{name: "cs_service_tests", path: "Services/UserServiceTests.cs", want: true},
		{name: "cs_service_test", path: "Services/UserServiceTest.cs", want: true},
		{name: "cs_plain_source", path: "Controllers/HomeController.cs", want: false},
		{name: "cs_program", path: "Program.cs", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTestFile(tt.path)
			assert.Equal(t, tt.want, got, "isTestFile(%q)", tt.path)
		})
	}
}

// --- hasTestCounterpart: C# patterns ---

func TestHasTestCounterpart_CSharpSameDir(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "Foo.cs"), []byte("// source\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "FooTests.cs"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(dir, "Foo.cs"),
		"Foo.cs",
		dir,
		nil,
	), "C# file with FooTests.cs in same dir should have test counterpart")
}

func TestHasTestCounterpart_CSharpTestSuffix(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "Foo.cs"), []byte("// source\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "FooTest.cs"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(dir, "Foo.cs"),
		"Foo.cs",
		dir,
		nil,
	), "C# file with FooTest.cs in same dir should have test counterpart")
}

func TestHasTestCounterpart_CSharpParallelTestsProject(t *testing.T) {
	dir := t.TempDir()

	// MyApp/Foo.cs
	srcDir := filepath.Join(dir, "MyApp")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Foo.cs"), []byte("// source\n"), 0o600))

	// MyApp.Tests/FooTests.cs
	testDir := filepath.Join(dir, "MyApp.Tests")
	require.NoError(t, os.MkdirAll(testDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "FooTests.cs"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(srcDir, "Foo.cs"),
		"MyApp/Foo.cs",
		dir,
		nil,
	), "C# file with MyApp.Tests/FooTests.cs should have test counterpart")
}

func TestHasTestCounterpart_CSharpUnitTestsProject(t *testing.T) {
	dir := t.TempDir()

	// MyApp/Services/UserService.cs
	srcDir := filepath.Join(dir, "MyApp", "Services")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "UserService.cs"), []byte("// source\n"), 0o600))

	// MyApp.UnitTests/Services/UserServiceTests.cs
	testDir := filepath.Join(dir, "MyApp.UnitTests", "Services")
	require.NoError(t, os.MkdirAll(testDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "UserServiceTests.cs"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(srcDir, "UserService.cs"),
		"MyApp/Services/UserService.cs",
		dir,
		nil,
	), "C# file with MyApp.UnitTests/Services/UserServiceTests.cs should have test counterpart")
}

func TestHasTestCounterpart_CSharpIntegrationTestsProject(t *testing.T) {
	dir := t.TempDir()

	// MyApp/Foo.cs
	srcDir := filepath.Join(dir, "MyApp")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Foo.cs"), []byte("// source\n"), 0o600))

	// MyApp.IntegrationTests/FooTest.cs
	testDir := filepath.Join(dir, "MyApp.IntegrationTests")
	require.NoError(t, os.MkdirAll(testDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "FooTest.cs"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(srcDir, "Foo.cs"),
		"MyApp/Foo.cs",
		dir,
		nil,
	), "C# file with MyApp.IntegrationTests/FooTest.cs should have test counterpart")
}

func TestHasTestCounterpart_CSharpNoTests(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "MyApp")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Foo.cs"), []byte("// source\n"), 0o600))

	assert.False(t, hasTestCounterpart(
		filepath.Join(srcDir, "Foo.cs"),
		"MyApp/Foo.cs",
		dir,
		nil,
	), "C# file without any test counterpart should return false")
}

// --- Collect integration: C# ---

func TestPatterns_CSharpMissingTestsDetected(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "MyApp")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))

	content := strings.Repeat("public class Foo {}\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Foo.cs"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	var missingTests []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "Foo.cs") {
			missingTests = append(missingTests, s)
		}
	}
	require.Len(t, missingTests, 1, "C# file without tests should produce missing-tests signal")
}

func TestPatterns_CSharpTestsProjectSuppressesMissingTests(t *testing.T) {
	dir := t.TempDir()

	// MyApp/Foo.cs
	srcDir := filepath.Join(dir, "MyApp")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	content := strings.Repeat("public class Foo {}\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Foo.cs"), []byte(content), 0o600))

	// MyApp.Tests/FooTests.cs
	testDir := filepath.Join(dir, "MyApp.Tests")
	require.NoError(t, os.MkdirAll(testDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "FooTests.cs"), []byte("// test\n"), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.HasSuffix(s.FilePath, "Foo.cs") && !strings.Contains(s.FilePath, ".Tests") {
			t.Error("C# file with parallel .Tests project should not produce missing-tests signal")
		}
	}
}

func TestPatterns_CSharpTestFilesRecognizedAsTests(t *testing.T) {
	dir := t.TempDir()

	testDir := filepath.Join(dir, "MyApp.Tests")
	require.NoError(t, os.MkdirAll(testDir, 0o750))

	content := strings.Repeat("// test code\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "FooTests.cs"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "FooTests.cs") {
			t.Error("*Tests.cs files should be recognized as test files")
		}
	}
}

// --- Collect integration: C# with .Test suffix ---

func TestPatterns_CSharpTestSuffixRecognizedAsTests(t *testing.T) {
	dir := t.TempDir()

	testDir := filepath.Join(dir, "MyApp.Tests")
	require.NoError(t, os.MkdirAll(testDir, 0o750))

	content := strings.Repeat("// test code\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "FooTest.cs"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "FooTest.cs") {
			t.Error("*Test.cs files should be recognized as test files")
		}
	}
}

// =============================================================================
// PHP ecosystem tests
// =============================================================================

// --- isTestFile: PHP patterns ---

func TestIsTestFile_PHP(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "php_source", path: "src/MyClass.php", want: false},
		{name: "php_test_suffix", path: "tests/MyClassTest.php", want: true},
		{name: "php_underscore_test", path: "src/my_class_test.php", want: true},
		{name: "php_test_dir", path: "tests/Unit/UserTest.php", want: true},
		{name: "php_test_dir_nested", path: "tests/Feature/Auth/LoginTest.php", want: true},
		{name: "php_plain_in_tests_dir", path: "tests/bootstrap.php", want: true},
		{name: "php_controller", path: "app/Http/Controllers/UserController.php", want: false},
		{name: "php_model", path: "app/Models/User.php", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTestFile(tt.path)
			assert.Equal(t, tt.want, got, "isTestFile(%q)", tt.path)
		})
	}
}

// --- hasTestCounterpart: PHP patterns ---

func TestHasTestCounterpart_PHPSameDir(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "UserService.php"), []byte("<?php\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "UserServiceTest.php"), []byte("<?php\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(dir, "UserService.php"),
		"UserService.php",
		dir,
		nil,
	), "PHP file with UserServiceTest.php in same dir should have test counterpart")
}

func TestHasTestCounterpart_PHPUnderscoreSuffix(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "user_service.php"), []byte("<?php\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "user_service_test.php"), []byte("<?php\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(dir, "user_service.php"),
		"user_service.php",
		dir,
		nil,
	), "PHP file with user_service_test.php in same dir should have test counterpart")
}

func TestHasTestCounterpart_PHPNoTests(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "UserService.php"), []byte("<?php\n"), 0o600))

	assert.False(t, hasTestCounterpart(
		filepath.Join(dir, "UserService.php"),
		"UserService.php",
		dir,
		nil,
	), "PHP file without test counterpart should return false")
}

// --- Integration: PHP missing-tests signal ---

func TestPatterns_PHPTestSuffixRecognizedAsTests(t *testing.T) {
	dir := t.TempDir()

	testDir := filepath.Join(dir, "tests")
	require.NoError(t, os.MkdirAll(testDir, 0o750))

	content := strings.Repeat("<?php\n// test code\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "UserTest.php"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "UserTest.php") {
			t.Error("*Test.php files should be recognized as test files")
		}
	}
}

func TestPatterns_PHPMissingTestsDetected(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))

	content := strings.Repeat("<?php\n// source code\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "UserService.php"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	found := false
	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "UserService.php") {
			found = true
			break
		}
	}
	assert.True(t, found, "PHP source file without test counterpart should produce missing-tests signal")
}

func TestPatterns_PHPCounterpartSuppressesMissingTests(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))

	content := strings.Repeat("<?php\n// source code\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "UserService.php"), []byte(content), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "UserServiceTest.php"), []byte("<?php\n"), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "UserService.php") {
			t.Error("PHP file with UserServiceTest.php counterpart should not produce missing-tests signal")
		}
	}
}

// =============================================================================
// Swift ecosystem tests
// =============================================================================

// --- isTestFile: Swift patterns ---

func TestIsTestFile_Swift(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "swift_source", path: "Sources/MyApp/ViewController.swift", want: false},
		{name: "swift_tests_suffix", path: "Tests/MyAppTests/ViewControllerTests.swift", want: true},
		{name: "swift_test_suffix", path: "Tests/MyAppTests/ViewControllerTest.swift", want: true},
		{name: "swift_in_tests_dir", path: "Tests/Helpers/TestHelper.swift", want: true},
		{name: "swift_in_tests_subdir", path: "Tests/MyAppTests/Models/UserTests.swift", want: true},
		{name: "swift_plain_source", path: "Sources/MyApp/Models/User.swift", want: false},
		{name: "swift_main", path: "Sources/MyApp/main.swift", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTestFile(tt.path)
			assert.Equal(t, tt.want, got, "isTestFile(%q)", tt.path)
		})
	}
}

// --- hasTestCounterpart: Swift patterns ---

func TestHasTestCounterpart_SwiftSameDir(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "Parser.swift"), []byte("// source\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ParserTests.swift"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(dir, "Parser.swift"),
		"Parser.swift",
		dir,
		nil,
	), "Swift file with ParserTests.swift in same dir should have test counterpart")
}

func TestHasTestCounterpart_SwiftTestSuffix(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "Parser.swift"), []byte("// source\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ParserTest.swift"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(dir, "Parser.swift"),
		"Parser.swift",
		dir,
		nil,
	), "Swift file with ParserTest.swift in same dir should have test counterpart")
}

func TestHasTestCounterpart_SwiftNoTests(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "Sources", "MyApp")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Parser.swift"), []byte("// source\n"), 0o600))

	assert.False(t, hasTestCounterpart(
		filepath.Join(srcDir, "Parser.swift"),
		"Sources/MyApp/Parser.swift",
		dir,
		nil,
	), "Swift file without test counterpart should return false")
}

// --- Collect integration: Swift ---

func TestPatterns_SwiftTestFilesRecognizedAsTests(t *testing.T) {
	dir := t.TempDir()

	testDir := filepath.Join(dir, "Tests", "MyAppTests")
	require.NoError(t, os.MkdirAll(testDir, 0o750))

	content := strings.Repeat("// test code\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "ParserTests.swift"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "ParserTests.swift") {
			t.Error("*Tests.swift files should be recognized as test files")
		}
	}
}

func TestPatterns_SwiftMissingTestsDetected(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "Sources", "MyApp")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))

	content := strings.Repeat("import Foundation\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "ViewController.swift"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	found := false
	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "ViewController.swift") {
			found = true
			break
		}
	}
	assert.True(t, found, "Swift source file without test counterpart should produce missing-tests signal")
}

func TestPatterns_SwiftCounterpartSuppressesMissingTests(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "Sources", "MyApp")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	content := strings.Repeat("import Foundation\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Parser.swift"), []byte(content), 0o600))

	// Test counterpart in SPM Tests/ tree (Tests/MyAppTests/ParserTests.swift).
	testDir := filepath.Join(dir, "Tests", "MyAppTests")
	require.NoError(t, os.MkdirAll(testDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "ParserTests.swift"), []byte("// test\n"), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "Parser.swift") && !strings.Contains(s.FilePath, "Tests") {
			t.Error("Swift file with ParserTests.swift counterpart should not produce missing-tests signal")
		}
	}
}

// =============================================================================
// Scala ecosystem tests
// =============================================================================

// --- isTestFile: Scala patterns ---

func TestIsTestFile_Scala(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "scala_source", path: "src/main/scala/com/app/Service.scala", want: false},
		{name: "scala_test_suffix", path: "src/test/scala/com/app/ServiceTest.scala", want: true},
		{name: "scala_tests_suffix", path: "src/test/scala/com/app/ServiceTests.scala", want: true},
		{name: "scala_spec_suffix", path: "src/test/scala/com/app/ServiceSpec.scala", want: true},
		{name: "scala_suite_suffix", path: "src/test/scala/com/app/ServiceSuite.scala", want: true},
		{name: "scala_maven_test_root", path: "src/test/scala/com/app/Plain.scala", want: true},
		{name: "scala_plain", path: "src/main/scala/com/app/App.scala", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTestFile(tt.path)
			assert.Equal(t, tt.want, got, "isTestFile(%q)", tt.path)
		})
	}
}

// --- hasTestCounterpart: Scala patterns ---

func TestHasTestCounterpart_ScalaSameDir(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "Service.scala"), []byte("class Service\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ServiceSpec.scala"), []byte("class ServiceSpec\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(dir, "Service.scala"),
		"Service.scala",
		dir,
		nil,
	), "Scala file with ServiceSpec.scala in same dir should have test counterpart")
}

func TestHasTestCounterpart_ScalaMavenConvention(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "src", "main", "scala", "com", "app")
	testDir := filepath.Join(dir, "src", "test", "scala", "com", "app")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.MkdirAll(testDir, 0o750))

	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Service.scala"), []byte("class Service\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "ServiceTest.scala"), []byte("class ServiceTest\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(srcDir, "Service.scala"),
		"src/main/scala/com/app/Service.scala",
		dir,
		nil,
	), "Scala file with Maven-convention test counterpart should be found")
}

func TestHasTestCounterpart_ScalaNoTests(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "Service.scala"), []byte("class Service\n"), 0o600))

	assert.False(t, hasTestCounterpart(
		filepath.Join(dir, "Service.scala"),
		"Service.scala",
		dir,
		nil,
	), "Scala file without test counterpart should return false")
}

// --- Integration: Scala missing-tests signal ---

func TestPatterns_ScalaMissingTestsDetected(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "src", "main", "scala")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))

	content := strings.Repeat("class Service {\n  def work(): Unit = {}\n}\n", 10)
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Service.scala"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	found := false
	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "Service.scala") {
			found = true
			break
		}
	}
	assert.True(t, found, "Scala source without test counterpart should produce missing-tests signal")
}

func TestPatterns_ScalaCounterpartSuppressesMissingTests(t *testing.T) {
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "src", "main", "scala")
	testDir := filepath.Join(dir, "src", "test", "scala")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.MkdirAll(testDir, 0o750))

	content := strings.Repeat("class Service {\n  def work(): Unit = {}\n}\n", 10)
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Service.scala"), []byte(content), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "ServiceSpec.scala"), []byte("class ServiceSpec\n"), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "Service.scala") && !strings.Contains(s.FilePath, "Spec") {
			t.Error("Scala file with ServiceSpec.scala counterpart should not produce missing-tests signal")
		}
	}
}

// =============================================================================
// Elixir ecosystem tests
// =============================================================================

// --- isTestFile: Elixir patterns ---

func TestIsTestFile_Elixir(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "elixir_source", path: "lib/my_app/server.ex", want: false},
		{name: "elixir_test", path: "test/my_app/server_test.exs", want: true},
		{name: "elixir_test_helper", path: "test/test_helper.exs", want: false},
		{name: "elixir_mix_config", path: "config/config.exs", want: false},
		{name: "elixir_mix_file", path: "mix.exs", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTestFile(tt.path)
			assert.Equal(t, tt.want, got, "isTestFile(%q)", tt.path)
		})
	}
}

// --- hasTestCounterpart: Elixir patterns ---

func TestHasTestCounterpart_ElixirLibToTest(t *testing.T) {
	dir := t.TempDir()

	libDir := filepath.Join(dir, "lib", "my_app")
	testDir := filepath.Join(dir, "test", "my_app")
	require.NoError(t, os.MkdirAll(libDir, 0o750))
	require.NoError(t, os.MkdirAll(testDir, 0o750))

	require.NoError(t, os.WriteFile(filepath.Join(libDir, "server.ex"), []byte("defmodule MyApp.Server do\nend\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "server_test.exs"), []byte("defmodule MyApp.ServerTest do\nend\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(libDir, "server.ex"),
		"lib/my_app/server.ex",
		dir,
		nil,
	), "Elixir lib/my_app/server.ex should find test/my_app/server_test.exs")
}

func TestHasTestCounterpart_ElixirSameDir(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "server.ex"), []byte("defmodule Server do\nend\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "server_test.exs"), []byte("defmodule ServerTest do\nend\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(dir, "server.ex"),
		"server.ex",
		dir,
		nil,
	), "Elixir file with server_test.exs in same dir should have test counterpart")
}

func TestHasTestCounterpart_ElixirNoTests(t *testing.T) {
	dir := t.TempDir()

	libDir := filepath.Join(dir, "lib")
	require.NoError(t, os.MkdirAll(libDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "server.ex"), []byte("defmodule Server do\nend\n"), 0o600))

	assert.False(t, hasTestCounterpart(
		filepath.Join(libDir, "server.ex"),
		"lib/server.ex",
		dir,
		nil,
	), "Elixir file without test counterpart should return false")
}

// --- Integration: Elixir missing-tests signal ---

func TestPatterns_ElixirMissingTestsDetected(t *testing.T) {
	dir := t.TempDir()

	libDir := filepath.Join(dir, "lib", "my_app")
	require.NoError(t, os.MkdirAll(libDir, 0o750))

	content := strings.Repeat("defmodule MyApp.Server do\n  def start, do: :ok\nend\n", 10)
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "server.ex"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	found := false
	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "server.ex") {
			found = true
			break
		}
	}
	assert.True(t, found, "Elixir source without test counterpart should produce missing-tests signal")
}

func TestPatterns_ElixirCounterpartSuppressesMissingTests(t *testing.T) {
	dir := t.TempDir()

	libDir := filepath.Join(dir, "lib", "my_app")
	testDir := filepath.Join(dir, "test", "my_app")
	require.NoError(t, os.MkdirAll(libDir, 0o750))
	require.NoError(t, os.MkdirAll(testDir, 0o750))

	content := strings.Repeat("defmodule MyApp.Server do\n  def start, do: :ok\nend\n", 10)
	require.NoError(t, os.WriteFile(filepath.Join(libDir, "server.ex"), []byte(content), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "server_test.exs"), []byte("defmodule MyApp.ServerTest do\nend\n"), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" && strings.Contains(s.FilePath, "server.ex") {
			t.Error("Elixir file with test counterpart should not produce missing-tests signal")
		}
	}
}
