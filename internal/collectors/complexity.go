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

// defaultMinComplexityScore is the minimum composite score to emit a signal.
const defaultMinComplexityScore = 6.0

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

// rubyBlockOpen matches Ruby's block-opening keywords used by
// extractKeywordBody to balance `end` keywords.
var rubyBlockOpen = regexp.MustCompile(
	`\b(?:def|class|module|do|if|unless|while|until|for|case|begin)\b`)

// rubyBlockEnd matches Ruby's `end` keyword.
var rubyBlockEnd = regexp.MustCompile(`\bend\b`)

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
				})
			}
		} else {
			funcs, analyzeErr := analyzeFile(path, relPath, spec, minLines)
			if analyzeErr != nil {
				return nil
			}
			allFunctions = append(allFunctions, funcs...)
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
				Tags:        []string{"complexity", "go", "ast-analyzed"},
			})
		} else {
			// Regex-analyzed: filter by minScore.
			if fc.Score < minScore {
				continue
			}
			conf := regexComplexityConfidence(fc.Score, fc.MaxNesting)
			signals = append(signals, signal.RawSignal{
				Source:      "complexity",
				Kind:        "complex-function",
				FilePath:    fc.FilePath,
				Line:        fc.StartLine,
				Title:       fmt.Sprintf("Complex function: %s (score %.1f, %d lines, %d branches, nesting %d)", fc.FuncName, fc.Score, fc.Lines, fc.Branches, fc.MaxNesting),
				Description: regexComplexityDescription(fc, minScore),
				Confidence:  conf,
				Tags:        []string{"complexity", "refactor-candidate"},
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
			ext := filepath.Ext(relPath)
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

	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		if quote != 0 {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == quote:
				quote = 0
				b.WriteRune(ch)
			}
			continue
		}
		switch {
		case ch == '"' || ch == '`' || (ch == '\'' && singleQuote):
			quote = ch
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
	fmt.Fprintf(&b, "CONTEXT: fires at score ≥ %.1f; tune via collectors.complexity.min_complexity_score. Non-Go languages are analyzed heuristically (indentation-derived nesting); Go gets AST-based cognitive complexity.", minScore)
	return b.String()
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
