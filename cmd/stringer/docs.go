package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/davetashner/stringer/internal/docs"
)

// Docs-specific flag values.
var (
	docsOutput string
	docsUpdate bool
)

// docsCmd is the subcommand for generating AGENTS.md documentation.
var docsCmd = &cobra.Command{
	Use:   "docs [path]",
	Short: "Generate an AGENTS.md scaffold from repository analysis",
	Long: `Analyze a repository and generate an AGENTS.md scaffold that documents
the project's architecture, tech stack, and build commands. Use --update to
regenerate auto-generated sections while preserving manual content.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDocs,
}

func init() {
	docsCmd.Flags().StringVarP(&docsOutput, "output", "o", "", "output file path (default: stdout)")
	docsCmd.Flags().BoolVar(&docsUpdate, "update", false, "update existing AGENTS.md, preserving manual sections")
}

func runDocs(cmd *cobra.Command, args []string) error {
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

	slog.Info("analyzing repository", "path", absPath)
	analysis, err := docs.Analyze(absPath)
	if err != nil {
		return fmt.Errorf("stringer: analysis failed (%v)", err)
	}

	slog.Info("analysis complete",
		"language", analysis.Language,
		"tech_stack", len(analysis.TechStack),
		"build_commands", len(analysis.BuildCommands),
		"patterns", len(analysis.Patterns),
	)

	if docsUpdate {
		existingPath := filepath.Join(absPath, "AGENTS.md")
		if _, err := cmdFS.Stat(existingPath); os.IsNotExist(err) {
			return fmt.Errorf("stringer: no existing AGENTS.md found at %s (remove --update to generate a new one)", existingPath)
		}

		w := cmd.OutOrStdout()
		if docsOutput != "" {
			f, err := cmdFS.Create(docsOutput)
			if err != nil {
				return fmt.Errorf("stringer: cannot create output file %q (%v)", docsOutput, err)
			}
			defer f.Close() //nolint:errcheck // best-effort close on output file
			w = f
		}

		if err := docs.Update(existingPath, analysis, w); err != nil {
			return fmt.Errorf("stringer: update failed (%v)", err)
		}

		slog.Info("AGENTS.md updated", "path", existingPath)
		return nil
	}

	w := cmd.OutOrStdout()
	if docsOutput != "" {
		f, err := cmdFS.Create(docsOutput)
		if err != nil {
			return fmt.Errorf("stringer: cannot create output file %q (%v)", docsOutput, err)
		}
		defer f.Close() //nolint:errcheck // best-effort close on output file
		w = f
	}

	if err := docs.Generate(analysis, w); err != nil {
		return fmt.Errorf("stringer: generation failed (%v)", err)
	}

	slog.Info("AGENTS.md generated")
	return nil
}
