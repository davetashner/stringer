package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/config"
)

func TestCollectorsCmd_IsRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "collectors" {
			found = true
			break
		}
	}
	assert.True(t, found, "collectors command should be registered on rootCmd")
}

func TestCollectorsSubcommands_AreRegistered(t *testing.T) {
	subs := map[string]bool{}
	for _, cmd := range collectorsCmd.Commands() {
		subs[cmd.Name()] = true
	}
	assert.True(t, subs["list"], "list subcommand should be registered")
	assert.True(t, subs["info"], "info subcommand should be registered")
}

func TestCollectorsList_ShowsAllCollectors(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"collectors", "list"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	// All registered collectors should appear.
	for _, name := range collector.List() {
		assert.Contains(t, out, name, "output should contain collector %q", name)
	}
	// Should have a header.
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "STATUS")
	assert.Contains(t, out, "DESCRIPTION")
}

func TestCollectorsList_ShowsDisabled(t *testing.T) {
	dir := t.TempDir()
	disabled := false
	yamlContent := "collectors:\n  todos:\n    enabled: false\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.FileName), []byte(yamlContent), 0o600))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	_ = disabled // suppress unused var warning

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"collectors", "list"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "disabled")
}

func TestCollectorsList_NoConfig(t *testing.T) {
	// Should work even with no config file — all collectors shown as enabled.
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"collectors", "list"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "enabled")
	assert.NotContains(t, out, "disabled")
}

func TestCollectorsList_RejectsArgs(t *testing.T) {
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"collectors", "list", "extra"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestCollectorsInfo_ValidCollector(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"collectors", "info", "todos"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Collector:")
	assert.Contains(t, out, "todos")
	assert.Contains(t, out, "Description:")
	assert.Contains(t, out, "Signal types:")
	assert.Contains(t, out, "todo")
	assert.Contains(t, out, "fixme")
	assert.Contains(t, out, "Configuration options:")
	assert.Contains(t, out, "enabled:")
	assert.Contains(t, out, "min_confidence:")
}

func TestCollectorsInfo_WithConfig(t *testing.T) {
	dir := t.TempDir()
	yamlContent := "collectors:\n  gitlog:\n    git_depth: 500\n    git_since: 90d\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.FileName), []byte(yamlContent), 0o600))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"collectors", "info", "gitlog"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "gitlog")
	assert.Contains(t, out, "git_depth:")
	assert.Contains(t, out, "500")
	assert.Contains(t, out, "git_since:")
	assert.Contains(t, out, "90d")
}

func TestCollectorsInfo_DisabledCollector(t *testing.T) {
	dir := t.TempDir()
	yamlContent := "collectors:\n  patterns:\n    enabled: false\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, config.FileName), []byte(yamlContent), 0o600))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"collectors", "info", "patterns"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "disabled")
}

func TestCollectorsInfo_UnknownCollector(t *testing.T) {
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"collectors", "info", "nonexistent"})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown collector")
}

func TestCollectorsInfo_RequiresOneArg(t *testing.T) {
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"collectors", "info"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestCollectorsInfo_AllKnownCollectors(t *testing.T) {
	// Every registered collector should have metadata in knownCollectors.
	for _, name := range collector.List() {
		_, ok := knownCollectors[name]
		assert.True(t, ok, "collector %q should have metadata in knownCollectors", name)
	}
}

func TestCollectorsInfo_LotteryRisk(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"collectors", "info", "lotteryrisk"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "lottery_risk_threshold:")
	assert.Contains(t, out, "directory_depth:")
	assert.Contains(t, out, "max_blame_files:")
}

func TestCollectorsInfo_GitHub(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"collectors", "info", "github"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "include_prs:")
	assert.Contains(t, out, "comment_depth:")
	assert.Contains(t, out, "max_issues_per_collector:")
}

func TestCollectorsCmd_Help(t *testing.T) {
	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"collectors", "--help"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	out := stdout.String()
	assert.Contains(t, out, "list")
	assert.Contains(t, out, "info")
}

func TestFormatFieldValue_Pointer(t *testing.T) {
	tests := []struct {
		name string
		cc   config.CollectorConfig
		want string
	}{
		{
			name: "nil pointer shows default",
			cc:   config.CollectorConfig{},
			want: "(default)",
		},
		{
			name: "false pointer shows false",
			cc:   config.CollectorConfig{Enabled: boolPtr(false)},
			want: "false",
		},
		{
			name: "true pointer shows true",
			cc:   config.CollectorConfig{Enabled: boolPtr(true)},
			want: "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout := new(bytes.Buffer)
			printConfigFields(stdout, tt.cc, []string{"enabled"})
			assert.Contains(t, stdout.String(), tt.want)
		})
	}
}

func TestFormatFieldValue_Slices(t *testing.T) {
	stdout := new(bytes.Buffer)
	cc := config.CollectorConfig{
		IncludePatterns: []string{"*.go", "*.py"},
	}
	printConfigFields(stdout, cc, []string{"include_patterns"})
	out := stdout.String()
	assert.Contains(t, out, "*.go")
	assert.Contains(t, out, "*.py")
}

func TestFormatFieldValue_EmptySlice(t *testing.T) {
	stdout := new(bytes.Buffer)
	cc := config.CollectorConfig{}
	printConfigFields(stdout, cc, []string{"include_patterns"})
	assert.Contains(t, stdout.String(), "(none)")
}

func boolPtr(b bool) *bool {
	return &b
}
