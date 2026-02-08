package main

import (
	"github.com/spf13/cobra"

	stringerlog "github.com/davetashner/stringer/internal/log"
)

// Global flag values.
var (
	verbose bool
	quiet   bool
	noColor bool
)

// rootCmd is the base command for stringer.
var rootCmd = &cobra.Command{
	Use:   "stringer",
	Short: "Mine your codebase for actionable work items",
	Long: `Stringer is a codebase archaeology tool that mines existing repositories
to produce Beads-formatted issues. It extracts actionable work items from
signals already present in the repo — TODOs, FIXMEs, git history patterns,
and more — giving agents instant situational awareness.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRun: func(_ *cobra.Command, _ []string) {
		stringerlog.Setup(verbose, quiet)
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "suppress non-essential output")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")

	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(docsCmd)
	rootCmd.AddCommand(versionCmd)
}
