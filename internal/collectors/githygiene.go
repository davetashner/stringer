// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

// defaultLargeBinaryThreshold is the minimum file size in bytes to flag a
// binary file as large. Default 1 MB.
const defaultLargeBinaryThreshold = 1_000_000

func init() {
	collector.Register(&GitHygieneCollector{})
}

// GitHygieneMetrics holds structured metrics from the git hygiene scan.
type GitHygieneMetrics struct {
	FilesScanned         int
	LargeBinaries        int
	MergeConflictMarkers int
	CommittedSecrets     int
	MixedLineEndings     int
}

// GitHygieneCollector detects repository-level hygiene problems:
// large committed binaries, merge conflict markers, committed secrets,
// and mixed line endings.
type GitHygieneCollector struct {
	metrics *GitHygieneMetrics
}

// Name returns the collector name used for registration and filtering.
func (c *GitHygieneCollector) Name() string { return "githygiene" }

// mergeConflictPattern matches git merge conflict markers.
var mergeConflictPattern = regexp.MustCompile(`^(<{7}|={7}|>{7})\s`)

// secretPatterns maps pattern names to their regex and confidence scores.
var secretPatterns = []struct {
	name       string
	pattern    *regexp.Regexp
	confidence float64
}{
	{
		name:       "AWS access key",
		pattern:    regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		confidence: 0.7,
	},
	{
		name:       "GitHub token",
		pattern:    regexp.MustCompile(`gh[ps]_[A-Za-z0-9_]{36,}`),
		confidence: 0.7,
	},
	{
		name:       "generic secret",
		pattern:    regexp.MustCompile(`(?i)(api[_-]?key|secret[_-]?key|password)\s*[:=]\s*["'][^"']{8,}`),
		confidence: 0.6,
	},
}

// Collect walks the repository, performing four hygiene checks per file
// in a single pass: large binaries, merge conflict markers, committed
// secrets, and mixed line endings.
func (c *GitHygieneCollector) Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	excludes := mergeExcludes(opts.ExcludePatterns)

	// Parse .gitattributes for LFS-tracked patterns.
	lfsPatterns := parseLFSPatterns(repoPath)

	var signals []signal.RawSignal
	metrics := &GitHygieneMetrics{}

	err := FS.WalkDir(repoPath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		relPath, relErr := filepath.Rel(repoPath, path)
		if relErr != nil {
			return nil
		}

		if d.IsDir() {
			if shouldExclude(relPath, excludes) {
				return filepath.SkipDir
			}
			return nil
		}

		if shouldExclude(relPath, excludes) {
			return nil
		}

		// Skip symlinks outside repo tree.
		if d.Type()&os.ModeSymlink != 0 {
			resolved, resolveErr := FS.EvalSymlinks(path)
			if resolveErr != nil {
				return nil
			}
			if !strings.HasPrefix(resolved, repoPath+string(filepath.Separator)) && resolved != repoPath {
				return nil
			}
		}

		if len(opts.IncludePatterns) > 0 && !matchesAny(relPath, opts.IncludePatterns) {
			return nil
		}

		metrics.FilesScanned++

		binary := isBinaryFile(path)

		// Check 1: Large binary detection.
		if binary {
			if !isLFSTracked(relPath, lfsPatterns) {
				info, statErr := FS.Stat(path)
				if statErr == nil && info.Size() >= defaultLargeBinaryThreshold {
					conf := 0.8
					if conf >= opts.MinConfidence {
						signals = append(signals, signal.RawSignal{
							Source:     "githygiene",
							Kind:       "large-binary",
							FilePath:   relPath,
							Title:      fmt.Sprintf("Large binary file: %s (%s)", relPath, humanSize(info.Size())),
							Confidence: conf,
							Tags:       []string{"git-hygiene", "large-binary"},
						})
						metrics.LargeBinaries++
					}
				}
			}
			// Skip text checks for binary files.
			return nil
		}

		if isGeneratedFile(path) {
			return nil
		}

		// Read file content for text-based checks.
		fileSignals := scanTextFileHygiene(path, relPath, opts.MinConfidence)
		for i := range fileSignals {
			switch fileSignals[i].Kind {
			case "merge-conflict-marker":
				metrics.MergeConflictMarkers++
			case "committed-secret":
				metrics.CommittedSecrets++
			case "mixed-line-endings":
				metrics.MixedLineEndings++
			}
		}
		signals = append(signals, fileSignals...)

		if opts.ProgressFunc != nil && metrics.FilesScanned%500 == 0 {
			opts.ProgressFunc(fmt.Sprintf("githygiene: scanned %d files", metrics.FilesScanned))
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking repo: %w", err)
	}

	c.metrics = metrics

	// Enrich signals with timestamps from git log.
	gitRoot := opts.GitRoot
	if gitRoot == "" {
		gitRoot = repoPath
	}
	enrichTimestamps(ctx, gitRoot, signals)

	return signals, nil
}

