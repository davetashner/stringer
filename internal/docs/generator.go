// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package docs

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// Generate writes an AGENTS.md scaffold to w based on the analysis.
func Generate(analysis *RepoAnalysis, w io.Writer) error {
	g := &genWriter{w: w}

	g.printf("# AGENTS.md — %s\n", analysis.Name)

	if analysis.Description != "" {
		g.printf("\n%s\n", analysis.Description)
	}

	// Architecture section (auto-generated)
	g.print("\n<!-- stringer:auto:start:architecture -->\n")
	g.print("## Architecture\n\n")
	g.print("```\n")
	g.print(renderDirectoryTree(analysis.DirectoryTree, analysis.Name))
	g.print("```\n")
	g.print("<!-- stringer:auto:end:architecture -->\n")

	// Tech Stack section (auto-generated)
	g.print("\n<!-- stringer:auto:start:techstack -->\n")
	g.print("## Tech Stack\n\n")
	for _, tc := range analysis.TechStack {
		g.printf("- **%s**", tc.Name)
		if tc.Version != "" {
			g.printf(" %s", tc.Version)
		}
		if tc.Source != "" {
			g.printf(" (detected from %s)", tc.Source)
		}
		g.print("\n")
	}
	g.print("\n<!-- stringer:auto:end:techstack -->\n")

	// Build & Test section (auto-generated)
	g.print("\n<!-- stringer:auto:start:build -->\n")
	g.print("## Build & Test\n\n")
	g.print("```bash\n")
	for _, cmd := range analysis.BuildCommands {
		g.printf("# %s\n%s\n\n", cmd.Name, cmd.Command)
	}
	g.print("```\n")
	g.print("<!-- stringer:auto:end:build -->\n")

	// Key Design Decisions
	g.print("\n## Key Design Decisions\n")
	if len(analysis.Patterns) > 0 {
		g.print("\n")
		for _, p := range analysis.Patterns {
			g.printf("- **%s**: %s\n", p.Name, p.Description)
		}
	} else {
		g.print("\n<!-- Add your key design decisions here -->\n")
	}

	// Decision Records
	g.print("\n## Decision Records\n\n")
	g.print("When making architectural or design decisions, create a decision record in `docs/decisions/`:\n\n")
	g.print("1. Create `docs/decisions/NNN-short-title.md`\n")
	g.print("2. Use the template: Status, Date, Context, Problem, Options, Recommendation, Decision\n")
	g.print("3. Set status to `Proposed` — do not implement until accepted\n")

	// Working on This Project
	g.print("\n## Working on This Project\n\n")
	g.print("### Before submitting changes\n\n")
	g.print("- Run the test suite\n")
	g.print("- Run the linter\n")
	g.print("- Ensure CI passes\n\n")
	g.print("### Main branch integrity\n\n")
	g.print("All changes require a pull request with passing CI. No direct pushes to main.\n")

	return g.err
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

// renderDirectoryTree renders the directory tree as a text tree diagram.
func renderDirectoryTree(entries []DirEntry, rootName string) string {
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

		name := filepath.Base(entry.Path)
		if entry.IsDir {
			name += "/"
		}
		sb.WriteString(prefix + connector + name + "\n")
	}

	return sb.String()
}
