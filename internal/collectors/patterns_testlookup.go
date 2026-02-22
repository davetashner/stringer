// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// hasTestCounterpart checks if a corresponding test file exists in the same
// directory or in a parallel test tree using naming heuristics.
func hasTestCounterpart(absPath, relPath, repoPath string, testRoots []string) bool {
	dir := filepath.Dir(absPath)
	base := filepath.Base(relPath)
	ext := filepath.Ext(base)
	nameWithoutExt := strings.TrimSuffix(base, ext)

	var candidates []string

	switch ext {
	case ".go":
		// foo.go → foo_test.go
		candidates = append(candidates, nameWithoutExt+"_test.go")
	case ".js", ".jsx":
		// foo.js → foo.test.js, foo.spec.js
		candidates = append(candidates,
			nameWithoutExt+".test"+ext,
			nameWithoutExt+".spec"+ext,
		)
	case ".ts", ".tsx":
		// foo.ts → foo.test.ts, foo.spec.ts
		candidates = append(candidates,
			nameWithoutExt+".test"+ext,
			nameWithoutExt+".spec"+ext,
		)
	case ".py":
		// foo.py → test_foo.py, foo_test.py
		candidates = append(candidates,
			"test_"+base,
			nameWithoutExt+"_test.py",
		)
	case ".rb":
		// foo.rb → foo_spec.rb, foo_test.rb
		candidates = append(candidates,
			nameWithoutExt+"_spec.rb",
			nameWithoutExt+"_test.rb",
		)
	case ".java":
		// Foo.java → FooTest.java, FooTests.java, FooSpec.java
		candidates = append(candidates,
			nameWithoutExt+"Test.java",
			nameWithoutExt+"Tests.java",
			nameWithoutExt+"Spec.java",
		)
	case ".kt":
		// Foo.kt → FooTest.kt, FooTests.kt, FooSpec.kt
		candidates = append(candidates,
			nameWithoutExt+"Test.kt",
			nameWithoutExt+"Tests.kt",
			nameWithoutExt+"Spec.kt",
		)
	case ".rs":
		// Rust: foo.rs → foo_test.rs (uncommon but exists)
		candidates = append(candidates, nameWithoutExt+"_test.rs")

		// Check for inline tests (#[cfg(test)]) before looking for external files.
		if hasInlineTests(absPath) {
			return true
		}

		// Check tests/ directory at repo root for integration tests.
		// tests/foo.rs or tests/foo/mod.rs
		testsDir := filepath.Join(repoPath, "tests")
		if _, err := FS.Stat(filepath.Join(testsDir, nameWithoutExt+".rs")); err == nil {
			return true
		}
		if _, err := FS.Stat(filepath.Join(testsDir, nameWithoutExt, "mod.rs")); err == nil {
			return true
		}
	case ".cs":
		// C#: Foo.cs → FooTests.cs, FooTest.cs
		candidates = append(candidates,
			nameWithoutExt+"Tests.cs",
			nameWithoutExt+"Test.cs",
		)

		// Check parallel .Tests project directories.
		// MyApp/Foo.cs → MyApp.Tests/FooTests.cs, MyApp.Tests/FooTest.cs
		// Also check MyApp.UnitTests/ and MyApp.IntegrationTests/
		csDir := filepath.Dir(relPath)
		csParts := strings.Split(filepath.ToSlash(csDir), "/")
		if len(csParts) > 0 {
			projectDir := csParts[0]
			rest := ""
			if len(csParts) > 1 {
				rest = strings.Join(csParts[1:], "/")
			}
			for _, suffix := range []string{".Tests", ".UnitTests", ".IntegrationTests"} {
				testProjectDir := projectDir + suffix
				var testDirPath string
				if rest != "" {
					testDirPath = filepath.Join(repoPath, testProjectDir, filepath.FromSlash(rest))
				} else {
					testDirPath = filepath.Join(repoPath, testProjectDir)
				}
				for _, testName := range []string{nameWithoutExt + "Tests.cs", nameWithoutExt + "Test.cs"} {
					if _, err := FS.Stat(filepath.Join(testDirPath, testName)); err == nil {
						return true
					}
				}
			}
		}
	case ".scala":
		// Scala: Foo.scala → FooTest.scala, FooSpec.scala, FooSuite.scala
		candidates = append(candidates,
			nameWithoutExt+"Test.scala",
			nameWithoutExt+"Tests.scala",
			nameWithoutExt+"Spec.scala",
			nameWithoutExt+"Suite.scala",
		)
	case ".ex":
		// Elixir: foo.ex → foo_test.exs (test/ mirrors lib/)
		candidates = append(candidates, nameWithoutExt+"_test.exs")

		// Elixir convention: lib/foo.ex → test/foo_test.exs
		testDir := filepath.Join(repoPath, "test")
		relDir := filepath.Dir(relPath)
		// Strip leading "lib/" if present
		trimmed := strings.TrimPrefix(filepath.ToSlash(relDir), "lib/")
		trimmed = strings.TrimPrefix(trimmed, "lib")
		if trimmed == "" {
			if _, err := FS.Stat(filepath.Join(testDir, nameWithoutExt+"_test.exs")); err == nil {
				return true
			}
		} else {
			if _, err := FS.Stat(filepath.Join(testDir, filepath.FromSlash(trimmed), nameWithoutExt+"_test.exs")); err == nil {
				return true
			}
		}
	case ".php":
		// PHP: Foo.php → FooTest.php, Foo_test.php
		candidates = append(candidates,
			nameWithoutExt+"Test.php",
			nameWithoutExt+"_test.php",
		)
	case ".swift":
		// Swift: Foo.swift → FooTests.swift, FooTest.swift
		candidates = append(candidates,
			nameWithoutExt+"Tests.swift",
			nameWithoutExt+"Test.swift",
		)

		// SPM convention: Tests/ directory at repo root (capital T).
		spmTestsDir := filepath.Join(repoPath, "Tests")
		for _, testName := range candidates {
			// Direct: Tests/FooTests.swift
			if _, err := FS.Stat(filepath.Join(spmTestsDir, testName)); err == nil {
				return true
			}
		}
		// Also search subdirectories of Tests/ (e.g., Tests/MyAppTests/FooTests.swift).
		entries, readErr := os.ReadDir(spmTestsDir)
		if readErr == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					for _, testName := range candidates {
						if _, err := FS.Stat(filepath.Join(spmTestsDir, entry.Name(), testName)); err == nil {
							return true
						}
					}
				}
			}
		}
	default:
		return false
	}

	// Check same-directory candidates.
	for _, candidate := range candidates {
		if _, err := FS.Stat(filepath.Join(dir, candidate)); err == nil {
			return true
		}
	}

	// Check parallel test directories.
	for _, testRoot := range testRoots {
		// Mirror the relative path under the test root.
		// e.g., "src/handler.py" -> "tests/src/test_handler.py"
		relDir := filepath.Dir(relPath)
		testDir := filepath.Join(repoPath, testRoot, relDir)
		for _, candidate := range candidates {
			if _, err := FS.Stat(filepath.Join(testDir, candidate)); err == nil {
				return true
			}
		}
	}

	// Maven/Gradle convention: src/main/{java,kotlin,scala}/... → src/test/{java,kotlin,scala}/...
	if testDir, ok := mavenTestDir(relPath); ok {
		for _, candidate := range candidates {
			if _, err := FS.Stat(filepath.Join(repoPath, testDir, candidate)); err == nil {
				return true
			}
		}
	}

	// Try stripping progressively more path components for projects where
	// the source root (e.g., "src/", "src/flask/") is not mirrored in the
	// test tree. For example, "src/flask/app.py" tries:
	//   strip 1: tests/flask/test_app.py
	//   strip 2: tests/test_app.py  (all components stripped)
	for _, testRoot := range testRoots {
		relDir := filepath.Dir(relPath)
		parts := strings.Split(relDir, string(filepath.Separator))
		for i := 1; i <= len(parts); i++ {
			stripped := filepath.Join(parts[i:]...)
			testDir := filepath.Join(repoPath, testRoot, stripped)
			for _, candidate := range candidates {
				if _, err := FS.Stat(filepath.Join(testDir, candidate)); err == nil {
					return true
				}
			}
		}
	}

	return false
}

