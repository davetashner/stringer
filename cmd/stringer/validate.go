// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/davetashner/stringer/internal/validate"
)

// Validate-specific flag values.
var (
	validateBdCheck bool
)

// validateCmd is the subcommand for validating JSONL files against the bd import schema.
var validateCmd = &cobra.Command{
	Use:   "validate [file]",
	Short: "Validate a JSONL file against the bd import schema",
	Long: `Validate a Beads JSONL file to ensure it will be accepted by 'bd import'.

Checks required fields (id, title, type, priority, status, created_by),
validates field types and allowed values, and reports errors with fix suggestions.

Pass a file path as an argument, or pipe JSONL via stdin:
  stringer validate output.jsonl
  stringer scan . | stringer validate
  cat output.jsonl | stringer validate`,
	Args: cobra.MaximumNArgs(1),
	RunE: runValidate,
}

func init() {
	validateCmd.Flags().BoolVar(&validateBdCheck, "bd-check", false,
		"also run 'bd import --dry-run' for additional validation (requires bd on PATH)")
}

func runValidate(cmd *cobra.Command, args []string) error {
	// Determine input source: file argument or stdin.
	var r *os.File
	if len(args) > 0 {
		filePath := args[0]
		f, err := os.Open(filePath) //nolint:gosec // user-provided path is expected
		if err != nil {
			return exitError(ExitInvalidArgs, "stringer: cannot open %q (%v)", filePath, err)
		}
		defer f.Close() //nolint:errcheck // best-effort close on input file
		r = f
	} else {
		r = os.Stdin
	}

	// Run validation.
	result := validate.Validate(r)

	// Print results.
	if result.Valid() {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "valid: %d beads\n", result.TotalLines)
	} else {
		for _, e := range result.Errors {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "line %d:", e.Line)
			if e.Field != "" {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), " %s:", e.Field)
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), " %s\n", e.Message)
			if e.Suggestion != "" {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "  fix: %s\n", e.Suggestion)
			}
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "\n%d error(s) found in %d lines\n",
			len(result.Errors), result.TotalLines)
	}

	// Optional: run bd import --dry-run.
	if validateBdCheck {
		if err := runBdCheck(cmd, args); err != nil {
			return err
		}
	}

	if !result.Valid() {
		return exitError(ExitInvalidArgs, "")
	}
	return nil
}

// runBdCheck runs `bd import --dry-run` on the file for additional validation.
func runBdCheck(cmd *cobra.Command, args []string) error {
	bdPath, err := exec.LookPath("bd")
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
			"\n--bd-check: 'bd' not found on PATH, skipping bd import validation\n")
		return nil
	}

	if len(args) == 0 {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
			"\n--bd-check: requires a file argument (stdin input not supported for bd import)\n")
		return nil
	}

	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "\nRunning bd import --dry-run...\n")

	bdCmd := exec.Command(bdPath, "import", "--dry-run", args[0]) //nolint:gosec // user-provided path + bd binary
	bdCmd.Stdout = cmd.OutOrStdout()
	bdCmd.Stderr = cmd.ErrOrStderr()

	if err := bdCmd.Run(); err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "bd import --dry-run failed: %v\n", err)
		// Don't fail the validate command because of bd errors — our validation is the source of truth.
	} else {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "bd import --dry-run: OK\n")
	}

	return nil
}
