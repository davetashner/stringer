package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/config"
)

func TestConfigCmd_IsRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "config" {
			found = true
			break
		}
	}
	assert.True(t, found, "config command should be registered on rootCmd")
}

func TestConfigSubcommands_AreRegistered(t *testing.T) {
	subs := map[string]bool{}
	for _, cmd := range configCmd.Commands() {
		subs[cmd.Name()] = true
	}
	assert.True(t, subs["get"], "get subcommand should be registered")
	assert.True(t, subs["set"], "set subcommand should be registered")
	assert.True(t, subs["list"], "list subcommand should be registered")
}

func TestConfigGet_TopLevel(t *testing.T) {
	resetConfigFlags()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, config.FileName),
		[]byte("output_format: json\n"),
		0o600,
	))

	// Run from temp dir with config file.
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"config", "get", "output_format"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "json")
}

func TestConfigGet_Nested(t *testing.T) {
	resetConfigFlags()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, config.FileName),
		[]byte("collectors:\n  todos:\n    min_confidence: 0.8\n"),
		0o600,
	))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"config", "get", "collectors.todos.min_confidence"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "0.8")
}

func TestConfigGet_NotFound(t *testing.T) {
	resetConfigFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"config", "get", "output_format"})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestConfigGet_CollectorBlock(t *testing.T) {
	resetConfigFlags()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, config.FileName),
		[]byte("collectors:\n  todos:\n    min_confidence: 0.5\n    error_mode: warn\n"),
		0o600,
	))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"config", "get", "collectors.todos"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	out := stdout.String()
	assert.Contains(t, out, "min_confidence")
	assert.Contains(t, out, "error_mode")
}

func TestConfigGet_Global(t *testing.T) {
	resetConfigFlags()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfgDir := filepath.Join(dir, "stringer")
	require.NoError(t, os.MkdirAll(cfgDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(cfgDir, "config.yaml"),
		[]byte("no_llm: true\n"),
		0o600,
	))

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"config", "get", "--global", "no_llm"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "true")
}

func TestConfigGet_RequiresOneArg(t *testing.T) {
	resetConfigFlags()
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"config", "get"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestConfigSet_Simple(t *testing.T) {
	resetConfigFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"config", "set", "output_format", "json"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Set output_format = json")

	// Verify the file was created.
	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "json", cfg.OutputFormat)
}

func TestConfigSet_Nested(t *testing.T) {
	resetConfigFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"config", "set", "collectors.todos.min_confidence", "0.8"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	cfg, err := config.Load(dir)
	require.NoError(t, err)
	require.Contains(t, cfg.Collectors, "todos")
	assert.InDelta(t, 0.8, cfg.Collectors["todos"].MinConfidence, 0.001)
}

func TestConfigSet_InvalidKey(t *testing.T) {
	resetConfigFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"config", "set", "invalid_key", "value"})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown key")
}

func TestConfigSet_InvalidValue(t *testing.T) {
	resetConfigFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"config", "set", "output_format", "invalid_format"})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestConfigSet_Global(t *testing.T) {
	resetConfigFlags()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"config", "set", "--global", "no_llm", "true"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Set no_llm = true")

	// Verify the file was created.
	cfg, err := config.LoadGlobal()
	require.NoError(t, err)
	assert.True(t, cfg.NoLLM)
}

func TestConfigSet_PreservesExisting(t *testing.T) {
	resetConfigFlags()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, config.FileName),
		[]byte("output_format: json\nmax_issues: 50\n"),
		0o600,
	))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"config", "set", "no_llm", "true"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	cfg, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "json", cfg.OutputFormat)
	assert.Equal(t, 50, cfg.MaxIssues)
	assert.True(t, cfg.NoLLM)
}

func TestConfigSet_PriorityOverrides(t *testing.T) {
	resetConfigFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"config", "set", "priority_overrides", "foo"})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "edit")
}

func TestConfigSet_RequiresTwoArgs(t *testing.T) {
	resetConfigFlags()
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"config", "set", "key_only"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestConfigList_Empty(t *testing.T) {
	resetConfigFlags()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"config", "list"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "No configuration set")
}

func TestConfigList_ShowsRepoValues(t *testing.T) {
	resetConfigFlags()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, config.FileName),
		[]byte("output_format: json\nmax_issues: 50\n"),
		0o600,
	))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"config", "list"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	out := stdout.String()
	assert.Contains(t, out, "output_format")
	assert.Contains(t, out, "json")
	assert.Contains(t, out, "max_issues")
	assert.Contains(t, out, "50")
	assert.Contains(t, out, "repo")
}

func TestConfigList_ShowsBothSources(t *testing.T) {
	resetConfigFlags()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Create global config.
	cfgDir := filepath.Join(dir, "stringer")
	require.NoError(t, os.MkdirAll(cfgDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(cfgDir, "config.yaml"),
		[]byte("no_llm: true\n"),
		0o600,
	))

	// Create repo config in cwd.
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, config.FileName),
		[]byte("output_format: json\n"),
		0o600,
	))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"config", "list"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	out := stdout.String()
	assert.Contains(t, out, "global")
	assert.Contains(t, out, "repo")
}

func TestConfigList_RejectsArgs(t *testing.T) {
	resetConfigFlags()
	rootCmd.SetOut(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"config", "list", "extra"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestConfigCmd_Help(t *testing.T) {
	resetConfigFlags()
	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"config", "--help"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	out := stdout.String()
	assert.Contains(t, out, "View and modify")
	assert.Contains(t, out, "get")
	assert.Contains(t, out, "set")
	assert.Contains(t, out, "list")
}

func TestConfigGetCmd_GlobalFlag(t *testing.T) {
	f := configGetCmd.Flags().Lookup("global")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
}

func TestConfigSetCmd_GlobalFlag(t *testing.T) {
	f := configSetCmd.Flags().Lookup("global")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
}
