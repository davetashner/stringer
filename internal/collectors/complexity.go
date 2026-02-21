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
	"sort"
	"strings"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

// defaultMinComplexityScore is the minimum composite score to emit a signal.
const defaultMinComplexityScore = 6.0

// defaultMinFunctionLines is the minimum function body lines to analyze.
const defaultMinFunctionLines = 5

func init() {
	collector.Register(&ComplexityCollector{})
}

// FunctionComplexity holds complexity metrics for a single detected function.
type FunctionComplexity struct {
	FilePath  string
	FuncName  string
	StartLine int
	Lines     int
	Branches  int
	Score     float64 // lines/50 + branches
}

// ComplexityMetrics holds structured metrics from the complexity scan.
type ComplexityMetrics struct {
	Functions      []FunctionComplexity // sorted by score desc
	FilesAnalyzed  int
	FunctionsFound int
}

// ComplexityCollector detects complex functions using regex-based function
// detection and control flow keyword counting. Produces scored signals for
// functions exceeding a configurable complexity threshold.
type ComplexityCollector struct {
	metrics *ComplexityMetrics
}

// Name returns the collector name used for registration and filtering.
func (c *ComplexityCollector) Name() string { return "complexity" }

// langSpec describes how to detect functions and their boundaries in a language.
type langSpec struct {
	extensions []string
	funcStart  *regexp.Regexp
	endMode    endDetection
}

type endDetection int

const (
	endBraceDepth endDetection = iota
	endDedent
	endKeyword // Ruby's "end"
)

// branchPattern matches control flow keywords across all supported languages.
// Matches whole words only to avoid false positives (e.g., "notify" matching "if").
var branchPattern = regexp.MustCompile(
	`\b(?:if|else\s+if|elif|elsif|for|while|switch|case|catch|except|guard|when|unless)\b`)

// logicalOpPattern matches && and || operators for branch counting.
var logicalOpPattern = regexp.MustCompile(`&&|\|\|`)

// commentLinePattern matches lines that are purely comments.
var commentLinePattern = regexp.MustCompile(
	`^\s*(?://|#|/\*|\*\s|\*/|--)\s*`)

