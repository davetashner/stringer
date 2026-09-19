// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/davetashner/stringer/internal/baseline"
	"github.com/davetashner/stringer/internal/output"
	"github.com/davetashner/stringer/internal/signal"
)

// Baseline check flags.
var (
	baselineCheckAccept bool
	baselineCheckPrune  bool
	baselineCheckReason string
)

// baselineCheckCmd compares a JSON scan against the baseline (DR-027).
var baselineCheckCmd = &cobra.Command{
	Use:   "check <scan.json>",
	Short: "Fail when a JSON scan has signals the baseline does not cover",
	Long: `Compare the signals in a JSON scan (stringer scan --format json
--no-baseline) with .stringer/baseline.json in the current directory.

A signal is covered when the baseline holds an unexpired entry under its
exact ID (str-) or its stable key (sts-: source, kind, file and title with
numbers masked, so line shifts and metric changes keep the key). Uncovered
signals are listed as NEW with the suppress command that accepts each one,
and the command exits 4. Baseline entries that match no signal are listed
as RESOLVED and never fail the check.

--accept adds every new signal under its stable key; --prune removes the
resolved entries. The scan must not be baseline-filtered, or every entry
looks resolved.`,
	Args: cobra.ExactArgs(1),
	RunE: runBaselineCheck,
}

func init() {
	baselineCheckCmd.Flags().BoolVar(&baselineCheckAccept, "accept", false,
		"add every new signal to the baseline under its stable key")
	baselineCheckCmd.Flags().BoolVar(&baselineCheckPrune, "prune", false,
		"remove baseline entries that match no signal")
	baselineCheckCmd.Flags().StringVar(&baselineCheckReason, "reason", "acknowledged",
		"suppression reason for --accept (acknowledged, won't-fix, false-positive)")
	baselineCmd.AddCommand(baselineCheckCmd)
}

func runBaselineCheck(cmd *cobra.Command, args []string) error {
	reason := baseline.Reason(baselineCheckReason)
	if err := baseline.ValidateReason(reason); err != nil {
		return exitError(ExitInvalidArgs, "stringer: %v", err)
	}
	signals, err := readScanSignals(args[0])
	if err != nil {
		return err
	}
	absPath, err := cmdFS.Abs(".")
	if err != nil {
		return exitError(ExitInvalidArgs, "stringer: cannot resolve path (%v)", err)
	}
	state, err := baseline.Load(absPath)
	if err != nil {
		return exitError(ExitTotalFailure, "stringer: failed to load baseline (%v)", err)
	}
	if state == nil {
		state = &baseline.BaselineState{Version: "1"}
	}

	fresh, resolved := compareWithBaseline(signals, state)
	w := cmd.OutOrStdout()
	annotate := os.Getenv("GITHUB_ACTIONS") == "true"
	_, _ = fmt.Fprintf(w, "baseline check: %d signals, %d new, %d resolved\n",
		len(signals), len(fresh), len(resolved))
	for _, sig := range fresh {
		printNewSignal(w, sig, annotate)
	}
	for _, s := range resolved {
		_, _ = fmt.Fprintf(w, "RESOLVED %s %s\n", s.SignalID, s.Comment)
		if annotate {
			_, _ = fmt.Fprintf(w, "::notice title=Resolved stringer finding::%s %s (drop it with --prune)\n",
				s.SignalID, escapeAnnotation(s.Comment))
		}
	}

	if baselineCheckAccept || baselineCheckPrune {
		return updateBaselineFromCheck(w, absPath, state, fresh, resolved, reason)
	}
	if len(fresh) > 0 {
		return exitError(ExitNewSignals,
			"stringer: %d signal(s) not in the baseline — fix them or accept them with the commands above", len(fresh))
	}
	return nil
}

