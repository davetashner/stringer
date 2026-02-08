// Package integration contains end-to-end tests for stringer.
//
// These tests build the stringer binary and exercise it against fixture
// repositories, verifying JSONL output, golden file matching, idempotency,
// and bd import compatibility.
package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repoRoot returns the stringer repository root directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	// test/integration/scan_test.go -> repo root
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// buildBinary compiles stringer into a temp directory.
func buildBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "stringer-test")
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/stringer") //nolint:gosec // test helper
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go build failed:\n%s", out)
	return binary
}

// fixtureDir returns the path to a named fixture directory.
func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(repoRoot(t), "testdata", "fixtures", name)
	_, err := os.Stat(dir)
	require.NoError(t, err, "fixture %q not found", name)
	return dir
}

// goldenFile returns the path to a named golden file.
func goldenFile(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "testdata", "golden", name)
}

func TestScan_GoldenFile(t *testing.T) {
	binary := buildBinary(t)
	fixture := fixtureDir(t, "sample-repo")

	// Run scan.
	cmd := exec.Command(binary, "scan", fixture, "--collectors=todos", "--quiet") //nolint:gosec // test helper
	stdout, err := cmd.Output()
	require.NoError(t, err, "stringer scan failed")

	// Load golden file.
	golden, err := os.ReadFile(goldenFile(t, "sample-repo.jsonl")) //nolint:gosec // test fixture
	require.NoError(t, err, "reading golden file")

	// Compare line by line for clearer diffs.
	gotLines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	wantLines := strings.Split(strings.TrimSpace(string(golden)), "\n")

	require.Equal(t, len(wantLines), len(gotLines), "line count mismatch")

	for i := range wantLines {
		// Normalize environment-specific fields before comparing.
		// created_at and created_by depend on git blame results which vary
		// by machine (author name) and time (commit timestamp). The fixture
		// directory has no .git of its own, so blame walks up to the stringer
		// repo's .git and returns the actual committer info.
		got := normalizeGoldenJSON(t, gotLines[i])
		want := normalizeGoldenJSON(t, wantLines[i])
		assert.JSONEq(t, want, got, "line %d mismatch", i)
	}
}

// normalizeGoldenJSON removes environment-specific fields from a JSON line
// so golden file comparisons are deterministic across machines and times.
func normalizeGoldenJSON(t *testing.T, line string) string {
	t.Helper()
	var rec map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(line), &rec), "invalid JSON: %s", line)
	delete(rec, "created_at")
	delete(rec, "created_by")
	out, err := json.Marshal(rec)
	require.NoError(t, err)
	return string(out)
}

func TestScan_Idempotent(t *testing.T) {
	binary := buildBinary(t)
	fixture := fixtureDir(t, "sample-repo")

	// Run scan twice.
	cmd1 := exec.Command(binary, "scan", fixture, "--collectors=todos", "--quiet") //nolint:gosec // test helper
	out1, err := cmd1.Output()
	require.NoError(t, err, "first scan failed")

	cmd2 := exec.Command(binary, "scan", fixture, "--collectors=todos", "--quiet") //nolint:gosec // test helper
	out2, err := cmd2.Output()
	require.NoError(t, err, "second scan failed")

	assert.Equal(t, string(out1), string(out2), "scan output is not idempotent")
}

func TestScan_FixtureSignalCount(t *testing.T) {
	binary := buildBinary(t)
	fixture := fixtureDir(t, "sample-repo")

	cmd := exec.Command(binary, "scan", fixture, "--collectors=todos", "--dry-run", "--json", "--quiet") //nolint:gosec // test helper
	stdout, err := cmd.Output()
	require.NoError(t, err, "dry-run failed")

	var result struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout, &result))

	// sample-repo has 11 TODO/FIXME/HACK/BUG/OPTIMIZE/XXX comments.
	assert.Equal(t, 11, result.TotalSignals, "expected 11 signals from sample-repo fixture")
}

func TestScan_JSONLValidity(t *testing.T) {
	binary := buildBinary(t)
	fixture := fixtureDir(t, "sample-repo")

	cmd := exec.Command(binary, "scan", fixture, "--collectors=todos", "--quiet") //nolint:gosec // test helper
	stdout, err := cmd.Output()
	require.NoError(t, err, "scan failed")

	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	require.NotEmpty(t, lines)

	requiredFields := []string{"id", "title", "type", "priority", "status"}

	for i, line := range lines {
		var rec map[string]interface{}
		require.NoError(t, json.Unmarshal([]byte(line), &rec), "line %d is not valid JSON", i)

		for _, field := range requiredFields {
			assert.Contains(t, rec, field, "line %d missing required field %q", i, field)
		}

		// ID must start with "str-".
		id, ok := rec["id"].(string)
		assert.True(t, ok, "line %d: id is not a string", i)
		assert.True(t, strings.HasPrefix(id, "str-"), "line %d: id %q does not start with str-", i, id)

		// Status must be "open".
		assert.Equal(t, "open", rec["status"], "line %d: status should be open", i)
	}
}

func TestScan_OutputFile(t *testing.T) {
	binary := buildBinary(t)
	fixture := fixtureDir(t, "sample-repo")
	outFile := filepath.Join(t.TempDir(), "output.jsonl")

	cmd := exec.Command(binary, "scan", fixture, "--collectors=todos", "-o", outFile, "--quiet") //nolint:gosec // test helper
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "scan failed:\n%s", out)

	data, err := os.ReadFile(outFile) //nolint:gosec // test fixture
	require.NoError(t, err, "reading output file")

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	assert.Equal(t, 11, len(lines), "expected 11 signals in output file")

	// Each line must be valid JSON.
	for i, line := range lines {
		assert.True(t, json.Valid([]byte(line)), "line %d is not valid JSON", i)
	}
}

