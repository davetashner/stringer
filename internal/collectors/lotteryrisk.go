package collectors

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/davetashner/stringer/internal/collector"
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

func init() {
	collector.Register(&LotteryRiskCollector{})
}

// LotteryRiskCollector analyzes git blame and commit history to identify
// directories with low lottery risk (single-author ownership risk).
type LotteryRiskCollector struct{}

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
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
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
	if err := blameDirectories(ctx, repo, repoPath, ownership, defaultMaxBlameFiles); err != nil {
		return nil, fmt.Errorf("blaming files: %w", err)
	}

	// Walk commits and attribute weighted commit activity to directories.
	if err := walkCommitsForOwnership(ctx, repo, repoPath, ownership); err != nil {
		return nil, fmt.Errorf("walking commits for ownership: %w", err)
	}

	// Compute lottery risk for each directory and build signals.
	var signals []signal.RawSignal
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

		if bf <= defaultLotteryRiskThreshold {
			sig := buildLotteryRiskSignal(own)
			signals = append(signals, sig)
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
func blameDirectories(ctx context.Context, repo *git.Repository, repoPath string, ownership map[string]*dirOwnership, maxFiles int) error {
	head, err := repo.Head()
	if err != nil {
		return nil // empty repo, no blame data
	}

	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil
	}

	// Count files per directory to respect the cap.
	dirFileCount := make(map[string]int)

	err = filepath.WalkDir(repoPath, func(path string, d os.DirEntry, walkErr error) error {
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

		// Blame the file.
		blameResult, blameErr := git.Blame(commit, filepath.ToSlash(relPath))
		if blameErr != nil {
			return nil // skip files that can't be blamed
		}

		own := ownership[dir]
		for _, line := range blameResult.Lines {
			author := line.AuthorName
			if author == "" {
				author = line.Author
			}
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
func walkCommitsForOwnership(ctx context.Context, repo *git.Repository, repoPath string, ownership map[string]*dirOwnership) error {
	head, err := repo.Head()
	if err != nil {
		return nil // empty repo
	}

	iter, err := repo.Log(&git.LogOptions{
		From:  head.Hash(),
		Order: git.LogOrderCommitterTime,
	})
	if err != nil {
		return fmt.Errorf("creating log iterator: %w", err)
	}

	now := time.Now()
	count := 0

	err = iter.ForEach(func(c *object.Commit) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if count >= maxCommitWalk {
			return errStopIter
		}
		count++

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

// buildLotteryRiskSignal constructs a RawSignal for a low-lottery-risk directory.
func buildLotteryRiskSignal(own *dirOwnership) signal.RawSignal {
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
		authors = append(authors, authorPct{Name: name, Pct: pct})
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

	// Ensure directory path ends with / for clarity.
	dirPath := own.Path
	if !strings.HasSuffix(dirPath, "/") && dirPath != "." {
		dirPath += "/"
	}

	return signal.RawSignal{
		Source:      "lotteryrisk",
		Kind:        "low-lottery-risk",
		FilePath:    dirPath,
		Line:        0,
		Title:       fmt.Sprintf("Low lottery risk: %s (lottery risk %d, primary: %s %.0f%%)", dirPath, own.LotteryRisk, primary.Name, primary.Pct),
		Description: strings.Join(descParts, "\n"),
		Confidence:  confidence,
		Tags:        []string{"low-lottery-risk", "stringer-generated"},
	}
}

// lotteryRiskConfidence maps lottery risk to confidence score per DR-006.
func lotteryRiskConfidence(busFactor int) float64 {
	switch {
	case busFactor <= 1:
		return 0.8
	case busFactor == 2:
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

// Compile-time interface check.
var _ collector.Collector = (*LotteryRiskCollector)(nil)
