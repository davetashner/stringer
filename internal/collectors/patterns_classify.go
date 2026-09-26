// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"bytes"
	"io"
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
// source tree (src/test/{java,kotlin,scala}/), either at the repo root or
// inside a module of a multi-module build (core/src/test/java/...). Files in
// these directories are test files regardless of their naming convention.
func isUnderMavenTestRoot(relPath string) bool {
	norm := filepath.ToSlash(relPath)
	for _, lang := range []string{"java", "kotlin", "scala"} {
		root := "src/test/" + lang + "/"
		if strings.HasPrefix(norm, root) || strings.Contains(norm, "/"+root) {
			return true
		}
	}
	return false
}

// isUnderTestRoot returns true if relPath is under one of the parallel test
// root directories (e.g., "tests/", "test/"). Files in test roots should not
// be flagged as missing tests.
func isUnderTestRoot(relPath string, testRoots []string) bool {
	relPath = filepath.ToSlash(relPath)
	for _, root := range testRoots {
		if strings.HasPrefix(relPath, root+"/") {
			return true
		}
	}
	return false
}

// testOnlyDirSegments are directory names that hold nothing but tests and
// their support code (fixtures, helpers, base classes). Matched
// case-insensitively at any depth so that tests/, src/test/, __tests__/ and
// Swift's Tests/ are all recognised.
var testOnlyDirSegments = map[string]bool{
	"tests":     true,
	"test":      true,
	"spec":      true,
	"__tests__": true,
	"benches":   true,
}

// isTestOnlyDir returns true if relPath sits under a test-only directory at
// any depth. Files there that do not carry a test-file name (conftest.py,
// TestCase.php, setup.js, fixtures) are test support code: they are neither
// source files to be covered nor test files, so a test tree never appears as
// a source directory in DirectoryTestRatios.
func isTestOnlyDir(relPath string) bool {
	for _, seg := range dirSegments(relPath) {
		if testOnlyDirSegments[strings.ToLower(seg)] {
			return true
		}
	}
	return false
}

// generatedHeaderLines is how many leading lines are inspected for a
// generated-code marker.
const generatedHeaderLines = 5

// generatedMarkers are substrings that identify machine-generated files when
// they appear in the first generatedHeaderLines lines.
var generatedMarkers = []string{
	"Code generated", // Go convention: "// Code generated by X. DO NOT EDIT."
	"DO NOT EDIT",
	"@generated",      // Facebook/Prettier/Relay convention
	"<auto-generated", // .NET: "// <auto-generated />"
}

// Minified-file heuristic: files whose average line length over the first
// minifiedSampleLines lines exceeds minifiedAvgLineLen are treated as
// minified bundles that nobody edits by hand.
const (
	minifiedSampleLines = 20
	minifiedAvgLineLen  = 400
	// classifyReadBytes bounds the single read used for both checks. It is
	// large enough to hold 20 lines at the minified threshold, so a file that
	// fills it with fewer than 20 newlines is minified by construction.
	classifyReadBytes = 16 * 1024
)

// isGeneratedFile returns true if the file appears to be machine-generated or
// minified. It checks Go stringer output (*_string.go), generated-code markers
// in the first few lines, and the minified-file line-length heuristic. It is
// the shared gate used by every source-walking collector.
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

	buf := make([]byte, classifyReadBytes)
	n, _ := io.ReadFull(f, buf)
	buf = buf[:n]
	return hasGeneratedHeader(buf) || isMinifiedContent(buf)
}

// hasGeneratedHeader reports whether any of the first generatedHeaderLines
// lines of content contains a generated-code marker.
func hasGeneratedHeader(content []byte) bool {
	rest := content
	for i := 0; i < generatedHeaderLines && len(rest) > 0; i++ {
		line, tail, _ := bytes.Cut(rest, []byte{'\n'})
		rest = tail
		for _, m := range generatedMarkers {
			if bytes.Contains(line, []byte(m)) {
				return true
			}
		}
	}
	return false
}

