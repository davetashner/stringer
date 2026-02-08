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

	"github.com/google/go-github/v68/github"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/gitcli"
	"github.com/davetashner/stringer/internal/signal"
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
	if _, err := gitcli.Run(ctx, gitRoot, "rev-parse", "--git-dir"); err != nil {
		return nil, fmt.Errorf("opening repo: %w", err)
	}

	// Check context before starting work.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Discover directories up to the configured depth.
	dirs, err := discoverDirectories(ctx, repoPath, defaultDirectoryDepth)
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
	if err := blameDirectories(ctx, repoPath, gitRoot, ownership, defaultMaxBlameFiles, opts); err != nil {
		return nil, fmt.Errorf("blaming files: %w", err)
	}

	// Walk commits and attribute weighted commit activity to directories.
	if err := walkCommitsForOwnership(ctx, repoPath, gitRoot, ownership, opts); err != nil {
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

	return signals, nil
}

// discoverDirectories walks the repo and returns unique directory paths
// up to the given depth (relative to repoPath). The root directory "." is
// included.
func discoverDirectories(ctx context.Context, repoPath string, maxDepth int) ([]string, error) {
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

		// Skip hidden directories and known non-source directories.
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") && relPath != "." {
			return filepath.SkipDir
		}
		if base == "vendor" || base == "node_modules" {
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
func blameDirectories(ctx context.Context, repoPath, gitRoot string, ownership map[string]*dirOwnership, maxFiles int, opts signal.CollectorOpts) error {
	// Verify HEAD exists (empty repo has no blame data).
	if _, err := gitcli.Run(ctx, gitRoot, "rev-parse", "HEAD"); err != nil {
		return nil // empty repo, no blame data
	}

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
			if base == "vendor" || base == "node_modules" {
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

		// For blame, we need the path relative to gitRoot.
		blameRelPath := relPath
		if gitRoot != repoPath {
			blameRelPath, _ = filepath.Rel(gitRoot, path)
		}

		// Blame the file using native git.
		blameResult, blameErr := gitcli.BlameFile(ctx, gitRoot, blameRelPath)
		if blameErr != nil {
			return nil // skip files that can't be blamed
		}

		own := ownership[dir]
		for _, line := range blameResult.Lines {
			author := line.AuthorName
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

// walkCommitsForOwnership iterates commits using native git log and applies
// recency-weighted attribution to directories based on changed files.
func walkCommitsForOwnership(ctx context.Context, repoPath, gitRoot string, ownership map[string]*dirOwnership, opts signal.CollectorOpts) error {
	maxWalk := maxCommitWalk
	if opts.GitDepth > 0 {
		maxWalk = opts.GitDepth
	}

	// Build git log command args.
	args := []string{"log", "--name-only", "--format=%H|%aN|%aI", "--max-count=" + fmt.Sprintf("%d", maxWalk)}
	if opts.GitSince != "" {
		if since, parseErr := ParseDuration(opts.GitSince); parseErr == nil {
			t := time.Now().Add(-since)
			args = append(args, "--since="+t.Format(time.RFC3339))
		}
	}

	out, err := gitcli.Run(ctx, gitRoot, args...)
	if err != nil {
		// Empty repo or other issue — degrade gracefully.
		if strings.Contains(err.Error(), "does not have any commits") ||
			strings.Contains(err.Error(), "bad default revision") {
			return nil
		}
		return fmt.Errorf("git log: %w", err)
	}

	now := time.Now()
	count := 0

	var currentAuthor string
	var currentTime time.Time

	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}

		// Try to parse as commit header: "<hash>|<author>|<ISO8601>"
		if parts := strings.SplitN(line, "|", 3); len(parts) == 3 && len(parts[0]) == 40 && isHex(parts[0]) {
			currentAuthor = parts[1]
			if t, parseErr := time.Parse(time.RFC3339, parts[2]); parseErr == nil {
				currentTime = t
			}
			count++
			if opts.ProgressFunc != nil && count%100 == 0 {
				opts.ProgressFunc(fmt.Sprintf("lotteryrisk: examined %d commits", count))
			}
			continue
		}

		// Otherwise it's a filename from --name-only output.
		if currentAuthor == "" {
			continue
		}

		dir := findOwningDir(line, ownership)
		if dir == "" {
			continue
		}

		daysOld := now.Sub(currentTime).Hours() / 24
		weight := recencyDecay(daysOld)

		own := ownership[dir]
		if own.Authors[currentAuthor] == nil {
			own.Authors[currentAuthor] = &authorStats{}
		}
		own.Authors[currentAuthor].CommitWeight += weight
	}

	return nil
}

// isHex reports whether s consists entirely of hexadecimal characters.
func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return len(s) > 0
}

// recencyDecay computes the exponential decay weight for a commit that is
// daysOld days in the past. weight = e^(-ln2/halfLife * daysOld).
func recencyDecay(daysOld float64) float64 {
	if daysOld < 0 {
		daysOld = 0
	}
	return math.Exp(-math.Ln2 / float64(decayHalfLifeDays) * daysOld)
}

// computeLotteryRisk calculates the lottery risk for a directory: the minimum
// number of authors whose combined ownership exceeds 50%.
func computeLotteryRisk(own *dirOwnership) int {
	if len(own.Authors) == 0 {
		return 0
	}

	// Compute combined ownership per author.
	totalBlameLines := own.TotalLines
	totalCW := totalCommitWeight(own)

	type authorOwnership struct {
		Name      string
		Ownership float64
	}

	var authors []authorOwnership
	for name, stats := range own.Authors {
		var blameFrac float64
		if totalBlameLines > 0 {
			blameFrac = float64(stats.BlameLines) / float64(totalBlameLines)
		}

		var commitFrac float64
		if totalCW > 0 {
			commitFrac = stats.CommitWeight / totalCW
		}

		ownership := blameFrac*blameWeight + commitFrac*commitWeightFraction
		authors = append(authors, authorOwnership{Name: name, Ownership: ownership})
	}

	// Sort by ownership descending (highest first).
	sort.Slice(authors, func(i, j int) bool {
		if authors[i].Ownership != authors[j].Ownership {
			return authors[i].Ownership > authors[j].Ownership
		}
		return authors[i].Name < authors[j].Name // deterministic tie-break
	})

	// Count how many authors are needed to exceed 50%.
	cumulative := 0.0
	for i, a := range authors {
		cumulative += a.Ownership
		if cumulative > ownershipMajority {
			return i + 1
		}
	}

	return len(authors)
}

// totalCommitWeight sums all authors' commit weights in a directory.
func totalCommitWeight(own *dirOwnership) float64 {
	var total float64
	for _, stats := range own.Authors {
		total += stats.CommitWeight
	}
	return total
}

// buildDirectoryOwnership converts internal dirOwnership into the exported
// DirectoryOwnership metrics type.
func buildDirectoryOwnership(own *dirOwnership) DirectoryOwnership {
	totalBlameLines := own.TotalLines
	totalCW := totalCommitWeight(own)

	var authors []AuthorShare
	for name, stats := range own.Authors {
		var blameFrac float64
		if totalBlameLines > 0 {
			blameFrac = float64(stats.BlameLines) / float64(totalBlameLines)
		}
		var commitFrac float64
		if totalCW > 0 {
			commitFrac = stats.CommitWeight / totalCW
		}
		ownership := blameFrac*blameWeight + commitFrac*commitWeightFraction
		authors = append(authors, AuthorShare{Name: name, Ownership: ownership})
	}

	sort.Slice(authors, func(i, j int) bool {
		if authors[i].Ownership != authors[j].Ownership {
			return authors[i].Ownership > authors[j].Ownership
		}
		return authors[i].Name < authors[j].Name
	})

	return DirectoryOwnership{
		Path:        own.Path,
		LotteryRisk: own.LotteryRisk,
		Authors:     authors,
		TotalLines:  own.TotalLines,
	}
}

// buildLotteryRiskSignal constructs a RawSignal for a low-lottery-risk directory.
// If anon is non-nil, author names are anonymized.
func buildLotteryRiskSignal(own *dirOwnership, anon *nameAnonymizer) signal.RawSignal {
	// Find primary author (highest ownership).
	totalBlameLines := own.TotalLines
	totalCW := totalCommitWeight(own)

	type authorPct struct {
		Name string
		Pct  float64
	}

	var authors []authorPct
	for name, stats := range own.Authors {
		var blameFrac float64
		if totalBlameLines > 0 {
			blameFrac = float64(stats.BlameLines) / float64(totalBlameLines)
		}
		var commitFrac float64
		if totalCW > 0 {
			commitFrac = stats.CommitWeight / totalCW
		}
		pct := (blameFrac*blameWeight + commitFrac*commitWeightFraction) * 100
		displayName := name
		if anon != nil {
			displayName = anon.anonymize(name)
		}
		authors = append(authors, authorPct{Name: displayName, Pct: pct})
	}

	// Sort by percentage descending, then by name for determinism.
	sort.Slice(authors, func(i, j int) bool {
		if authors[i].Pct != authors[j].Pct {
			return authors[i].Pct > authors[j].Pct
		}
		return authors[i].Name < authors[j].Name
	})

	primary := authors[0]

	// Build description with top authors.
	var descParts []string
	descParts = append(descParts, fmt.Sprintf("Lottery risk: %d", own.LotteryRisk))
	descParts = append(descParts, "Top authors:")
	for _, a := range authors {
		if a.Pct < 1.0 {
			break // skip negligible contributors
		}
		descParts = append(descParts, fmt.Sprintf("  - %s: %.0f%%", a.Name, a.Pct))
	}

	confidence := lotteryRiskConfidence(own.LotteryRisk)

	return signal.RawSignal{
		Source:      "lotteryrisk",
		Kind:        "low-lottery-risk",
		FilePath:    own.Path,
		Line:        0,
		Title:       fmt.Sprintf("Low lottery risk: %s (lottery risk %d, primary: %s %.0f%%)", own.Path, own.LotteryRisk, primary.Name, primary.Pct),
		Description: strings.Join(descParts, "\n"),
		Confidence:  confidence,
		Tags:        []string{"low-lottery-risk"},
	}
}

// lotteryRiskConfidence maps lottery risk to confidence score per DR-006.
func lotteryRiskConfidence(riskScore int) float64 {
	switch {
	case riskScore <= 1:
		return 0.8
	case riskScore == 2:
		return 0.5
	default:
		return 0.3
	}
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

// nameAnonymizer provides stable, deterministic anonymization of author names.
// The same real name always maps to the same label within a single scan.
type nameAnonymizer struct {
	mapping map[string]string
	next    int
}

// newNameAnonymizer creates a new anonymizer.
func newNameAnonymizer() *nameAnonymizer {
	return &nameAnonymizer{mapping: make(map[string]string)}
}

// anonymize returns a stable anonymous label for the given name.
func (a *nameAnonymizer) anonymize(name string) string {
	if label, ok := a.mapping[name]; ok {
		return label
	}
	label := contributorLabel(a.next)
	a.mapping[name] = label
	a.next++
	return label
}

// contributorLabel returns "Contributor A", "Contributor B", ..., "Contributor Z",
// "Contributor AA", "Contributor AB", etc.
func contributorLabel(id int) string {
	if id < 26 {
		return "Contributor " + string(rune('A'+id))
	}
	// For id >= 26: AA=26, AB=27, ..., AZ=51, BA=52, ...
	first := (id / 26) - 1
	second := id % 26
	return "Contributor " + string(rune('A'+first)) + string(rune('A'+second))
}

// resolveAnonymize determines whether author names should be anonymized based
// on the mode ("always", "never", "auto") and the repository visibility.
func resolveAnonymize(ctx context.Context, ghCtx *githubContext, mode string) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	case "auto", "":
		// Auto mode: anonymize if the repo is public.
		if ghCtx == nil {
			return false // no API available, default to not anonymizing
		}
		repo, _, err := ghCtx.API.GetRepository(ctx, ghCtx.Owner, ghCtx.Repo)
		if err != nil || repo == nil {
			return false // can't determine visibility, default to not anonymizing
		}
		// Public repos → anonymize; private repos → don't.
		return !repo.GetPrivate()
	default:
		return false
	}
}

