// Package collectors provides signal extraction modules for stringer.
package collectors

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"errors"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

// maxCommitWalk is the upper bound on commits examined per scan.
// Hardcoded for now since there is no config system yet.
const maxCommitWalk = 1000

// churnWindowDays defines the look-back window for churn analysis.
const churnWindowDays = 90

// churnThreshold is the minimum number of modifications in the window to flag.
const churnThreshold = 10

// staleBranchDays is the minimum inactivity period to consider a branch stale.
const staleBranchDays = 30

// protectedBranches are branch names excluded from stale-branch detection.
var protectedBranches = map[string]bool{
	"main":    true,
	"master":  true,
	"develop": true,
	"HEAD":    true,
}

// revertSubjectPattern matches "Revert "<original message>"" in a subject line.
var revertSubjectPattern = regexp.MustCompile(`(?i)^Revert\s+"(.+)"`)

// revertPrefixPattern matches "revert: <message>" conventional-commit style.
var revertPrefixPattern = regexp.MustCompile(`(?i)^revert:\s*(.+)`)

// revertBodyPattern matches "This reverts commit <hash>" in the commit body.
var revertBodyPattern = regexp.MustCompile(`(?i)This reverts commit ([0-9a-f]{7,40})`)

func init() {
	collector.Register(&GitlogCollector{})
}

// GitlogMetrics holds structured metrics from the git log analysis.
type GitlogMetrics struct {
	FileChurns       []FileChurn
	RevertCount      int
	StaleBranchCount int
}

// FileChurn describes change frequency for a single file.
type FileChurn struct {
	Path        string
	ChangeCount int
	AuthorCount int
}

// GitlogCollector examines git history for reverts, high-churn files, and
// stale branches.
type GitlogCollector struct {
	metrics *GitlogMetrics
}

// Name returns the collector name used for registration and filtering.
func (c *GitlogCollector) Name() string { return "gitlog" }

// Collect scans the repository at repoPath for git-level signals.
func (c *GitlogCollector) Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	// Use GitRoot if set (subdirectory scans), otherwise fall back to repoPath.
	gitRoot := repoPath
	if opts.GitRoot != "" {
		gitRoot = opts.GitRoot
	}
	repo, err := git.PlainOpen(gitRoot)
	if err != nil {
		return nil, fmt.Errorf("opening repo: %w", err)
	}

	var signals []signal.RawSignal

	// Collect reverts and build churn data in a single commit walk.
	reverts, churnSignals, fileChanges, fileAuthors, err := c.walkCommits(ctx, repo, opts)
	if err != nil {
		return nil, fmt.Errorf("walking commits: %w", err)
	}
	signals = append(signals, reverts...)
	signals = append(signals, churnSignals...)

	// Check context before stale-branch scan.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	staleBranches, err := c.detectStaleBranches(ctx, repo)
	if err != nil {
		return nil, fmt.Errorf("detecting stale branches: %w", err)
	}
	signals = append(signals, staleBranches...)

	// Build metrics from all files (not just above-threshold).
	var churns []FileChurn
	for path, count := range fileChanges {
		authorCount := len(fileAuthors[path])
		churns = append(churns, FileChurn{
			Path:        path,
			ChangeCount: count,
			AuthorCount: authorCount,
		})
	}
	sort.Slice(churns, func(i, j int) bool {
		return churns[i].Path < churns[j].Path
	})

	c.metrics = &GitlogMetrics{
		FileChurns:       churns,
		RevertCount:      len(reverts),
		StaleBranchCount: len(staleBranches),
	}

	return signals, nil
}

