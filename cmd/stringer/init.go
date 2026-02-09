package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/davetashner/stringer/internal/bootstrap"
)

// Init-specific flag values.
var initForce bool

// initCmd is the subcommand for bootstrapping stringer in a repository.
var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Bootstrap stringer in a repository",
	Long: `Initialize stringer for a repository by detecting its characteristics
and generating a starter configuration. Creates .stringer.yaml with sensible
defaults and appends a stringer integration section to AGENTS.md.

This command is non-destructive by default: it skips files that already exist.
Use --force to regenerate .stringer.yaml.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVar(&initForce, "force", false, "overwrite existing .stringer.yaml")
}

func runInit(cmd *cobra.Command, args []string) error {
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

	slog.Info("initializing stringer", "path", absPath)

	result, err := bootstrap.Run(bootstrap.InitConfig{
		RepoPath: absPath,
		Force:    initForce,
	})
	if err != nil {
		return fmt.Errorf("stringer: init failed (%v)", err)
	}

	// Print summary to cobra's stdout so tests can capture it.
	w := cmd.OutOrStdout()
	bold := color.New(color.Bold)
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)
	dim := color.New(color.Faint)

	_, _ = fmt.Fprintln(w)
	_, _ = bold.Fprintln(w, "stringer init complete")
	_, _ = fmt.Fprintln(w)

	for _, a := range result.Actions {
		var prefix string
		switch a.Operation {
		case "created":
			prefix = green.Sprint("  + ")
		case "updated":
			prefix = yellow.Sprint("  ~ ")
		default:
			prefix = dim.Sprint("  - ")
		}
		_, _ = fmt.Fprintf(w, "%s%-20s %s\n", prefix, a.File, dim.Sprintf("(%s)", a.Description))
	}

	// Show next steps only if something was created.
	hasCreated := false
	for _, a := range result.Actions {
		if a.Operation == "created" || a.Operation == "updated" {
			hasCreated = true
			break
		}
	}

	if hasCreated {
		_, _ = fmt.Fprintln(w)
		_, _ = bold.Fprintln(w, "Next steps:")
		_, _ = fmt.Fprintln(w, "  1. Review .stringer.yaml and adjust settings")
		_, _ = fmt.Fprintln(w, "  2. Run: stringer scan .")
		_, _ = fmt.Fprintln(w, "  3. Import results: stringer scan . | bd import")
	}

	_, _ = fmt.Fprintln(w)
	return nil
}
