// Package collectors provides signal extraction modules for stringer.
package collectors

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

// todoKeyword maps a recognized keyword to its base confidence score per DR-004.
var todoKeyword = map[string]float64{
	"BUG":      0.7,
	"FIXME":    0.6,
	"HACK":     0.55,
	"TODO":     0.5,
	"XXX":      0.5,
	"OPTIMIZE": 0.4,
}

// todoPattern matches TODO-style comments in common programming languages.
//
// Supported formats:
//
//	// TODO: message        (C/Go/Java/JS single-line)
//	// TODO(author): msg    (Go convention)
//	# TODO: message         (Python/Ruby/Shell)
//	/* TODO: message */      (C-style block)
//	* TODO: message          (Javadoc/JSDoc)
//	-- TODO: message         (SQL/Haskell)
//
// The keyword match is case-insensitive.
var todoPattern = regexp.MustCompile(
	`(?i)(?://|#|/\*|\*|--)\s*` + // comment prefix
		`(TODO|FIXME|HACK|XXX|BUG|OPTIMIZE)\b` + // keyword (captured, word boundary prevents TODOIST etc.)
		`(?:\([^)]*\))?` + // optional (author)
		`\s*[:>\-]?\s*` + // optional separator
		`(.*)`, // message (captured)
)

// defaultExcludePatterns are directory/file globs skipped unless overridden.
var defaultExcludePatterns = []string{
	"vendor/**",
	"node_modules/**",
	".git/**",
	"testdata/**",
}

func init() {
	collector.Register(&TodoCollector{})
}

// TodoMetrics holds structured metrics from the TODO scan.
type TodoMetrics struct {
	Total         int
	ByKind        map[string]int
	WithTimestamp int
}

// TodoCollector scans repository files for TODO, FIXME, HACK, XXX, BUG, and
// OPTIMIZE comments, enriches them with git blame data, and produces scored
// RawSignal values.
type TodoCollector struct {
	metrics *TodoMetrics
}

// Name returns the collector name used for registration and filtering.
func (c *TodoCollector) Name() string { return "todos" }

