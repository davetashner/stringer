package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =======================================================================
// T2.3: CLI Flag Combination Integration Tests
//
// These tests exercise flag combinations for the scan command to verify
// that flags work individually and when combined. Both in-process (via
// cobra cmd.Execute()) and subprocess (via buildBinary()) approaches are
// used depending on the test's requirements.
// =======================================================================

// -----------------------------------------------------------------------
// --collectors subset selection
// -----------------------------------------------------------------------

func TestFlagCombo_CollectorsSubset_SingleCollector(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--collectors=todos", "--dry-run", "--json", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		TotalSignals int `json:"total_signals"`
		Collectors   []struct {
			Name string `json:"name"`
		} `json:"collectors"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed), "output: %s", stdout.String())

	// Only the todos collector should run.
	require.Len(t, parsed.Collectors, 1)
	assert.Equal(t, "todos", parsed.Collectors[0].Name)
	assert.Greater(t, parsed.TotalSignals, 0)
}

func TestFlagCombo_CollectorsSubset_TwoCollectors(t *testing.T) {
	resetScanFlags()
	root := initTestRepo(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--collectors=todos,patterns", "--dry-run", "--json", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		Collectors []struct {
			Name string `json:"name"`
		} `json:"collectors"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed), "output: %s", stdout.String())

	// Exactly two collectors should run.
	require.Len(t, parsed.Collectors, 2)
	names := make(map[string]bool)
	for _, c := range parsed.Collectors {
		names[c.Name] = true
	}
	assert.True(t, names["todos"], "todos collector should be present")
	assert.True(t, names["patterns"], "patterns collector should be present")
}

func TestFlagCombo_CollectorsSubset_CommaSeparated(t *testing.T) {
	t.Parallel()
	binary := buildBinary(t)
	root := initTestRepo(t)

	cmd := exec.Command(binary, "scan", root, //nolint:gosec // test helper
		"--collectors=todos,patterns,gitlog", "--dry-run", "--json", "--quiet")
	stdout, err := cmd.Output()
	require.NoError(t, err)

	var parsed struct {
		Collectors []struct {
			Name string `json:"name"`
		} `json:"collectors"`
	}
	require.NoError(t, json.Unmarshal(stdout, &parsed), "output: %s", stdout)
	assert.Len(t, parsed.Collectors, 3)
}

// -----------------------------------------------------------------------
// --format options
// -----------------------------------------------------------------------

func TestFlagCombo_FormatBeads(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=beads", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	require.NotEmpty(t, lines)

	// Beads format: each line is valid JSONL with id and title fields.
	for i, line := range lines {
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), "line %d: %s", i, line)
		assert.Contains(t, rec, "id", "line %d missing 'id'", i)
		assert.Contains(t, rec, "title", "line %d missing 'title'", i)
	}
}

func TestFlagCombo_FormatJSON(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=json", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	// JSON format: entire output is valid JSON (array or object).
	var result json.RawMessage
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
}

func TestFlagCombo_FormatMarkdown(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=markdown", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "#", "markdown output should contain headers")
}

func TestFlagCombo_FormatTasks(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=tasks", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.NotEmpty(t, stdout.String())
}

func TestFlagCombo_FormatInvalid(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=csv"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown format")
}

// -----------------------------------------------------------------------
// --dry-run mode
// -----------------------------------------------------------------------

func TestFlagCombo_DryRunText(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "dry run")
	assert.Contains(t, out, "signal(s) found")
	// Dry-run should NOT produce JSONL formatted output.
	assert.NotContains(t, out, `"id"`)
}

