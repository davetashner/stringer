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
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

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

	// GitOpener is the opener used to access the git repository.
	// If nil, testable.DefaultGitOpener is used.
	GitOpener testable.GitOpener
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
	opener := c.GitOpener
	if opener == nil {
		opener = testable.DefaultGitOpener
	}
	repo, err := opener.PlainOpen(gitRoot)
	if err != nil {
		return nil, fmt.Errorf("opening repo: %w", err)
	}

	// Check context before starting work.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	excludes := mergeExcludes(opts.ExcludePatterns)

	// Discover directories up to the configured depth.
	dirs, err := discoverDirectories(ctx, repoPath, defaultDirectoryDepth, excludes, opts.IncludeDemoPaths)
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
	if err := blameDirectories(ctx, gitRoot, repoPath, ownership, defaultMaxBlameFiles, excludes, opts); err != nil {
		return nil, fmt.Errorf("blaming files: %w", err)
	}

	// Walk commits and attribute weighted commit activity to directories.
	if err := walkCommitsForOwnership(ctx, repo, repoPath, ownership, opts); err != nil {
		return nil, fmt.Errorf("walking commits for ownership: %w", err)
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

		if bf <= defaultLotteryRiskThreshold {
			sig := buildLotteryRiskSignal(own, anon)
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

	// Enrich signals with timestamps from git log.
	enrichTimestamps(ctx, gitRoot, signals)

	return signals, nil
}

// discoverDirectories walks the repo and returns unique directory paths
// up to the given depth (relative to repoPath). The root directory "." is
// included. Directories matching excludes or demo patterns are skipped.
func discoverDirectories(ctx context.Context, repoPath string, maxDepth int, excludes []string, includeDemoPaths bool) ([]string, error) {
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
		if shouldExclude(relPath, excludes) {
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

// blameDirectories blames source files and attributes line counts to their
// containing directories. It caps blame at maxFiles per directory.
// Uses native git CLI for blame (DR-011) for performance.
func blameDirectories(ctx context.Context, gitDir string, repoPath string, ownership map[string]*dirOwnership, maxFiles int, excludes []string, opts signal.CollectorOpts) error {
	// Count files per directory to respect the cap.
	dirFileCount := make(map[string]int)
	totalFiles := 0

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
			if shouldExclude(relPath, excludes) {
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

		// Skip binary files.
		if isBinaryFile(path) {
			return nil
		}

		// Only blame source-like files.
		ext := filepath.Ext(path)
		if !isSourceExtension(ext) {
			return nil
		}

		// Find the owning directory in our ownership map.
		dir := findOwningDir(relPath, ownership)
		if dir == "" {
			return nil
		}

		// Respect per-directory file cap.
		if dirFileCount[dir] >= maxFiles {
			return nil
		}
		dirFileCount[dir]++
		totalFiles++
		if opts.ProgressFunc != nil && totalFiles%50 == 0 {
			opts.ProgressFunc(fmt.Sprintf("lotteryrisk: blamed %d files", totalFiles))
		}

		// Blame the file via native git.
		blameCtx, cancel := context.WithTimeout(ctx, gitcli.DefaultTimeout)
		blameResult, blameErr := gitcli.BlameFile(blameCtx, gitDir, filepath.ToSlash(relPath))
		cancel()
		if blameErr != nil {
			return nil // skip files that can't be blamed
		}

		own := ownership[dir]
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

		return nil
	})

	return err
}

// walkCommitsForOwnership iterates commits and applies recency-weighted
// attribution to directories based on changed files.
func walkCommitsForOwnership(ctx context.Context, repo testable.GitRepository, repoPath string, ownership map[string]*dirOwnership, opts signal.CollectorOpts) error {
	head, err := repo.Head()
	if err != nil {
		return nil // empty repo
	}

	logOpts := &git.LogOptions{
		From:  head.Hash(),
		Order: git.LogOrderCommitterTime,
	}
	if opts.GitSince != "" {
		if since, parseErr := ParseDuration(opts.GitSince); parseErr == nil {
			t := time.Now().Add(-since)
			logOpts.Since = &t
		}
	}

	iter, err := repo.Log(logOpts)
	if err != nil {
		return fmt.Errorf("creating log iterator: %w", err)
	}

	maxWalk := maxCommitWalk
	if opts.GitDepth > 0 {
		maxWalk = opts.GitDepth
	}

	now := time.Now()
	count := 0

	err = iter.ForEach(func(c *object.Commit) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if count >= maxWalk {
			return errStopIter
		}
		count++

		if opts.ProgressFunc != nil && count%100 == 0 {
			opts.ProgressFunc(fmt.Sprintf("lotteryrisk: examined %d commits", count))
		}

		author := c.Author.Name
		if author == "" {
			return nil
		}

		daysOld := now.Sub(c.Author.When).Hours() / 24
		weight := recencyDecay(daysOld)

		files, filesErr := changedFiles(c)
		if filesErr != nil {
			return nil // skip commits we can't diff
		}

		for _, f := range files {
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

		return nil
	})

	if err != nil && err != errStopIter {
		// Shallow clones may lack parent objects — degrade gracefully.
		if strings.Contains(err.Error(), "object not found") {
			return nil
		}
		return err
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