// langSpecs defines function detection patterns per language.
var langSpecs = []langSpec{
	{
		extensions: []string{".go"},
		funcStart:  regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s+)?(\w+)\s*\(`),
		endMode:    endBraceDepth,
	},
	{
		extensions: []string{".py"},
		funcStart:  regexp.MustCompile(`^\s*def\s+(\w+)\s*\(`),
		endMode:    endDedent,
	},
	{
		extensions: []string{".js", ".ts", ".jsx", ".tsx"},
		funcStart: regexp.MustCompile(
			`(?:^\s*(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*\()` +
				`|(?:^\s*(?:export\s+)?(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?(?:\([^)]*\)|[^=])\s*=>)` +
				`|(?:^\s*(?:async\s+)?(\w+)\s*\([^)]*\)\s*\{)`),
		endMode: endBraceDepth,
	},
	{
		extensions: []string{".java"},
		funcStart: regexp.MustCompile(
			`^\s*(?:(?:public|private|protected|static|final|abstract|synchronized|native)\s+)*\w[\w<>\[\],\s]*\s+(\w+)\s*\(`),
		endMode: endBraceDepth,
	},
	{
		extensions: []string{".rs"},
		funcStart:  regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?fn\s+(\w+)`),
		endMode:    endBraceDepth,
	},
	{
		extensions: []string{".rb"},
		funcStart:  regexp.MustCompile(`^\s*def\s+(\w+[?!]?)`),
		endMode:    endKeyword,
	},
	{
		extensions: []string{".php"},
		funcStart: regexp.MustCompile(
			`^\s*(?:(?:public|private|protected|static|final|abstract)\s+)*function\s+(\w+)\s*\(`),
		endMode: endBraceDepth,
	},
	{
		extensions: []string{".swift"},
		funcStart: regexp.MustCompile(
			`^\s*(?:(?:public|private|fileprivate|internal|open|static|class|override|@objc|mutating)\s+)*func\s+(\w+)`),
		endMode: endBraceDepth,
	},
	{
		extensions: []string{".scala"},
		funcStart:  regexp.MustCompile(`^\s*(?:(?:private|protected|override|final|abstract)\s+)*def\s+(\w+)`),
		endMode:    endBraceDepth,
	},
	{
		extensions: []string{".ex", ".exs"},
		funcStart:  regexp.MustCompile(`^\s*(?:defp?|defmacrop?)\s+(\w+[?!]?)`),
		endMode:    endKeyword,
	},
}

// extToSpec maps file extensions to their language spec for fast lookup.
var extToSpec map[string]*langSpec

func init() {
	extToSpec = make(map[string]*langSpec)
	for i := range langSpecs {
		for _, ext := range langSpecs[i].extensions {
			extToSpec[ext] = &langSpecs[i]
		}
	}
}

// Collect walks source files in repoPath, detects complex functions, and
// returns them as raw signals.
func (c *ComplexityCollector) Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	excludes := mergeExcludes(opts.ExcludePatterns)

	minScore := defaultMinComplexityScore
	if opts.MinComplexityScore > 0 {
		minScore = opts.MinComplexityScore
	}
	minLines := defaultMinFunctionLines
	if opts.MinFunctionLines > 0 {
		minLines = opts.MinFunctionLines
	}

	var allFunctions []FunctionComplexity
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
		spec := extToSpec[ext]
		if spec == nil {
			return nil
		}

		if isBinaryFile(path) {
			return nil
		}

		if isGeneratedFile(path) {
			return nil
		}

		funcs, analyzeErr := analyzeFile(path, relPath, spec, minLines)
		if analyzeErr != nil {
			return nil
		}

		allFunctions = append(allFunctions, funcs...)
		fileCount++

		if opts.ProgressFunc != nil && fileCount%500 == 0 {
			opts.ProgressFunc(fmt.Sprintf("complexity: scanned %d files", fileCount))
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking repo: %w", err)
	}

	// Sort by score descending.
	sort.Slice(allFunctions, func(i, j int) bool {
		return allFunctions[i].Score > allFunctions[j].Score
	})

	// Build signals for functions above threshold.
	var signals []signal.RawSignal
	for _, fc := range allFunctions {
		if fc.Score < minScore {
			continue
		}
		conf := complexityConfidence(fc.Score)
		signals = append(signals, signal.RawSignal{
			Source:     "complexity",
			Kind:       "complex-function",
			FilePath:   fc.FilePath,
			Line:       fc.StartLine,
			Title:      fmt.Sprintf("Complex function: %s (score %.1f, %d lines, %d branches)", fc.FuncName, fc.Score, fc.Lines, fc.Branches),
			Confidence: conf,
			Tags:       []string{"complexity", "refactor-candidate"},
		})
	}

	c.metrics = &ComplexityMetrics{
		Functions:      allFunctions,
		FilesAnalyzed:  fileCount,
		FunctionsFound: len(allFunctions),
	}

	// Enrich signals with timestamps from git log.
	gitRoot := opts.GitRoot
	if gitRoot == "" {
		gitRoot = repoPath
	}
	enrichTimestamps(ctx, gitRoot, signals)

	return signals, nil
}

// analyzeFile detects functions in a file and computes complexity metrics.
func analyzeFile(absPath, relPath string, spec *langSpec, minLines int) ([]FunctionComplexity, error) {
	f, err := FS.Open(absPath)
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

	return extractFunctions(lines, relPath, spec, minLines), nil
}

// extractFunctions finds functions in lines and computes their complexity.
func extractFunctions(lines []string, relPath string, spec *langSpec, minLines int) []FunctionComplexity {
	var results []FunctionComplexity
	i := 0

	for i < len(lines) {
		funcName, startLine := matchFuncStart(lines[i], spec, i+1)
		if funcName == "" {
			i++
			continue
		}

		// Determine function body boundaries.
		var bodyLines []string
		var endIdx int

		switch spec.endMode {
		case endBraceDepth:
			bodyLines, endIdx = extractBraceBody(lines, i)
		case endDedent:
			bodyLines, endIdx = extractDedentBody(lines, i)
		case endKeyword:
			bodyLines, endIdx = extractKeywordBody(lines, i)
		}

		if len(bodyLines) >= minLines {
			branches := countBranches(bodyLines)
			nonBlank := countNonBlank(bodyLines)
			score := float64(nonBlank)/50.0 + float64(branches)

			results = append(results, FunctionComplexity{
				FilePath:  relPath,
				FuncName:  funcName,
				StartLine: startLine,
				Lines:     nonBlank,
				Branches:  branches,
				Score:     score,
			})
		}

		if endIdx > i {
			i = endIdx + 1
		} else {
			i++
		}
	}

	return results
}

// matchFuncStart checks if a line matches the function start pattern for the
// given language spec. Returns the function name and 1-based line number.
func matchFuncStart(line string, spec *langSpec, lineNo int) (string, int) {
	matches := spec.funcStart.FindStringSubmatch(line)
	if matches == nil {
		return "", 0
	}

	// Return the first non-empty capture group.
	for _, m := range matches[1:] {
		if m != "" {
			return m, lineNo
		}
	}
	return "", 0
}

// extractBraceBody extracts the function body using brace depth tracking.
// startIdx is the index of the line containing the function signature.
func extractBraceBody(lines []string, startIdx int) ([]string, int) {
	depth := 0
	started := false

	for i := startIdx; i < len(lines); i++ {
		line := lines[i]
		for _, ch := range line {
			switch ch {
			case '{':
				depth++
				started = true
			case '}':
				depth--
			}
		}
		if started && depth <= 0 {
			// Body is from line after the opening brace to this line.
			bodyStart := startIdx + 1
			if bodyStart > i {
				return nil, i
			}
			return lines[bodyStart:i], i
		}
	}

	// No closing brace found — return what we have.
	if startIdx+1 < len(lines) {
		return lines[startIdx+1:], len(lines) - 1
	}
	return nil, startIdx
}

// extractDedentBody extracts a Python function body based on indentation.
func extractDedentBody(lines []string, startIdx int) ([]string, int) {
	// Find the indentation of the def line.
	defLine := lines[startIdx]
	defIndent := leadingSpaces(defLine)

	// The body starts on the next line and must be indented more than the def.
	bodyStart := startIdx + 1
	if bodyStart >= len(lines) {
		return nil, startIdx
	}

	// Find the first non-blank line to determine body indentation.
	bodyIndent := -1
	for i := bodyStart; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || trimmed == "#" {
			continue
		}
		bodyIndent = leadingSpaces(lines[i])
		break
	}

	if bodyIndent <= defIndent {
		return nil, startIdx
	}

	// Collect lines until dedent.
	var body []string
	endIdx := startIdx
	for i := bodyStart; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			body = append(body, lines[i])
			endIdx = i
			continue
		}
		indent := leadingSpaces(lines[i])
		if indent <= defIndent {
			break
		}
		body = append(body, lines[i])
		endIdx = i
	}

	return body, endIdx
}

