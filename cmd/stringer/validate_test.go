package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetValidateFlags resets all package-level validate flags to their default values.
func resetValidateFlags() {
	validateBdCheck = false

	validateCmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
}

// writeJSONLFile creates a temporary JSONL file with the given content.
func writeJSONLFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// validJSONL is a valid JSONL line for testing.
const validJSONL = `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer"}`

// -----------------------------------------------------------------------
// Flag registration tests
// -----------------------------------------------------------------------

func TestValidateCmd_FlagsRegistered(t *testing.T) {
	f := validateCmd.Flags().Lookup("bd-check")
	require.NotNil(t, f, "flag --bd-check not registered")
	assert.Equal(t, "false", f.DefValue)
}

func TestValidateCmd_Help(t *testing.T) {
	resetValidateFlags()

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"validate", "--help"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Validate a Beads JSONL file")
	assert.Contains(t, out, "--bd-check")
	assert.Contains(t, out, "bd import")
}

// -----------------------------------------------------------------------
// Valid file tests
// -----------------------------------------------------------------------

func TestRunValidate_ValidFile(t *testing.T) {
	resetValidateFlags()
	path := writeJSONLFile(t, validJSONL+"\n")

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"validate", path})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "valid: 1 beads")
}

func TestRunValidate_MultipleValidLines(t *testing.T) {
	resetValidateFlags()
	content := validJSONL + "\n" +
		`{"id":"str-87654321","title":"Add feature","type":"feature","priority":1,"status":"open","created_by":"alice"}` + "\n"
	path := writeJSONLFile(t, content)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"validate", path})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "valid: 2 beads")
}

// -----------------------------------------------------------------------
// Invalid file tests — exit code
// -----------------------------------------------------------------------

func TestRunValidate_InvalidFile_ExitCode(t *testing.T) {
	resetValidateFlags()
	content := `{"id":"str-12345678","title":"Fix bug","type":"wrong","priority":2,"status":"open","created_by":"stringer"}` + "\n"
	path := writeJSONLFile(t, content)

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"validate", path})

	err := cmd.Execute()
	require.Error(t, err)

	var ece *exitCodeError
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, ExitInvalidArgs, ece.ExitCode())
}

func TestRunValidate_InvalidFile_ErrorOutput(t *testing.T) {
	resetValidateFlags()
	content := `{"id":"str-12345678","title":"Fix bug","type":"wrong","priority":7,"status":"done","created_by":"stringer"}` + "\n"
	path := writeJSONLFile(t, content)

	cmd, _, stderr := newTestCmd()
	cmd.SetArgs([]string{"validate", path})

	err := cmd.Execute()
	require.Error(t, err)

	out := stderr.String()
	assert.Contains(t, out, "line 1:")
	assert.Contains(t, out, "type:")
	assert.Contains(t, out, "invalid type")
	assert.Contains(t, out, "priority:")
	assert.Contains(t, out, "invalid priority")
	assert.Contains(t, out, "status:")
	assert.Contains(t, out, "invalid status")
	assert.Contains(t, out, "error(s) found")
}

// -----------------------------------------------------------------------
// Missing field error output tests
// -----------------------------------------------------------------------

func TestRunValidate_MissingRequiredFields(t *testing.T) {
	resetValidateFlags()
	content := `{"id":"str-12345678"}` + "\n"
	path := writeJSONLFile(t, content)

	cmd, _, stderr := newTestCmd()
	cmd.SetArgs([]string{"validate", path})

	err := cmd.Execute()
	require.Error(t, err)

	out := stderr.String()
	assert.Contains(t, out, "title:")
	assert.Contains(t, out, "missing required field")
	assert.Contains(t, out, "type:")
	assert.Contains(t, out, "priority:")
	assert.Contains(t, out, "status:")
	assert.Contains(t, out, "created_by:")
}

// -----------------------------------------------------------------------
// Fix suggestion output tests
// -----------------------------------------------------------------------

func TestRunValidate_FixSuggestions(t *testing.T) {
	resetValidateFlags()
	content := `{"id":"str-12345678","title":"Fix bug","type":"bugg","priority":2,"status":"open","created_by":"stringer"}` + "\n"
	path := writeJSONLFile(t, content)

	cmd, _, stderr := newTestCmd()
	cmd.SetArgs([]string{"validate", path})

	err := cmd.Execute()
	require.Error(t, err)

	out := stderr.String()
	assert.Contains(t, out, "fix:")
	assert.Contains(t, out, "did you mean")
}

// -----------------------------------------------------------------------
// File not found test
// -----------------------------------------------------------------------

func TestRunValidate_FileNotFound(t *testing.T) {
	resetValidateFlags()

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"validate", "/nonexistent/path/test.jsonl"})

	err := cmd.Execute()
	require.Error(t, err)

	var ece *exitCodeError
	require.True(t, errors.As(err, &ece))
	assert.Equal(t, ExitInvalidArgs, ece.ExitCode())
	assert.Contains(t, ece.Error(), "cannot open")
}

// -----------------------------------------------------------------------
// Too many args test
// -----------------------------------------------------------------------

func TestRunValidate_TooManyArgs(t *testing.T) {
	resetValidateFlags()

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"validate", "file1", "file2"})

	err := cmd.Execute()
	require.Error(t, err)
}

// -----------------------------------------------------------------------
// Invalid JSON test
// -----------------------------------------------------------------------

