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
}

// DeadCodeMetrics holds structured metrics from the dead code scan.
type DeadCodeMetrics struct {
	FilesAnalyzed      int
	SymbolsFound       int
	DeadSymbols        int
	SkippedCapExceeded bool
}

// DeadCodeCollector detects unused functions and types using regex-based
// symbol extraction and in-memory reference searching. Follows the
// regex-over-AST philosophy from DR-013/DR-014.
type DeadCodeCollector struct {
	metrics *DeadCodeMetrics
}

// Name returns the collector name used for registration and filtering.
func (c *DeadCodeCollector) Name() string { return "deadcode" }

// typePatterns maps file extensions to regex patterns for type/class/struct
// definitions. Each regex has one capture group for the type name.
var typePatterns = map[string]*regexp.Regexp{
	".go":   regexp.MustCompile(`^\s*type\s+(\w+)\s+(?:struct|interface)\s*\{`),
	".py":   regexp.MustCompile(`^\s*class\s+(\w+)`),
	".js":   regexp.MustCompile(`^\s*(?:export\s+)?class\s+(\w+)`),
	".ts":   regexp.MustCompile(`^\s*(?:export\s+)?(?:class|interface|type)\s+(\w+)`),
	".tsx":  regexp.MustCompile(`^\s*(?:export\s+)?(?:class|interface|type)\s+(\w+)`),
	".jsx":  regexp.MustCompile(`^\s*(?:export\s+)?class\s+(\w+)`),
	".java": regexp.MustCompile(`^\s*(?:(?:public|private|protected|abstract|final|static)\s+)*(?:class|interface|enum)\s+(\w+)`),
	".rs":   regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:struct|enum|trait)\s+(\w+)`),
	".rb":   regexp.MustCompile(`^\s*class\s+(\w+)`),
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
func wordBoundary(name string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
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

	// Pass 1: Walk files, extract symbols, cache content.
	var symbols []symbolDef
	var files []fileContents
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
		if fileCount > defaultFileCountCap {
			return fmt.Errorf("file count exceeds cap (%d)", defaultFileCountCap)
		}

		// Read file content.
		content, readErr := readFileContent(path)
		if readErr != nil {
			return nil
		}

		testFile := isTestFile(relPath)
		files = append(files, fileContents{
			relPath: relPath,
			content: content,
			isTest:  testFile,
		})

		// Don't extract symbols from test files.
		if testFile {
			return nil
		}

		// Extract symbols.
		syms := extractSymbols(content, relPath, ext)
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

	// Pass 2: Search for references to each symbol.
	var signals []signal.RawSignal
	deadCount := 0

	for i := range symbols {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		sym := &symbols[i]
		if shouldSkipSymbol(sym.Name) {
			continue
		}

		dead, testOnly := isDeadSymbol(sym, files)
		if !dead && !testOnly {
			continue
		}

		conf := deadCodeConfidence(sym, testOnly)
		if conf < opts.MinConfidence {
			continue
		}

		title := fmt.Sprintf("Unused %s: %s",
			strings.TrimPrefix(sym.Kind, "unused-"), sym.Name)

		tags := []string{"dead-code", "cleanup-candidate"}
		if testOnly {
			tags = append(tags, "test-only-reference")
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
func extractSymbols(content, relPath, ext string) []symbolDef {
	var syms []symbolDef
	lines := strings.Split(content, "\n")
	inInternal := strings.Contains(relPath, "internal/") || strings.HasPrefix(relPath, "internal/")

	// Extract functions using existing langSpecs.
	spec := extToSpec[ext]
	if spec != nil {
		for i, line := range lines {
			name, _ := matchFuncStart(line, spec, i+1)
			if name == "" {
				continue
			}
			exported := isExported(name, ext)
			// Rust: check if line has "pub" keyword.
			if ext == ".rs" {
				exported = strings.Contains(line, "pub ")
			}
			syms = append(syms, symbolDef{
				Name:       name,
				FilePath:   relPath,
				Line:       i + 1,
				Kind:       "unused-function",
				Exported:   exported,
				Language:   ext,
				InInternal: inInternal,
			})
		}
	}

	// Extract types.
	typePat := typePatterns[ext]
	if typePat != nil {
		for i, line := range lines {
			matches := typePat.FindStringSubmatch(line)
			if matches == nil {
				continue
			}
			name := matches[1]
			if name == "" {
				continue
			}
			exported := isExported(name, ext)
			if ext == ".rs" {
				exported = strings.Contains(line, "pub ")
			}
			syms = append(syms, symbolDef{
				Name:       name,
				FilePath:   relPath,
				Line:       i + 1,
				Kind:       "unused-type",
				Exported:   exported,
				Language:   ext,
				InInternal: inInternal,
			})
		}
	}

	return syms
}

// isDeadSymbol checks if a symbol has no references outside its definition.
// Returns (dead, testOnly) where testOnly means the only external references
// are in test files.
func isDeadSymbol(sym *symbolDef, files []fileContents) (dead bool, testOnly bool) {
	// Fast pre-filter: check if the name appears in any other file.
	pat := wordBoundary(sym.Name)
	foundInNonTest := false
	foundInTest := false

	for i := range files {
		fc := &files[i]

		// Fast pre-filter.
		if !strings.Contains(fc.content, sym.Name) {
			continue
		}

		if fc.relPath == sym.FilePath {
			// Same file: count occurrences. If >1, it's used locally.
			count := len(pat.FindAllStringIndex(fc.content, -1))
			if count > 1 {
				return false, false
			}
			continue
		}

		// Different file: any match means it's referenced.
		if pat.MatchString(fc.content) {
			if fc.isTest {
				foundInTest = true
			} else {
				foundInNonTest = true
			}
		}
	}

	if foundInNonTest {
		return false, false
	}
	if foundInTest {
		return false, true
	}
	return true, false
}

// deadCodeConfidence returns the confidence score for a dead code signal
// based on the symbol's visibility and language context.
func deadCodeConfidence(sym *symbolDef, testOnly bool) float64 {
	if testOnly {
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
