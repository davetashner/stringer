package context

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/davetashner/stringer/internal/docs"
	"github.com/davetashner/stringer/internal/state"
)

// ContextJSON is the top-level JSON structure for context --format json output.
type ContextJSON struct {
	Name      string              `json:"name"`
	Language  string              `json:"language"`
	TechStack []TechComponentJSON `json:"tech_stack"`
	BuildCmds []BuildCmdJSON      `json:"build_commands"`
	Patterns  []PatternJSON       `json:"patterns"`
	History   *HistoryJSON        `json:"history,omitempty"`
	TechDebt  *TechDebtJSON       `json:"tech_debt,omitempty"`
}

// TechComponentJSON is the JSON representation of a tech stack component.
type TechComponentJSON struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
}

// BuildCmdJSON is the JSON representation of a build command.
type BuildCmdJSON struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Source  string `json:"source,omitempty"`
}

// PatternJSON is the JSON representation of a detected code pattern.
type PatternJSON struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// HistoryJSON is the JSON representation of git history analysis.
type HistoryJSON struct {
	TotalCommits int             `json:"total_commits"`
	TopAuthors   []AuthorJSON    `json:"top_authors"`
	Milestones   []MilestoneJSON `json:"milestones,omitempty"`
	RecentWeeks  []WeekJSON      `json:"recent_weeks,omitempty"`
}

// AuthorJSON is the JSON representation of an author.
type AuthorJSON struct {
	Name    string `json:"name"`
	Commits int    `json:"commits"`
}

// MilestoneJSON is the JSON representation of a version tag.
type MilestoneJSON struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
	Date string `json:"date"`
}

// WeekJSON is the JSON representation of a week of activity.
type WeekJSON struct {
	WeekStart string   `json:"week_start"`
	Commits   int      `json:"commits"`
	Tags      []string `json:"tags,omitempty"`
}

// TechDebtJSON is the JSON representation of technical debt from scan state.
type TechDebtJSON struct {
	SignalCount int            `json:"signal_count"`
	ByKind      map[string]int `json:"by_kind"`
}

// RenderJSON writes the context analysis as machine-readable JSON.
func RenderJSON(analysis *docs.RepoAnalysis, history *GitHistory, scanState *state.ScanState, w interface{ Write([]byte) (int, error) }) error {
	out := ContextJSON{
		Name:     analysis.Name,
		Language: analysis.Language,
	}

	// Tech stack.
	for _, tc := range analysis.TechStack {
		out.TechStack = append(out.TechStack, TechComponentJSON{
			Name:    tc.Name,
			Version: tc.Version,
			Source:  tc.Source,
		})
	}

	// Build commands.
	for _, bc := range analysis.BuildCommands {
		out.BuildCmds = append(out.BuildCmds, BuildCmdJSON{
			Name:    bc.Name,
			Command: bc.Command,
			Source:  bc.Source,
		})
	}

	// Patterns.
	for _, p := range analysis.Patterns {
		out.Patterns = append(out.Patterns, PatternJSON{
			Name:        p.Name,
			Description: p.Description,
		})
	}

	// Git history.
	if history != nil {
		hj := &HistoryJSON{
			TotalCommits: history.TotalCommits,
		}
		for _, a := range history.TopAuthors {
			hj.TopAuthors = append(hj.TopAuthors, AuthorJSON(a))
		}
		for _, m := range history.Milestones {
			hj.Milestones = append(hj.Milestones, MilestoneJSON{
				Name: m.Name,
				Hash: m.Hash,
				Date: m.Date.Format("2006-01-02"),
			})
		}
		for _, w := range history.RecentWeeks {
			wj := WeekJSON{
				WeekStart: w.WeekStart.Format("2006-01-02"),
				Commits:   len(w.Commits),
			}
			for _, t := range w.Tags {
				wj.Tags = append(wj.Tags, t.Name)
			}
			hj.RecentWeeks = append(hj.RecentWeeks, wj)
		}
		out.History = hj
	}

	// Tech debt from scan state.
	if scanState != nil && len(scanState.SignalMetas) > 0 {
		byKind := make(map[string]int)
		for _, m := range scanState.SignalMetas {
			byKind[m.Kind]++
		}
		// Sort kinds for deterministic output.
		kinds := make([]string, 0, len(byKind))
		for k := range byKind {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		sortedByKind := make(map[string]int, len(byKind))
		for _, k := range kinds {
			sortedByKind[k] = byKind[k]
		}
		out.TechDebt = &TechDebtJSON{
			SignalCount: scanState.SignalCount,
			ByKind:      sortedByKind,
		}
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON marshal: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
