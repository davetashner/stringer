package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildBinary compiles the stringer binary into a temp directory for integration testing.
func buildBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "stringer-test")
	build := exec.Command("go", "build", //nolint:gosec // test helper with fixed args
		"-o", binary,
		".",
	)
	build.Dir, _ = os.Getwd()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return binary
}

// repoRoot returns the root of the stringer git repo (two dirs up from cmd/stringer).
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// cmd/stringer -> repo root
	return filepath.Dir(filepath.Dir(wd))
}

func TestScan_ProducesJSONL(t *testing.T) {
	binary := buildBinary(t)
	root := repoRoot(t)

	cmd := exec.Command(binary, "scan", root, "--quiet") //nolint:gosec // test helper
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("stringer scan failed: %v", err)
	}

	// Output should contain at least one JSONL line (this repo has TODOs).
	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("expected at least one JSONL line from scan")
	}

	// Each line should be valid JSON with expected fields.
	for i, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Errorf("line %d is not valid JSON: %v\nline: %s", i, err, line)
			continue
		}
		if _, ok := rec["id"]; !ok {
			t.Errorf("line %d missing 'id' field", i)
		}
		if _, ok := rec["title"]; !ok {
			t.Errorf("line %d missing 'title' field", i)
		}
	}
}

func TestScan_DryRun(t *testing.T) {
	binary := buildBinary(t)
	root := repoRoot(t)

	cmd := exec.Command(binary, "scan", root, "--dry-run") //nolint:gosec // test helper
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("stringer scan --dry-run failed: %v", err)
	}

	out := string(stdout)
	if !strings.Contains(out, "dry run") {
		t.Errorf("dry-run output missing 'dry run', got:\n%s", out)
	}
	if !strings.Contains(out, "signal(s) found") {
		t.Errorf("dry-run output missing signal count, got:\n%s", out)
	}
}

func TestScan_DryRunJSON(t *testing.T) {
	binary := buildBinary(t)
	root := repoRoot(t)

	cmd := exec.Command(binary, "scan", root, "--dry-run", "--json", "--quiet") //nolint:gosec // test helper
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("stringer scan --dry-run --json failed: %v", err)
	}

	var result struct {
		TotalSignals int `json:"total_signals"`
		Collectors   []struct {
			Name    string `json:"name"`
			Signals int    `json:"signals"`
		} `json:"collectors"`
		ExitCode int `json:"exit_code"`
	}
	if err := json.Unmarshal(stdout, &result); err != nil {
		t.Fatalf("dry-run JSON is not valid: %v\noutput: %s", err, stdout)
	}
	if result.TotalSignals < 1 {
		t.Error("expected at least 1 signal in dry-run JSON")
	}
	if len(result.Collectors) < 1 {
		t.Error("expected at least 1 collector in dry-run JSON")
	}
	if result.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", result.ExitCode)
	}
}

func TestScan_CollectorsFlag(t *testing.T) {
	binary := buildBinary(t)
	root := repoRoot(t)

	cmd := exec.Command(binary, "scan", root, "--collectors=todos", "--dry-run") //nolint:gosec // test helper
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("stringer scan --collectors=todos failed: %v", err)
	}

	if !strings.Contains(string(stdout), "todos") {
		t.Errorf("output should mention 'todos' collector, got:\n%s", stdout)
	}
}

func TestScan_UnknownCollector(t *testing.T) {
	binary := buildBinary(t)
	root := repoRoot(t)

	cmd := exec.Command(binary, "scan", root, "--collectors=nonexistent") //nolint:gosec // test helper
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for unknown collector")
	}

	if !strings.Contains(stderr.String(), "nonexistent") {
		t.Errorf("error should mention unknown collector name, got:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "available") {
		t.Errorf("error should suggest available collectors, got:\n%s", stderr.String())
	}
}

func TestScan_InvalidPath(t *testing.T) {
	binary := buildBinary(t)

	cmd := exec.Command(binary, "scan", "/nonexistent/path/that/does/not/exist") //nolint:gosec // test helper
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for invalid path")
	}

	errOut := stderr.String()
	if !strings.Contains(errOut, "does not exist") && !strings.Contains(errOut, "cannot resolve path") {
		t.Errorf("error should mention path problem, got:\n%s", errOut)
	}
}

func TestScan_FormatBeads(t *testing.T) {
	binary := buildBinary(t)
	root := repoRoot(t)

	cmd := exec.Command(binary, "scan", root, "--format=beads", "--quiet") //nolint:gosec // test helper
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("stringer scan --format=beads failed: %v", err)
	}

	// Should produce valid JSONL.
	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("expected JSONL output")
	}
}

func TestScan_UnknownFormat(t *testing.T) {
	binary := buildBinary(t)
	root := repoRoot(t)

	cmd := exec.Command(binary, "scan", root, "--format=yaml") //nolint:gosec // test helper
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit for unknown format")
	}

	if !strings.Contains(stderr.String(), "unknown format") {
		t.Errorf("error should mention unknown format, got:\n%s", stderr.String())
	}
}

func TestScan_OutputFile(t *testing.T) {
	binary := buildBinary(t)
	root := repoRoot(t)
	outFile := filepath.Join(t.TempDir(), "out.jsonl")

	cmd := exec.Command(binary, "scan", root, "-o", outFile, "--quiet") //nolint:gosec // test helper
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("stringer scan -o failed: %v\n%s", err, out)
	}

	data, err := os.ReadFile(outFile) //nolint:gosec // test file with known path
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("expected JSONL in output file")
	}

	// Verify first line is valid JSON.
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Errorf("output file line 0 is not valid JSON: %v", err)
	}
}

func TestScan_QuietSuppressesStderr(t *testing.T) {
	binary := buildBinary(t)
	root := repoRoot(t)

	cmd := exec.Command(binary, "scan", root, "--quiet") //nolint:gosec // test helper
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("stringer scan --quiet failed: %v", err)
	}

	if stderr.Len() > 0 {
		t.Errorf("--quiet should suppress stderr, got:\n%s", stderr.String())
	}
}

func TestScan_MaxIssues(t *testing.T) {
	binary := buildBinary(t)
	root := repoRoot(t)

	cmd := exec.Command(binary, "scan", root, "--max-issues=1", "--quiet") //nolint:gosec // test helper
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("stringer scan --max-issues=1 failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	if len(lines) != 1 {
		t.Errorf("expected exactly 1 JSONL line with --max-issues=1, got %d", len(lines))
	}
}

func TestScan_DefaultPath(t *testing.T) {
	binary := buildBinary(t)
	root := repoRoot(t)

	// Run from the repo root without specifying a path.
	cmd := exec.Command(binary, "scan", "--dry-run", "--quiet") //nolint:gosec // test helper
	cmd.Dir = root
	stdout, err := cmd.Output()
	if err != nil {
		t.Fatalf("stringer scan (default path) failed: %v", err)
	}

	// Should produce dry-run JSON (we used --quiet so only stdout matters).
	if len(stdout) == 0 {
		t.Fatal("expected some output from default path scan")
	}
}