// walkCommits iterates over the most recent commits and returns revert signals,
// churn signals, and the raw file-change/author maps for metrics.
func (c *GitlogCollector) walkCommits(ctx context.Context, repo *git.Repository, opts signal.CollectorOpts) ([]signal.RawSignal, []signal.RawSignal, map[string]int, map[string]map[string]bool, error) {
	head, err := repo.Head()
	if err != nil {
		// Empty repo or detached HEAD with no commits.
		return nil, nil, nil, nil, nil //nolint:nilerr // gracefully handle repos with no commits
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
		return nil, nil, nil, nil, fmt.Errorf("creating log iterator: %w", err)
	}

	maxWalk := maxCommitWalk
	if opts.GitDepth > 0 {
		maxWalk = opts.GitDepth
	}

	var reverts []signal.RawSignal
	churnWindow := time.Now().AddDate(0, 0, -churnWindowDays)
	fileChanges := make(map[string]int)             // filepath -> modification count
	fileAuthors := make(map[string]map[string]bool) // filepath -> set of authors
	count := 0

	err = iter.ForEach(func(commit *object.Commit) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if count >= maxWalk {
			return errStopIter
		}
		count++

		if opts.ProgressFunc != nil && count%100 == 0 {
			opts.ProgressFunc(fmt.Sprintf("gitlog: examined %d commits", count))
		}

		// --- Revert detection ---
		if sig, ok := detectRevert(commit); ok {
			reverts = append(reverts, sig)
		}

		// --- Churn counting (only within the time window) ---
		if commit.Committer.When.After(churnWindow) {
			files, filesErr := changedFiles(commit)
			if filesErr == nil {
				author := commit.Author.Name
				for _, name := range files {
					fileChanges[name]++
					if fileAuthors[name] == nil {
						fileAuthors[name] = make(map[string]bool)
					}
					fileAuthors[name][author] = true
				}
			}
		}

		return nil
	})
	if err != nil && err != errStopIter {
		// Shallow clones may lack parent objects — degrade gracefully.
		if errors.Is(err, plumbing.ErrObjectNotFound) {
			return reverts, buildChurnSignals(fileChanges, fileAuthors), fileChanges, fileAuthors, nil
		}
		return nil, nil, nil, nil, err
	}

	// Build churn signals from aggregated data.
	churnSignals := buildChurnSignals(fileChanges, fileAuthors)

	return reverts, churnSignals, fileChanges, fileAuthors, nil
}

// errStopIter is a sentinel used to stop the commit iterator after reaching
// the max walk limit.
var errStopIter = fmt.Errorf("stop iteration")

// detectRevert checks if a commit is a revert and returns the corresponding signal.
func detectRevert(commit *object.Commit) (signal.RawSignal, bool) {
	msg := commit.Message
	subject := firstLine(msg)

	var originalSummary string
	var originalHash string

	// Check subject-line patterns.
	if m := revertSubjectPattern.FindStringSubmatch(subject); m != nil {
		originalSummary = m[1]
	} else if m := revertPrefixPattern.FindStringSubmatch(subject); m != nil {
		originalSummary = m[1]
	}

	// Check body for "This reverts commit <hash>".
	if m := revertBodyPattern.FindStringSubmatch(msg); m != nil {
		originalHash = m[1]
	}

	// Must match at least one pattern to be considered a revert.
	if originalSummary == "" && originalHash == "" {
		return signal.RawSignal{}, false
	}

	if originalSummary == "" {
		originalSummary = "commit " + shortHash(originalHash)
	}

	// Gather affected files for the description.
	var filesDesc string
	files, err := changedFiles(commit)
	if err == nil && len(files) > 0 {
		filesDesc = fmt.Sprintf("\nFiles affected: %s", strings.Join(files, ", "))
	}

	desc := fmt.Sprintf("Revert commit: %s\nAuthor: %s",
		commit.Hash.String(), commit.Author.Name)
	if originalHash != "" {
		desc += fmt.Sprintf("\nOriginal commit: %s", originalHash)
	}
	desc += filesDesc

	return signal.RawSignal{
		Source:      "gitlog",
		Kind:        "revert",
		FilePath:    firstFileName(files),
		Line:        0,
		Title:       fmt.Sprintf("Reverted commit: %s", originalSummary),
		Description: desc,
		Author:      commit.Author.Name,
		Timestamp:   commit.Author.When,
		Confidence:  0.7,
		Tags:        []string{"revert", "stringer-generated"},
	}, true
}

// buildChurnSignals converts per-file modification counts into signals for
// files that exceed the churn threshold.
func buildChurnSignals(fileChanges map[string]int, fileAuthors map[string]map[string]bool) []signal.RawSignal {
	var signals []signal.RawSignal

	for filePath, count := range fileChanges {
		if count < churnThreshold {
			continue
		}

		authors := sortedKeys(fileAuthors[filePath])
		confidence := churnConfidence(count)

		signals = append(signals, signal.RawSignal{
			Source:   "gitlog",
			Kind:     "churn",
			FilePath: filePath,
			Line:     0,
			Title:    fmt.Sprintf("High churn: %s (modified %d times in %d days)", filePath, count, churnWindowDays),
			Description: fmt.Sprintf("File modified %d times in the last %d days.\nRecent authors: %s",
				count, churnWindowDays, strings.Join(authors, ", ")),
			Confidence: confidence,
			Tags:       []string{"churn", "stringer-generated"},
		})
	}

	// Sort by file path for deterministic output.
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].FilePath < signals[j].FilePath
	})

	return signals
}

