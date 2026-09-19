// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/baseline"
	"github.com/davetashner/stringer/internal/output"
	"github.com/davetashner/stringer/internal/signal"
)

var (
	checkMerge = signal.RawSignal{Source: "complexity", Kind: "complex-function", FilePath: "internal/config/merge.go",
		Line: 14, Title: "Complex function: Merge (cyclomatic: 88, cognitive: 163, nesting: 4)", Confidence: 0.9}
	checkNew = signal.RawSignal{Source: "complexity", Kind: "complex-function", FilePath: "internal/x/new.go",
		Line: 7, Title: "Complex function: Tangle (cyclomatic: 40, cognitive: 90, nesting: 6)", Confidence: 0.9}
)

// setupBaselineCheck chdirs into a temp repo, writes the scan file and the
// baseline, and returns the scan file path.
func setupBaselineCheck(t *testing.T, signals []signal.RawSignal, sups []baseline.Suppression) string {
	t.Helper()
	resetBaselineFlags()
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	if sups != nil {
		require.NoError(t, baseline.Save(dir, &baseline.BaselineState{Version: "1", Suppressions: sups}))
	}
	data, err := json.Marshal(output.JSONEnvelope{Signals: signals})
	require.NoError(t, err)
	scan := filepath.Join(dir, "scan.json")
	require.NoError(t, os.WriteFile(scan, data, 0o600))
	return scan
}

func runCheck(t *testing.T, args ...string) (string, error) {
	t.Helper()
	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs(append([]string{"baseline", "check"}, args...))
	err := rootCmd.Execute()
	return stdout.String(), err
}

func stableSup(sig signal.RawSignal) baseline.Suppression {
	return baseline.Suppression{SignalID: output.StableSignalID(sig), Reason: baseline.ReasonAcknowledged,
		Comment: checkComment(sig), SuppressedAt: time.Now()}
}

func TestBaselineCheck_AllCovered(t *testing.T) {
	// Merge moved down a line and got simpler: the stable key still covers it.
	moved := checkMerge
	moved.Line = 15
	moved.Title = "Complex function: Merge (cyclomatic: 85, cognitive: 150, nesting: 4)"
	scan := setupBaselineCheck(t, []signal.RawSignal{moved}, []baseline.Suppression{stableSup(checkMerge)})

	out, err := runCheck(t, scan)
	require.NoError(t, err)
	assert.Contains(t, out, "1 signals, 0 new, 0 resolved")
}

func TestBaselineCheck_NewSignalFails(t *testing.T) {
	scan := setupBaselineCheck(t, []signal.RawSignal{checkMerge, checkNew}, []baseline.Suppression{stableSup(checkMerge)})
	t.Setenv("GITHUB_ACTIONS", "true")

	out, err := runCheck(t, scan)
	require.Error(t, err)
	var ece *exitCodeError
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, ExitNewSignals, ece.ExitCode())

	key := output.StableSignalID(checkNew)
	assert.Contains(t, out, "1 new, 0 resolved")
	assert.Contains(t, out, "NEW internal/x/new.go:7 Complex function: Tangle")
	assert.Contains(t, out, "stringer baseline suppress "+key+" --reason acknowledged --comment 'internal/x/new.go: Complex function: Tangle")
	assert.Contains(t, out, "::error file=internal/x/new.go,line=7,title=New stringer finding ("+key+")::")
}

func TestBaselineCheck_ExactIDAndExpiry(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	expired := stableSup(checkNew)
	expired.ExpiresAt = &past
	exact := baseline.Suppression{SignalID: output.SignalID(checkMerge, "str-"), Reason: baseline.ReasonWontFix}
	scan := setupBaselineCheck(t, []signal.RawSignal{checkMerge, checkNew}, []baseline.Suppression{exact, expired})

	out, err := runCheck(t, scan)
	require.Error(t, err, "an expired suppression no longer covers its signal")
	assert.Contains(t, out, "1 new, 0 resolved")
	assert.Contains(t, out, "NEW internal/x/new.go:7")
}

func TestBaselineCheck_ResolvedIsNotice(t *testing.T) {
	scan := setupBaselineCheck(t, nil, []baseline.Suppression{stableSup(checkMerge)})
	t.Setenv("GITHUB_ACTIONS", "true")

	out, err := runCheck(t, scan)
	require.NoError(t, err, "resolved findings never fail the check")
	assert.Contains(t, out, "0 new, 1 resolved")
	assert.Contains(t, out, "RESOLVED "+output.StableSignalID(checkMerge)+" internal/config/merge.go: Complex function: Merge")
	assert.Contains(t, out, "::notice title=Resolved stringer finding::")
}

func TestBaselineCheck_AcceptAndPrune(t *testing.T) {
	dup := checkNew
	dup.Line = 99 // shares checkNew's stable key
	scan := setupBaselineCheck(t, []signal.RawSignal{checkNew, dup}, []baseline.Suppression{stableSup(checkMerge)})

	out, err := runCheck(t, scan, "--accept", "--prune", "--reason", "won't-fix")
	require.NoError(t, err)
	assert.Contains(t, out, "baseline updated: 1 accepted, 1 pruned, 1 entries")

	dir, _ := os.Getwd()
	state, err := baseline.Load(dir)
	require.NoError(t, err)
	require.Len(t, state.Suppressions, 1)
	assert.Equal(t, output.StableSignalID(checkNew), state.Suppressions[0].SignalID)
	assert.Equal(t, baseline.ReasonWontFix, state.Suppressions[0].Reason)
	assert.Equal(t, checkComment(checkNew), state.Suppressions[0].Comment)

	out, err = runCheck(t, scan)
	require.NoError(t, err)
	assert.Contains(t, out, "2 signals, 0 new, 0 resolved")
}

func TestBaselineCheck_NoBaselineFile(t *testing.T) {
	scan := setupBaselineCheck(t, []signal.RawSignal{checkMerge}, nil)
	out, err := runCheck(t, scan)
	require.Error(t, err)
	assert.Contains(t, out, "1 new, 0 resolved")
}

func TestBaselineCheck_BadInput(t *testing.T) {
	scan := setupBaselineCheck(t, nil, nil)

	_, err := runCheck(t, "missing.json")
	assert.ErrorContains(t, err, "cannot read scan file")

	require.NoError(t, os.WriteFile(scan, []byte("not json"), 0o600))
	_, err = runCheck(t, scan)
	assert.ErrorContains(t, err, "is not a JSON scan")

	_, err = runCheck(t, scan, "--reason", "because")
	assert.ErrorContains(t, err, "invalid suppression reason")
}

func TestBaselineCheck_CorruptBaseline(t *testing.T) {
	scan := setupBaselineCheck(t, nil, nil)
	require.NoError(t, os.MkdirAll(".stringer", 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(".stringer", "baseline.json"), []byte("{"), 0o600))
	_, err := runCheck(t, scan)
	assert.ErrorContains(t, err, "failed to load baseline")
}

func TestBaselineCheck_SuppressAcceptsStableKey(t *testing.T) {
	setupBaselineCheck(t, nil, nil)
	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "suppress", "sts-0123abcd", "--comment", "a.go: x"})
	require.NoError(t, rootCmd.Execute())
	assert.Contains(t, stdout.String(), "Suppressed sts-0123abcd")
}

func TestShellQuoteAndAnnotationEscaping(t *testing.T) {
	assert.Equal(t, `'it'\''s'`, shellQuote("it's"))
	assert.Equal(t, "100%25%0Anext%0D", escapeAnnotation("100%\nnext\r"))
	assert.Equal(t, "a%3Ab%2Cc", escapeAnnotationProperty("a:b,c"))
}