func TestFlagCombo_DryRunWithJSON(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--json", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	// JSON dry-run output should be valid JSON.
	var parsed struct {
		TotalSignals int `json:"total_signals"`
		Collectors   []struct {
			Name    string `json:"name"`
			Signals int    `json:"signals"`
		} `json:"collectors"`
		ExitCode int `json:"exit_code"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed), "output: %s", stdout.String())
	assert.Equal(t, 0, parsed.ExitCode)
	assert.Greater(t, parsed.TotalSignals, 0)
}

// -----------------------------------------------------------------------
// --min-confidence threshold filtering
// -----------------------------------------------------------------------

func TestFlagCombo_MinConfidenceZero(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--min-confidence=0", "--dry-run", "--json", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed))
	// With min-confidence=0, all signals should pass through.
	assert.Greater(t, parsed.TotalSignals, 0)
}

func TestFlagCombo_MinConfidenceHigh(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--min-confidence=0.99", "--dry-run", "--json", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed))
	// With very high threshold, most signals should be filtered.
	// The exact count depends on TODO confidence, but should be <= all signals.
}

func TestFlagCombo_MinConfidenceOutOfRange(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	tests := []struct {
		name  string
		value string
	}{
		{"above_one", "1.5"},
		{"negative", "-0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetScanFlags()
			cmd, _, _ := newTestCmd()
			cmd.SetArgs([]string{"scan", dir, "--min-confidence=" + tt.value})
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--min-confidence must be between 0.0 and 1.0")
		})
	}
}

// -----------------------------------------------------------------------
// --git-depth limiting
// -----------------------------------------------------------------------

func TestFlagCombo_GitDepthLimiting(t *testing.T) {
	resetScanFlags()
	root := initTestRepo(t)

	// With --git-depth=1, the gitlog collector should only look at the last commit.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--git-depth=1", "--dry-run", "--json", "--quiet", "--collectors=gitlog"})

	err := cmd.Execute()
	require.NoError(t, err)

	var result struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
	// Should succeed without error, signal count depends on implementation.
}

func TestFlagCombo_GitDepthZeroMeansDefault(t *testing.T) {
	resetScanFlags()
	root := initTestRepo(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--git-depth=0", "--dry-run", "--quiet", "--collectors=gitlog"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

// -----------------------------------------------------------------------
// --output file writing
// -----------------------------------------------------------------------

func TestFlagCombo_OutputFileBeads(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)
	outFile := filepath.Join(t.TempDir(), "output.jsonl")

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=beads", "-o", outFile, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(outFile) //nolint:gosec // test path
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.NotEmpty(t, lines)

	// Each line should be valid JSONL.
	for i, line := range lines {
		assert.True(t, json.Valid([]byte(line)), "line %d not valid JSON: %s", i, line)
	}
}

func TestFlagCombo_OutputFileJSON(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)
	outFile := filepath.Join(t.TempDir(), "output.json")

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=json", "-o", outFile, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(outFile) //nolint:gosec // test path
	require.NoError(t, err)

	var result json.RawMessage
	require.NoError(t, json.Unmarshal(data, &result), "output file should contain valid JSON")
}

func TestFlagCombo_OutputFileMarkdown(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)
	outFile := filepath.Join(t.TempDir(), "output.md")

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=markdown", "-o", outFile, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(outFile) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Contains(t, string(data), "#", "markdown file should contain headers")
}

func TestFlagCombo_OutputFileInvalidPath(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "-o", "/nonexistent/dir/file.jsonl", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot create output file")
}

// -----------------------------------------------------------------------
// Error cases for invalid flag values
// -----------------------------------------------------------------------

func TestFlagCombo_InvalidCollector(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--collectors=bogus_collector"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus_collector")
	assert.Contains(t, err.Error(), "available")
}

func TestFlagCombo_InvalidFormat(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=xml"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown format")
}

func TestFlagCombo_InvalidPath(t *testing.T) {
	resetScanFlags()

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", "/nonexistent/bogus/path"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve path")
}

// -----------------------------------------------------------------------
// Flag combinations working together
// -----------------------------------------------------------------------

func TestFlagCombo_CollectorsAndFormat(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--collectors=todos", "--format=json", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Output should be valid JSON.
	var result json.RawMessage
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result))
}

func TestFlagCombo_CollectorsFormatAndOutput(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)
	outFile := filepath.Join(t.TempDir(), "combo.json")

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--collectors=todos", "--format=json", "-o", outFile, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(outFile) //nolint:gosec // test path
	require.NoError(t, err)

	var result json.RawMessage
	require.NoError(t, json.Unmarshal(data, &result))
}

func TestFlagCombo_DryRunIgnoresFormat(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	// When --dry-run is set, --format should have no effect on output.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--format=json", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Dry-run text output, not JSON format.
	out := stdout.String()
	assert.Contains(t, out, "dry run")
	assert.Contains(t, out, "signal(s) found")
}

func TestFlagCombo_CollectorsWithMinConfidence(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	// Combine --collectors with --min-confidence.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--collectors=todos",
		"--min-confidence=0.5", "--dry-run", "--json", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed))
	// All TODO signals have some confidence; result depends on thresholds.
}

func TestFlagCombo_CollectorsWithGitDepth(t *testing.T) {
	resetScanFlags()
	root := initTestRepo(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--collectors=gitlog", "--git-depth=2",
		"--dry-run", "--json", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		TotalSignals int `json:"total_signals"`
		Collectors   []struct {
			Name string `json:"name"`
		} `json:"collectors"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed))
	require.Len(t, parsed.Collectors, 1)
	assert.Equal(t, "gitlog", parsed.Collectors[0].Name)
}

func TestFlagCombo_CollectorsWithKindFilter(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--collectors=todos", "--kind=todo",
		"--dry-run", "--json", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed))
	// todo kind should match TODO signals.
	assert.Greater(t, parsed.TotalSignals, 0)
}