// churnConfidence scales from 0.4 (10 changes) to 0.8 (30+ changes).
func churnConfidence(count int) float64 {
	if count >= 30 {
		return 0.8
	}
	// Linear interpolation: 10 -> 0.4, 30 -> 0.8
	return 0.4 + 0.4*float64(count-churnThreshold)/float64(30-churnThreshold)
}

// detectStaleBranches returns signals for branches with no recent activity.
func (c *GitlogCollector) detectStaleBranches(ctx context.Context, repo *git.Repository) ([]signal.RawSignal, error) {
	refs, err := repo.References()
	if err != nil {
		return nil, fmt.Errorf("listing references: %w", err)
	}

	now := time.Now()
	staleThreshold := now.AddDate(0, 0, -staleBranchDays)
	var signals []signal.RawSignal

	err = refs.ForEach(func(ref *plumbing.Reference) error {
		if err := ctx.Err(); err != nil {
			return err
		}

		// Only consider local branch references.
		if !ref.Name().IsBranch() {
			return nil
		}

		branchName := ref.Name().Short()

		// Skip protected branches.
		if protectedBranches[branchName] {
			return nil
		}

		// Resolve the commit at the tip of this branch.
		commit, commitErr := repo.CommitObject(ref.Hash())
		if commitErr != nil {
			return nil // skip refs that don't resolve to a commit
		}

		lastActivity := commit.Committer.When
		if lastActivity.After(staleThreshold) {
			return nil // branch is active
		}

		daysSinceActivity := int(math.Round(now.Sub(lastActivity).Hours() / 24))
		confidence := staleBranchConfidence(daysSinceActivity)

		signals = append(signals, signal.RawSignal{
			Source:   "gitlog",
			Kind:     "stale-branch",
			FilePath: branchName,
			Line:     0,
			Title:    fmt.Sprintf("Stale branch: %s (last activity %d days ago)", branchName, daysSinceActivity),
			Description: fmt.Sprintf("Last commit by %s: %q\n%d days since last activity.",
				commit.Author.Name, firstLine(commit.Message), daysSinceActivity),
			Author:     commit.Author.Name,
			Timestamp:  lastActivity,
			Confidence: confidence,
			Tags:       []string{"stale-branch", "stringer-generated"},
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort by branch name for deterministic output.
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].FilePath < signals[j].FilePath
	})

	return signals, nil
}

// staleBranchConfidence scales from 0.3 (30 days) to 0.6 (90+ days).
func staleBranchConfidence(daysSinceActivity int) float64 {
	if daysSinceActivity >= 90 {
		return 0.6
	}
	// Linear interpolation: 30 -> 0.3, 90 -> 0.6
	return 0.3 + 0.3*float64(daysSinceActivity-staleBranchDays)/float64(90-staleBranchDays)
}

// changedFiles returns the names of files changed in a commit by comparing
// it to its first parent. For root commits (no parents), it returns an empty
// slice. This avoids the expensive Patch computation needed by Stats().
func changedFiles(commit *object.Commit) ([]string, error) {
	// Root commits have no parent to diff against.
	if commit.NumParents() == 0 {
		return nil, nil
	}

	parent, err := commit.Parent(0)
	if err != nil {
		return nil, err
	}

	parentTree, err := parent.Tree()
	if err != nil {
		return nil, err
	}

	commitTree, err := commit.Tree()
	if err != nil {
		return nil, err
	}

	changes, err := parentTree.Diff(commitTree)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(changes))
	for _, ch := range changes {
		name := ch.To.Name
		if name == "" {
			name = ch.From.Name
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// firstFileName returns the first file name from the slice, or empty string.
func firstFileName(files []string) string {
	if len(files) > 0 {
		return files[0]
	}
	return ""
}

// firstLine returns the first line of a string.
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}

// shortHash returns the first 7 characters of a hash string.
func shortHash(hash string) string {
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
}

// sortedKeys returns the keys of a map[string]bool in sorted order.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Metrics returns structured metrics from the git log analysis.
func (c *GitlogCollector) Metrics() any { return c.metrics }

// Compile-time interface checks.
var _ collector.Collector = (*GitlogCollector)(nil)
var _ collector.MetricsProvider = (*GitlogCollector)(nil)
