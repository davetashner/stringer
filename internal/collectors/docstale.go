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
	"github.com/davetashner/stringer/internal/gitcli"
	"github.com/davetashner/stringer/internal/signal"
)

// defaultStalenessThresholdDays is the minimum age gap (in days) between
// source and doc last-commit to flag a stale-doc signal.
const defaultStalenessThresholdDays = 180

// defaultDriftMinCommits is the minimum source commits required before
// flagging doc-code-drift (source had commits but doc had zero).
const defaultDriftMinCommits = 10

func init() {
	collector.Register(&DocStaleCollector{})
}

// DocStaleMetrics holds structured metrics from the doc staleness scan.
type DocStaleMetrics struct {
	DocsScanned  int
	StaleDocs    int
	DriftSignals int
	BrokenLinks  int
}

// DocStaleCollector detects stale documentation: docs that haven't been
// updated when their associated source code changed, and broken internal
// links within markdown files.
type DocStaleCollector struct {
	metrics *DocStaleMetrics
}

// Name returns the collector name used for registration and filtering.
func (c *DocStaleCollector) Name() string { return "docstale" }

// docExtensions are file extensions considered documentation.
var docExtensions = map[string]bool{
	".md":  true,
	".rst": true,
	".txt": true,
}

// rootDocPrefixes are filename prefixes that are considered root-level docs.
var rootDocPrefixes = []string{
	"README",
	"CONTRIBUTING",
	"CHANGELOG",
	"CHANGES",
	"HISTORY",
}

// docDirs are directory names that contain documentation.
var docDirs = []string{"docs", "doc"}

// mdLinkPattern matches markdown links: [text](target)
var mdLinkPattern = regexp.MustCompile(`\[(?:[^\]]*)\]\(([^)]+)\)`)

// Collect walks the repository looking for stale documentation, co-change
// drift, and broken internal links.
func (c *DocStaleCollector) Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	excludes := mergeExcludes(opts.ExcludePatterns)
	metrics := &DocStaleMetrics{}

	gitRoot := opts.GitRoot
	if gitRoot == "" {
		gitRoot = repoPath
	}

	// Phase 1: Discover doc files and collect signals for stale-doc and broken-doc-link.
	var docFiles []string
	var signals []signal.RawSignal

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

		if len(opts.IncludePatterns) > 0 && !matchesAny(relPath, opts.IncludePatterns) {
			return nil
		}

		if !isDocFile(relPath) {
			return nil
		}

		metrics.DocsScanned++
		docFiles = append(docFiles, relPath)

		// Signal 3: broken internal links (markdown only).
		if strings.HasSuffix(strings.ToLower(relPath), ".md") {
			broken := findBrokenLinks(repoPath, relPath)
			for _, bl := range broken {
				conf := 0.6
				if conf >= opts.MinConfidence {
					signals = append(signals, signal.RawSignal{
						Source:     "docstale",
						Kind:       "broken-doc-link",
						FilePath:   relPath,
						Line:       bl.line,
						Title:      fmt.Sprintf("Broken link in %s:%d → %s", relPath, bl.line, bl.target),
						Confidence: conf,
						Tags:       []string{"documentation", "broken-link"},
					})
					metrics.BrokenLinks++
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking repo for docs: %w", err)
	}

	// Signal 1: stale-doc — compare doc age vs associated source age.
	for _, docRel := range docFiles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		sourceDir := inferSourceDir(repoPath, docRel)
		if sourceDir == "" {
			continue
		}

		docTime, docErr := gitcli.LastCommitTime(ctx, gitRoot, docRel)
		if docErr != nil || docTime.IsZero() {
			continue
		}

		srcTime, srcErr := gitcli.LastCommitTime(ctx, gitRoot, sourceDir)
		if srcErr != nil || srcTime.IsZero() {
			continue
		}

		driftDays := int(srcTime.Sub(docTime).Hours() / 24)
		if driftDays >= defaultStalenessThresholdDays {
			conf := staleConfidence(driftDays)
			if conf >= opts.MinConfidence {
				signals = append(signals, signal.RawSignal{
					Source:     "docstale",
					Kind:       "stale-doc",
					FilePath:   docRel,
					Title:      fmt.Sprintf("Doc %s is %d days behind source %s", docRel, driftDays, sourceDir),
					Confidence: conf,
					Timestamp:  docTime,
					Tags:       []string{"documentation", "stale"},
				})
				metrics.StaleDocs++
			}
		}
	}

	// Signal 2: doc-code-drift — co-change ratio analysis.
	since := opts.GitSince
	if since == "" {
		since = "1y"
	}
	depth := opts.GitDepth
	if depth == 0 {
		depth = 1000
	}

	commits, logErr := gitcli.LogNumstat(ctx, gitRoot, depth, since)
	if logErr == nil && len(commits) > 0 {
		driftSignals := detectDocCodeDrift(commits, docFiles, repoPath, opts.MinConfidence)
		signals = append(signals, driftSignals...)
		metrics.DriftSignals += len(driftSignals)
	}

	c.metrics = metrics

	// Enrich timestamps from git log.
	enrichTimestamps(ctx, gitRoot, signals)

	return signals, nil
}

