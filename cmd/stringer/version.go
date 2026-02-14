// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

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
	Run: func(cmd *cobra.Command, _ []string) {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "stringer %s\n", Version)
	},
}
