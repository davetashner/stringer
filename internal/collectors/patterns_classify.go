// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"bufio"
	"path/filepath"
	"strings"
)

// isTestFile returns true if the filename matches common test-file naming
// conventions across languages.
func isTestFile(relPath string) bool {
	// Files under Maven/Gradle test source roots are always test files.
	if isUnderMavenTestRoot(relPath) {
		return true
	}

	base := filepath.Base(relPath)

	// Go: *_test.go
	if strings.HasSuffix(base, "_test.go") {
		return true
	}
	// JS/TS: *.test.js, *.test.ts, *.test.jsx, *.test.tsx, *.spec.js, etc.
	for _, suffix := range []string{".test.js", ".test.ts", ".test.jsx", ".test.tsx", ".spec.js", ".spec.ts", ".spec.jsx", ".spec.tsx"} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	// Python: test_*.py, *_test.py
	if strings.HasSuffix(base, ".py") {
		name := strings.TrimSuffix(base, ".py")
		if strings.HasPrefix(name, "test_") || strings.HasSuffix(name, "_test") {
			return true
		}
	}
	// Ruby: *_spec.rb, *_test.rb, test_*.rb
	if strings.HasSuffix(base, "_spec.rb") || strings.HasSuffix(base, "_test.rb") {
		return true
	}
	if strings.HasSuffix(base, ".rb") && strings.HasPrefix(base, "test_") {
		return true
	}
	// Java/Kotlin: *Test.java, *Tests.java, *Spec.java, *Test.kt, *Tests.kt, *Spec.kt
	for _, suffix := range []string{"Test.java", "Tests.java", "Spec.java", "Test.kt", "Tests.kt", "Spec.kt"} {
		if strings.HasSuffix(base, suffix) && len(base) > len(suffix) {
			return true
		}
	}
	// Rust: files in tests/ directory (integration tests) or benches/ directory.
	// Also recognize the uncommon foo_test.rs naming.
	if strings.HasSuffix(base, ".rs") {
		dir := filepath.Dir(relPath)
		parts := strings.Split(filepath.ToSlash(dir), "/")
		for _, p := range parts {
			if p == "tests" || p == "benches" {
				return true
			}
		}
		name := strings.TrimSuffix(base, ".rs")
		if strings.HasSuffix(name, "_test") {
			return true
		}
	}
	// C#: *Tests.cs, *Test.cs (NUnit/xUnit/MSTest conventions)
	if strings.HasSuffix(base, ".cs") {
		name := strings.TrimSuffix(base, ".cs")
		if strings.HasSuffix(name, "Tests") || strings.HasSuffix(name, "Test") {
			return true
		}
	}
	// PHP: *Test.php (PHPUnit convention), *_test.php, files in tests/ directories
	if strings.HasSuffix(base, ".php") {
		name := strings.TrimSuffix(base, ".php")
		if strings.HasSuffix(name, "Test") || strings.HasSuffix(name, "_test") {
			return true
		}
		dir := filepath.Dir(relPath)
		parts := strings.Split(filepath.ToSlash(dir), "/")
		for _, p := range parts {
			if p == "tests" {
				return true
			}
		}
	}
	// Scala: *Test.scala, *Tests.scala, *Spec.scala, files under src/test/scala/
	for _, suffix := range []string{"Test.scala", "Tests.scala", "Spec.scala", "Suite.scala"} {
		if strings.HasSuffix(base, suffix) && len(base) > len(suffix) {
			return true
		}
	}
	// Elixir: *_test.exs
	if strings.HasSuffix(base, "_test.exs") {
		return true
	}
	// Swift: *Tests.swift, *Test.swift (XCTest convention), files in Tests/ directories (SPM convention)
	if strings.HasSuffix(base, ".swift") {
		name := strings.TrimSuffix(base, ".swift")
		if strings.HasSuffix(name, "Tests") || strings.HasSuffix(name, "Test") {
			return true
		}
		dir := filepath.Dir(relPath)
		parts := strings.Split(filepath.ToSlash(dir), "/")
		for _, p := range parts {
			if p == "Tests" {
				return true
			}
		}
	}
	return false
}

// isUnderMavenTestRoot returns true if relPath is under a Maven/Gradle test
// source tree (src/test/{java,kotlin,scala}/). Files in these directories
// are test files regardless of their naming convention.
func isUnderMavenTestRoot(relPath string) bool {
	norm := filepath.ToSlash(relPath)
	for _, lang := range []string{"java", "kotlin", "scala"} {
		if strings.HasPrefix(norm, "src/test/"+lang+"/") {
			return true
		}
	}
	return false
}

// isUnderTestRoot returns true if relPath is under one of the parallel test
// root directories (e.g., "tests/", "test/"). Files in test roots should not
// be flagged as missing tests.
func isUnderTestRoot(relPath string, testRoots []string) bool {
	for _, root := range testRoots {
		if strings.HasPrefix(relPath, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// isGeneratedFile returns true if the file appears to be machine-generated.
// Checks for Go stringer output (*_string.go) and files with a "Code generated"
// header in the first line.
func isGeneratedFile(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, "_string.go") {
		return true
	}

	f, err := FS.Open(path)
	if err != nil {
		return false
	}
	defer f.Close() //nolint:errcheck // read-only file

	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "Code generated") {
			return true
		}
	}
	return false
}