func TestRunValidate_InvalidJSON(t *testing.T) {
	resetValidateFlags()
	content := "this is not json\n"
	path := writeJSONLFile(t, content)

	cmd, _, stderr := newTestCmd()
	cmd.SetArgs([]string{"validate", path})

	err := cmd.Execute()
	require.Error(t, err)

	out := stderr.String()
	assert.Contains(t, out, "line 1:")
	assert.Contains(t, out, "invalid JSON")
}

// -----------------------------------------------------------------------
// Empty file test
// -----------------------------------------------------------------------

func TestRunValidate_EmptyFile(t *testing.T) {
	resetValidateFlags()
	path := writeJSONLFile(t, "")

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"validate", path})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "valid: 0 beads")
}

// -----------------------------------------------------------------------
// Optional fields valid test
// -----------------------------------------------------------------------

func TestRunValidate_AllOptionalFields(t *testing.T) {
	resetValidateFlags()
	content := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"closed","created_by":"stringer","created_at":"2024-01-15T10:30:00Z","closed_at":"2024-02-01T12:00:00Z","labels":["bug"],"description":"Details","close_reason":"resolved"}` + "\n"
	path := writeJSONLFile(t, content)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"validate", path})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "valid: 1 beads")
}

// -----------------------------------------------------------------------
// Invalid timestamp test
// -----------------------------------------------------------------------

func TestRunValidate_InvalidTimestamp(t *testing.T) {
	resetValidateFlags()
	content := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer","created_at":"not-a-date"}` + "\n"
	path := writeJSONLFile(t, content)

	cmd, _, stderr := newTestCmd()
	cmd.SetArgs([]string{"validate", path})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, stderr.String(), "created_at:")
	assert.Contains(t, stderr.String(), "ISO 8601")
}

// -----------------------------------------------------------------------
// Invalid labels test
// -----------------------------------------------------------------------

func TestRunValidate_InvalidLabels(t *testing.T) {
	resetValidateFlags()
	content := `{"id":"str-12345678","title":"Fix bug","type":"task","priority":2,"status":"open","created_by":"stringer","labels":"not-an-array"}` + "\n"
	path := writeJSONLFile(t, content)

	cmd, _, stderr := newTestCmd()
	cmd.SetArgs([]string{"validate", path})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, stderr.String(), "labels:")
}

// -----------------------------------------------------------------------
// --bd-check flag behavior (bd not on PATH)
// -----------------------------------------------------------------------

func TestRunValidate_BdCheckNoBd(t *testing.T) {
	resetValidateFlags()
	path := writeJSONLFile(t, validJSONL+"\n")

	// Override PATH to ensure bd is not found.
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir()) // empty dir, no binaries
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })

	cmd, stdout, stderr := newTestCmd()
	cmd.SetArgs([]string{"validate", path, "--bd-check"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "valid: 1 beads")
	assert.Contains(t, stderr.String(), "not found on PATH")
}

// -----------------------------------------------------------------------
// --bd-check with stdin (should skip)
// -----------------------------------------------------------------------

func TestRunValidate_BdCheckStdinSkip(t *testing.T) {
	resetValidateFlags()

	// Create a pipe for stdin.
	pipeR, pipeW, err := os.Pipe()
	require.NoError(t, err)
	_, _ = pipeW.WriteString(validJSONL + "\n")
	_ = pipeW.Close()

	// Temporarily replace os.Stdin.
	origStdin := os.Stdin
	os.Stdin = pipeR
	t.Cleanup(func() { os.Stdin = origStdin })

	// Make sure bd is not on path so we get the skip message, not a bd error.
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })

	cmd := rootCmd
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"validate", "--bd-check"})

	execErr := cmd.Execute()
	require.NoError(t, execErr)
	assert.Contains(t, stdout.String(), "valid: 1 beads")
}

// -----------------------------------------------------------------------
// Mixed valid and invalid lines — summary message
// -----------------------------------------------------------------------

func TestRunValidate_MixedLines(t *testing.T) {
	resetValidateFlags()
	content := validJSONL + "\n" +
		`{"id":"bad"}` + "\n" +
		validJSONL + "\n"
	path := writeJSONLFile(t, content)

	cmd, _, stderr := newTestCmd()
	cmd.SetArgs([]string{"validate", path})

	err := cmd.Execute()
	require.Error(t, err)

	out := stderr.String()
	assert.Contains(t, out, "error(s) found in 3 lines")
}

// -----------------------------------------------------------------------
// Priority edge cases via CLI
// -----------------------------------------------------------------------

func TestRunValidate_PriorityBoundary(t *testing.T) {
	resetValidateFlags()

	tests := []struct {
		name    string
		content string
		valid   bool
	}{
		{"zero", `{"id":"str-12345678","title":"T","type":"task","priority":0,"status":"open","created_by":"s"}`, true},
		{"four", `{"id":"str-12345678","title":"T","type":"task","priority":4,"status":"open","created_by":"s"}`, true},
		{"five", `{"id":"str-12345678","title":"T","type":"task","priority":5,"status":"open","created_by":"s"}`, false},
		{"neg", `{"id":"str-12345678","title":"T","type":"task","priority":-1,"status":"open","created_by":"s"}`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetValidateFlags()
			path := writeJSONLFile(t, tt.content+"\n")

			cmd, _, _ := newTestCmd()
			cmd.SetArgs([]string{"validate", path})

			err := cmd.Execute()
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

// -----------------------------------------------------------------------
// Validate registered in root command
// -----------------------------------------------------------------------

func TestRootCmd_HasValidateSubcommand(t *testing.T) {
	found := false
	for _, sub := range rootCmd.Commands() {
		if sub.Name() == "validate" {
			found = true
			break
		}
	}
	assert.True(t, found, "validate subcommand should be registered")
}
