// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

// defaultFileCountCap is the maximum number of source files to analyze.
// Repositories exceeding this are skipped with a warning.
const defaultFileCountCap = 10_000

func init() {
	collector.Register(&DeadCodeCollector{})
}

// symbolDef represents a symbol extracted from source code.
type symbolDef struct {
	Name       string
	FilePath   string // relative path
	Line       int
	Kind       string // "unused-function" or "unused-type"
	Exported   bool
	Language   string // file extension (e.g., ".go")
	InInternal bool   // Go: inside internal/ directory
	InTest     bool   // defined in a test file (only with include_tests)
}

// DeadCodeMetrics holds structured metrics from the dead code scan.
type DeadCodeMetrics struct {
	FilesAnalyzed      int
	SymbolsFound       int
	DeadSymbols        int
	SkippedCapExceeded bool
	// IsLibrary reports whether the repository was classified as a library
	// (manifest marker or no application entry point).
	IsLibrary bool
	// PublicSuppressed counts exported symbols of a library repository that
	// looked unreferenced but were not reported because include_public_api
	// is false.
	PublicSuppressed int
}

// DeadCodeCollector detects unused functions and types using regex-based
// symbol extraction and in-memory reference searching. Follows the
// regex-over-AST philosophy from DR-013/DR-014.
type DeadCodeCollector struct {
	metrics    *DeadCodeMetrics
	regexCache map[string]*regexp.Regexp
}

// Name returns the collector name used for registration and filtering.
func (c *DeadCodeCollector) Name() string { return "deadcode" }