// scanTextFileHygiene reads a text file and checks for merge conflict
// markers, committed secrets, and mixed line endings in a single pass.
func scanTextFileHygiene(path, relPath string, minConfidence float64) []signal.RawSignal {
	f, err := FS.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck // read-only file

	// Read raw bytes to detect line endings before scanner normalizes them.
	var rawBytes []byte
	buf := make([]byte, 32*1024)
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			rawBytes = append(rawBytes, buf[:n]...)
		}
		if readErr != nil {
			break
		}
		// Cap at 1 MB to avoid excessive memory usage.
		if len(rawBytes) > 1024*1024 {
			break
		}
	}

	// Count line endings from raw bytes.
	crlfCount := strings.Count(string(rawBytes), "\r\n")
	// Total \n minus CRLF gives bare LF count.
	totalLF := strings.Count(string(rawBytes), "\n")
	lfCount := totalLF - crlfCount

	var signals []signal.RawSignal

	// Split into lines for pattern matching.
	lines := strings.Split(string(rawBytes), "\n")
	conflictReported := false

	for lineNo, rawLine := range lines {
		// Trim trailing \r from CRLF lines for consistent matching.
		line := strings.TrimRight(rawLine, "\r")

		// Check for merge conflict markers (report only first occurrence).
		if !conflictReported && mergeConflictPattern.MatchString(line) {
			conf := 0.9
			if conf >= minConfidence {
				signals = append(signals, signal.RawSignal{
					Source:     "githygiene",
					Kind:       "merge-conflict-marker",
					FilePath:   relPath,
					Line:       lineNo + 1,
					Title:      fmt.Sprintf("Merge conflict marker in %s:%d", relPath, lineNo+1),
					Confidence: conf,
					Tags:       []string{"git-hygiene", "merge-conflict"},
				})
				conflictReported = true
			}
		}

		// Check for committed secrets.
		for _, sp := range secretPatterns {
			if sp.confidence < minConfidence {
				continue
			}
			if sp.pattern.MatchString(line) {
				signals = append(signals, signal.RawSignal{
					Source:     "githygiene",
					Kind:       "committed-secret",
					FilePath:   relPath,
					Line:       lineNo + 1,
					Title:      fmt.Sprintf("Possible %s in %s:%d", sp.name, relPath, lineNo+1),
					Confidence: sp.confidence,
					Tags:       []string{"git-hygiene", "security", "secret"},
				})
				break // one secret signal per line
			}
		}
	}

	// Check for mixed line endings (need both types present, with at least
	// 2 lines of each to avoid false positives from single-line anomalies).
	if crlfCount >= 2 && lfCount >= 2 {
		conf := 0.7
		if conf >= minConfidence {
			signals = append(signals, signal.RawSignal{
				Source:     "githygiene",
				Kind:       "mixed-line-endings",
				FilePath:   relPath,
				Title:      fmt.Sprintf("Mixed line endings in %s (%d CRLF, %d LF)", relPath, crlfCount, lfCount),
				Confidence: conf,
				Tags:       []string{"git-hygiene", "line-endings"},
			})
		}
	}

	return signals
}

// parseLFSPatterns reads .gitattributes and returns glob patterns for
// files tracked by Git LFS.
func parseLFSPatterns(repoPath string) []string {
	path := filepath.Join(repoPath, ".gitattributes")
	f, err := FS.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck // read-only file

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "filter=lfs") {
			// Pattern is the first field before any whitespace.
			fields := strings.Fields(line)
			if len(fields) > 0 {
				patterns = append(patterns, fields[0])
			}
		}
	}
	return patterns
}

// isLFSTracked returns true if the given relative path matches any of the
// LFS glob patterns from .gitattributes.
func isLFSTracked(relPath string, lfsPatterns []string) bool {
	for _, pattern := range lfsPatterns {
		matched, err := filepath.Match(pattern, filepath.Base(relPath))
		if err == nil && matched {
			return true
		}
		// Also try full path match.
		matched, err = filepath.Match(pattern, relPath)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// humanSize formats a byte count as a human-readable string.
func humanSize(bytes int64) string {
	switch {
	case bytes >= 1_000_000_000:
		return fmt.Sprintf("%.1f GB", float64(bytes)/1_000_000_000)
	case bytes >= 1_000_000:
		return fmt.Sprintf("%.1f MB", float64(bytes)/1_000_000)
	case bytes >= 1_000:
		return fmt.Sprintf("%.1f KB", float64(bytes)/1_000)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// Metrics returns structured metrics from the git hygiene scan.
func (c *GitHygieneCollector) Metrics() any { return c.metrics }

// Compile-time interface checks.
var _ collector.Collector = (*GitHygieneCollector)(nil)
var _ collector.MetricsProvider = (*GitHygieneCollector)(nil)
