package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	strcontext "github.com/davetashner/stringer/internal/context"
	"github.com/davetashner/stringer/internal/docs"
	"github.com/davetashner/stringer/internal/state"
)

// Context-specific flag values.
var (
	contextOutput string
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
	contextCmd.Flags().IntVar(&contextWeeks, "weeks", 4, "weeks of git history to include")
}

func runContext(cmd *cobra.Command, args []string) error {
	repoPath := "."
	if len(args) > 0 {
		repoPath = args[0]
	}

	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("stringer: cannot resolve path %q (%v)", repoPath, err)
	}

	absPath, err = filepath.EvalSymlinks(absPath)
	if err != nil {
		return fmt.Errorf("stringer: cannot resolve path %q (%v)", repoPath, err)
	}

	info, err := os.Stat(absPath)
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
		f, err := os.Create(contextOutput) //nolint:gosec // user-specified output path
		if err != nil {
			return fmt.Errorf("stringer: cannot create output file %q (%v)", contextOutput, err)
		}
		defer f.Close() //nolint:errcheck // best-effort close on output file
		w = f
	}

	if err := strcontext.Generate(analysis, history, scanState, w); err != nil {
		return fmt.Errorf("stringer: generation failed (%v)", err)
	}

	slog.Info("CONTEXT.md generated")
	return nil
}
