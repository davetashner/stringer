// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/gitcli"
	"github.com/davetashner/stringer/internal/signal"
)

// defaultLargeFileThreshold is the default large-file threshold in lines.
// Files exceeding this are flagged. Can be overridden via CollectorOpts.
const defaultLargeFileThreshold = 1500

// minSourceLinesForTestCheck is the minimum number of lines a source file must
// have before we report a missing-test signal. Very small files (stubs, config)
// are typically not worth flagging.
const minSourceLinesForTestCheck = 20

// minSourceFilesForRatio is the minimum number of source files a directory must
// contain before we report a low-test-ratio signal.
const minSourceFilesForRatio = 3

// lowTestRatioThreshold is the minimum test-to-source file ratio. Directories
// below this threshold are flagged.
const lowTestRatioThreshold = 0.1

// missingTestConfidence is the confidence score for missing-test signals.
const missingTestConfidence = 0.3

// lowTestRatioConfidence is the confidence score for low-test-ratio signals.
const lowTestRatioConfidence = 0.4

// sourceExtensions defines the file extensions we consider as "source code"
// for test-detection heuristics.
var sourceExtensions = map[string]bool{
	".go":    true,
	".js":    true,
	".ts":    true,
	".jsx":   true,
	".tsx":   true,
	".py":    true,
	".rb":    true,
	".java":  true,
	".cs":    true,
	".rs":    true,
	".cpp":   true,
	".c":     true,
	".h":     true,
	".hpp":   true,
	".swift": true,
	".kt":    true,
	".scala": true,
	".php":   true,
}

func init() {
	collector.Register(&PatternsCollector{})
}

// PatternsMetrics holds structured metrics from the patterns analysis.
type PatternsMetrics struct {
	LargeFiles          int
	DirectoryTestRatios []DirectoryTestRatio
}

// DirectoryTestRatio describes the test coverage ratio for a directory.
type DirectoryTestRatio struct {
	Path        string
	SourceFiles int
	TestFiles   int
	Ratio       float64
}

// PatternsCollector detects structural code-quality patterns such as
// oversized files, missing tests, and low test-to-source ratios.
type PatternsCollector struct {
	metrics *PatternsMetrics
}

// Name returns the collector name used for registration and filtering.
func (c *PatternsCollector) Name() string { return "patterns" }

// detectTestRoots finds parallel test directories at the repo root.
func detectTestRoots(repoPath string) []string {
	candidates := []string{"tests", "test", "spec", "__tests__", "benches"}
	var roots []string
	for _, dir := range candidates {
		info, err := FS.Stat(filepath.Join(repoPath, dir))
		if err == nil && info.IsDir() {
			roots = append(roots, dir)
		}
	}
	return roots
}

