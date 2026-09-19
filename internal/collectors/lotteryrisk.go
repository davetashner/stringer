// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/gitcli"
	"github.com/davetashner/stringer/internal/signal"
	"github.com/davetashner/stringer/internal/testable"
)

// defaultLotteryRiskThreshold is the lottery risk threshold below or at which a
// signal is emitted. Directories with lottery risk <= this value are flagged.
const defaultLotteryRiskThreshold = 1

// defaultDirectoryDepth is how many levels deep to walk when collecting
// directory-level ownership data.
const defaultDirectoryDepth = 2

// defaultMaxBlameFiles caps the number of source files blamed per directory
// to keep blame cost bounded.
const defaultMaxBlameFiles = 50

// decayHalfLifeDays is the half-life in days for the exponential recency
// decay applied to commit weights.
const decayHalfLifeDays = 180

// blameWeight is the fraction of ownership attributed to blame lines.
const blameWeight = 0.6

// commitWeight is the fraction of ownership attributed to commit activity.
const commitWeightFraction = 0.4

// ownershipMajority is the threshold for combined ownership to determine
// lottery risk (min authors exceeding this fraction).
const ownershipMajority = 0.5

// reviewConcentrationThreshold is the fraction of reviews above which a
// single reviewer is flagged as a review bottleneck.
const reviewConcentrationThreshold = 0.7

// maxReviewPRs caps the number of merged PRs examined for review analysis.
const maxReviewPRs = 50

// blameWorkers is the number of concurrent git-blame subprocesses.
const blameWorkers = 8

func init() {
	collector.Register(&LotteryRiskCollector{})
}

// LotteryRiskMetrics holds structured metrics from the lottery risk analysis.
type LotteryRiskMetrics struct {
	Directories []DirectoryOwnership
}

// DirectoryOwnership describes ownership distribution for a single directory.
type DirectoryOwnership struct {
	Path        string
	LotteryRisk int
	Authors     []AuthorShare
	TotalLines  int
}

// AuthorShare describes a single author's ownership share of a directory.
type AuthorShare struct {
	Name      string
	Ownership float64
}

// LotteryRiskCollector analyzes git blame and commit history to identify
// directories with low lottery risk (single-author ownership risk).
type LotteryRiskCollector struct {
	// ghCtx is set during testing to inject a mock GitHub API.
	ghCtx *githubContext

	// metrics stores structured ownership data for all analyzed directories.
	metrics *LotteryRiskMetrics
}

// authorStats tracks per-author contribution metrics within a directory.
type authorStats struct {
	BlameLines   int
	CommitWeight float64
}

// dirOwnership holds aggregated ownership data for a single directory.
type dirOwnership struct {
	Path        string
	Authors     map[string]*authorStats
	TotalLines  int
	SourceFiles int // source files found in the directory (before the blame cap)
	LotteryRisk int
}

// Name returns the collector name used for registration and filtering.
func (c *LotteryRiskCollector) Name() string { return "lotteryrisk" }