// readScanSignals loads the signals of a JSON-format scan file.
func readScanSignals(path string) ([]signal.RawSignal, error) {
	data, err := os.ReadFile(path) //nolint:gosec // user-supplied scan file
	if err != nil {
		return nil, exitError(ExitInvalidArgs, "stringer: cannot read scan file (%v)", err)
	}
	var env output.JSONEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, exitError(ExitInvalidArgs, "stringer: %s is not a JSON scan (%v)", path, err)
	}
	return env.Signals, nil
}

// compareWithBaseline splits a scan into signals the baseline does not cover
// (no entry, or an expired one), sorted by location, and baseline entries
// that matched no signal.
func compareWithBaseline(signals []signal.RawSignal, state *baseline.BaselineState) ([]signal.RawSignal, []baseline.Suppression) {
	lookup := baseline.Lookup(state)
	matched := make(map[string]bool)
	var fresh []signal.RawSignal
	for _, sig := range signals {
		sup, ok := output.LookupSuppression(lookup, sig, "str-")
		if ok {
			matched[sup.SignalID] = true
		}
		if !ok || baseline.IsExpired(sup) {
			fresh = append(fresh, sig)
		}
	}
	sort.SliceStable(fresh, func(i, j int) bool {
		if fresh[i].FilePath != fresh[j].FilePath {
			return fresh[i].FilePath < fresh[j].FilePath
		}
		return fresh[i].Line < fresh[j].Line
	})
	var resolved []baseline.Suppression
	for _, s := range state.Suppressions {
		if !matched[s.SignalID] {
			resolved = append(resolved, s)
		}
	}
	return fresh, resolved
}

// printNewSignal writes one NEW line and the command that accepts it.
func printNewSignal(w io.Writer, sig signal.RawSignal, annotate bool) {
	key := output.StableSignalID(sig)
	_, _ = fmt.Fprintf(w, "NEW %s:%d %s [%s]\n", sig.FilePath, sig.Line, sig.Title, key)
	_, _ = fmt.Fprintf(w, "    accept: stringer baseline suppress %s --reason acknowledged --comment %s\n",
		key, shellQuote(checkComment(sig)))
	if annotate {
		_, _ = fmt.Fprintf(w, "::error file=%s,line=%d,title=New stringer finding (%s)::%s\n",
			escapeAnnotationProperty(sig.FilePath), sig.Line, key, escapeAnnotation(sig.Title))
	}
}

// updateBaselineFromCheck applies --accept and --prune and saves the baseline.
func updateBaselineFromCheck(w io.Writer, absPath string, state *baseline.BaselineState,
	fresh []signal.RawSignal, resolved []baseline.Suppression, reason baseline.Reason) error {
	accepted, pruned := make(map[string]bool), 0
	if baselineCheckAccept {
		now := time.Now().UTC().Truncate(time.Second)
		for _, sig := range fresh {
			key := output.StableSignalID(sig)
			if accepted[key] {
				continue // several signals can share a stable key
			}
			accepted[key] = true
			baseline.AddOrUpdate(state, baseline.Suppression{
				SignalID: key, Reason: reason, Comment: checkComment(sig), SuppressedAt: now,
			})
		}
	}
	if baselineCheckPrune {
		for _, s := range resolved {
			if baseline.Remove(state, s.SignalID) {
				pruned++
			}
		}
	}
	if err := baseline.Save(absPath, state); err != nil {
		return exitError(ExitTotalFailure, "stringer: failed to save baseline (%v)", err)
	}
	_, _ = fmt.Fprintf(w, "baseline updated: %d accepted, %d pruned, %d entries\n",
		len(accepted), pruned, len(state.Suppressions))
	return nil
}

// checkComment is the human-readable baseline comment for a signal.
func checkComment(sig signal.RawSignal) string {
	return sig.FilePath + ": " + sig.Title
}

// shellQuote wraps s in single quotes for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// escapeAnnotation escapes a GitHub Actions workflow-command message.
func escapeAnnotation(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

// escapeAnnotationProperty escapes a GitHub Actions workflow-command property.
func escapeAnnotationProperty(s string) string {
	return strings.NewReplacer(":", "%3A", ",", "%2C").Replace(escapeAnnotation(s))
}