// typePatterns maps file extensions to regex patterns for type/class/struct
// definitions. Each regex has one capture group for the type name.
var typePatterns = map[string]*regexp.Regexp{
	".go":    regexp.MustCompile(`^\s*type\s+(\w+)\s+(?:struct|interface)\s*\{`),
	".py":    regexp.MustCompile(`^\s*class\s+(\w+)`),
	".js":    regexp.MustCompile(`^\s*(?:export\s+)?class\s+(\w+)`),
	".ts":    regexp.MustCompile(`^\s*(?:export\s+)?(?:class|interface|type)\s+(\w+)`),
	".tsx":   regexp.MustCompile(`^\s*(?:export\s+)?(?:class|interface|type)\s+(\w+)`),
	".jsx":   regexp.MustCompile(`^\s*(?:export\s+)?class\s+(\w+)`),
	".java":  regexp.MustCompile(`^\s*(?:(?:public|private|protected|abstract|final|static)\s+)*(?:class|interface|enum)\s+(\w+)`),
	".rs":    regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:struct|enum|trait)\s+(\w+)`),
	".rb":    regexp.MustCompile(`^\s*class\s+(\w+)`),
	".php":   regexp.MustCompile(`^\s*(?:(?:abstract|final)\s+)?class\s+(\w+)`),
	".swift": regexp.MustCompile(`^\s*(?:(?:public|private|fileprivate|internal|open|final)\s+)*(?:class|struct|enum|protocol)\s+(\w+)`),
	".scala": regexp.MustCompile(`^\s*(?:(?:private|protected|abstract|sealed|final|case)\s+)*(?:class|object|trait)\s+(\w+)`),
	".ex":    regexp.MustCompile(`^\s*defmodule\s+([\w.]+)`),
	".cs":    regexp.MustCompile(`^\s*(?:(?:public|private|protected|internal|static|abstract|sealed|partial|readonly|ref|unsafe|new)\s+)*(?:class|struct|interface|enum|record(?:\s+(?:class|struct))?)\s+(\w+)`),
}

// skipNames are symbol names that should never be flagged as dead code.
var skipNames = map[string]bool{
	"main": true, "init": true, "setup": true, "teardown": true,
	"constructor": true, "render": true, "componentDidMount": true,
	"componentDidUpdate": true, "componentWillUnmount": true,
	"setUp": true, "tearDown": true, "run": true,
	"__init__": true, "__str__": true, "__repr__": true,
	"__enter__": true, "__exit__": true, "__call__": true,
	"__len__": true, "__getitem__": true, "__setitem__": true,
	"__delitem__": true, "__iter__": true, "__next__": true,
	"__eq__": true, "__hash__": true, "__lt__": true,
	"__le__": true, "__gt__": true, "__ge__": true,
	"__add__": true, "__sub__": true, "__mul__": true,
	"__contains__": true, "__bool__": true, "__new__": true,
	"initialize": true, // Ruby
}

// skipPrefixes are symbol name prefixes that indicate test/benchmark functions.
var skipPrefixes = []string{"Test", "Benchmark", "Example", "test_", "test"}

// wordBoundary builds a regex to match a symbol name at word boundaries.
// Ruby/Elixir predicate and bang names (`valid?`, `save!`) end in a
// non-word byte, after which `\b` could only match before another word
// byte, so the trailing boundary is dropped for them: `\bvalid\?` matches
// `obj.valid?` and `valid?(x)` but not the plain method `valid`.
// Results are cached on the collector to avoid redundant compilation.
func (c *DeadCodeCollector) wordBoundary(name string) *regexp.Regexp {
	if re, ok := c.regexCache[name]; ok {
		return re
	}
	pat := `\b` + regexp.QuoteMeta(name)
	if !strings.HasSuffix(name, "?") && !strings.HasSuffix(name, "!") {
		pat += `\b`
	}
	re := regexp.MustCompile(pat)
	c.regexCache[name] = re
	return re
}

// shouldSkipSymbol returns true if the symbol name should never be flagged.
func shouldSkipSymbol(name string) bool {
	if len(name) <= 2 {
		return true
	}
	if skipNames[name] {
		return true
	}
	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	// Skip dunder methods.
	if strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__") {
		return true
	}
	return false
}

// isExported determines if a symbol is exported/public for the given language.
func isExported(name, ext string) bool {
	switch ext {
	case ".go":
		if len(name) == 0 {
			return false
		}
		return name[0] >= 'A' && name[0] <= 'Z'
	case ".rs":
		// Handled by the pub keyword detection in extractSymbols.
		// Default to exported; the caller sets this from regex context.
		return true
	case ".py", ".rb":
		// Python/Ruby: underscore prefix = private convention.
		return !strings.HasPrefix(name, "_")
	case ".java":
		// Java: assume public unless lowercase first char (unusual).
		if len(name) == 0 {
			return false
		}
		return name[0] >= 'A' && name[0] <= 'Z'
	default:
		// JS/TS: export keyword handled by extractSymbols. Default to exported.
		return true
	}
}

// fileContents caches file content for the reference search pass.
type fileContents struct {
	relPath string
	content string
	isTest  bool
}

// Collect walks source files, extracts symbol definitions, then searches for
// references to determine which symbols are dead code.
func (c *DeadCodeCollector) Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	excludes := mergeExcludes(opts.ExcludePatterns)

	// Resolve configurable file cap with default.
	fileCap := opts.DeadcodeMaxFiles
	if fileCap == 0 {
		fileCap = defaultFileCountCap
	}

	// Pass 1: Walk files, extract symbols, cache content.
	var symbols []symbolDef
	var files []fileContents
	var fileCount int
	hasEntryPoint := false

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
			if isEntryPointPath(relPath, true) {
				hasEntryPoint = true
			}
			if shouldExclude(relPath, excludes) {
				return filepath.SkipDir
			}
			return nil
		}

		if shouldExclude(relPath, excludes) {
			return nil
		}
		if isEntryPointPath(relPath, false) {
			hasEntryPoint = true
		}

		// Skip symlinks outside repo tree.
		if d.Type()&os.ModeSymlink != 0 && isSymlinkOutsideRepo(path, repoPath) {
			return nil
		}

		if len(opts.IncludePatterns) > 0 && !matchesAny(relPath, opts.IncludePatterns) {
			return nil
		}

		ext := filepath.Ext(path)
		// Must be a supported language (has either function or type patterns).
		if extToSpec[ext] == nil && typePatterns[ext] == nil {
			return nil
		}

		if isBinaryFile(path) {
			return nil
		}

		if isGeneratedFile(path) {
			return nil
		}

		fileCount++
		if fileCount > fileCap {
			return fmt.Errorf("file count exceeds cap (%d)", fileCap)
		}

		// Read file content.
		content, readErr := readFileContent(path)
		if readErr != nil {
			return nil
		}

		// Test files (naming conventions plus test/tests/__tests__/spec
		// directories) only count as references unless include_tests is set:
		// their helpers, fixtures and URL-dispatched views are discovered by
		// the harness, not called by name (stringer-nxx.3).
		testFile := isComplexityTestFile(relPath)
		files = append(files, fileContents{
			relPath: relPath,
			content: content,
			isTest:  testFile,
		})
		if testFile && !opts.IncludeTests {
			return nil
		}

		// Extract symbols.
		syms := extractSymbols(content, relPath, ext, opts.IncludeTests)
		for i := range syms {
			syms[i].InTest = testFile
		}
		symbols = append(symbols, syms...)

		if opts.ProgressFunc != nil && fileCount%500 == 0 {
			opts.ProgressFunc(fmt.Sprintf("deadcode: scanned %d files", fileCount))
		}

		return nil
	})

	capExceeded := false
	if err != nil {
		if strings.Contains(err.Error(), "file count exceeds cap") {
			capExceeded = true
		} else {
			return nil, fmt.Errorf("walking repo: %w", err)
		}
	}

	// A repository without an application entry point, or whose manifest
	// declares it a library, exports its public symbols for downstream
	// consumers the reference search cannot see (stringer-nxx.3).
	libManifest, appManifest := manifestKind(repoPath)
	isLibrary := libManifest || (!hasEntryPoint && !appManifest)

	// Pass 2: Tokenize every file once into an inverted index, then resolve
	// each symbol with a lookup instead of a regexp scan over every file.
	c.regexCache = make(map[string]*regexp.Regexp)
	idx := buildSymbolIndex(files, symbols)
	var signals []signal.RawSignal
	deadCount := 0
	publicSuppressed := 0

	for i := range symbols {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		sym := &symbols[i]
		if shouldSkipSymbol(sym.Name) {
			continue
		}

		dead, testOnly := c.isDeadSymbol(sym, idx)
		if !dead && !testOnly {
			continue
		}

		// A library's exported symbols exist for downstream consumers the
		// reference search cannot see; they are counted, not reported,
		// unless include_public_api opts back into the 0.3 tier (stringer-jfh.2).
		publicAPI := isLibrary && sym.Exported && !sym.InInternal && !sym.InTest
		if publicAPI && !opts.IncludePublicAPI {
			publicSuppressed++
			continue
		}
		conf := deadCodeConfidence(sym, testOnly, publicAPI)
		if conf < opts.MinConfidence {
			continue
		}

		title := fmt.Sprintf("Unused %s: %s",
			strings.TrimPrefix(sym.Kind, "unused-"), sym.Name)

		tags := []string{"dead-code", "cleanup-candidate"}
		if testOnly {
			tags = append(tags, "test-only-reference")
		}
		if publicAPI {
			tags = append(tags, "public-api")
		}
		if sym.InTest {
			tags = append(tags, "test-file")
		}

		signals = append(signals, signal.RawSignal{
			Source:     "deadcode",
			Kind:       sym.Kind,
			FilePath:   sym.FilePath,
			Line:       sym.Line,
			Title:      title,
			Confidence: conf,
			Tags:       tags,
		})
		deadCount++
	}

	c.metrics = &DeadCodeMetrics{
		FilesAnalyzed:      fileCount,
		SymbolsFound:       len(symbols),
		DeadSymbols:        deadCount,
		SkippedCapExceeded: capExceeded,
		IsLibrary:          isLibrary,
		PublicSuppressed:   publicSuppressed,
	}
	if publicSuppressed > 0 {
		slog.Info("deadcode: public symbols not reported (library repo; set collectors.deadcode.include_public_api to see them)", "count", publicSuppressed)
	}

	// Enrich signals with timestamps from git log.
	gitRoot := opts.GitRoot
	if gitRoot == "" {
		gitRoot = repoPath
	}
	enrichTimestamps(ctx, gitRoot, signals)

	return signals, nil
}

// readFileContent reads a file and returns its content as a string.
func readFileContent(path string) (string, error) {
	f, err := FS.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck // read-only file

	var sb strings.Builder
	scanner := bufio.NewScanner(f)
	// Increase buffer for large files.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		sb.WriteString(scanner.Text())
		sb.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// extractSymbols finds function and type definitions in file content.
// Definitions that are alive through a mechanism the reference search
// cannot observe are not extracted: Rust fns inside `impl Trait for Type`
// (dispatched through the trait), Rust test fns and #[cfg(test)] blocks
// (unless includeTests), and functions or types registered by a
// decorator/attribute/annotation (routes, handlers, fixtures, tests).
func extractSymbols(content, relPath, ext string, includeTests bool) []symbolDef {
	var syms []symbolDef
	lines := strings.Split(content, "\n")
	inInternal := strings.Contains(relPath, "internal/") || strings.HasPrefix(relPath, "internal/")

	var rc rustLineContext
	if ext == ".rs" {
		rc = scanRustContext(lines)
	}
	// skip reports whether the definition on line i must not be extracted.
	skip := func(i int, isFunc bool) bool {
		if ext == ".rs" {
			// `_name` is Rust's explicit "intentionally unused" convention
			// (compile-time assertions such as `fn _assert_kinds()`).
			if isFunc && (rc.traitImpl[i] || strings.HasPrefix(lines[i][indentWidth(lines[i]):], "fn _")) {
				return true
			}
			if !includeTests && (rc.cfgTest[i] || precededByRustTestAttr(lines, i)) {
				return true
			}
		}
		return precededByDecorator(lines, i, ext)
	}
	add := func(i int, name, kind string) {
		exported := symbolExported(name, lines[i], ext)
		if ext == ".cs" {
			if csharpNeverDead(name, lines, i) {
				return
			}
			exported = csharpExported(lines[i])
		}
		syms = append(syms, symbolDef{
			Name:       name,
			FilePath:   relPath,
			Line:       i + 1,
			Kind:       kind,
			Exported:   exported,
			Language:   ext,
			InInternal: inInternal,
		})
	}

	// Extract functions using existing langSpecs.
	if spec := extToSpec[ext]; spec != nil {
		for i, line := range lines {
			name, _ := matchFuncStart(line, spec, i+1)
			if name == "" || skip(i, true) {
				continue
			}
			add(i, name, "unused-function")
		}
	}

	// Extract types.
	if typePat := typePatterns[ext]; typePat != nil {
		for i, line := range lines {
			matches := typePat.FindStringSubmatch(line)
			if matches == nil || matches[1] == "" || skip(i, false) {
				continue
			}
			add(i, matches[1], "unused-type")
		}
	}

	return syms
}

// isDeadSymbol checks if a symbol has no references outside its definition.
// Returns (dead, testOnly) where testOnly means the only external references
// are in test files.
//
// Semantics are those of matching `\b<name>\b` against every file: more than
// one match in the defining file means the symbol is used locally; any match
// in another non-test file means it is referenced; matches only in test files
// mean it is test-only. Word-only names (the common case) are resolved from
// the token index; names with non-word bytes (Elixir "Foo.Bar", Ruby
// "valid?") fall back to the regexp over index-bounded candidate files.
//
// A symbol defined in a test file (include_tests) is referenced by any other
// file, test or not, since test files are its natural callers.
func (c *DeadCodeCollector) isDeadSymbol(sym *symbolDef, idx *symbolIndex) (dead bool, testOnly bool) {
	if isWordOnly(sym.Name) {
		return idx.lookupToken(sym.Name, sym.FilePath, sym.InTest)
	}
	return idx.lookupRegex(c.wordBoundary(sym.Name), sym.Name, sym.FilePath, sym.InTest)
}

// deadCodeConfidence returns the confidence score for a dead code signal
// based on the symbol's visibility and language context. publicAPI marks
// an exported symbol of a library repository, which downstream consumers
// may reference: it caps confidence at 0.3.
func deadCodeConfidence(sym *symbolDef, testOnly, publicAPI bool) float64 {
	if testOnly || publicAPI {
		return 0.3
	}

	switch sym.Language {
	case ".go":
		if !sym.Exported {
			return 0.7
		}
		if sym.InInternal {
			return 0.6
		}
		return 0.3 // public package export
	case ".rs":
		if !sym.Exported {
			return 0.6
		}
		return 0.4
	case ".py", ".rb":
		if !sym.Exported {
			return 0.5
		}
		return 0.4
	default: // JS/TS, Java
		if !sym.Exported {
			return 0.5
		}
		return 0.4
	}
}

// Metrics returns structured metrics from the dead code scan.
func (c *DeadCodeCollector) Metrics() any { return c.metrics }

// Compile-time interface checks.
var _ collector.Collector = (*DeadCodeCollector)(nil)
var _ collector.MetricsProvider = (*DeadCodeCollector)(nil)