// Collect scans the repository at repoPath for directories with low bus
// factor and returns them as raw signals.
func (c *LotteryRiskCollector) Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	// Use GitRoot if set (subdirectory scans), otherwise fall back to repoPath.
	gitRoot := repoPath
	if opts.GitRoot != "" {
		gitRoot = opts.GitRoot
	}

	// Check context before starting work.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	excludes := mergeExcludes(opts.ExcludePatterns)

	// In a monorepo scan every workspace shares the repository history, so
	// directories of nested member workspaces are left to those workspaces
	// and commit activity is attributed only to this workspace's own files.
	scope := newWorkspaceScope(repoPath, gitRoot, opts.WorkspaceMembers)

	// Discover directories up to the configured depth.
	dirs, err := discoverDirectories(ctx, repoPath, defaultDirectoryDepth, excludes, opts.IncludeDemoPaths, scope)
	if err != nil {
		return nil, fmt.Errorf("discovering directories: %w", err)
	}

	// Build per-directory ownership from blame.
	ownership := make(map[string]*dirOwnership)
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ownership[dir] = &dirOwnership{
			Path:    dir,
			Authors: make(map[string]*authorStats),
		}
	}

	// Blame source files and attribute lines to directories.
	if err := blameDirectories(ctx, repoPath, ownership, defaultMaxBlameFiles, excludes, opts, scope); err != nil {
		return nil, fmt.Errorf("blaming files: %w", err)
	}

	// Walk commits and attribute weighted commit activity to directories.
	// Commit weight is scoped per directory: each changed file credits only
	// its owning directory (see findOwningDir).
	if err := walkCommitsForOwnership(ctx, gitRoot, ownership, opts, scope); err != nil {
		return nil, fmt.Errorf("walking commits for ownership: %w", err)
	}

	// Shallow clones over-report ownership; detect once and annotate signals.
	hist := detectHistory(ctx, gitRoot)
	if hist.Shallow {
		slog.Warn("lottery risk: shallow clone detected, ownership is over-estimated and confidence is capped",
			"commits", hist.Commits, "cap", shallowConfidenceCap)
	}

	// Resolve anonymization mode.
	ghCtx := c.ghCtx
	if ghCtx == nil {
		ghCtx = newGitHubContext(repoPath)
	}
	var anon *nameAnonymizer
	if resolveAnonymize(ctx, ghCtx, opts.Anonymize) {
		anon = newNameAnonymizer()
	}

	// Compute lottery risk for each directory and build signals + metrics.
	var signals []signal.RawSignal
	var metricsDirectories []DirectoryOwnership
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		own := ownership[dir]
		if own.TotalLines == 0 && totalCommitWeight(own) == 0 {
			continue // skip empty directories
		}

		bf := computeLotteryRisk(own)
		own.LotteryRisk = bf

		// Build metrics entry for every non-empty directory.
		metricsDirectories = append(metricsDirectories, buildDirectoryOwnership(own))

		if bf <= defaultLotteryRiskThreshold && hasMinimumSubstance(own) {
			sig := buildLotteryRiskSignal(own, anon)
			if hist.Shallow {
				applyShallowCaveat(&sig, hist.Commits)
			}
			signals = append(signals, sig)
		}
	}

	c.metrics = &LotteryRiskMetrics{Directories: metricsDirectories}

	// Review participation analysis via GitHub API (optional).
	if ghCtx != nil {
		reviewData, reviewErr := fetchReviewParticipation(ctx, ghCtx, ownership, maxReviewPRs)
		if reviewErr != nil {
			slog.Warn("review participation analysis failed, continuing without it", "error", reviewErr)
		} else {
			reviewSignals := buildReviewConcentrationSignals(reviewData, anon)
			signals = append(signals, reviewSignals...)
		}
	}

	// Sort by directory path for deterministic output.
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].FilePath < signals[j].FilePath
	})

	// Enrich signals with timestamps from git log. Signal paths are relative
	// to repoPath, so git runs there.
	enrichTimestamps(ctx, repoPath, signals)

	return signals, nil
}