// mavenTestDir checks if relPath follows Maven/Gradle convention
// (src/main/{java,kotlin,scala}/...) and returns the corresponding test
// directory (src/test/{java,kotlin,scala}/...). Returns ("", false) if the
// path doesn't match the convention.
func mavenTestDir(relPath string) (string, bool) {
	norm := filepath.ToSlash(relPath)
	for _, lang := range []string{"java", "kotlin", "scala"} {
		prefix := "src/main/" + lang + "/"
		if strings.HasPrefix(norm, prefix) {
			rest := strings.TrimPrefix(norm, prefix)
			dir := filepath.Dir(rest)
			testBase := "src/test/" + lang
			if dir != "." {
				testBase += "/" + dir
			}
			return filepath.FromSlash(testBase), true
		}
	}
	return "", false
}

// hasInlineTests checks if a Rust source file contains inline tests using
// the #[cfg(test)] attribute. Rust inline test modules are conventionally
// placed at the bottom of source files, so the entire file is scanned.
func hasInlineTests(path string) bool {
	if !strings.HasSuffix(path, ".rs") {
		return false
	}

	f, err := FS.Open(path)
	if err != nil {
		return false
	}
	defer f.Close() //nolint:errcheck // read-only file

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "#[cfg(test)]" {
			return true
		}
	}
	return false
}
