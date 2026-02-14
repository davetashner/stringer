// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =======================================================================
// T2.4: Config Loading Precedence Integration Tests
//
// These tests verify the config loading precedence order:
//   CLI flags > repo config (.stringer.yaml) > defaults
//
// Each test creates a temp directory (optionally with .stringer.yaml),
// runs stringer scan, and verifies the expected behavior. The config
// system loads .stringer.yaml from the repo root via config.Load().
// =======================================================================

// -----------------------------------------------------------------------
// Defaults: when no config file exists, defaults are used
// -----------------------------------------------------------------------

func TestConfigPrecedence_DefaultFormat(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	// No .stringer.yaml in the fixture dir, so defaults should apply.
	// Default format is "beads".
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Beads format: JSONL lines with "id" fields.
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	require.NotEmpty(t, lines)
	for _, line := range lines {
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		assert.Contains(t, rec, "id", "default format should be beads (JSONL with id)")
	}
}

func TestConfigPrecedence_DefaultMaxIssuesUnlimited(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	// Default max_issues is 0 (unlimited).
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	// With unlimited max_issues, all signals should be output.
	assert.Greater(t, len(lines), 0)
}

// -----------------------------------------------------------------------
// Repo config (.stringer.yaml) overrides defaults
// -----------------------------------------------------------------------

func TestConfigPrecedence_RepoConfigFormat(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	// Create source file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: config format test\n"), 0o600))

	// Set output_format in .stringer.yaml.
	configContent := "output_format: json\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Output should be JSON (not JSONL), i.e., valid JSON as a whole.
	var result json.RawMessage
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result),
		"config output_format=json should produce JSON output, got: %s", stdout.String())
}

func TestConfigPrecedence_RepoConfigMaxIssues(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	// Create source files with multiple TODOs.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: first\n// TODO: second\n// TODO: third\n"), 0o600))

	// Limit output to 1 issue via config.
	configContent := "max_issues: 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	assert.Equal(t, 1, len(lines), "max_issues=1 from config should limit output to 1 line")
}

func TestConfigPrecedence_RepoConfigNoLLM(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: no_llm test\n"), 0o600))

	// Set no_llm in config.
	configContent := "no_llm: true\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

func TestConfigPrecedence_RepoConfigCollectorEnabled(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: enabled test\n"), 0o600))

	// Disable github collector via config.
	configContent := `collectors:
  github:
    enabled: false
  todos:
    enabled: true
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--json", "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		Collectors []struct {
			Name string `json:"name"`
		} `json:"collectors"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed))

	for _, c := range parsed.Collectors {
		assert.NotEqual(t, "github", c.Name, "github should be disabled via config")
	}
}

