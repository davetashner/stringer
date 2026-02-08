// Package context provides CONTEXT.md generation for agent onboarding.
package context

import (
	"regexp"
	"sort"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// TagInfo holds metadata about a version tag.
type TagInfo struct {
	Name string    // e.g. "v1.2.0"
	Hash string    // 8-char short hash
	Date time.Time // commit date
}

// semverTagPattern matches semver-like tag names (with or without "v" prefix).
var semverTagPattern = regexp.MustCompile(`^v?\d+\.\d+\.\d+`)

// CommitSummary holds metadata about a single commit.
type CommitSummary struct {
	Hash    string
	Message string
	Author  string
	Date    time.Time
	Files   int    // number of files changed
	IsMerge bool   // true if commit has 2+ parents
	Tag     string // non-empty if commit has a semver tag
}

// WeekActivity groups commits by ISO week.
type WeekActivity struct {
	WeekStart time.Time
	Commits   []CommitSummary
	Tags      []TagInfo // version tags that landed in this week
}

// AuthorStats tracks commit count per contributor.
type AuthorStats struct {
	Name    string
	Commits int
}

// GitHistory holds aggregated git log analysis.
type GitHistory struct {
	RecentWeeks  []WeekActivity
	TopAuthors   []AuthorStats
	TotalCommits int
	Milestones   []TagInfo // all version tags, newest first, max 10
}

// AnalyzeHistory walks the git log and groups commits by week.
// weeks controls how many weeks of history to include (default: 4).
func AnalyzeHistory(repoPath string, weeks int) (*GitHistory, error) {
	return analyzeHistoryWithNow(repoPath, weeks, time.Now())
}

// analyzeHistoryWithNow is the testable inner function.
func analyzeHistoryWithNow(repoPath string, weeks int, now time.Time) (*GitHistory, error) {
	if weeks <= 0 {
		weeks = 4
	}

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, err
	}

	cutoff := startOfWeek(now).AddDate(0, 0, -7*(weeks-1))

	iter, err := repo.Log(&git.LogOptions{
		Order: git.LogOrderCommitterTime,
	})
	if err != nil {
		// Empty repo with no commits returns "reference not found".
		return &GitHistory{}, nil
	}

	authorCounts := make(map[string]int)
	weekBuckets := make(map[time.Time][]CommitSummary)
	total := 0

	err = iter.ForEach(func(c *object.Commit) error {
		total++
		authorCounts[c.Author.Name]++

		if c.Author.When.Before(cutoff) {
			return nil // still count for totals/authors, just don't bucket
		}

		ws := startOfWeek(c.Author.When)
		cs := CommitSummary{
			Hash:    c.Hash.String()[:8],
			Message: firstLine(c.Message),
			Author:  c.Author.Name,
			Date:    c.Author.When,
			IsMerge: c.NumParents() >= 2,
		}

		// Count changed files (compare with parent).
		stats, err := c.Stats()
		if err == nil {
			cs.Files = len(stats)
		}

		weekBuckets[ws] = append(weekBuckets[ws], cs)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Collect version tags.
	milestones := collectTags(repo)

	// Build hash→tag lookup and assign tags to commits.
	tagsByHash := make(map[string]string, len(milestones))
	for _, t := range milestones {
		tagsByHash[t.Hash] = t.Name
	}
	for ws, commits := range weekBuckets {
		for i := range commits {
			if tag, ok := tagsByHash[commits[i].Hash]; ok {
				commits[i].Tag = tag
			}
		}
		weekBuckets[ws] = commits
	}

	// Build tag-to-week lookup for distributing tags to weeks.
	tagsByWeek := make(map[time.Time][]TagInfo)
	for _, t := range milestones {
		ws := startOfWeek(t.Date)
		tagsByWeek[ws] = append(tagsByWeek[ws], t)
	}

	// Build sorted week activities (newest first).
	var recentWeeks []WeekActivity
	for ws, commits := range weekBuckets {
		recentWeeks = append(recentWeeks, WeekActivity{
			WeekStart: ws,
			Commits:   commits,
			Tags:      tagsByWeek[ws],
		})
	}
	sort.Slice(recentWeeks, func(i, j int) bool {
		return recentWeeks[i].WeekStart.After(recentWeeks[j].WeekStart)
	})

	// Build sorted author stats (most commits first, max 10).
	var topAuthors []AuthorStats
	for name, count := range authorCounts {
		topAuthors = append(topAuthors, AuthorStats{Name: name, Commits: count})
	}
	sort.Slice(topAuthors, func(i, j int) bool {
		if topAuthors[i].Commits == topAuthors[j].Commits {
			return topAuthors[i].Name < topAuthors[j].Name
		}
		return topAuthors[i].Commits > topAuthors[j].Commits
	})
	if len(topAuthors) > 10 {
		topAuthors = topAuthors[:10]
	}

	return &GitHistory{
		RecentWeeks:  recentWeeks,
		TopAuthors:   topAuthors,
		TotalCommits: total,
		Milestones:   milestones,
	}, nil
}

// collectTags returns semver-matching tags sorted newest-first, capped at 10.
func collectTags(repo *git.Repository) []TagInfo {
	tagRefs, err := repo.Tags()
	if err != nil {
		return nil
	}

	var tags []TagInfo
	_ = tagRefs.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().Short()
		if !semverTagPattern.MatchString(name) {
			return nil
		}

		// Resolve annotated tags to their target commit.
		hash := ref.Hash()
		tagObj, err := repo.TagObject(hash)
		if err == nil {
			// Annotated tag — follow to commit.
			commit, err := tagObj.Commit()
			if err != nil {
				return nil
			}
			tags = append(tags, TagInfo{
				Name: name,
				Hash: commit.Hash.String()[:8],
				Date: commit.Author.When,
			})
			return nil
		}

		// Lightweight tag — hash points directly to commit.
		commit, err := repo.CommitObject(hash)
		if err != nil {
			return nil
		}
		tags = append(tags, TagInfo{
			Name: name,
			Hash: commit.Hash.String()[:8],
			Date: commit.Author.When,
		})
		return nil
	})

	// Sort newest first.
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Date.After(tags[j].Date)
	})

	if len(tags) > 10 {
		tags = tags[:10]
	}

	return tags
}

// startOfWeek returns the Monday 00:00 UTC for the given time's ISO week.
func startOfWeek(t time.Time) time.Time {
	t = t.UTC()
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday = 7
	}
	monday := t.AddDate(0, 0, -(weekday - 1))
	return time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.UTC)
}

// firstLine returns the first line of a multi-line string.
func firstLine(s string) string {
	for i, c := range s {
		if c == '\n' {
			return s[:i]
		}
	}
	return s
}