// extractKeywordBody extracts a Ruby function body using end keyword matching.
func extractKeywordBody(lines []string, startIdx int) ([]string, int) {
	depth := 1 // the def itself opens a block
	bodyStart := startIdx + 1

	// Ruby block-opening keywords.
	blockOpen := regexp.MustCompile(`\b(?:def|class|module|do|if|unless|while|until|for|case|begin)\b`)
	blockEnd := regexp.MustCompile(`\bend\b`)

	for i := bodyStart; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Count block openers and closers on this line.
		depth += len(blockOpen.FindAllString(lines[i], -1))
		depth -= len(blockEnd.FindAllString(lines[i], -1))

		if depth <= 0 {
			if bodyStart > i {
				return nil, i
			}
			return lines[bodyStart:i], i
		}
	}

	if bodyStart < len(lines) {
		return lines[bodyStart:], len(lines) - 1
	}
	return nil, startIdx
}

// leadingSpaces returns the number of leading space characters in a line.
// Tabs count as 4 spaces (consistent with Python's typical indent).
func leadingSpaces(line string) int {
	count := 0
	for _, ch := range line {
		switch ch {
		case ' ':
			count++
		case '\t':
			count += 4
		default:
			return count
		}
	}
	return count
}

// countBranches counts control flow keywords and logical operators in lines,
// skipping comment-only lines.
func countBranches(lines []string) int {
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if commentLinePattern.MatchString(line) {
			continue
		}
		count += len(branchPattern.FindAllString(line, -1))
		count += len(logicalOpPattern.FindAllString(line, -1))
	}
	return count
}

// countNonBlank counts non-blank lines.
func countNonBlank(lines []string) int {
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

// complexityConfidence maps a complexity score to a confidence value per DR-013:
//   - score >= 15: 0.8
//   - score 8–15: linear interpolation 0.6–0.8
//   - score 6–8: linear interpolation 0.5–0.6
//   - score < 6: not emitted (handled by caller)
func complexityConfidence(score float64) float64 {
	switch {
	case score >= 15:
		return 0.8
	case score >= 8:
		// Linear from 0.6 at 8 to 0.8 at 15.
		return 0.6 + 0.2*(score-8)/(15-8)
	case score >= 6:
		// Linear from 0.5 at 6 to 0.6 at 8.
		return 0.5 + 0.1*(score-6)/(8-6)
	default:
		return 0.5
	}
}

// Metrics returns structured metrics from the complexity scan.
func (c *ComplexityCollector) Metrics() any { return c.metrics }

// Compile-time interface checks.
var _ collector.Collector = (*ComplexityCollector)(nil)
var _ collector.MetricsProvider = (*ComplexityCollector)(nil)
