// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

// couplingFileCountCap is the maximum number of source files to analyze.
const couplingFileCountCap = 10_000

func init() {
	collector.Register(&CouplingCollector{})
}

// CouplingMetrics holds structured metrics from the coupling scan.
type CouplingMetrics struct {
	FilesScanned       int
	ModulesFound       int
	CircularDeps       int
	HighCouplingCount  int
	SkippedCapExceeded bool
}

// CouplingCollector detects circular dependencies and high-coupling modules
// by building an import graph from source files and running Tarjan's SCC
// algorithm for cycle detection.
type CouplingCollector struct {
	metrics *CouplingMetrics
}

var _ collector.Collector = (*CouplingCollector)(nil)
var _ collector.MetricsProvider = (*CouplingCollector)(nil)

// Name returns the collector name used for registration and filtering.
func (c *CouplingCollector) Name() string { return "coupling" }

// Metrics returns the structured metrics from the last scan.
func (c *CouplingCollector) Metrics() any { return c.metrics }

// Collect walks source files in repoPath, builds an import graph, detects
// circular dependencies and high-coupling modules, and returns them as signals.
func (c *CouplingCollector) Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	excludes := mergeExcludes(opts.ExcludePatterns)

	// Phase 1: Walk files and assign modules.
	type fileInfo struct {
		relPath string
		ext     string
		module  string
	}
	var files []fileInfo
	moduleSet := make(map[string]bool) // all discovered modules
	var fileCount int
	capExceeded := false

	// Read Go module path for intra-project import filtering.
	goModulePath := readGoModulePath(repoPath)

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

		// Only process files we have an import extractor for.
		if _, ok := importExtractors[ext]; !ok {
			return nil
		}

		if isBinaryFile(path) {
			return nil
		}

		if isGeneratedFile(path) {
			return nil
		}

		fileCount++
		if fileCount > couplingFileCountCap {
			return fmt.Errorf("file count exceeds cap (%d)", couplingFileCountCap)
		}

		mod := moduleForFile(relPath, ext)
		files = append(files, fileInfo{relPath: relPath, ext: ext, module: mod})
		moduleSet[mod] = true

		if opts.ProgressFunc != nil && fileCount%500 == 0 {
			opts.ProgressFunc(fmt.Sprintf("coupling: scanned %d files", fileCount))
		}

		return nil
	})

	if err != nil {
		if strings.Contains(err.Error(), "file count exceeds cap") {
			capExceeded = true
			if opts.ProgressFunc != nil {
				opts.ProgressFunc(fmt.Sprintf("coupling: file cap reached (%d files)", couplingFileCountCap))
			}
		} else {
			return nil, fmt.Errorf("walking repo: %w", err)
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Phase 2: Extract imports and build graph.
	graph := make(importGraph)

	// Ensure all modules have an entry in the graph.
	for mod := range moduleSet {
		if _, ok := graph[mod]; !ok {
			graph[mod] = nil
		}
	}

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		lines, readErr := readFileLines(filepath.Join(repoPath, f.relPath))
		if readErr != nil {
			continue
		}

		extractor, ok := importExtractors[f.ext]
		if !ok {
			continue
		}

		imports := extractor(lines, f.relPath, goModulePath, moduleSet)
		for _, imp := range imports {
			if imp != f.module { // skip self-imports
				graph[f.module] = append(graph[f.module], imp)
			}
		}
	}

	// Phase 3: Detect cycles via Tarjan's SCC.
	sccs := tarjanSCC(graph)

	// Phase 4: Compute fan-out.
	highFanOut := fanOutModules(graph, defaultFanOutThreshold)

	// Phase 5: Generate signals.
	var signals []signal.RawSignal

	for _, scc := range sccs {
		sort.Strings(scc)
		sig := buildCycleSignal(scc, opts.MinConfidence)
		if sig != nil {
			signals = append(signals, *sig)
		}
	}

	// Sort high-fan-out modules for deterministic output.
	fanOutMods := make([]string, 0, len(highFanOut))
	for mod := range highFanOut {
		fanOutMods = append(fanOutMods, mod)
	}
	sort.Strings(fanOutMods)

	for _, mod := range fanOutMods {
		count := highFanOut[mod]
		sig := buildFanOutSignal(mod, count, opts.MinConfidence)
		if sig != nil {
			signals = append(signals, *sig)
		}
	}

	// Set metrics.
	c.metrics = &CouplingMetrics{
		FilesScanned:       fileCount,
		ModulesFound:       len(moduleSet),
		CircularDeps:       len(sccs),
		HighCouplingCount:  len(highFanOut),
		SkippedCapExceeded: capExceeded,
	}

	// Enrich timestamps from git log.
	gitRoot := opts.GitRoot
	if gitRoot == "" {
		gitRoot = repoPath
	}
	enrichTimestamps(ctx, gitRoot, signals)

	return signals, nil
}

// buildCycleSignal creates a circular-dependency signal from an SCC.
// Returns nil if the confidence is below minConfidence.
func buildCycleSignal(scc []string, minConfidence float64) *signal.RawSignal {
	conf := cycleConfidence(len(scc))
	if conf < minConfidence {
		return nil
	}

	// Build cycle path: A → B → C → A
	cyclePath := strings.Join(scc, " → ") + " → " + scc[0]

	title := fmt.Sprintf("Circular dependency: %s", cyclePath)
	desc := fmt.Sprintf(
		"Strongly connected component with %d modules forming a dependency cycle. "+
			"Circular dependencies make code harder to test, refactor, and reason about independently.",
		len(scc),
	)

	// Use the first module's path as the file path.
	filePath := scc[0]

	return &signal.RawSignal{
		Source:      "coupling",
		Kind:        "circular-dependency",
		FilePath:    filePath,
		Title:       title,
		Description: desc,
		Confidence:  conf,
		Tags:        []string{"architecture", "coupling"},
	}
}

// buildFanOutSignal creates a high-coupling signal for a module.
// Returns nil if the confidence is below minConfidence.
func buildFanOutSignal(module string, count int, minConfidence float64) *signal.RawSignal {
	conf := fanOutConfidence(count)
	if conf < minConfidence {
		return nil
	}

	title := fmt.Sprintf("High coupling: %s imports %d modules", module, count)
	desc := fmt.Sprintf(
		"Module %q has %d direct dependencies, which is above the threshold of %d. "+
			"High fan-out increases the risk of cascading breakage when any dependency changes.",
		module, count, defaultFanOutThreshold,
	)

	return &signal.RawSignal{
		Source:      "coupling",
		Kind:        "high-coupling",
		FilePath:    module,
		Title:       title,
		Description: desc,
		Confidence:  conf,
		Tags:        []string{"architecture", "coupling"},
	}
}

// cycleConfidence returns the confidence for a circular dependency signal.
func cycleConfidence(cycleLen int) float64 {
	switch {
	case cycleLen <= 2:
		return 0.80
	case cycleLen == 3:
		return 0.75
	default:
		return 0.70
	}
}

// fanOutConfidence returns the confidence for a high-coupling signal.
func fanOutConfidence(count int) float64 {
	switch {
	case count >= 20:
		return 0.70
	case count >= 15:
		// Linear interpolation: 15→0.55, 19→0.70
		return 0.55 + float64(count-15)*0.15/4.0
	case count >= 10:
		// Linear interpolation: 10→0.40, 14→0.55
		return 0.40 + float64(count-10)*0.15/4.0
	default:
		return 0.0
	}
}