func TestFlagCombo_ExcludeCollectorsWithCollectors(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	// --collectors=todos,patterns with -x patterns should result in just todos.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--collectors=todos,patterns",
		"-x", "patterns", "--dry-run", "--json", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		Collectors []struct {
			Name string `json:"name"`
		} `json:"collectors"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed))
	require.Len(t, parsed.Collectors, 1)
	assert.Equal(t, "todos", parsed.Collectors[0].Name)
}

func TestFlagCombo_StrictWithPartialFailure(t *testing.T) {
	t.Parallel()
	binary := buildBinary(t)
	dir := t.TempDir()

	// Create a temp directory (no git repo) and scan with gitlog + strict.
	// gitlog will fail since there is no git repo, but todos should work.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: strict test\n"), 0o600))

	cmd := exec.Command(binary, "scan", dir, //nolint:gosec // test helper
		"--collectors=todos,gitlog", "--strict", "--dry-run", "--quiet")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()

	// With --strict, partial failure should cause non-zero exit.
	// gitlog fails (no git repo), todos succeeds, so exit code should be non-zero.
	assert.Error(t, err, "strict mode should exit non-zero on partial failure")
}

func TestFlagCombo_MaxIssuesWithFormat(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--max-issues=1", "--format=beads", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	assert.Equal(t, 1, len(lines), "expected exactly 1 line with --max-issues=1")
}

func TestFlagCombo_OutputWithDryRun(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)
	outFile := filepath.Join(t.TempDir(), "dry-output.jsonl")

	// --dry-run should not write to the output file.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "-o", outFile, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "dry run")

	// Output file should not be created in dry-run mode (dry-run exits
	// before writeScanOutput is called).
	_, statErr := os.Stat(outFile)
	assert.True(t, os.IsNotExist(statErr), "output file should not be created in dry-run mode")
}

func TestFlagCombo_GitDepthAndGitSince(t *testing.T) {
	resetScanFlags()
	root := initTestRepo(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--git-depth=100", "--git-since=90d",
		"--dry-run", "--quiet", "--collectors=gitlog"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

func TestFlagCombo_CollectorTimeoutWithCollectors(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--collectors=todos",
		"--collector-timeout=30s", "--dry-run", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

func TestFlagCombo_AllFormatsSubprocess(t *testing.T) {
	t.Parallel()
	binary := buildBinary(t)
	root := initTestRepo(t)

	formats := []string{"beads", "json", "markdown", "tasks"}
	for _, format := range formats {
		t.Run(format, func(t *testing.T) {
			outFile := filepath.Join(t.TempDir(), "out."+format)
			cmd := exec.Command(binary, "scan", root, //nolint:gosec // test helper
				"--format="+format, "-o", outFile, "--quiet", "--collectors=todos")
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "format=%s failed: %s", format, out)

			data, readErr := os.ReadFile(outFile) //nolint:gosec // test path
			require.NoError(t, readErr)
			assert.NotEmpty(t, data, "output file for format=%s should not be empty", format)
		})
	}
}

func TestFlagCombo_ExcludePatternsWithCollectors(t *testing.T) {
	t.Parallel()
	binary := buildBinary(t)

	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	vendorDir := filepath.Join(dir, "vendor")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.MkdirAll(vendorDir, 0o750))

	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "main.go"),
		[]byte("package main\n// TODO: keep this\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(vendorDir, "lib.go"),
		[]byte("package vendor\n// TODO: exclude this\n"), 0o600))

	cmd := exec.Command(binary, "scan", dir, //nolint:gosec // test helper
		"--collectors=todos", "--exclude=vendor/**",
		"--dry-run", "--json", "--quiet")
	stdout, err := cmd.Output()
	require.NoError(t, err)

	var parsed struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout, &parsed))
	assert.Equal(t, 1, parsed.TotalSignals, "vendor should be excluded")
}

func TestFlagCombo_PathsWithCollectors(t *testing.T) {
	t.Parallel()
	binary := buildBinary(t)

	dir := t.TempDir()
	for _, sub := range []string{"cmd", "internal", "docs"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, sub, "file.go"),
			[]byte("package x\n// TODO: in "+sub+"\n"), 0o600))
	}

	// Only scan cmd/ directory.
	cmd := exec.Command(binary, "scan", dir, //nolint:gosec // test helper
		"--collectors=todos", "--paths=cmd/**",
		"--dry-run", "--json", "--quiet")
	stdout, err := cmd.Output()
	require.NoError(t, err)

	var parsed struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout, &parsed))
	assert.Equal(t, 1, parsed.TotalSignals, "only cmd/ TODOs should be found")
}
