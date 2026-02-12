package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGlobalConfigDir_Default(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	dir := GlobalConfigDir()
	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, ".config", "stringer"), dir)
}

func TestGlobalConfigDir_XDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	dir := GlobalConfigDir()
	assert.Equal(t, "/custom/config/stringer", dir)
}

func TestGlobalConfigPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	path := GlobalConfigPath()
	assert.Equal(t, "/custom/config/stringer/config.yaml", path)
}

func TestLoadGlobal_Missing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, err := LoadGlobal()
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, "", cfg.OutputFormat)
}

func TestLoadGlobal_Valid(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfgDir := filepath.Join(dir, "stringer")
	require.NoError(t, os.MkdirAll(cfgDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(cfgDir, "config.yaml"),
		[]byte("output_format: json\nno_llm: true\n"),
		0o600,
	))

	cfg, err := LoadGlobal()
	require.NoError(t, err)
	assert.Equal(t, "json", cfg.OutputFormat)
	assert.True(t, cfg.NoLLM)
}

func TestLoadGlobal_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfgDir := filepath.Join(dir, "stringer")
	require.NoError(t, os.MkdirAll(cfgDir, 0o750))
	require.NoError(t, os.WriteFile(
		filepath.Join(cfgDir, "config.yaml"),
		[]byte("{{invalid yaml"),
		0o600,
	))

	_, err := LoadGlobal()
	assert.Error(t, err)
}
