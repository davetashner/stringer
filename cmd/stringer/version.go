package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// versionCmd prints the stringer version.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  "Print the version of the stringer binary.",
	Args:  cobra.NoArgs,
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Printf("stringer %s\n", Version)
	},
}
