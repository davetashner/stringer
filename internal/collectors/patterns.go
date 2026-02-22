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
	".ex":    true,
	".exs":   true,
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

		// Build metrics for every directory with source files,
		// excluding non-source/demo directories.
		if stats.sourceFiles > 0 && (opts.IncludeDemoPaths || !isDemoPath(dir)) {
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