// Collect walks source files in repoPath, detects pattern-based signals, and
// returns them as raw signals.
func (c *PatternsCollector) Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	excludes := mergeExcludes(opts.ExcludePatterns)

	// Detect parallel test directories before the walk.
	testRoots := detectTestRoots(repoPath)

	// Determine large-file threshold (configurable via opts).
	threshold := defaultLargeFileThreshold
	if opts.LargeFileThreshold > 0 {
		threshold = opts.LargeFileThreshold
	}

	var signals []signal.RawSignal
	var fileCount int

	// Track per-directory file counts for test-ratio analysis.
	type dirStats struct {
		sourceFiles int
		testFiles   int
	}
	dirMap := make(map[string]*dirStats)

	err := FS.WalkDir(repoPath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		relPath, relErr := filepath.Rel(repoPath, path)
		if relErr != nil {
			return nil
		}

		// Skip directories that match exclude patterns early.
		if d.IsDir() {
			if shouldExclude(relPath, excludes) {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip excluded files.
		if shouldExclude(relPath, excludes) {
			return nil
		}

		// Skip symlinks that resolve outside the repo tree.
		if d.Type()&os.ModeSymlink != 0 {
			resolved, resolveErr := FS.EvalSymlinks(path)
			if resolveErr != nil {
				return nil
			}
			if !strings.HasPrefix(resolved, repoPath+string(filepath.Separator)) && resolved != repoPath {
				return nil
			}
		}

		// Apply include-pattern filtering if patterns are set.
		if len(opts.IncludePatterns) > 0 && !matchesAny(relPath, opts.IncludePatterns) {
			return nil
		}

		// Skip binary files.
		if isBinaryFile(path) {
			return nil
		}

		ext := filepath.Ext(path)
		if !sourceExtensions[ext] {
			return nil
		}

		// Count lines.
		lineCount, countErr := countLines(path)
		if countErr != nil {
			return nil // skip files we can't read
		}

		// C3.1: Large file detection.
		if lineCount > threshold && !isGeneratedFile(path) {
			confidence := largeFileConfidence(lineCount, threshold)
			signals = append(signals, signal.RawSignal{
				Source:      "patterns",
				Kind:        "large-file",
				FilePath:    relPath,
				Line:        0,
				Title:       fmt.Sprintf("Large file: %s (%d lines)", relPath, lineCount),
				Description: fmt.Sprintf("File exceeds %d-line threshold. Consider breaking it into smaller, focused modules.", threshold),
				Confidence:  confidence,
				Tags:        []string{"large-file"},
			})
		}

		// Track directory stats for test-ratio and missing-test analysis.
		dir := filepath.Dir(relPath)
		if dirMap[dir] == nil {
			dirMap[dir] = &dirStats{}
		}

		if isTestFile(relPath) {
			dirMap[dir].testFiles++
		} else {
			dirMap[dir].sourceFiles++

			// C3.2: Missing test detection — only for non-test source files
			// with meaningful size. Suppressed in demo/example paths, test root
			// dirs, and generated files by default.
			if lineCount >= minSourceLinesForTestCheck &&
				!isUnderTestRoot(relPath, testRoots) &&
				!isUnderMavenTestRoot(relPath) &&
				!isGeneratedFile(path) {
				if !hasTestCounterpart(path, relPath, repoPath, testRoots) {
					if opts.IncludeDemoPaths || !isDemoPath(relPath) {
						signals = append(signals, signal.RawSignal{
							Source:      "patterns",
							Kind:        "missing-tests",
							FilePath:    relPath,
							Line:        0,
							Title:       fmt.Sprintf("No test file found for %s", relPath),
							Description: "No corresponding test file was found using naming heuristics. Consider adding tests.",
							Confidence:  missingTestConfidence,
							Tags:        []string{"missing-tests"},
						})
					}
				}
			}
		}

		fileCount++
		if opts.ProgressFunc != nil && fileCount%500 == 0 {
			opts.ProgressFunc(fmt.Sprintf("patterns: scanned %d files", fileCount))
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking repo: %w", err)
	}

	// C3.3: Test-to-source ratio per directory.
	// Also build metrics from ALL directories (not just below-threshold).
	largeFileCount := 0
	for _, sig := range signals {
		if sig.Kind == "large-file" {
			largeFileCount++
		}
	}

	var dirRatios []DirectoryTestRatio
	for dir, stats := range dirMap {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Build metrics for every directory with source files.
		if stats.sourceFiles > 0 {
			ratio := float64(stats.testFiles) / float64(stats.sourceFiles)
			dirRatios = append(dirRatios, DirectoryTestRatio{
				Path:        dir,
				SourceFiles: stats.sourceFiles,
				TestFiles:   stats.testFiles,
				Ratio:       ratio,
			})
		}

		if stats.sourceFiles < minSourceFilesForRatio {
			continue
		}

		// Suppress low-test-ratio in demo/example paths by default.
		if !opts.IncludeDemoPaths && isDemoPath(dir) {
			continue
		}

		ratio := float64(stats.testFiles) / float64(stats.sourceFiles)
		if ratio < lowTestRatioThreshold {
			signals = append(signals, signal.RawSignal{
				Source:      "patterns",
				Kind:        "low-test-ratio",
				FilePath:    dir,
				Line:        0,
				Title:       fmt.Sprintf("Low test ratio in %s: %d test files / %d source files", dir, stats.testFiles, stats.sourceFiles),
				Description: fmt.Sprintf("Test-to-source ratio is %.1f%%, below the %.0f%% threshold. Consider adding more tests.", ratio*100, lowTestRatioThreshold*100),
				Confidence:  lowTestRatioConfidence,
				Tags:        []string{"low-test-ratio"},
			})
		}
	}

	sort.Slice(dirRatios, func(i, j int) bool {
		return dirRatios[i].Path < dirRatios[j].Path
	})

	c.metrics = &PatternsMetrics{
		LargeFiles:          largeFileCount,
		DirectoryTestRatios: dirRatios,
	}

	// Enrich signals with timestamps from git log.
	gitRoot := opts.GitRoot
	if gitRoot == "" {
		gitRoot = repoPath
	}
	enrichTimestamps(ctx, gitRoot, signals)

	return signals, nil
}

// countLines counts the number of lines in a file using bufio.Scanner.
func countLines(path string) (int, error) {
	f, err := FS.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close() //nolint:errcheck // read-only file

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, err
	}
	return count, nil
}

// largeFileConfidence scales confidence from 0.4 (just over threshold) to 0.8
// (at 2x threshold or more).
func largeFileConfidence(lineCount, threshold int) float64 {
	// Linear interpolation from threshold..2*threshold → 0.4..0.8
	ratio := float64(lineCount) / float64(threshold)
	// ratio > 1.0 (since lineCount > threshold)
	// At ratio=1.0 → 0.4, at ratio>=2.0 → 0.8
	confidence := 0.4 + 0.4*(ratio-1.0)
	return math.Min(confidence, 0.8)
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
		// Check if file is under tests/ directory
		dir := filepath.Dir(relPath)
		parts := strings.Split(filepath.ToSlash(dir), "/")
		for _, p := range parts {
			if p == "tests" {
				return true
			}
		}
	}
	return false
}

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

// Metrics returns structured metrics from the patterns analysis.
func (c *PatternsCollector) Metrics() any { return c.metrics }

// enrichTimestamps sets the Timestamp field on signals that have a zero
// timestamp by querying git for the most recent commit touching that path.
// Errors are logged and silently skipped.
func enrichTimestamps(ctx context.Context, gitRoot string, signals []signal.RawSignal) {
	for i := range signals {
		if !signals[i].Timestamp.IsZero() {
			continue
		}
		t, err := gitcli.LastCommitTime(ctx, gitRoot, signals[i].FilePath)
		if err != nil {
			slog.Debug("enrichTimestamps: git log failed", "path", signals[i].FilePath, "error", err)
			continue
		}
		signals[i].Timestamp = t
	}
}

// Compile-time interface checks.
var _ collector.Collector = (*PatternsCollector)(nil)
var _ collector.MetricsProvider = (*PatternsCollector)(nil)
