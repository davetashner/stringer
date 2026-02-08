package context

import (
	"fmt"
	"io"
	"strings"

	"github.com/davetashner/stringer/internal/docs"
	"github.com/davetashner/stringer/internal/state"
)

// Generate writes a CONTEXT.md to w based on analysis, git history, and scan state.
// scanState may be nil if no previous scan exists.
func Generate(analysis *docs.RepoAnalysis, history *GitHistory, scanState *state.ScanState, w io.Writer) error {
	g := &genWriter{w: w}

	g.printf("# CONTEXT.md — %s\n", analysis.Name)

	// Project Overview
	g.print("\n## Project Overview\n\n")
	if analysis.Language != "" {
		g.printf("- **Primary Language**: %s\n", analysis.Language)
	}
	if len(analysis.TechStack) > 0 {
		g.print("- **Tech Stack**: ")
		names := make([]string, 0, len(analysis.TechStack))
		for _, tc := range analysis.TechStack {
			entry := tc.Name
			if tc.Version != "" {
				entry += " " + tc.Version
			}
			names = append(names, entry)
		}
		g.print(strings.Join(names, ", "))
		g.print("\n")
	}
	if len(analysis.BuildCommands) > 0 {
		g.print("\n### Build Commands\n\n")
		g.print("```bash\n")
		for _, cmd := range analysis.BuildCommands {
			g.printf("# %s\n%s\n\n", cmd.Name, cmd.Command)
		}
		g.print("```\n")
	}

	// Architecture
	g.print("\n## Architecture\n\n")
	if len(analysis.DirectoryTree) > 0 {
		g.print("```\n")
		g.print(renderDirectoryTree(analysis.DirectoryTree, analysis.Name))
		g.print("```\n")
	}
	if len(analysis.Patterns) > 0 {
		g.print("\n### Key Patterns\n\n")
		for _, p := range analysis.Patterns {
			g.printf("- **%s**: %s\n", p.Name, p.Description)
		}
	}

	// Recent Activity
	if history != nil && len(history.RecentWeeks) > 0 {
		g.printf("\n## Recent Activity (%d commits total)\n", history.TotalCommits)
		for _, week := range history.RecentWeeks {
			g.printf("\n### Week of %s (%d commits)\n\n",
				week.WeekStart.Format("Jan 2, 2006"),
				len(week.Commits))
			for _, c := range week.Commits {
				g.printf("- `%s` %s (%s)\n", c.Hash, c.Message, c.Author)
			}
		}
	}

	// Active Contributors
	if history != nil && len(history.TopAuthors) > 0 {
		g.print("\n## Active Contributors\n\n")
		for _, a := range history.TopAuthors {
			g.printf("- **%s**: %d commits\n", a.Name, a.Commits)
		}
	}

	// Known Technical Debt
	g.print("\n## Known Technical Debt\n\n")
	if scanState != nil && len(scanState.SignalMetas) > 0 {
		// Group by kind
		byKind := make(map[string][]state.SignalMeta)
		for _, m := range scanState.SignalMetas {
			byKind[m.Kind] = append(byKind[m.Kind], m)
		}

		// Sort kinds for deterministic output
		var kinds []string
		for k := range byKind {
			kinds = append(kinds, k)
		}
		sortStrings(kinds)

		g.printf("Found %d signals from last scan:\n\n", scanState.SignalCount)
		for _, kind := range kinds {
			metas := byKind[kind]
			g.printf("### %s (%d)\n\n", formatKindLabel(kind), len(metas))
			// Show top 5 examples
			limit := 5
			if len(metas) < limit {
				limit = len(metas)
			}
			for _, m := range metas[:limit] {
				loc := m.FilePath
				if m.Line > 0 {
					loc = fmt.Sprintf("%s:%d", m.FilePath, m.Line)
				}
				g.printf("- %s (`%s`)\n", m.Title, loc)
			}
			if len(metas) > 5 {
				g.printf("- ... and %d more\n", len(metas)-5)
			}
			g.print("\n")
		}
	} else {
		g.print("No scan data available. Run `stringer scan --delta .` to populate.\n")
	}

	return g.err
}

// renderDirectoryTree produces a text tree diagram (same as docs package).
func renderDirectoryTree(entries []docs.DirEntry, rootName string) string {
	if len(entries) == 0 {
		return rootName + "/\n"
	}

	var sb strings.Builder
	sb.WriteString(rootName + "/\n")

	for i, entry := range entries {
		isLast := i == len(entries)-1 || (i < len(entries)-1 && entries[i+1].Depth <= entry.Depth)

		prefix := ""
		for d := 1; d < entry.Depth; d++ {
			prefix += "│   "
		}

		connector := "├── "
		if isLast || (i < len(entries)-1 && entries[i+1].Depth < entry.Depth) {
			connector = "└── "
		}

		name := lastPathElement(entry.Path)
		if entry.IsDir {
			name += "/"
		}
		sb.WriteString(prefix + connector + name + "\n")
	}

	return sb.String()
}

// lastPathElement returns the last element of a filepath.
func lastPathElement(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

// formatKindLabel converts a kind string to a human-readable label.
func formatKindLabel(kind string) string {
	switch kind {
	case "todo":
		return "TODOs"
	case "fixme":
		return "FIXMEs"
	case "hack", "xxx":
		return "Hacks"
	case "bug":
		return "Bugs"
	case "churn":
		return "Churn Hotspots"
	case "large_file":
		return "Large Files"
	case "lottery_risk":
		return "Lottery Risk"
	default:
		return strings.ToUpper(kind[:1]) + kind[1:]
	}
}

// sortStrings sorts a string slice in place.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// genWriter wraps an io.Writer and captures the first error.
type genWriter struct {
	w   io.Writer
	err error
}

func (g *genWriter) print(s string) {
	if g.err != nil {
		return
	}
	_, g.err = fmt.Fprint(g.w, s)
}

func (g *genWriter) printf(format string, args ...any) {
	if g.err != nil {
		return
	}
	_, g.err = fmt.Fprintf(g.w, format, args...)
}
