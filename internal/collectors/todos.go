// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

// Package collectors provides signal extraction modules for stringer.
package collectors

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/gitcli"
	"github.com/davetashner/stringer/internal/signal"
	"github.com/davetashner/stringer/internal/testable"
)

// FS is the file system implementation used by this package.
// Override in tests with a testable.MockFileSystem.
var FS testable.FileSystem = testable.DefaultFS

// todoKeyword maps a recognized keyword to its base confidence score per DR-004.
var todoKeyword = map[string]float64{
	"BUG":      0.8,
	"FIXME":    0.65,
	"HACK":     0.55,
	"TODO":     0.5,
	"XXX":      0.45,
	"OPTIMIZE": 0.35,
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

// defaultExcludePatterns are directory/file globs skipped by every collector
// unless a user opts back in with a negated exclude pattern (e.g. "!**/*.pb.go").
var defaultExcludePatterns = []string{
	"vendor/**",
	"node_modules/**",
	".git/**",
	"testdata/**",
	"CHANGELOG*",
	"CHANGES*",
	"HISTORY*",
	"NEWS*",
	"third_party/**",
	"3rdparty/**",
	"extern/**",
	"external/**",
	"bower_components/**",
	"wwwroot/lib/**",
	"eval/**",
	".beads/**",
	".stringer/**",
	".claude/worktrees/**",
	// Vendored precompiled bundles and minified assets: never hand-edited, so
	// every complexity/duplication/todo signal in them is noise.
	"**/compiled/**",
	"**/*.min.js",
	"**/*.min.css",
	"**/*.bundle.js",
	"**/*.production.js",
	"**/*.development.js",
	// Machine-generated source (Go codegen, protobuf, C# designers, …).
	"**/zz_generated*.go",
	"**/*.pb.go",
	"**/*_string.go",
	"**/*.generated.*",
	"**/*.g.cs",
	"**/*.Designer.cs",
}

// excludeNegationPrefix marks a user exclude pattern that removes a default
// exclude instead of adding one: "!**/*.min.js" re-enables minified JS.
const excludeNegationPrefix = "!"

// defaultDemoPatterns are directory globs for demo/example/tutorial paths.
// Noise-prone signal kinds (missing-tests, low-test-ratio, low-lottery-risk)
// are suppressed in these paths by default unless IncludeDemoPaths is set.
var defaultDemoPatterns = []string{
	"examples/**",
	"example/**",
	"tutorials/**",
	"tutorial/**",
	"demos/**",
	"demo/**",
	"samples/**",
	"sample/**",
	"_examples/**",
	// Non-source directories where tests are not expected.
	"docs/**",
	"doc/**",
	"extras/**",
	"packaging/**",
	"scripts/**",
	"tools/**",
	"contrib/**",
	"misc/**",
	"build/**",
	"deploy/**",
	".github/**",
	".ci/**",
}

// isDemoPath returns true if relPath falls under a demo/example/tutorial directory.
func isDemoPath(relPath string) bool {
	return shouldExclude(relPath, defaultDemoPatterns)
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

	// Determine git root for blame lookups.
	// Use GitRoot if set (subdirectory scans), otherwise fall back to repoPath.
	gitRoot := repoPath
	if opts.GitRoot != "" {
		gitRoot = opts.GitRoot
	}
	gitDir := ""
	if gitcli.Available() == nil && isGitRepo(gitRoot) {
		gitDir = gitRoot
	}

	var signals []signal.RawSignal
	var fileCount int

	err := FS.WalkDir(repoPath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		relPath, relErr := relSlash(repoPath, path)
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
		if d.Type()&os.ModeSymlink != 0 && isSymlinkOutsideRepo(path, repoPath) {
			return nil
		}

		// Apply include-pattern filtering if patterns are set.
		if len(opts.IncludePatterns) > 0 && !matchesAny(relPath, opts.IncludePatterns) {
			return nil
		}

		// Skip binary files.
		if isBinaryFile(path) {
			return nil
		}

		// Skip generated and minified files unless the user explicitly
		// asked for them via include patterns.
		if len(opts.IncludePatterns) == 0 && isGeneratedFile(path) {
			return nil
		}

		found, scanErr := scanFile(path, relPath)
		if scanErr != nil {
			return nil // skip files we can't read
		}

		// For blame, we need the path relative to gitRoot (not repoPath).
		blameRelPath := relPath
		if gitRoot != repoPath {
			blameRelPath, _ = filepath.Rel(gitRoot, path) //nolint:errcheck // best-effort relative path; falls back to absolute
		}

		for i := range found {
			enrichWithBlame(ctx, gitDir, blameRelPath, &found[i], path)
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

// isInsideStringLiteral walks line up to matchStart, tracking whether we are
// inside a single-quoted, double-quoted, or backtick string literal (respecting
// backslash escapes).  Returns true if matchStart falls inside a string.
func isInsideStringLiteral(line string, matchStart int) bool {
	inSingle, inDouble, inBacktick := false, false, false
	for i := 0; i < matchStart && i < len(line); i++ {
		if line[i] == '\\' && i+1 < matchStart {
			i++ // skip escaped character
			continue
		}
		switch line[i] {
		case '\'':
			if !inDouble && !inBacktick {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle && !inBacktick {
				inDouble = !inDouble
			}
		case '`':
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
		}
	}
	return inSingle || inDouble || inBacktick
}

// scanFile reads a file line by line and extracts TODO-style signals.
func scanFile(absPath, relPath string) ([]signal.RawSignal, error) {
	f, err := FS.Open(absPath)
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

		loc := todoPattern.FindStringSubmatchIndex(line)
		if loc == nil {
			continue
		}

		// Skip matches that fall inside string literals (e.g. '.get("//todo@txt")').
		if isInsideStringLiteral(line, loc[0]) {
			continue
		}

		keyword := strings.ToUpper(line[loc[2]:loc[3]])
		message := strings.TrimSpace(line[loc[4]:loc[5]])
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
// Uses native git CLI with a per-line blame for efficiency (DR-011).
// When blame fails (e.g. shallow clones), falls back to the file's mtime
// and tags the signal with "estimated-timestamp".
func enrichWithBlame(ctx context.Context, gitDir string, relPath string, sig *signal.RawSignal, absPath string) {
	if gitDir == "" {
		return
	}

	if sig.Line <= 0 {
		return
	}

	blameCtx, cancel := context.WithTimeout(ctx, gitcli.DefaultTimeout)
	bl, err := gitcli.BlameSingleLine(blameCtx, gitDir, filepath.ToSlash(relPath), sig.Line)
	cancel()

	if err != nil || bl == nil {
		// Blame failed — fall back to file mtime.
		if info, statErr := FS.Stat(absPath); statErr == nil {
			sig.Timestamp = info.ModTime()
			sig.Tags = append(sig.Tags, "estimated-timestamp")
		}
		return
	}

	if bl.AuthorName != "" {
		sig.Author = bl.AuthorName
	}
	sig.Timestamp = bl.AuthorTime
}

// isGitRepo returns true if dir contains a .git directory or file.
func isGitRepo(dir string) bool {
	_, err := FS.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// computeConfidence calculates the confidence score per DR-004:
//   - Base score from keyword
//   - Recency boost: +0.1 if < 30 days old
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
		thirtyDays := 30 * 24 * time.Hour

		if age < thirtyDays {
			score += 0.1
		}
	}

	return math.Min(score, 1.0)
}

// shouldExclude returns true if relPath matches any of the exclude patterns.
func shouldExclude(relPath string, patterns []string) bool {
	relPath = filepath.ToSlash(relPath)
	for _, pattern := range patterns {
		if matchesExcludePattern(relPath, pattern) {
			return true
		}
	}
	return false
}

// matchesExcludePattern reports whether a single exclude glob matches relPath.
//
// Supported forms:
//
//	vendor/**        directory at the root or at any interior depth
//	*.min.js         filename at any depth
//	**/compiled/**   same as compiled/** (explicit any-depth prefix)
//	**/gen/*.go      path suffix at any depth
//	/api/**          anchored: the api directory at the root only, never
//	                 an interior internal/api (used for nested workspaces)
//	/docs/*.md       anchored: a glob matched against the whole path only
//
// Patterns are slash-separated, so relPath is matched in slash form on every
// OS (callers may pass filepath.Rel output, which uses `\` on Windows).
func matchesExcludePattern(relPath, pattern string) bool {
	relPath = filepath.ToSlash(relPath)
	const sep = "/"
	if anchored, ok := strings.CutPrefix(pattern, "/"); ok {
		return matchesAnchoredExclude(relPath, anchored)
	}
	if rest, ok := strings.CutPrefix(pattern, "**/"); ok {
		if matchesExcludePattern(relPath, rest) {
			return true
		}
		// Path patterns like "gen/*.go" must also match at every depth.
		if strings.Contains(rest, "/") && !strings.HasSuffix(rest, "/**") {
			segs := strings.Split(relPath, sep)
			for i := 1; i < len(segs); i++ {
				if matched, err := path.Match(rest, strings.Join(segs[i:], sep)); err == nil && matched {
					return true
				}
			}
		}
		return false
	}
	matched, err := path.Match(pattern, relPath)
	if err == nil && matched {
		return true
	}
	// Match the pattern against just the filename for non-path patterns
	// like "*.min.js" that should apply to files in any directory.
	if !strings.Contains(pattern, "/") && !strings.Contains(pattern, "**") {
		matched, err = path.Match(pattern, path.Base(relPath))
		if err == nil && matched {
			return true
		}
	}
	// Handle ** patterns: "vendor/**" should match vendor/ and anything below.
	if strings.HasSuffix(pattern, "/**") {
		dir := strings.TrimSuffix(pattern, "/**")
		// Match at root: vendor/foo.go
		if relPath == dir || strings.HasPrefix(relPath, dir+sep) {
			return true
		}
		// Match interior segments: "wwwroot/lib/**" matches
		// "samples/foo/wwwroot/lib/bootstrap.js"
		if strings.Contains(relPath, sep+dir+sep) || strings.HasSuffix(relPath, sep+dir) {
			return true
		}
	}
	return false
}

// matchesAnchoredExclude matches pattern (an exclude glob with its leading
// "/" removed) against relPath from the walk root only. "dir/**" covers the
// directory itself and everything below it; any other glob is matched
// against the whole path, never against interior segments or the basename.
func matchesAnchoredExclude(relPath, pattern string) bool {
	if dir, ok := strings.CutSuffix(pattern, "/**"); ok {
		return relPath == dir || strings.HasPrefix(relPath, dir+"/")
	}
	matched, err := path.Match(pattern, relPath)
	return err == nil && matched
}

// matchesAny returns true if relPath matches any of the given glob patterns.
func matchesAny(relPath string, patterns []string) bool {
	relPath = filepath.ToSlash(relPath)
	for _, pattern := range patterns {
		matched, err := path.Match(pattern, relPath)
		if err == nil && matched {
			return true
		}
		// Match against just the filename for non-path patterns.
		if !strings.Contains(pattern, "/") && !strings.Contains(pattern, "**") {
			matched, err = path.Match(pattern, path.Base(relPath))
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
				matched, err = path.Match(suffix, path.Base(rest))
				if err == nil && matched {
					return true
				}
			}
		}
	}
	return false
}

// mergeExcludes returns the union of default and user-provided exclude patterns.
// User patterns are appended to (not replacing) the defaults. A user pattern
// prefixed with "!" removes the identically-spelled default instead, so
// "!**/*.pb.go" opts protobuf output back in.
func mergeExcludes(userPatterns []string) []string {
	negated := make(map[string]bool)
	for _, p := range userPatterns {
		if neg, ok := strings.CutPrefix(p, excludeNegationPrefix); ok {
			negated[neg] = true
		}
	}
	merged := make([]string, 0, len(defaultExcludePatterns)+len(userPatterns))
	for _, p := range defaultExcludePatterns {
		if !negated[p] {
			merged = append(merged, p)
		}
	}
	for _, p := range userPatterns {
		if !strings.HasPrefix(p, excludeNegationPrefix) {
			merged = append(merged, p)
		}
	}
	return merged
}

// isBinaryFile returns true if the file appears to contain binary content.
// It reads the first 512 bytes and checks for null bytes.
func isBinaryFile(path string) bool {
	f, err := FS.Open(path)
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