func TestScan_MaxIssuesCap(t *testing.T) {
	binary := buildBinary(t)
	fixture := fixtureDir(t, "sample-repo")

	cmd := exec.Command(binary, "scan", fixture, "--collectors=todos", "--max-issues=3", "--quiet") //nolint:gosec // test helper
	stdout, err := cmd.Output()
	require.NoError(t, err, "scan failed")

	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	assert.Equal(t, 3, len(lines), "expected exactly 3 signals with --max-issues=3")
}

func TestScan_DryRunNoOutput(t *testing.T) {
	binary := buildBinary(t)
	fixture := fixtureDir(t, "sample-repo")

	cmd := exec.Command(binary, "scan", fixture, "--collectors=todos", "--dry-run") //nolint:gosec // test helper
	stdout, err := cmd.Output()
	require.NoError(t, err, "dry-run failed")

	out := string(stdout)
	assert.Contains(t, out, "dry run")
	assert.Contains(t, out, "signal(s) found")

	// Dry run should NOT contain JSONL (no lines starting with '{').
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		assert.False(t, strings.HasPrefix(line, "{"), "dry-run should not output JSONL, got: %s", line)
	}
}

func TestScan_DeltaFirstRun(t *testing.T) {
	binary := buildBinary(t)
	dir := t.TempDir()

	// Create a file with TODOs.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n// TODO: first\n// TODO: second\n"), 0o600))

	// First delta run — no state exists, all signals are new.
	cmd := exec.Command(binary, "scan", dir, "--collectors=todos", "--delta", "--quiet") //nolint:gosec // test helper
	stdout, err := cmd.Output()
	require.NoError(t, err, "first delta scan failed")

	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	assert.Equal(t, 2, len(lines), "first delta run should output all signals")

	// State file should exist.
	stateFile := filepath.Join(dir, ".stringer", "last-scan.json")
	_, err = os.Stat(stateFile)
	assert.NoError(t, err, "state file should be created after first delta scan")
}

func TestScan_DeltaIncremental(t *testing.T) {
	binary := buildBinary(t)
	dir := t.TempDir()

	// Create initial file with one TODO.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n// TODO: original\n"), 0o600))

	// First delta run.
	cmd1 := exec.Command(binary, "scan", dir, "--collectors=todos", "--delta", "--quiet") //nolint:gosec // test helper
	out1, err := cmd1.Output()
	require.NoError(t, err, "first delta scan failed")
	lines1 := strings.Split(strings.TrimSpace(string(out1)), "\n")
	assert.Equal(t, 1, len(lines1), "first run should output 1 signal")

	// Add a new TODO.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n// TODO: original\n// TODO: brand new\n"), 0o600))

	// Second delta run — only new signal.
	cmd2 := exec.Command(binary, "scan", dir, "--collectors=todos", "--delta", "--quiet") //nolint:gosec // test helper
	out2, err := cmd2.Output()
	require.NoError(t, err, "second delta scan failed")
	lines2 := strings.Split(strings.TrimSpace(string(out2)), "\n")
	assert.Equal(t, 1, len(lines2), "second delta run should output only the new signal")

	// Verify the new signal is the new TODO.
	assert.Contains(t, lines2[0], "brand new", "new signal should be the added TODO")
}

func TestScan_DeltaDryRun(t *testing.T) {
	binary := buildBinary(t)
	dir := t.TempDir()

	// Create a file with TODOs.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n// TODO: test\n"), 0o600))

	// First delta run to create state.
	cmd1 := exec.Command(binary, "scan", dir, "--collectors=todos", "--delta", "--quiet") //nolint:gosec // test helper
	_, err := cmd1.Output()
	require.NoError(t, err, "first delta scan failed")

	// Read state file.
	stateFile := filepath.Join(dir, ".stringer", "last-scan.json")
	before, err := os.ReadFile(stateFile) //nolint:gosec // test
	require.NoError(t, err)

	// Add a new TODO.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n// TODO: test\n// TODO: dry-run test\n"), 0o600))

	// Delta dry-run — should NOT update state.
	cmd2 := exec.Command(binary, "scan", dir, "--collectors=todos", "--delta", "--dry-run", "--quiet") //nolint:gosec // test helper
	out2, err := cmd2.Output()
	require.NoError(t, err, "delta dry-run failed")
	assert.Contains(t, string(out2), "signal(s) found")

	// State file should be unchanged.
	after, err := os.ReadFile(stateFile) //nolint:gosec // test
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "dry-run should not modify state file")
}

func TestScan_ErrorMessages(t *testing.T) {
	binary := buildBinary(t)

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "nonexistent path",
			args:       []string{"scan", "/no/such/path"},
			wantStderr: "cannot resolve path",
		},
		{
			name:       "unknown collector",
			args:       []string{"scan", ".", "--collectors=bogus"},
			wantStderr: "available",
		},
		{
			name:       "unknown format",
			args:       []string{"scan", ".", "--format=yaml"},
			wantStderr: "unknown format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(binary, tt.args...) //nolint:gosec // test helper
			cmd.Dir = repoRoot(t)
			out, err := cmd.CombinedOutput()
			assert.Error(t, err, "expected non-zero exit")
			assert.Contains(t, string(out), tt.wantStderr)
		})
	}
}
