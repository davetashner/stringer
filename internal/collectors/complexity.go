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
	"regexp"
	"sort"
	"strings"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

// defaultMinComplexityScore is the minimum Go cyclomatic complexity to emit
// an AST-analyzed signal.
const defaultMinComplexityScore = 6.0

// defaultMinRegexScore is the minimum nesting-weighted score for
// regex-analyzed (non-Go) functions. Under DR-024's bands a score of 12
// maps to confidence 0.5, so ordinary 16-line/4-branch methods (score ~7,
// confidence ~0.42) are no longer emitted by default (stringer-nxx.5).
// collectors.complexity.min_complexity_score overrides both paths.
const defaultMinRegexScore = 12.0

// maxTestCallbackLabel bounds the string literal echoed into a JS/TS test
// callback name so titles stay readable.
const maxTestCallbackLabel = 60

// defaultMinFunctionLines is the minimum function body lines to analyze.
const defaultMinFunctionLines = 5

func init() {
	collector.Register(&ComplexityCollector{})
}

// FunctionComplexity holds complexity metrics for a single detected function.
type FunctionComplexity struct {
	FilePath   string
	FuncName   string
	StartLine  int
	EndLine    int
	Lines      int
	Branches   int     // raw branch keywords + logical operators
	Score      float64 // lines/50 + nesting-weighted branches (regex) or cognitive (AST)
	Cyclomatic int     // AST-based cyclomatic complexity (0 if regex-analyzed)
	Cognitive  int     // AST-based cognitive complexity (0 if regex-analyzed)
	MaxNesting int     // max nesting depth: AST-derived (Go) or indentation-derived (regex)
	ASTBased   bool    // true if analyzed via Go AST, false if regex-based
	IsTest     bool    // true if the function lives in a test file or is itself a test (see isComplexityTestFile)
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
	// funcReject, when set, vetoes a funcStart match. Languages whose
	// constructor syntax has no leading keyword (C#'s `Foo(int x)`) need it
	// to keep `if (`, `return Foo(` and `record Foo(` out of the function
	// list, since RE2 has no negative lookahead.
	funcReject *regexp.Regexp
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

// csharpBranchPattern adds C#'s `foreach` to the shared branch keywords.
// `do` is deliberately absent: its trailing `while` already counts the
// loop once, and counting both would double-charge every do-while.
var csharpBranchPattern = regexp.MustCompile(`\bforeach\b`)

// csharpConditionalOpPattern matches C#'s null-coalescing (`??`, `??=`),
// null-conditional (`?.`) and ternary (` ? `) operators. Like && and ||
// they count at depth-independent weight 1: conditions, not structure.
// Ternary requires surrounding whitespace so nullable types (`int?`)
// don't count.
var csharpConditionalOpPattern = regexp.MustCompile(`\?\?=?|\?\.|\s\?\s`)

// commentLinePattern matches lines that are purely comments.
var commentLinePattern = regexp.MustCompile(
	`^\s*(?://|#|/\*|\*\s|\*/|--)\s*`)

// rubyBlockOpen matches Ruby's block-opening keywords used by
// extractKeywordBody to balance `end` keywords.
var rubyBlockOpen = regexp.MustCompile(
	`\b(?:def|class|module|do|if|unless|while|until|for|case|begin)\b`)

// rubyBlockEnd matches Ruby's `end` keyword.
var rubyBlockEnd = regexp.MustCompile(`\bend\b`)

// rustTestAttr matches a Rust attribute line that marks the following fn
// as a test (`#[test]`, `#[tokio::test]`, `#[bench]`, `#[cfg(test)]`), so
// inline `mod tests` blocks in src/ files are treated as test code even
// though the file name is not a test convention (stringer-nxx.5).
var rustTestAttr = regexp.MustCompile(`^\s*#\[(?:cfg\(test\)|(?:\w+::)*(?:test|bench)\b)`)

// jsTestCallbacks are the JS/TS test-framework calls whose callback bodies
// the JS funcStart regex mistakes for a function named after the call
// (`describe('app', function() {` reads as a function called "describe").
var jsTestCallbacks = map[string]bool{
	"describe": true, "it": true, "test": true, "context": true, "suite": true,
	"specify": true, "beforeEach": true, "afterEach": true, "beforeAll": true,
	"afterAll": true, "before": true, "after": true,
}

// jsStringLiteral captures the first string literal on a line.
var jsStringLiteral = regexp.MustCompile("[\"'`]([^\"'`]*)[\"'`]")

// complexityTestDirs are path components that mark test scaffolding
// regardless of file naming: express keeps its suite in test/app.js,
// Jest projects in __tests__/, RSpec in spec/. Complements the shared
// isTestFile naming conventions from patterns_classify.go.
var complexityTestDirs = map[string]bool{
	"test": true, "tests": true, "__tests__": true, "spec": true, "testdata": true,
}

// isComplexityTestFile reports whether relPath is test code for the
// purposes of complexity scoring: either a language test-file naming
// convention (shared isTestFile) or any path component that is a
// conventional test directory.
func isComplexityTestFile(relPath string) bool {
	if isTestFile(relPath) {
		return true
	}
	for _, p := range strings.Split(filepath.ToSlash(filepath.Dir(relPath)), "/") {
		if complexityTestDirs[p] {
			return true
		}
	}
	return false
}

// isJSExt reports whether ext is one of the JavaScript/TypeScript extensions.
func isJSExt(ext string) bool {
	return ext == ".js" || ext == ".ts" || ext == ".jsx" || ext == ".tsx"
}

// testCallbackName renders a JS/TS test-framework callback by its enclosing
// string literal so the title reads `describe("app.render") callback`
// rather than `describe` (stringer-nxx.5).
func testCallbackName(call, line string) string {
	m := jsStringLiteral.FindStringSubmatch(line)
	if m == nil || strings.TrimSpace(m[1]) == "" {
		return call + " callback"
	}
	label := m[1]
	if len(label) > maxTestCallbackLabel {
		label = label[:maxTestCallbackLabel] + "…"
	}
	return fmt.Sprintf("%s(%q) callback", call, label)
}

// precededByRustTestAttr reports whether the attribute block directly
// above the fn at idx (attributes, comments and blank lines only, bounded
// to ten lines) contains a Rust test attribute.
func precededByRustTestAttr(lines []string, idx int) bool {
	for j := idx - 1; j >= 0 && j >= idx-10; j-- {
		trimmed := strings.TrimSpace(lines[j])
		switch {
		case trimmed == "" || strings.HasPrefix(trimmed, "//"):
			continue
		case strings.HasPrefix(trimmed, "#["):
			if rustTestAttr.MatchString(lines[j]) {
				return true
			}
		default:
			return false
		}
	}
	return false
}

// langSpecs defines function detection patterns per language.
//
// To add a new language, append a single entry here with the file
// extensions it owns, a regex that matches a function declaration line
// (capturing the name), and the endDetection mode appropriate for the
// language's block structure. This is the single table referenced by
// the L1 Language Support Expansion epic (stringer-043).
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
	{
		// C#: methods and constructors. Modifiers, an optional generic /
		// array / nullable / tuple return type, then `Name(` or `Name<T>(`. The
		// return type is optional so constructors match; funcReject
		// filters the keyword-led statements that otherwise look like
		// constructor calls. Expression-bodied members (`=> expr;`) and
		// abstract/interface signatures end on `;` before any brace, which
		// extractBraceBody reads as an empty body (stringer-nxx.9).
		extensions: []string{".cs"},
		funcStart: regexp.MustCompile(
			`^\s*(?:(?:public|private|protected|internal|static|async|override|virtual|abstract|partial|sealed|extern|unsafe|new|readonly)\s+)*` +
				`(?:(?:[\w.]+(?:<[^()=;{}]*>)?(?:\[[,\s]*\])*|\((?:[^()]|\([^()]*\))*\))\??\s+)?(\w+)(?:<[^()=;{}]*>)?\s*\(`),
		funcReject: regexp.MustCompile(
			`^\s*(?:if|else|for|foreach|while|switch|catch|using|lock|fixed|return|new|throw|await|yield|case|do|namespace|delegate|event|get|set|init|add|remove|base|this)\b` +
				`|^\s*(?:\w+\s+)*(?:class|struct|record|interface|enum)\s+\w+`),
		endMode: endBraceDepth,
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
	minRegexScore := defaultMinRegexScore
	if opts.MinComplexityScore > 0 {
		minScore = opts.MinComplexityScore
		minRegexScore = opts.MinComplexityScore
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

		relPath, relErr := relSlash(repoPath, path)
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
		if d.Type()&os.ModeSymlink != 0 && isSymlinkOutsideRepo(path, repoPath) {
			return nil
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

		// Test code is skipped by default: table-driven Go tests, JS
		// describe/it callbacks and JUnit fixtures score high on every
		// metric but are never refactor candidates (stringer-nxx.5).
		// collectors.complexity.include_tests restores them, tagged.
		testFile := isComplexityTestFile(relPath)
		if testFile && !opts.IncludeTests {
			return nil
		}

		// Use AST analysis for Go files; regex for everything else.
		if ext == ".go" {
			goFuncs, astErr := analyzeGoFile(path)
			if astErr != nil {
				slog.Warn("complexity: Go AST parse failed, skipping file", "path", relPath, "error", astErr)
				return nil
			}
			for _, gf := range goFuncs {
				if gf.Lines < minLines {
					continue
				}
				if gf.Cyclomatic < int(minScore) {
					continue
				}
				funcName := gf.Name
				if gf.Receiver != "" {
					funcName = gf.Receiver + "." + gf.Name
				}
				allFunctions = append(allFunctions, FunctionComplexity{
					FilePath:   relPath,
					FuncName:   funcName,
					StartLine:  gf.StartLine,
					EndLine:    gf.EndLine,
					Lines:      gf.Lines,
					Branches:   gf.Cyclomatic - 1, // branches = cyclomatic - 1
					Score:      float64(gf.Cognitive),
					Cyclomatic: gf.Cyclomatic,
					Cognitive:  gf.Cognitive,
					MaxNesting: gf.MaxNesting,
					ASTBased:   true,
					IsTest:     testFile,
				})
			}
		} else {
			funcs, analyzeErr := analyzeFile(path, relPath, spec, minLines)
			if analyzeErr != nil {
				return nil
			}
			for _, fc := range funcs {
				fc.IsTest = fc.IsTest || testFile
				if fc.IsTest && !opts.IncludeTests {
					continue
				}
				allFunctions = append(allFunctions, fc)
			}
		}
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
		if fc.ASTBased {
			// AST-analyzed: already filtered by cyclomatic threshold above.
			titleKind := "Complex function"
			if strings.Contains(fc.FuncName, ".") {
				titleKind = "Complex method"
			}
			conf := astComplexityConfidence(fc.Cyclomatic, fc.Cognitive, fc.MaxNesting)
			signals = append(signals, signal.RawSignal{
				Source:      "complexity",
				Kind:        "complex-function",
				FilePath:    fc.FilePath,
				Line:        fc.StartLine,
				Title:       fmt.Sprintf("%s: %s (cyclomatic: %d, cognitive: %d, nesting: %d)", titleKind, fc.FuncName, fc.Cyclomatic, fc.Cognitive, fc.MaxNesting),
				Description: astComplexityDescription(fc),
				Confidence:  conf,
				Tags:        complexityTags(fc, "complexity", "go", "ast-analyzed"),
			})
		} else {
			// Regex-analyzed: filter by the (higher) regex floor.
			if fc.Score < minRegexScore {
				continue
			}
			conf := regexComplexityConfidence(fc.Score, fc.MaxNesting)
			signals = append(signals, signal.RawSignal{
				Source:      "complexity",
				Kind:        "complex-function",
				FilePath:    fc.FilePath,
				Line:        fc.StartLine,
				Title:       fmt.Sprintf("Complex function: %s (score %.1f, %d lines, %d branches, nesting %d)", fc.FuncName, fc.Score, fc.Lines, fc.Branches, fc.MaxNesting),
				Description: regexComplexityDescription(fc, minRegexScore),
				Confidence:  conf,
				Tags:        complexityTags(fc, "complexity", "refactor-candidate"),
			})
		}
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

	ext := filepath.Ext(relPath)

	for i < len(lines) {
		funcName, startLine := matchFuncStart(lines[i], spec, i+1)
		if funcName == "" {
			i++
			continue
		}

		// Test detection below the file level: JS/TS test-framework
		// callbacks and Rust #[test] fns are tests wherever they live.
		isTest := false
		if isJSExt(ext) && jsTestCallbacks[funcName] {
			funcName = testCallbackName(funcName, lines[i])
			isTest = true
		}
		if ext == ".rs" && precededByRustTestAttr(lines, i) {
			isTest = true
		}

		// Determine function body boundaries.
		var bodyLines []string
		var endIdx int

		switch spec.endMode {
		case endBraceDepth:
			bodyLines, endIdx = extractBraceBody(lines, i, ext)
		case endDedent:
			bodyLines, endIdx = extractDedentBody(lines, i)
		case endKeyword:
			bodyLines, endIdx = extractKeywordBody(lines, i)
		}

		if len(bodyLines) >= minLines {
			body := analyzeBody(bodyLines, ext)
			nonBlank := countNonBlank(bodyLines)
			// Lines contribute marginally; the score is dominated by
			// nesting-weighted branch points so that a flat guard list
			// scores far below equally-branchy nested code (stringer-t98).
			score := float64(nonBlank)/50.0 + body.WeightedBranches

			results = append(results, FunctionComplexity{
				FilePath:   relPath,
				FuncName:   funcName,
				StartLine:  startLine,
				Lines:      nonBlank,
				Branches:   body.Branches,
				Score:      score,
				MaxNesting: body.MaxNesting,
				IsTest:     isTest,
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
	if spec.funcReject != nil && spec.funcReject.MatchString(line) {
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
// Braces inside string literals and comments are ignored (except in
// .jsx/.tsx, where apostrophes in JSX text would read as open quotes). A
// signature that reaches `;` before any brace — an abstract or interface
// method, a C# expression-bodied member, a JS `const f = x => x + 1;` — has
// no body, so the scan stops there instead of swallowing the next
// function's braces (stringer-nxx.9).
func extractBraceBody(lines []string, startIdx int, ext string) ([]string, int) {
	depth := 0
	started := false
	stripStrings := ext != ".jsx" && ext != ".tsx"

	for i := startIdx; i < len(lines); i++ {
		line := lines[i]
		if stripStrings {
			line = stripStringsAndComments(line, ext)
		}
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
		if !started && strings.HasSuffix(strings.TrimSpace(line), ";") {
			return nil, i
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
// The block-open / block-end regexes are module-level (rubyBlockOpen,
// rubyBlockEnd) so callers don't re-compile them per function.
func extractKeywordBody(lines []string, startIdx int) ([]string, int) {
	depth := 1 // the def itself opens a block
	bodyStart := startIdx + 1

	for i := bodyStart; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Count block openers and closers on this line.
		depth += len(rubyBlockOpen.FindAllString(lines[i], -1))
		depth -= len(rubyBlockEnd.FindAllString(lines[i], -1))

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

// bodyAnalysis holds the branch metrics for one function body.
type bodyAnalysis struct {
	Branches         int     // raw branch keywords + logical operators
	WeightedBranches float64 // nesting-weighted branch cost (see analyzeBody)
	MaxNesting       int     // indentation-derived max depth (1 = flat)
}

// jsxLogicalOpWeight discounts && and || in .jsx/.tsx files: `{cond && <X/>}`
// is React's conditional-rendering idiom, not control flow a reader must
// hold in their head (stringer-sby). Distinguishing JSX expressions from
// real logic without a parser is impractical line-by-line, so logical
// operators in these files count at half weight, documented in AGENTS.md.
const jsxLogicalOpWeight = 0.5

// maxIndentDepth caps indentation-derived nesting so continuation-line
// indentation cannot run the depth to absurd values.
const maxIndentDepth = 10

// analyzeBody counts branch points in a function body, weighting each
// control-flow keyword by the nesting depth of its line the way cognitive
// complexity does: a branch at depth 1 costs 1, at depth d costs d. Depth
// is derived from indentation (relative to the shallowest code line, in
// units of the smallest observed indent step), which separates "twenty
// flat guards" from "four conditions nested four deep" without a parser
// (stringer-t98). String literals and comments are stripped before
// matching so message text and trailing notes don't count as control flow
// (stringer-sby). Logical operators count at depth-independent weight 1
// (0.5 in .jsx/.tsx): they are conditions, not structure.
func analyzeBody(lines []string, ext string) bodyAnalysis {
	logicalWeight := 1.0
	if ext == ".jsx" || ext == ".tsx" {
		logicalWeight = jsxLogicalOpWeight
	}

	type codeLine struct {
		clean  string
		indent int
	}
	var code []codeLine
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if commentLinePattern.MatchString(line) {
			continue
		}
		clean := stripStringsAndComments(line, ext)
		if strings.TrimSpace(clean) == "" {
			continue
		}
		code = append(code, codeLine{clean: clean, indent: leadingSpaces(line)})
	}
	if len(code) == 0 {
		return bodyAnalysis{MaxNesting: 1}
	}

	base := code[0].indent
	indentSet := make(map[int]bool)
	for _, cl := range code {
		if cl.indent < base {
			base = cl.indent
		}
		indentSet[cl.indent] = true
	}
	unit := inferIndentUnit(indentSet)

	out := bodyAnalysis{MaxNesting: 1}
	for _, cl := range code {
		depth := 1 + (cl.indent-base)/unit
		if depth > maxIndentDepth {
			depth = maxIndentDepth
		}

		branches := len(branchPattern.FindAllString(cl.clean, -1))
		logicals := len(logicalOpPattern.FindAllString(cl.clean, -1))
		if ext == ".cs" {
			branches += len(csharpBranchPattern.FindAllString(cl.clean, -1))
			logicals += len(csharpConditionalOpPattern.FindAllString(cl.clean, -1))
		}

		out.Branches += branches + logicals
		out.WeightedBranches += float64(branches*depth) + float64(logicals)*logicalWeight

		// Nesting is a structural property: track it on lines that carry
		// control flow, so continuation-line indentation doesn't inflate it.
		if branches > 0 && depth > out.MaxNesting {
			out.MaxNesting = depth
		}
	}
	return out
}

// inferIndentUnit returns the indentation step size for a body: the
// smallest positive difference between observed indent levels, clamped to
// at least 2 columns (1-column deltas are usually alignment, not nesting).
// Falls back to 4 when the body has a single indent level.
func inferIndentUnit(indents map[int]bool) int {
	levels := make([]int, 0, len(indents))
	for i := range indents {
		levels = append(levels, i)
	}
	sort.Ints(levels)

	unit := 0
	for i := 1; i < len(levels); i++ {
		d := levels[i] - levels[i-1]
		if d >= 2 && (unit == 0 || d < unit) {
			unit = d
		}
	}
	if unit == 0 {
		return 4
	}
	return unit
}

// stripStringsAndComments removes string-literal contents and trailing
// comments from a line so tokens inside them ("retry if this fails",
// `// if unset, defaults`) don't count as branches (stringer-sby). It is a
// single-pass quote-state scan, deliberately line-local: multi-line
// strings are already approximated by the surrounding heuristics.
func stripStringsAndComments(line, ext string) string {
	// Languages where # starts a comment.
	hashComment := ext == ".py" || ext == ".rb" || ext == ".ex" || ext == ".exs"
	// Rust lifetimes ('a) would read as an unterminated char literal and
	// swallow the rest of the line, so ' is not a string quote there.
	singleQuote := ext != ".rs"

	var b strings.Builder
	runes := []rune(line)
	var quote rune
	escaped := false
	// C# verbatim strings (@"...", $@"...", @$"...") have no backslash
	// escapes and double a quote to embed one, so `@"C:\"` must close at
	// the final quote and `@"say ""hi"""` must not close early.
	verbatim := false

	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		if quote != 0 {
			switch {
			case verbatim && ch == '"' && i+1 < len(runes) && runes[i+1] == '"':
				i++
			case escaped:
				escaped = false
			case ch == '\\' && !verbatim:
				escaped = true
			case ch == quote:
				quote = 0
				verbatim = false
				b.WriteRune(ch)
			}
			continue
		}
		switch {
		case ch == '"' || ch == '`' || (ch == '\'' && singleQuote):
			quote = ch
			verbatim = ext == ".cs" && ch == '"' && precededByVerbatimPrefix(runes, i)
			b.WriteRune(ch)
		case ch == '/' && i+1 < len(runes) && runes[i+1] == '/':
			return b.String()
		case ch == '/' && i+1 < len(runes) && runes[i+1] == '*':
			rest := string(runes[i+2:])
			end := strings.Index(rest, "*/")
			if end < 0 {
				return b.String()
			}
			i += 2 + end + 1
		case ch == '#' && hashComment:
			return b.String()
		default:
			b.WriteRune(ch)
		}
	}
	return b.String()
}

// precededByVerbatimPrefix reports whether the quote at runes[i] opens a C#
// verbatim string: the character before it is `@`, or `$@` / `@$` for
// interpolated verbatim strings.
func precededByVerbatimPrefix(runes []rune, i int) bool {
	if i >= 1 && runes[i-1] == '@' {
		return true
	}
	return i >= 2 && runes[i-1] == '$' && runes[i-2] == '@'
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

// regexComplexityConfidence maps a regex-path score to confidence, then
// caps mostly-flat functions at 0.55 (P3 after priority mapping): when
// nesting is 1–2 the score is driven by branch count alone, and a flat
// guard list — a validator, a dispatch table — is often the clearest way
// to write that code (stringer-t98). The bead body says so explicitly.
func regexComplexityConfidence(score float64, maxNesting int) float64 {
	conf := complexityConfidence(score)
	if maxNesting <= 2 && conf > 0.55 {
		conf = 0.55
	}
	return conf
}

// regexComplexityDescription builds the WHAT/WHY/ACTION/DISMISS/CONTEXT body
// for a regex-analyzed finding, so a generated bead carries its metrics,
// its rationale, and an honest statement of when to close it (stringer-h51).
func regexComplexityDescription(fc FunctionComplexity, minScore float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "WHAT: score %.1f = %d non-blank lines ÷ 50 + nesting-weighted branch points (%d raw branches/operators, max nesting depth %d).\n",
		fc.Score, fc.Lines, fc.Branches, fc.MaxNesting)
	b.WriteString("WHY: branches buried under nesting are where readers lose track of state; a flat list of guards reads cheaply even when long.\n")
	if fc.MaxNesting <= 2 {
		b.WriteString("ACTION: likely none — see DISMISS.\n")
		b.WriteString("DISMISS: this function is mostly flat (nesting ≤ 2), so the score is driven by branch count alone; validators, dispatch tables, and config builders are often clearest as a flat rule list. Confidence has been capped accordingly — close as working-as-intended unless the branch logic genuinely interleaves.\n")
	} else {
		b.WriteString("ACTION: extract the most deeply nested blocks into named helpers, or invert conditions with early returns to flatten the structure.\n")
		b.WriteString("DISMISS: if the nesting mirrors an inherent structure (a state machine, a recursive descent), a rewrite may not clarify — close with a comment saying so.\n")
	}
	fmt.Fprintf(&b, "CONTEXT: fires at score ≥ %.1f; tune via collectors.complexity.min_complexity_score. Test files are skipped unless collectors.complexity.include_tests is true. Non-Go languages are analyzed heuristically (indentation-derived nesting); Go gets AST-based cognitive complexity.", minScore)
	return b.String()
}

// complexityTags returns the base tags plus "test-file" when the function
// is test code that was kept because include_tests is set, so consumers
// can filter those findings downstream.
func complexityTags(fc FunctionComplexity, base ...string) []string {
	if fc.IsTest {
		return append(base, "test-file")
	}
	return base
}

// astComplexityDescription builds the body for a Go AST-analyzed finding.
func astComplexityDescription(fc FunctionComplexity) string {
	var b strings.Builder
	fmt.Fprintf(&b, "WHAT: cyclomatic %d, cognitive %d, max nesting %d. Lines %d-%d in %s.\n",
		fc.Cyclomatic, fc.Cognitive, fc.MaxNesting, fc.StartLine, fc.EndLine, fc.FilePath)
	b.WriteString("WHY: cognitive complexity measures how much state a reader must hold; it grows superlinearly with nesting.\n")
	b.WriteString("ACTION: extract the most deeply nested blocks into named functions; prefer early returns over else-chains.\n")
	b.WriteString("DISMISS: table-driven or generated code with low nesting relative to cyclomatic count is often fine as-is — close with a comment saying so.")
	return b.String()
}

// complexityConfidence maps a nesting-weighted score to confidence.
// Recalibrated for DR-024's depth-weighted scoring, which roughly doubled
// raw scores (a branch at depth d costs d): the DR-013 bands (0.8 at 15)
// were tuned for raw branch counts and pushed ordinary loop+guard
// validators to P1. New bands, mirroring the AST path's cognitive/30 shape:
//   - score >= 30: 0.8 (P1 territory — genuinely tangled)
//   - score 18–30: linear 0.6–0.8
//   - score 6–18: linear 0.4–0.6
//   - score < 6: not emitted (handled by caller)
func complexityConfidence(score float64) float64 {
	switch {
	case score >= 30:
		return 0.8
	case score >= 18:
		return 0.6 + 0.2*(score-18)/(30-18)
	case score >= 6:
		return 0.4 + 0.2*(score-6)/(18-6)
	default:
		return 0.4
	}
}

// astComplexityConfidence computes confidence for AST-analyzed functions.
// Formula: max(cyclomatic/20, cognitive/30, nesting/5), clamped to [0.3, 0.9].
func astComplexityConfidence(cyclomatic, cognitive, nesting int) float64 {
	conf := math.Max(float64(cyclomatic)/20.0, math.Max(float64(cognitive)/30.0, float64(nesting)/5.0))
	if conf < 0.3 {
		conf = 0.3
	}
	if conf > 0.9 {
		conf = 0.9
	}
	return conf
}

// Metrics returns structured metrics from the complexity scan.
func (c *ComplexityCollector) Metrics() any { return c.metrics }

// Compile-time interface checks.
var _ collector.Collector = (*ComplexityCollector)(nil)
var _ collector.MetricsProvider = (*ComplexityCollector)(nil)