// reviewParticipation tracks review activity in a directory.
type reviewParticipation struct {
	Reviewers map[string]int // reviewer login → review count
	Authors   map[string]int // PR author login → PR count
}

// fetchReviewParticipation fetches merged PRs and their reviews from GitHub,
// then maps review activity to directories based on changed files.
func fetchReviewParticipation(ctx context.Context, ghCtx *githubContext, ownership map[string]*dirOwnership, maxPRs int) (map[string]*reviewParticipation, error) {
	result := make(map[string]*reviewParticipation)

	// Fetch recently merged PRs.
	opts := &github.PullRequestListOptions{
		State:     "closed",
		Sort:      "updated",
		Direction: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	fetched := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		prs, resp, err := ghCtx.API.ListPullRequests(ctx, ghCtx.Owner, ghCtx.Repo, opts)
		if err != nil {
			return nil, fmt.Errorf("listing merged PRs for review analysis: %w", err)
		}

		for _, pr := range prs {
			if !pr.GetMerged() {
				continue
			}
			if fetched >= maxPRs {
				return result, nil
			}
			fetched++

			if err := ctx.Err(); err != nil {
				return nil, err
			}

			// Fetch reviews for this PR.
			reviews, reviewErr := fetchAllReviews(ctx, ghCtx.API, ghCtx.Owner, ghCtx.Repo, pr.GetNumber())
			if reviewErr != nil {
				continue // skip PRs with review fetch errors
			}

			// Fetch files changed in this PR.
			files, _, filesErr := ghCtx.API.ListPullRequestFiles(ctx, ghCtx.Owner, ghCtx.Repo, pr.GetNumber(), &github.ListOptions{PerPage: 100})
			if filesErr != nil {
				continue // skip PRs with file fetch errors
			}

			// Determine which directories this PR touches.
			touchedDirs := make(map[string]bool)
			for _, f := range files {
				dir := findOwningDir(f.GetFilename(), ownership)
				if dir != "" {
					touchedDirs[dir] = true
				}
			}

			// Attribute reviews and authorship to directories.
			for dir := range touchedDirs {
				if result[dir] == nil {
					result[dir] = &reviewParticipation{
						Reviewers: make(map[string]int),
						Authors:   make(map[string]int),
					}
				}

				result[dir].Authors[pr.GetUser().GetLogin()]++

				for _, review := range reviews {
					state := strings.ToUpper(review.GetState())
					if state == "APPROVED" || state == "CHANGES_REQUESTED" {
						result[dir].Reviewers[review.GetUser().GetLogin()]++
					}
				}
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return result, nil
}

// buildReviewConcentrationSignals produces signals for directories where a
// single reviewer handles more than 70% of all reviews.
// If anon is non-nil, reviewer names are anonymized.
func buildReviewConcentrationSignals(reviewData map[string]*reviewParticipation, anon *nameAnonymizer) []signal.RawSignal {
	var signals []signal.RawSignal

	for dir, rp := range reviewData {
		totalReviews := 0
		for _, count := range rp.Reviewers {
			totalReviews += count
		}
		if totalReviews < 3 {
			continue // not enough data to draw conclusions
		}

		for reviewer, count := range rp.Reviewers {
			fraction := float64(count) / float64(totalReviews)
			if fraction > reviewConcentrationThreshold {
				displayName := reviewer
				if anon != nil {
					displayName = anon.anonymize(reviewer)
				}

				signals = append(signals, signal.RawSignal{
					Source:      "lotteryrisk",
					Kind:        "review-concentration",
					FilePath:    dir,
					Line:        0,
					Title:       fmt.Sprintf("Review bottleneck: %s reviews %.0f%% of PRs in %s", displayName, fraction*100, dir),
					Description: fmt.Sprintf("Reviewer %s handled %d of %d reviews (%.0f%%) in %s. Consider distributing review responsibility to reduce knowledge silos.", displayName, count, totalReviews, fraction*100, dir),
					Confidence:  0.6,
					Tags:        []string{"review-concentration"},
				})
			}
		}
	}

	return signals
}

// Metrics returns structured ownership data for all analyzed directories.
func (c *LotteryRiskCollector) Metrics() any { return c.metrics }

// Compile-time interface checks.
var _ collector.Collector = (*LotteryRiskCollector)(nil)
var _ collector.MetricsProvider = (*LotteryRiskCollector)(nil)