// Collect walks source files in repoPath, extracts TODO-style comments, and
// returns them as raw signals with confidence scores and blame attribution.
func (c *TodoCollector) Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	excludes := mergeExcludes(opts.ExcludePatterns)

	// Open the git repo once for blame lookups.
	// Use GitRoot if set (subdirectory scans), otherwise fall back to repoPath.
	gitRoot := repoPath
	if opts.GitRoot != "" {
		gitRoot = opts.GitRoot
	}
	repo, err := git.PlainOpen(gitRoot)
	if err != nil {
		// If it's not a git repo, we can still scan files, just without blame.
		repo = nil
	}

	var signals []signal.RawSignal
	var fileCount int

	err = filepath.WalkDir(repoPath, func(path string, d os.DirEntry, walkErr error) error {
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

		// Skip symlinks that resolve outside the repo tree to prevent traversal.
		if d.Type()&os.ModeSymlink != 0 {
			resolved, resolveErr := filepath.EvalSymlinks(path)
			if resolveErr != nil {
				return nil // skip unresolvable symlinks
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

		found, scanErr := scanFile(path, relPath)
		if scanErr != nil {
			return nil // skip files we can't read
		}

		// For blame, we need the path relative to gitRoot (not repoPath).
		blameRelPath := relPath
		if gitRoot != repoPath {
			blameRelPath, _ = filepath.Rel(gitRoot, path)
		}

		for i := range found {
			enrichWithBlame(repo, blameRelPath, &found[i])
			found[i].Confidence = computeConfidence(found[i])
		}

		signals = append(signals, found...)

		fileCount++
		if opts.ProgressFunc != nil && fileCount%500 == 0 {
			opts.ProgressFunc(fmt.Sprintf("todos: scanned %d files", fileCount))
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking repo: %w", err)
	}

	// Build metrics from collected signals.
	byKind := make(map[string]int)
	withTimestamp := 0
	for _, sig := range signals {
		byKind[sig.Kind]++
		if !sig.Timestamp.IsZero() {
			withTimestamp++
		}
	}
	c.metrics = &TodoMetrics{
		Total:         len(signals),
		ByKind:        byKind,
		WithTimestamp: withTimestamp,
	}

	return signals, nil
}

// scanFile reads a file line by line and extracts TODO-style signals.
func scanFile(absPath, relPath string) ([]signal.RawSignal, error) {
	f, err := os.Open(absPath) //nolint:gosec // path is from filepath.WalkDir
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only file, close error is inconsequential

	var signals []signal.RawSignal
	scanner := bufio.NewScanner(f)
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		line := scanner.Text()

		matches := todoPattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		keyword := strings.ToUpper(matches[1])
		message := strings.TrimSpace(matches[2])
		// Strip trailing block-comment close if present.
		message = strings.TrimSuffix(message, "*/")
		message = strings.TrimSpace(message)

		if message == "" {
			message = keyword + " comment (no description)"
		}

		kind := strings.ToLower(keyword)

		signals = append(signals, signal.RawSignal{
			Source:   "todos",
			Kind:     kind,
			FilePath: relPath,
			Line:     lineNo,
			Title:    fmt.Sprintf("%s: %s", keyword, message),
			Tags:     []string{kind},
		})
	}

	if err := scanner.Err(); err != nil {
		return signals, err
	}

	return signals, nil
}

// enrichWithBlame populates Author and Timestamp from git blame if available.
func enrichWithBlame(repo *git.Repository, relPath string, sig *signal.RawSignal) {
	if repo == nil {
		return
	}

	result, err := blameFile(repo, relPath)
	if err != nil || result == nil {
		return
	}

	// Lines are 0-indexed in go-git blame result.
	idx := sig.Line - 1
	if idx < 0 || idx >= len(result.Lines) {
		return
	}

	blameLine := result.Lines[idx]
	// Prefer AuthorName (human-readable) over Author (email).
	if blameLine.AuthorName != "" {
		sig.Author = blameLine.AuthorName
	} else {
		sig.Author = blameLine.Author
	}
	sig.Timestamp = blameLine.Date
}

// blameFile returns the git blame result for the given file path relative to
// the repo root.
func blameFile(repo *git.Repository, relPath string) (*git.BlameResult, error) {
	head, err := repo.Head()
	if err != nil {
		return nil, err
	}

	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, err
	}

	// go-git blame uses forward slashes.
	blameResult, err := git.Blame(commit, filepath.ToSlash(relPath))
	if err != nil {
		return nil, err
	}

	return blameResult, nil
}

// computeConfidence calculates the confidence score per DR-004:
//   - Base score from keyword
//   - Age boost: +0.1 if > 6 months, +0.2 if > 1 year
//   - Capped at 1.0
func computeConfidence(sig signal.RawSignal) float64 {
	keyword := strings.ToUpper(sig.Kind)
	base, ok := todoKeyword[keyword]
	if !ok {
		base = 0.5
	}

	score := base

	if !sig.Timestamp.IsZero() {
		age := time.Since(sig.Timestamp)
		sixMonths := 6 * 30 * 24 * time.Hour // ~180 days
		oneYear := 365 * 24 * time.Hour      // ~365 days

		if age > oneYear {
			score += 0.2
		} else if age > sixMonths {
			score += 0.1
		}
	}

	return math.Min(score, 1.0)
}

// shouldExclude returns true if relPath matches any of the exclude patterns.
func shouldExclude(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, relPath)
		if err == nil && matched {
			return true
		}
		// Match the pattern against just the filename for non-path patterns
		// like "*.min.js" that should apply to files in any directory.
		if !strings.Contains(pattern, "/") && !strings.Contains(pattern, "**") {
			matched, err = filepath.Match(pattern, filepath.Base(relPath))
			if err == nil && matched {
				return true
			}
		}
		// Handle ** patterns: "vendor/**" should match vendor/ and anything below.
		if strings.HasSuffix(pattern, "/**") {
			dir := strings.TrimSuffix(pattern, "/**")
			if relPath == dir || strings.HasPrefix(relPath, dir+string(filepath.Separator)) {
				return true
			}
		}
	}
	return false
}

// matchesAny returns true if relPath matches any of the given glob patterns.
func matchesAny(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := filepath.Match(pattern, relPath)
		if err == nil && matched {
			return true
		}
		// Match against just the filename for non-path patterns.
		if !strings.Contains(pattern, "/") && !strings.Contains(pattern, "**") {
			matched, err = filepath.Match(pattern, filepath.Base(relPath))
			if err == nil && matched {
				return true
			}
		}
		// Handle ** patterns by checking prefix.
		if strings.Contains(pattern, "**") {
			prefix := strings.Split(pattern, "**")[0]
			suffix := strings.Split(pattern, "**")[1]
			suffix = strings.TrimPrefix(suffix, "/")
			if strings.HasPrefix(relPath, prefix) {
				if suffix == "" {
					return true
				}
				rest := strings.TrimPrefix(relPath, prefix)
				matched, err = filepath.Match(suffix, filepath.Base(rest))
				if err == nil && matched {
					return true
				}
			}
		}
	}
	return false
}

// mergeExcludes returns the union of default and user-provided exclude patterns.
// User patterns are appended to (not replacing) the defaults.
func mergeExcludes(userPatterns []string) []string {
	merged := make([]string, len(defaultExcludePatterns))
	copy(merged, defaultExcludePatterns)
	merged = append(merged, userPatterns...)
	return merged
}

// isBinaryFile returns true if the file appears to contain binary content.
// It reads the first 512 bytes and checks for null bytes.
func isBinaryFile(path string) bool {
	f, err := os.Open(path) //nolint:gosec // path is from filepath.WalkDir
	if err != nil {
		return true // treat unreadable as binary to skip
	}
	defer f.Close() //nolint:errcheck // read-only file, close error is inconsequential

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return true
	}

	for _, b := range buf[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}

// Metrics returns structured metrics from the TODO scan.
func (c *TodoCollector) Metrics() any { return c.metrics }

// Compile-time interface checks.
var _ collector.Collector = (*TodoCollector)(nil)
var _ collector.MetricsProvider = (*TodoCollector)(nil)