// isMinifiedContent reports whether the average line length over the first
// minifiedSampleLines lines of content exceeds minifiedAvgLineLen.
func isMinifiedContent(content []byte) bool {
	if len(content) == 0 {
		return false
	}
	var total, lines int
	rest := content
	for lines < minifiedSampleLines && len(rest) > 0 {
		line, tail, found := bytes.Cut(rest, []byte{'\n'})
		rest = tail
		if !found && len(line) == 0 {
			break
		}
		total += len(line)
		lines++
	}
	if lines == 0 {
		return false
	}
	return total/lines > minifiedAvgLineLen
}

// nonSourceDirSegments are directory names (matched case-insensitively at any
// depth) that hold documentation, tutorials, or demo code where tests are not
// expected. They extend defaultDemoPatterns for the patterns collector and
// are honoured unless IncludeDemoPaths is set.
var nonSourceDirSegments = map[string]bool{
	"docs_src": true,
	"docs":     true,
	"doc":      true,
	"examples": true,
	"example":  true,
	"extras":   true,
	"samples":  true,
	"sample":   true,
	"demos":    true,
	"demo":     true,
}

// configDirSegments are directory names that conventionally hold
// configuration. Files under them are only treated as configuration when
// their format says so (see configFileExtensions and isPHPConfigArray): Go,
// Java, Kotlin, C#, Rust and Python packages named config are still source.
var configDirSegments = map[string]bool{
	"config":   true,
	"configs":  true,
	"settings": true,
}

// configFileExtensions are configuration formats. A file with one of these
// extensions under a configDirSegments directory is configuration, not
// source. Dotfiles such as .env and .env.local are handled by the dotfile rule.
var configFileExtensions = map[string]bool{
	".yaml":       true,
	".yml":        true,
	".json":       true,
	".toml":       true,
	".ini":        true,
	".xml":        true,
	".properties": true,
	".cfg":        true,
	".conf":       true,
}

// configFileNames are file basenames that are configuration, not source.
var configFileNames = map[string]bool{
	"settings.py": true,
	"conf.py":     true,
	"setup.py":    true,
}

// phpConfigReadBytes bounds how much of a PHP file isPHPConfigArray reads
// while looking for a leading `return [` statement.
const (
	phpConfigReadBytes = 4 * 1024
	phpConfigMaxLines  = 20
)

// dataClassDirSegments are directory names that, in class-per-file languages,
// conventionally hold pure data carriers (events, DTOs, models, entities),
// interfaces/contracts, or exception classes — none of which warrant a
// dedicated test file.
var dataClassDirSegments = map[string]bool{
	"events":     true,
	"contracts":  true,
	"exceptions": true,
	"dto":        true,
	"dtos":       true,
	"models":     true,
	"entities":   true,
	"interfaces": true,
}

// dataClassExtensions are the class-per-file languages where the directory
// and class-name checks in isDataClassPath are reliable.
var dataClassExtensions = map[string]bool{
	".php":   true,
	".cs":    true,
	".java":  true,
	".kt":    true,
	".scala": true,
}

// dirSegments splits the directory part of relPath into its components.
func dirSegments(relPath string) []string {
	dir := filepath.ToSlash(filepath.Dir(relPath))
	if dir == "." || dir == "" {
		return nil
	}
	return strings.Split(dir, "/")
}

// isDocOrDemoTree returns true if relPath sits under a documentation, example,
// tutorial, or demo directory (docs_src/, docs/, examples/, extras/, samples/,
// tutorial*/, ...) at any depth, or under one of defaultDemoPatterns.
func isDocOrDemoTree(relPath string) bool {
	if isDemoPath(relPath) {
		return true
	}
	for _, seg := range dirSegments(relPath) {
		lower := strings.ToLower(seg)
		if nonSourceDirSegments[lower] || strings.HasPrefix(lower, "tutorial") {
			return true
		}
	}
	return false
}

