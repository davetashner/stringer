package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// scanCmd is the subcommand for scanning a repository.
var scanCmd = &cobra.Command{
	Use:   "scan [path]",
	Short: "Scan a repository for actionable work items",
	Long: `Scan a repository for actionable work items such as TODOs, FIXMEs,
git history patterns, and other signals. Outputs Beads-formatted JSONL
suitable for import with 'bd import'.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(_ *cobra.Command, _ []string) error {
		fmt.Println("stringer scan: not implemented yet")
		return nil
	},
}