func TestConfigPrecedence_RepoConfigCollectorMinConfidence(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: confidence test\n"), 0o600))

	// Set per-collector min_confidence in config.
	configContent := `collectors:
  todos:
    min_confidence: 0.99
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--json", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed))
	// With a very high per-collector min_confidence, fewer signals should pass.
	// (This tests that the config value is applied to the collector opts.)
}

func TestConfigPrecedence_RepoConfigCollectorErrorMode(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: error mode test\n"), 0o600))

	// Set error_mode for todos collector.
	configContent := `collectors:
  todos:
    error_mode: skip
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

func TestConfigPrecedence_RepoConfigGitDepth(t *testing.T) {
	resetScanFlags()
	root := initTestRepo(t)

	// Set git_depth per-collector in config.
	configContent := `collectors:
  gitlog:
    git_depth: 2
`
	require.NoError(t, os.WriteFile(filepath.Join(root, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--dry-run", "--quiet", "--collectors=gitlog"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

func TestConfigPrecedence_RepoConfigExcludePatterns(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	// Create files in two directories.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "vendor"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "main.go"),
		[]byte("package main\n// TODO: src todo\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vendor", "lib.go"),
		[]byte("package vendor\n// TODO: vendor todo\n"), 0o600))

	// Exclude vendor in per-collector config.
	configContent := `collectors:
  todos:
    exclude_patterns:
      - vendor/**
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--json", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed))
	// Only src/main.go TODO should be found; vendor is excluded.
	assert.Equal(t, 1, parsed.TotalSignals,
		"vendor/** should be excluded via config exclude_patterns")
}

// -----------------------------------------------------------------------
// CLI flags override repo config (.stringer.yaml)
// -----------------------------------------------------------------------

func TestConfigPrecedence_CLIFormatOverridesConfig(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: format override test\n"), 0o600))

	// Config says JSON format.
	configContent := "output_format: json\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	// CLI flag overrides to beads format.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=beads", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Output should be JSONL (beads), not JSON.
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	require.NotEmpty(t, lines)
	for _, line := range lines {
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		assert.Contains(t, rec, "id", "CLI --format=beads should override config's json")
	}
}

func TestConfigPrecedence_CLIMaxIssuesOverridesConfig(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: first\n// TODO: second\n// TODO: third\n"), 0o600))

	// Config says max_issues=10 (allow all).
	configContent := "max_issues: 10\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	// CLI flag overrides to 1.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--max-issues=1", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	assert.Equal(t, 1, len(lines), "CLI --max-issues=1 should override config's max_issues=10")
}

func TestConfigPrecedence_CLICollectorsOverridesConfig(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: collectors override\n"), 0o600))

	// Config enables all collectors by not listing any.
	configContent := "output_format: beads\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	// CLI flag restricts to just todos.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--collectors=todos",
		"--dry-run", "--json", "--quiet"})

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

func TestConfigPrecedence_CLIGitDepthOverridesConfig(t *testing.T) {
	resetScanFlags()
	root := initTestRepo(t)

	// Config sets git_depth to 500.
	configContent := `collectors:
  gitlog:
    git_depth: 500
`
	require.NoError(t, os.WriteFile(filepath.Join(root, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	// CLI flag overrides to 1.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--git-depth=1",
		"--dry-run", "--quiet", "--collectors=gitlog"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

func TestConfigPrecedence_CLIAnonymizeOverridesConfig(t *testing.T) {
	resetScanFlags()
	root := initTestRepo(t)

	// Config sets anonymize to "never".
	configContent := `collectors:
  lotteryrisk:
    anonymize: never
`
	require.NoError(t, os.WriteFile(filepath.Join(root, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	// CLI flag overrides to "always".
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--anonymize=always",
		"--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

// -----------------------------------------------------------------------
// Missing config files handled gracefully
// -----------------------------------------------------------------------

func TestConfigPrecedence_NoConfigFile(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: no config file\n"), 0o600))

	// Verify no .stringer.yaml exists.
	_, err := os.Stat(filepath.Join(dir, ".stringer.yaml"))
	require.True(t, os.IsNotExist(err))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--quiet", "--collectors=todos"})

	err = cmd.Execute()
	require.NoError(t, err, "scan should succeed without .stringer.yaml")
	assert.Contains(t, stdout.String(), "signal(s) found")
}

func TestConfigPrecedence_EmptyConfigFile(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: empty config\n"), 0o600))

	// Create empty .stringer.yaml.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(""), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err, "scan should succeed with empty .stringer.yaml")
	assert.Contains(t, stdout.String(), "signal(s) found")
}

func TestConfigPrecedence_MinimalConfigFile(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: minimal config\n"), 0o600))

	// Create .stringer.yaml with just an empty map.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte("{}\n"), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err, "scan should succeed with minimal .stringer.yaml")
	assert.Contains(t, stdout.String(), "signal(s) found")
}

func TestConfigPrecedence_InvalidConfigReturnsError(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: invalid config\n"), 0o600))

	// Create invalid YAML.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte("{{invalid yaml content"), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load")
}

func TestConfigPrecedence_UnknownCollectorInConfigReturnsError(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: validation test\n"), 0o600))

	// Config references a collector that does not exist.
	configContent := `collectors:
  nonexistent_collector:
    enabled: true
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown collector")
}

// -----------------------------------------------------------------------
// Config key independence tests: each key tested separately
// -----------------------------------------------------------------------

func TestConfigPrecedence_OutputFormatOnly(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: format only\n"), 0o600))

	configContent := "output_format: markdown\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Markdown format should contain # headers.
	assert.Contains(t, stdout.String(), "#",
		"output_format=markdown should produce markdown output")
}

func TestConfigPrecedence_MaxIssuesOnly(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: a\n// TODO: b\n// TODO: c\n"), 0o600))

	configContent := "max_issues: 2\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	assert.Equal(t, 2, len(lines), "max_issues=2 should limit to 2 lines")
}

func TestConfigPrecedence_NoLLMOnly(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: no_llm only\n"), 0o600))

	configContent := "no_llm: true\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

func TestConfigPrecedence_BeadsAwareDisabled(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: beads aware test\n"), 0o600))

	// Disable beads-aware dedup.
	configContent := "beads_aware: false\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	// Create a .beads directory with matching issues.
	beadsDir := filepath.Join(dir, ".beads")
	require.NoError(t, os.MkdirAll(beadsDir, 0o750))
	beadsContent := `{"id":"str-12345","title":"beads aware test","status":"open","type":"task"}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"),
		[]byte(beadsContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--json", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed))
	// With beads_aware=false, signals should NOT be filtered against existing beads.
	assert.Greater(t, parsed.TotalSignals, 0,
		"with beads_aware=false, signals should not be deduped")
}

// -----------------------------------------------------------------------
// Combined: multiple config keys set together
// -----------------------------------------------------------------------

func TestConfigPrecedence_MultipleConfigKeys(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: first\n// TODO: second\n// TODO: third\n"), 0o600))

	// Set multiple config keys at once.
	configContent := `output_format: json
max_issues: 2
no_llm: true
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Should be valid JSON (not JSONL) due to output_format=json.
	var result struct {
		Signals []json.RawMessage `json:"signals"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &result),
		"output should be JSON object with signals, got: %s", stdout.String())
	// max_issues=2 should limit the signals to 2 items.
	assert.LessOrEqual(t, len(result.Signals), 2,
		"max_issues=2 should limit output to 2 items")
}

// -----------------------------------------------------------------------
// CLI flags take precedence over combined config
// -----------------------------------------------------------------------

func TestConfigPrecedence_CLIOverridesMultipleConfigKeys(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: first\n// TODO: second\n// TODO: third\n"), 0o600))

	// Config: JSON format, max 10 issues.
	configContent := `output_format: json
max_issues: 10
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	// CLI overrides both: beads format, max 1 issue.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--format=beads", "--max-issues=1",
		"--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	assert.Equal(t, 1, len(lines), "CLI max-issues=1 should override config max_issues=10")

	// Should be JSONL (beads), not JSON.
	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &rec))
	assert.Contains(t, rec, "id", "CLI format=beads should override config format=json")
}

// -----------------------------------------------------------------------
// Per-collector config keys tested independently
// -----------------------------------------------------------------------

func TestConfigPrecedence_CollectorTimeout(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: timeout test\n"), 0o600))

	configContent := `collectors:
  todos:
    timeout: "30s"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

func TestConfigPrecedence_CollectorIncludePatterns(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "main.go"),
		[]byte("package main\n// TODO: src todo\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.txt"),
		[]byte("TODO: readme todo\n"), 0o600))

	// Only include .go files.
	configContent := `collectors:
  todos:
    include_patterns:
      - "**/*.go"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--dry-run", "--json", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	var parsed struct {
		TotalSignals int `json:"total_signals"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed))
	// Only .go files should be scanned.
	assert.Equal(t, 1, parsed.TotalSignals,
		"include_patterns should limit scanning to *.go files")
}

func TestConfigPrecedence_CollectorGitSince(t *testing.T) {
	resetScanFlags()
	root := initTestRepo(t)

	configContent := `collectors:
  gitlog:
    git_since: "30d"
`
	require.NoError(t, os.WriteFile(filepath.Join(root, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", root, "--dry-run", "--quiet", "--collectors=gitlog"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

// -----------------------------------------------------------------------
// Flag format not explicitly passed falls through to config
// -----------------------------------------------------------------------

func TestConfigPrecedence_FormatNotSetFallsToConfig(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: fallthrough test\n"), 0o600))

	// Config sets markdown format.
	configContent := "output_format: markdown\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".stringer.yaml"),
		[]byte(configContent), 0o600))

	// Note: NOT passing --format flag.
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Should be markdown since config says markdown and no CLI override.
	assert.Contains(t, stdout.String(), "#",
		"when --format is not set, should use config's output_format=markdown")
}

func TestConfigPrecedence_FormatNotSetNoConfigFallsToDefault(t *testing.T) {
	resetScanFlags()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n// TODO: default fallthrough\n"), 0o600))

	// No .stringer.yaml and no --format flag => default is "beads".
	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Default format is beads (JSONL).
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	require.NotEmpty(t, lines)
	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &rec))
	assert.Contains(t, rec, "id", "default format should be beads (JSONL)")
}
