// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/davetashner/stringer/internal/config"
)

func TestGenerateConfig_CreatesFile(t *testing.T) {
	dir := t.TempDir()

	action, err := GenerateConfig(dir, true, false, nil)
	require.NoError(t, err)
	assert.Equal(t, config.FileName, action.File)
	assert.Equal(t, "created", action.Operation)
	assert.Contains(t, action.Description, "github collector enabled")

	// Verify file exists and is valid YAML.
	data, err := os.ReadFile(filepath.Join(dir, config.FileName)) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Contains(t, string(data), "enabled: true")

	// Round-trip through yaml.Unmarshal.
	var cfg config.Config
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	assert.NotNil(t, cfg.Collectors["github"])
	assert.True(t, *cfg.Collectors["github"].Enabled)
}

func TestGenerateConfig_GitHubDisabled(t *testing.T) {
	dir := t.TempDir()

	action, err := GenerateConfig(dir, false, false, nil)
	require.NoError(t, err)
	assert.Contains(t, action.Description, "github collector disabled")

	data, err := os.ReadFile(filepath.Join(dir, config.FileName)) //nolint:gosec // test path
	require.NoError(t, err)

	var cfg config.Config
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	assert.False(t, *cfg.Collectors["github"].Enabled)
}

func TestGenerateConfig_SkipsExisting(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, config.FileName)
	require.NoError(t, os.WriteFile(existing, []byte("existing: true\n"), 0o600))

	action, err := GenerateConfig(dir, true, false, nil)
	require.NoError(t, err)
	assert.Equal(t, "skipped", action.Operation)
	assert.Contains(t, action.Description, "--force")

	// Original content preserved.
	data, err := os.ReadFile(existing) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, "existing: true\n", string(data))
}

func TestGenerateConfig_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, config.FileName)
	require.NoError(t, os.WriteFile(existing, []byte("existing: true\n"), 0o600))

	action, err := GenerateConfig(dir, true, true, nil)
	require.NoError(t, err)
	assert.Equal(t, "created", action.Operation)
	assert.Contains(t, action.Description, "regenerated")

	// Content replaced.
	data, err := os.ReadFile(existing) //nolint:gosec // test path
	require.NoError(t, err)
	assert.NotContains(t, string(data), "existing: true")
	assert.Contains(t, string(data), "collectors:")
}

func TestGenerateConfig_AllCollectorsPresent(t *testing.T) {
	dir := t.TempDir()

	_, err := GenerateConfig(dir, true, false, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, config.FileName)) //nolint:gosec // test path
	require.NoError(t, err)
	content := string(data)

	for _, collector := range []string{"todos", "gitlog", "patterns", "lotteryrisk", "github", "dephealth", "vuln"} {
		assert.Contains(t, content, collector+":", "config should contain %s collector", collector)
	}
}

func TestGenerateConfig_ValidYAMLRoundTrip(t *testing.T) {
	dir := t.TempDir()

	_, err := GenerateConfig(dir, true, false, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, config.FileName)) //nolint:gosec // test path
	require.NoError(t, err)

	var cfg config.Config
	require.NoError(t, yaml.Unmarshal(data, &cfg))

	// Verify all collectors are configured.
	assert.Len(t, cfg.Collectors, 7)
	for _, name := range []string{"todos", "gitlog", "patterns", "lotteryrisk", "github", "dephealth", "vuln"} {
		cc, ok := cfg.Collectors[name]
		require.True(t, ok, "collector %s should be in config", name)
		require.NotNil(t, cc.Enabled, "collector %s should have enabled field", name)
	}
}

func TestGenerateConfig_WithWizardResults(t *testing.T) {
	dir := t.TempDir()

	wizard := &WizardResult{
		Collectors: map[string]bool{
			"todos":       true,
			"gitlog":      true,
			"patterns":    false,
			"lotteryrisk": true,
			"github":      false,
			"dephealth":   true,
			"vuln":        false,
		},
		GitDepth:         500,
		GitSince:         "6m",
		LargeFileThresh:  300,
		LotteryThreshold: 60,
	}

	action, err := GenerateConfig(dir, false, false, wizard)
	require.NoError(t, err)
	assert.Equal(t, "created", action.Operation)
	assert.Contains(t, action.Description, "wizard")

	data, err := os.ReadFile(filepath.Join(dir, config.FileName)) //nolint:gosec // test path
	require.NoError(t, err)
	content := string(data)

	// Verify wizard values were applied.
	assert.Contains(t, content, "git_depth: 500")
	assert.Contains(t, content, `git_since: "6m"`)
	assert.Contains(t, content, "large_file_threshold: 300")
	assert.Contains(t, content, "lottery_risk_threshold: 60")

	// Parse and verify collector enabled states.
	var cfg config.Config
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	assert.True(t, *cfg.Collectors["todos"].Enabled)
	assert.False(t, *cfg.Collectors["patterns"].Enabled)
	assert.False(t, *cfg.Collectors["github"].Enabled)
	assert.False(t, *cfg.Collectors["vuln"].Enabled)
}

func TestGenerateConfig_IncludesDocComments(t *testing.T) {
	dir := t.TempDir()

	_, err := GenerateConfig(dir, true, false, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, config.FileName)) //nolint:gosec // test path
	require.NoError(t, err)
	content := string(data)

	// Documentation comments should be present.
	assert.Contains(t, content, "Scans source code for TODO")
	assert.Contains(t, content, "Analyzes git history")
	assert.Contains(t, content, "knowledge silos")
	assert.Contains(t, content, "known vulnerabilities")
	assert.Contains(t, content, "GITHUB_TOKEN")
}