// isDocFile returns true if the relative path is a documentation file.
func isDocFile(relPath string) bool {
	ext := strings.ToLower(filepath.Ext(relPath))
	if !docExtensions[ext] {
		return false
	}

	// Root-level doc files.
	dir := filepath.Dir(relPath)
	base := filepath.Base(relPath)
	if dir == "." {
		for _, prefix := range rootDocPrefixes {
			if strings.HasPrefix(strings.ToUpper(base), prefix) {
				return true
			}
		}
	}

	// Files in doc directories.
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	for _, part := range parts {
		for _, dd := range docDirs {
			if strings.EqualFold(part, dd) {
				return true
			}
		}
	}

	return false
}

// inferSourceDir maps a doc file to its most likely associated source directory.
// Returns empty string if no association can be inferred.
func inferSourceDir(repoPath, docRel string) string {
	dir := filepath.Dir(docRel)
	base := filepath.Base(docRel)
	nameNoExt := strings.TrimSuffix(base, filepath.Ext(base))

	// Root docs → compare against whole repo.
	if dir == "." {
		return "."
	}

	// docs/auth.md → look for internal/auth/, src/auth/, auth/, pkg/auth/.
	candidatePrefixes := []string{"internal/", "src/", "pkg/", ""}
	for _, prefix := range candidatePrefixes {
		candidate := prefix + nameNoExt
		fullPath := filepath.Join(repoPath, candidate)
		info, err := FS.Stat(fullPath)
		if err == nil && info.IsDir() {
			return candidate
		}
	}

	return ""
}

// staleConfidence returns a confidence score based on the age gap in days.
func staleConfidence(driftDays int) float64 {
	switch {
	case driftDays >= 730: // 2+ years
		return 0.7
	case driftDays >= 365: // 1+ year
		return 0.5
	default: // 6mo+
		return 0.3
	}
}

// brokenLink describes a broken internal link in a markdown file.
type brokenLink struct {
	target string
	line   int
}

// findBrokenLinks scans a markdown file for internal links that point to
// non-existent files.
func findBrokenLinks(repoPath, relPath string) []brokenLink {
	absPath := filepath.Join(repoPath, relPath)
	f, err := FS.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck // read-only file

	docDir := filepath.Dir(absPath)
	var broken []brokenLink

	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()

		matches := mdLinkPattern.FindAllStringSubmatch(line, -1)
		for _, m := range matches {
			target := m[1]

			// Skip external URLs.
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
				continue
			}

			// Skip mailto links.
			if strings.HasPrefix(target, "mailto:") {
				continue
			}

			// Strip anchor fragments.
			if idx := strings.Index(target, "#"); idx >= 0 {
				target = target[:idx]
			}

			// Skip pure anchor links.
			if target == "" {
				continue
			}

			// Resolve relative to the markdown file's directory.
			resolved := filepath.Join(docDir, target)
			if _, statErr := FS.Stat(resolved); statErr != nil {
				broken = append(broken, brokenLink{target: target, line: lineNo})
			}
		}
	}

	return broken
}

// detectDocCodeDrift analyzes commit history to find source dirs with many
// commits but zero associated doc updates.
func detectDocCodeDrift(commits []gitcli.NumstatCommit, docFiles []string, repoPath string, minConfidence float64) []signal.RawSignal {
	// Build doc→source and source→doc mappings.
	type dirPair struct {
		docFile   string
		sourceDir string
	}

	var pairs []dirPair
	for _, docRel := range docFiles {
		sourceDir := inferSourceDir(repoPath, docRel)
		if sourceDir != "" {
			pairs = append(pairs, dirPair{docFile: docRel, sourceDir: sourceDir})
		}
	}

	if len(pairs) == 0 {
		return nil
	}

	// Count commits touching each source dir and each doc file.
	type counts struct {
		sourceCommits int
		docCommits    int
	}
	pairCounts := make(map[int]*counts, len(pairs))
	for i := range pairs {
		pairCounts[i] = &counts{}
	}

	for _, commit := range commits {
		for _, file := range commit.Files {
			fileSlash := filepath.ToSlash(file)
			for i, pair := range pairs {
				srcPrefix := filepath.ToSlash(pair.sourceDir)
				if srcPrefix == "." || strings.HasPrefix(fileSlash, srcPrefix+"/") || fileSlash == srcPrefix {
					pairCounts[i].sourceCommits++
				}
				if fileSlash == filepath.ToSlash(pair.docFile) {
					pairCounts[i].docCommits++
				}
			}
		}
	}

	var signals []signal.RawSignal
	conf := 0.3
	if conf < minConfidence {
		return nil
	}

	for i, pair := range pairs {
		c := pairCounts[i]
		if c.sourceCommits >= defaultDriftMinCommits && c.docCommits == 0 {
			signals = append(signals, signal.RawSignal{
				Source:     "docstale",
				Kind:       "doc-code-drift",
				FilePath:   pair.docFile,
				Title:      fmt.Sprintf("Doc %s has 0 updates while %s had %d commits", pair.docFile, pair.sourceDir, c.sourceCommits),
				Confidence: conf,
				Tags:       []string{"documentation", "drift"},
			})
		}
	}

	return signals
}

// Metrics returns structured metrics from the doc staleness scan.
func (c *DocStaleCollector) Metrics() any { return c.metrics }

// Compile-time interface checks.
var _ collector.Collector = (*DocStaleCollector)(nil)
var _ collector.MetricsProvider = (*DocStaleCollector)(nil)
