package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"github.com/spf13/cobra"

	strcontext "github.com/davetashner/stringer/internal/context"
	"github.com/davetashner/stringer/internal/docs"
	"github.com/davetashner/stringer/internal/state"
)

// Context-specific flag values.
var (
	contextOutput string
	contextFormat string
	contextWeeks  int
)

// contextCmd is the subcommand for generating CONTEXT.md.
var contextCmd = &cobra.Command{
	Use:   "context [path]",
	Short: "Generate a CONTEXT.md for agent onboarding",
	Long: `Analyze a repository and generate a CONTEXT.md that summarizes the project's
architecture, recent git activity, active contributors, and known technical debt.
This gives agents instant situational awareness when starting work.

Run 'stringer scan --delta .' first to populate technical debt data.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runContext,
}

func init() {
	contextCmd.Flags().StringVarP(&contextOutput, "output", "o", "", "output file path (default: stdout)")
	contextCmd.Flags().StringVarP(&contextFormat, "format", "f", "", "output format (json for machine-readable)")
	contextCmd.Flags().IntVar(&contextWeeks, "weeks", 4, "weeks of git history to include")
}

func runContext(cmd *cobra.Command, args []string) error {
	// Validate --format flag.
	if contextFormat != "" && contextFormat != "json" {
		return fmt.Errorf("stringer: unsupported context format %q (supported: json)", contextFormat)
	}

	repoPath := "."
	if len(args) > 0 {
		repoPath = args[0]
	}

	absPath, err := cmdFS.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("stringer: cannot resolve path %q (%v)", repoPath, err)
	}

	absPath, err = cmdFS.EvalSymlinks(absPath)
	if err != nil {
		return fmt.Errorf("stringer: cannot resolve path %q (%v)", repoPath, err)
	}

	info, err := cmdFS.Stat(absPath)
	if err != nil {
		return fmt.Errorf("stringer: path %q does not exist", repoPath)
	}
	if !info.IsDir() {
		return fmt.Errorf("stringer: %q is not a directory", repoPath)
	}

	// 1. Analyze architecture.
	slog.Info("analyzing repository", "path", absPath)
	analysis, err := docs.Analyze(absPath)
	if err != nil {
		return fmt.Errorf("stringer: analysis failed (%v)", err)
	}

	// 2. Analyze git history.
	slog.Info("analyzing git history", "weeks", contextWeeks)
	history, err := strcontext.AnalyzeHistory(absPath, contextWeeks)
	if err != nil {
		slog.Warn("git history analysis failed, continuing without it", "error", err)
		history = nil
	}

	// 3. Load scan state (optional).
	scanState, err := state.Load(absPath)
	if err != nil {
		slog.Warn("failed to load scan state, continuing without it", "error", err)
		scanState = nil
	}

	// 4. Generate CONTEXT.md.
	w := cmd.OutOrStdout()
	if contextOutput != "" {
		f, err := cmdFS.Create(contextOutput)
		if err != nil {
			return fmt.Errorf("stringer: cannot create output file %q (%v)", contextOutput, err)
		}
		defer f.Close() //nolint:errcheck // best-effort close on output file
		w = f
	}

	if contextFormat == "json" {
		if err := renderContextJSON(analysis, history, scanState, w); err != nil {
			return fmt.Errorf("stringer: generation failed (%v)", err)
		}
	} else {
		if err := strcontext.Generate(analysis, history, scanState, w); err != nil {
			return fmt.Errorf("stringer: generation failed (%v)", err)
		}
	}

	slog.Info("CONTEXT.md generated")
	return nil
}

// contextJSON is the top-level JSON structure for context --format json output.
type contextJSON struct {
	Name      string              `json:"name"`
	Language  string              `json:"language"`
	TechStack []techComponentJSON `json:"tech_stack"`
	BuildCmds []buildCmdJSON      `json:"build_commands"`
	Patterns  []patternJSON       `json:"patterns"`
	History   *historyJSON        `json:"history,omitempty"`
	TechDebt  *techDebtJSON       `json:"tech_debt,omitempty"`
}

// techComponentJSON is the JSON representation of a tech stack component.
type techComponentJSON struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
}

// buildCmdJSON is the JSON representation of a build command.
type buildCmdJSON struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Source  string `json:"source,omitempty"`
}

// patternJSON is the JSON representation of a detected code pattern.
type patternJSON struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// historyJSON is the JSON representation of git history analysis.
type historyJSON struct {
	TotalCommits int             `json:"total_commits"`
	TopAuthors   []authorJSON    `json:"top_authors"`
	Milestones   []milestoneJSON `json:"milestones,omitempty"`
	RecentWeeks  []weekJSON      `json:"recent_weeks,omitempty"`
}

// authorJSON is the JSON representation of an author.
type authorJSON struct {
	Name    string `json:"name"`
	Commits int    `json:"commits"`
}

// milestoneJSON is the JSON representation of a version tag.
type milestoneJSON struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
	Date string `json:"date"`
}

// weekJSON is the JSON representation of a week of activity.
type weekJSON struct {
	WeekStart string   `json:"week_start"`
	Commits   int      `json:"commits"`
	Tags      []string `json:"tags,omitempty"`
}

// techDebtJSON is the JSON representation of technical debt from scan state.
type techDebtJSON struct {
	SignalCount int            `json:"signal_count"`
	ByKind      map[string]int `json:"by_kind"`
}

// renderContextJSON writes the context analysis as machine-readable JSON.
func renderContextJSON(analysis *docs.RepoAnalysis, history *strcontext.GitHistory, scanState *state.ScanState, w interface{ Write([]byte) (int, error) }) error {
	out := contextJSON{
		Name:     analysis.Name,
		Language: analysis.Language,
	}

	// Tech stack.
	for _, tc := range analysis.TechStack {
		out.TechStack = append(out.TechStack, techComponentJSON{
			Name:    tc.Name,
			Version: tc.Version,
			Source:  tc.Source,
		})
	}

	// Build commands.
	for _, bc := range analysis.BuildCommands {
		out.BuildCmds = append(out.BuildCmds, buildCmdJSON{
			Name:    bc.Name,
			Command: bc.Command,
			Source:  bc.Source,
		})
	}

	// Patterns.
	for _, p := range analysis.Patterns {
		out.Patterns = append(out.Patterns, patternJSON{
			Name:        p.Name,
			Description: p.Description,
		})
	}

	// Git history.
	if history != nil {
		hj := &historyJSON{
			TotalCommits: history.TotalCommits,
		}
		for _, a := range history.TopAuthors {
			hj.TopAuthors = append(hj.TopAuthors, authorJSON{
				Name:    a.Name,
				Commits: a.Commits,
			})
		}
		for _, m := range history.Milestones {
			hj.Milestones = append(hj.Milestones, milestoneJSON{
				Name: m.Name,
				Hash: m.Hash,
				Date: m.Date.Format("2006-01-02"),
			})
		}
		for _, w := range history.RecentWeeks {
			wj := weekJSON{
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
		out.TechDebt = &techDebtJSON{
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