// discoverDirectories walks the repo and returns unique directory paths
// up to the given depth (relative to repoPath). The root directory "." is
// included. Directories matching excludes or demo patterns, and member
// workspaces nested in scope, are skipped.
func discoverDirectories(ctx context.Context, repoPath string, maxDepth int, excludes []string, includeDemoPaths bool, scope workspaceScope) ([]string, error) {
	dirSet := make(map[string]bool)
	dirSet["."] = true

	err := filepath.WalkDir(repoPath, func(path string, d os.DirEntry, walkErr error) error {
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

		if !d.IsDir() {
			return nil
		}

		// Skip hidden directories.
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") && relPath != "." {
			return filepath.SkipDir
		}

		// Skip directories matching exclude patterns.
		if shouldExclude(relPath, excludes) || scope.nestedDir(relPath) {
			return filepath.SkipDir
		}

		// Skip demo paths unless opted in.
		if !includeDemoPaths && isDemoPath(relPath) {
			return filepath.SkipDir
		}

		depth := strings.Count(relPath, string(filepath.Separator))
		if relPath != "." {
			depth++ // "internal" is depth 1, "internal/collectors" is depth 2
		}

		if depth > maxDepth {
			return filepath.SkipDir
		}

		dirSet[relPath] = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	dirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// blameFile holds a file to be blamed and its owning directory.
type blameFile struct {
	relPath   string
	owningDir string
}

// blameDirectories blames source files and attributes line counts to their
// containing directories. It caps blame at maxFiles per directory.
// Uses native git CLI for blame (DR-011) with parallel workers for performance.
// git runs in repoPath, so workspace-relative paths resolve even when the
// repository root is a parent directory.
func blameDirectories(ctx context.Context, repoPath string, ownership map[string]*dirOwnership, maxFiles int, excludes []string, opts signal.CollectorOpts, scope workspaceScope) error {
	// Phase 1: Walk the filesystem to collect files to blame.
	dirFileCount := make(map[string]int)
	var files []blameFile

	err := filepath.WalkDir(repoPath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		if d.IsDir() {
			base := filepath.Base(path)
			if strings.HasPrefix(base, ".") {
				relPath, _ := filepath.Rel(repoPath, path)
				if relPath != "." {
					return filepath.SkipDir
				}
			}
			relPath, _ := filepath.Rel(repoPath, path)
			if shouldExclude(relPath, excludes) || scope.nestedDir(relPath) {
				return filepath.SkipDir
			}
			if !opts.IncludeDemoPaths && isDemoPath(relPath) {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, relErr := filepath.Rel(repoPath, path)
		if relErr != nil {
			return nil
		}

		if isBinaryFile(path) {
			return nil
		}

		ext := filepath.Ext(path)
		if !isSourceExtension(ext) {
			return nil
		}

		dir := findOwningDir(relPath, ownership)
		if dir == "" {
			return nil
		}
		ownership[dir].SourceFiles++

		if dirFileCount[dir] >= maxFiles {
			return nil
		}
		dirFileCount[dir]++

		files = append(files, blameFile{relPath: relPath, owningDir: dir})
		return nil
	})
	if err != nil {
		return err
	}

	// Phase 2: Blame files in parallel.
	var mu sync.Mutex
	var blamed int64

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(blameWorkers)

	for _, f := range files {
		f := f // capture
		g.Go(func() error {
			blameCtx, cancel := context.WithTimeout(gctx, gitcli.DefaultTimeout)
			blameResult, blameErr := gitcli.BlameFile(blameCtx, repoPath, filepath.ToSlash(f.relPath))
			cancel()
			if blameErr != nil {
				return nil // skip files that can't be blamed
			}

			mu.Lock()
			own := ownership[f.owningDir]
			for _, bl := range blameResult {
				author := bl.AuthorName
				if author == "" {
					continue
				}

				if own.Authors[author] == nil {
					own.Authors[author] = &authorStats{}
				}
				own.Authors[author].BlameLines++
				own.TotalLines++
			}
			blamed++
			if opts.ProgressFunc != nil && blamed%50 == 0 {
				opts.ProgressFunc(fmt.Sprintf("lotteryrisk: blamed %d files", blamed))
			}
			mu.Unlock()

			return nil
		})
	}

	return g.Wait()
}

// numstatHistoryKey identifies one `git log --numstat` walk: the repository,
// its HEAD and the depth/since window. Any change to these forces a new walk.
type numstatHistoryKey struct {
	gitDir string
	head   string
	depth  int
	since  string
}

// numstatHistories memoises the most recent numstat walk so that a monorepo
// scan runs `git log --numstat` once per repository rather than once per
// workspace (stringer-jfh.5). The commits are shared read-only.
var numstatHistories historyMemo[numstatHistoryKey, []gitcli.NumstatCommit]

// resetNumstatHistoryCache drops the memoised numstat walk. Tests call it to
// force a fresh walk.
func resetNumstatHistoryCache() { numstatHistories.reset() }

// repoHeadHash returns the HEAD commit hash of the repository at gitDir, or
// "" when it cannot be resolved (empty repository, not a repository). It is
// only used to key history memos, so failures are not errors.
func repoHeadHash(gitDir string) string {
	repo, err := testable.DefaultGitOpener.PlainOpen(gitDir)
	if err != nil {
		return ""
	}
	head, err := repo.Head()
	if err != nil {
		return ""
	}
	return head.Hash().String()
}

// walkCommitsForOwnership runs `git log --numstat` and applies recency-weighted
// attribution to directories based on changed files. This replaced the earlier
// go-git tree-diff approach for performance (DR-011). The numstat output is
// memoised per repository so every workspace of a monorepo scan shares it;
// numstat paths are repository-relative and are mapped into scope, so only
// changes to this workspace's own files count.
func walkCommitsForOwnership(ctx context.Context, gitDir string, ownership map[string]*dirOwnership, opts signal.CollectorOpts, scope workspaceScope) error {
	maxWalk := maxCommitWalk
	if opts.GitDepth > 0 {
		maxWalk = opts.GitDepth
	}

	key := numstatHistoryKey{
		gitDir: filepath.Clean(gitDir),
		head:   repoHeadHash(gitDir),
		depth:  maxWalk,
		since:  opts.GitSince,
	}
	commits, err := numstatHistories.load(ctx, key, func(ctx context.Context) ([]gitcli.NumstatCommit, error) {
		var since string
		if opts.GitSince != "" {
			if d, parseErr := ParseDuration(opts.GitSince); parseErr == nil {
				since = time.Now().Add(-d).Format(time.RFC3339)
			}
		}
		return gitcli.LogNumstat(ctx, gitDir, maxWalk, since)
	})
	if err != nil {
		errMsg := err.Error()
		// Empty repos, shallow clones, and other non-fatal git errors —
		// degrade gracefully. git returns exit status 128 for empty repos
		// ("does not have any commits") and similar conditions.
		if strings.Contains(errMsg, "does not have any commits") ||
			strings.Contains(errMsg, "bad default revision") ||
			strings.Contains(errMsg, "object not found") ||
			strings.Contains(errMsg, "exit status 128") {
			slog.Warn("lottery risk: limited git history detected, commit-weighted ownership unavailable (shallow or empty clone?)", "error", err)
			return nil
		}
		return fmt.Errorf("git log --numstat: %w", err)
	}

	now := time.Now()

	for i, c := range commits {
		if err := ctx.Err(); err != nil {
			return err
		}

		if opts.ProgressFunc != nil && (i+1)%100 == 0 {
			opts.ProgressFunc(fmt.Sprintf("lotteryrisk: examined %d commits", i+1))
		}

		author := c.Author
		if author == "" {
			continue
		}

		daysOld := now.Sub(c.AuthorTime).Hours() / 24
		weight := recencyDecay(daysOld)

		for _, f := range c.Files {
			f, ok := scope.relPath(f)
			if !ok {
				continue
			}
			dir := findOwningDir(f, ownership)
			if dir == "" {
				continue
			}

			own := ownership[dir]
			if own.Authors[author] == nil {
				own.Authors[author] = &authorStats{}
			}
			own.Authors[author].CommitWeight += weight
		}
	}

	return nil
}

// recencyDecay computes the exponential decay weight for a commit that is
// daysOld days in the past. weight = e^(-ln2/halfLife * daysOld).
func recencyDecay(daysOld float64) float64 {
	if daysOld < 0 {
		daysOld = 0
	}
	return math.Exp(-math.Ln2 / float64(decayHalfLifeDays) * daysOld)
}

// findOwningDir returns the most specific directory in the ownership map
// that contains the given file path, or empty string if none match.
func findOwningDir(relPath string, ownership map[string]*dirOwnership) string {
	dir := filepath.Dir(relPath)

	// Walk up from the file's directory to find the deepest matching dir.
	for dir != "" {
		if _, ok := ownership[dir]; ok {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Check root.
	if _, ok := ownership["."]; ok {
		return "."
	}

	return ""
}

// isSourceExtension returns true for file extensions considered source code.
func isSourceExtension(ext string) bool {
	return sourceExtensions[ext]
}

// Metrics returns structured ownership data for all analyzed directories.
func (c *LotteryRiskCollector) Metrics() any { return c.metrics }

// Compile-time interface checks.
var _ collector.Collector = (*LotteryRiskCollector)(nil)
var _ collector.MetricsProvider = (*LotteryRiskCollector)(nil)