// isConfigPath returns true if the file at path (with repo-relative relPath)
// is a configuration file: dotfiles (.eslintrc.js, .env.local), *.config.*
// files (webpack.config.js, jest.config.ts), well-known names such as
// settings.py, conf.py and setup.py, and, under a config/, configs/ or
// settings/ directory, configuration formats (yaml, json, toml, ini, xml,
// properties, cfg, conf) plus Laravel-style PHP files that only return an
// array. Source files in other languages under those directories are source.
func isConfigPath(path, relPath string) bool {
	base := filepath.Base(relPath)
	if strings.HasPrefix(base, ".") || configFileNames[base] {
		return true
	}
	ext := strings.ToLower(filepath.Ext(base))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if strings.HasSuffix(strings.ToLower(stem), ".config") {
		return true
	}
	if !isUnderConfigDir(relPath) {
		return false
	}
	if configFileExtensions[ext] {
		return true
	}
	return ext == ".php" && isPHPConfigArray(path)
}

// isUnderConfigDir returns true if any directory segment of relPath is one of
// configDirSegments.
func isUnderConfigDir(relPath string) bool {
	for _, seg := range dirSegments(relPath) {
		if configDirSegments[strings.ToLower(seg)] {
			return true
		}
	}
	return false
}

// isPHPConfigArray returns true if the PHP file at path opens with `<?php`
// and its first statement, ignoring blank lines, comments, declare() and use
// imports, is `return [` or `return array(` — the shape of a Laravel config
// file. At most the first phpConfigMaxLines lines are examined.
func isPHPConfigArray(path string) bool {
	f, err := FS.Open(path)
	if err != nil {
		return false
	}
	defer f.Close() //nolint:errcheck // read-only file

	buf := make([]byte, phpConfigReadBytes)
	n, _ := io.ReadFull(f, buf)
	rest := bytes.TrimLeft(buf[:n], "\xEF\xBB\xBF \t\r\n")
	if !bytes.HasPrefix(rest, []byte("<?php")) {
		return false
	}
	rest = rest[len("<?php"):]
	inComment := false
	for i := 0; i < phpConfigMaxLines && len(rest) > 0; i++ {
		var line []byte
		line, rest, _ = bytes.Cut(rest, []byte{'\n'})
		line = bytes.TrimSpace(line)
		switch {
		case inComment:
			if bytes.Contains(line, []byte("*/")) {
				inComment = false
			}
		case len(line) == 0,
			bytes.HasPrefix(line, []byte("//")),
			bytes.HasPrefix(line, []byte("#")),
			bytes.HasPrefix(line, []byte("use ")),
			bytes.HasPrefix(line, []byte("declare(")):
		case bytes.HasPrefix(line, []byte("/*")):
			inComment = !bytes.Contains(line, []byte("*/"))
		default:
			return bytes.HasPrefix(line, []byte("return [")) ||
				bytes.HasPrefix(line, []byte("return array("))
		}
	}
	return false
}

// isDataClassPath returns true for class-per-file languages (PHP, C#, Java,
// Kotlin, Scala) when relPath lives under an Events/, Contracts/, Exceptions/,
// Dto(s)/, Models/, Entities/ or Interfaces/ directory, or the class name ends
// in Exception, Dto, DTO or Interface. These are data carriers or declarations
// that do not warrant a dedicated test file.
func isDataClassPath(relPath string) bool {
	ext := filepath.Ext(relPath)
	if !dataClassExtensions[ext] {
		return false
	}
	for _, seg := range dirSegments(relPath) {
		if dataClassDirSegments[strings.ToLower(seg)] {
			return true
		}
	}
	stem := strings.TrimSuffix(filepath.Base(relPath), ext)
	for _, suf := range []string{"Exception", "Dto", "DTO", "Interface"} {
		if strings.HasSuffix(stem, suf) && len(stem) > len(suf) {
			return true
		}
	}
	return false
}

// isNonSourceForTests returns true if the file at path (repo-relative
// relPath) should be left out of missing-tests detection and the
// per-directory test-ratio metric: config files, data-only class files, and
// (unless includeDemo is set) documentation and demo trees.
func isNonSourceForTests(path, relPath string, includeDemo bool) bool {
	if isConfigPath(path, relPath) || isDataClassPath(relPath) {
		return true
	}
	return !includeDemo && isDocOrDemoTree(relPath)
}
