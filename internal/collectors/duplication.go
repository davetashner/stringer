// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

// maxDuplicationFiles is the file cap to prevent runaway on large repos.
const maxDuplicationFiles = 10000

func init() {
	collector.Register(&DuplicationCollector{})
}

// DuplicationMetrics holds structured metrics from the duplication scan.
type DuplicationMetrics struct {
	FilesScanned    int
	ExactClones     int
	NearClones      int
	DuplicatedLines int
}

// DuplicationCollector detects copy-paste code duplication using a token-based
// sliding window approach with FNV-64a hashing. It finds both exact duplicates
// (Type 1) and near-clones with renamed identifiers (Type 2).
type DuplicationCollector struct {
	metrics *DuplicationMetrics
}

// Name returns the collector name used for registration and filtering.
func (c *DuplicationCollector) Name() string { return "duplication" }

// Collect walks source files in repoPath, detects duplicated code blocks,
// and returns them as raw signals.
func (c *DuplicationCollector) Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	excludes := mergeExcludes(opts.ExcludePatterns)

	// Phase 1: Walk files and read source lines.
	type fileData struct {
		relPath string
		lines   []string
	}
	var files []fileData
	var fileCount int

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

		ext := filepath.Ext(path)
		if !sourceExtensions[ext] {
			return nil
		}

		if isBinaryFile(path) {
			return nil
		}

		if isGeneratedFile(path) {
			return nil
		}

		// Enforce file cap.
		if fileCount >= maxDuplicationFiles {
			if opts.ProgressFunc != nil {
				opts.ProgressFunc(fmt.Sprintf("duplication: file cap reached (%d files), skipping remaining", maxDuplicationFiles))
			}
			return filepath.SkipAll
		}

		lines, readErr := readFileLines(path)
		if readErr != nil {
			return nil
		}

		files = append(files, fileData{relPath: relPath, lines: lines})
		fileCount++

		if opts.ProgressFunc != nil && fileCount%500 == 0 {
			opts.ProgressFunc(fmt.Sprintf("duplication: scanned %d files", fileCount))
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking repo: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Phase 2: Build Type 1 (exact) hash windows.
	var type1Entries []windowEntry
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		normalized := normalizeType1(f.lines)
		type1Entries = append(type1Entries, buildWindowHashes(normalized, f.relPath)...)
	}

	type1Groups := groupClones(type1Entries)

	// Phase 3: Build Type 2 (near-clone) hash windows.
	var type2Entries []windowEntry
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		normalized := normalizeType2(f.lines)
		type2Entries = append(type2Entries, buildWindowHashes(normalized, f.relPath)...)
	}

	type2Groups := groupClones(type2Entries)
	for i := range type2Groups {
		type2Groups[i].NearClone = true
	}

	// Phase 4: Subtract Type 1 ranges from Type 2 results.
	type2Groups = subtractType1Ranges(type2Groups, type1Groups)

	// Phase 5: Generate signals.
	var signals []signal.RawSignal
	exactCount := 0
	nearCount := 0
	dupLines := 0

	for _, g := range type1Groups {
		sig := cloneGroupToSignal(g)
		if opts.MinConfidence > 0 && sig.Confidence < opts.MinConfidence {
			continue
		}
		signals = append(signals, sig)
		exactCount++
		dupLines += g.Lines * len(g.Locations)
	}

	for _, g := range type2Groups {
		sig := cloneGroupToSignal(g)
		if opts.MinConfidence > 0 && sig.Confidence < opts.MinConfidence {
			continue
		}
		signals = append(signals, sig)
		nearCount++
		dupLines += g.Lines * len(g.Locations)
	}

	// Sort signals by confidence descending.
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].Confidence > signals[j].Confidence
	})

	// Cap output to prevent overwhelming results on large repos.
	maxDupSignals := 200
	if opts.MaxIssues > 0 {
		maxDupSignals = opts.MaxIssues
	}
	if len(signals) > maxDupSignals {
		signals = signals[:maxDupSignals]
	}

	c.metrics = &DuplicationMetrics{
		FilesScanned:    fileCount,
		ExactClones:     exactCount,
		NearClones:      nearCount,
		DuplicatedLines: dupLines,
	}

	// Enrich signals with timestamps from git log.
	gitRoot := opts.GitRoot
	if gitRoot == "" {
		gitRoot = repoPath
	}
	enrichTimestamps(ctx, gitRoot, signals)

	return signals, nil
}

// cloneGroupToSignal converts a clone group into a RawSignal.
func cloneGroupToSignal(g cloneGroup) signal.RawSignal {
	kind := "code-clone"
	tags := []string{"code-clone", "duplication"}
	titleVerb := "Duplicated"

	if g.NearClone {
		kind = "near-clone"
		tags = []string{"near-clone", "duplication"}
		titleVerb = "Near-duplicate"
	}

	title := fmt.Sprintf("%s block (%d lines, %d locations)", titleVerb, g.Lines, len(g.Locations))
	if g.NearClone {
		title = fmt.Sprintf("%s block (%d lines, %d locations, renamed identifiers)", titleVerb, g.Lines, len(g.Locations))
	}

	// Build description listing all locations.
	var desc strings.Builder
	desc.WriteString("Duplicated code found in:\n")
	for _, loc := range g.Locations {
		fmt.Fprintf(&desc, "  - %s:%d\n", loc.Path, loc.StartLine)
	}

	confidence := duplicationConfidence(g.Lines, len(g.Locations), g.NearClone)

	return signal.RawSignal{
		Source:      "duplication",
		Kind:        kind,
		FilePath:    g.Locations[0].Path,
		Line:        g.Locations[0].StartLine,
		Title:       title,
		Description: desc.String(),
		Confidence:  confidence,
		Tags:        tags,
	}
}

// duplicationConfidence computes confidence per DR-017:
//
//	50+ lines → 0.75 base
//	30–49 lines → 0.60–0.75 (linear)
//	15–29 lines → 0.45–0.60 (linear)
//	6–14 lines → 0.35–0.45 (linear)
//	3+ locations: +0.05, 4+ locations: +0.10
//	near-clone: −0.05
//	cap: 0.80
func duplicationConfidence(lines, locations int, nearClone bool) float64 {
	var base float64
	switch {
	case lines >= 50:
		base = 0.75
	case lines >= 30:
		// Linear from 0.60 at 30 to 0.75 at 49.
		base = 0.60 + 0.15*float64(lines-30)/float64(49-30)
	case lines >= 15:
		// Linear from 0.45 at 15 to 0.60 at 29.
		base = 0.45 + 0.15*float64(lines-15)/float64(29-15)
	default:
		// Linear from 0.35 at 6 to 0.45 at 14.
		base = 0.35 + 0.10*float64(lines-6)/float64(14-6)
	}

	// Location bonus.
	if locations >= 4 {
		base += 0.10
	} else if locations >= 3 {
		base += 0.05
	}

	// Near-clone penalty.
	if nearClone {
		base -= 0.05
	}

	return math.Min(base, 0.80)
}

// readFileLines reads all lines from a file.
func readFileLines(path string) ([]string, error) {
	f, err := FS.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only file

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// Metrics returns structured metrics from the duplication scan.
func (c *DuplicationCollector) Metrics() any { return c.metrics }

// Compile-time interface checks.
var _ collector.Collector = (*DuplicationCollector)(nil)
var _ collector.MetricsProvider = (*DuplicationCollector)(nil)
