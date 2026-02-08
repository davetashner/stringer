// Package context provides CONTEXT.md generation for agent onboarding.
package context

import (
	"sort"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// CommitSummary holds metadata about a single commit.
type CommitSummary struct {
	Hash    string
	Message string
	Author  string
	Date    time.Time
	Files   int // number of files changed
}

// WeekActivity groups commits by ISO week.
type WeekActivity struct {
	WeekStart time.Time
	Commits   []CommitSummary
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

	// Build sorted week activities (newest first).
	var recentWeeks []WeekActivity
	for ws, commits := range weekBuckets {
		recentWeeks = append(recentWeeks, WeekActivity{
			WeekStart: ws,
			Commits:   commits,
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
	}, nil
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
