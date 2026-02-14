// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_MissingFile(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Empty(t, cfg.OutputFormat)
	assert.Nil(t, cfg.Collectors)
}

func TestLoad_ValidFile(t *testing.T) {
	dir := t.TempDir()
	content := `
output_format: json
max_issues: 100
collectors:
  todos:
    min_confidence: 0.6
    exclude_patterns:
      - vendor/**
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o600))

	cfg, err := Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "json", cfg.OutputFormat)
	assert.Equal(t, 100, cfg.MaxIssues)
	require.Contains(t, cfg.Collectors, "todos")
	assert.InDelta(t, 0.6, cfg.Collectors["todos"].MinConfidence, 0.001)
	assert.Equal(t, []string{"vendor/**"}, cfg.Collectors["todos"].ExcludePatterns)
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, FileName), []byte("{{invalid yaml"), 0o600))

	cfg, err := Load(dir)
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

func TestLoad_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, FileName), []byte(""), 0o600))

	cfg, err := Load(dir)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Empty(t, cfg.OutputFormat)
}

func TestLoad_PermissionError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	require.NoError(t, os.WriteFile(path, []byte("output_format: json"), 0o600))

	// Remove read permission.
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() {
		_ = os.Chmod(path, 0o600) // restore for cleanup
	})

	cfg, err := Load(dir)
	assert.Error(t, err, "should fail when file is unreadable")
	assert.Nil(t, cfg)
}

func TestWrite(t *testing.T) {
	enabled := true
	cfg := &Config{
		OutputFormat: "markdown",
		MaxIssues:    25,
		Collectors: map[string]CollectorConfig{
			"todos": {
				Enabled:       &enabled,
				MinConfidence: 0.7,
			},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, Write(&buf, cfg))

	out := buf.String()
	assert.Contains(t, out, "output_format: markdown")
	assert.Contains(t, out, "max_issues: 25")
	assert.Contains(t, out, "min_confidence: 0.7")
}

func TestWrite_EmptyConfig(t *testing.T) {
	cfg := &Config{}
	var buf bytes.Buffer
	require.NoError(t, Write(&buf, cfg))
	assert.Contains(t, buf.String(), "{}")
}

func TestLoadRaw_MissingFile(t *testing.T) {
	m, err := LoadRaw(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	require.NoError(t, err)
	assert.NotNil(t, m)
	assert.Empty(t, m)
}

func TestLoadRaw_ValidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.yaml")
	require.NoError(t, os.WriteFile(path, []byte("output_format: json\nmax_issues: 50\n"), 0o600))

	m, err := LoadRaw(path)
	require.NoError(t, err)
	assert.Equal(t, "json", m["output_format"])
	assert.Equal(t, 50, m["max_issues"])
}

func TestLoadRaw_InvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte("{{invalid yaml"), 0o600))

	_, err := LoadRaw(path)
	assert.Error(t, err)
}

func TestLoadRaw_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.yaml")
	require.NoError(t, os.WriteFile(path, []byte(""), 0o600))

	m, err := LoadRaw(path)
	require.NoError(t, err)
	assert.NotNil(t, m)
	assert.Empty(t, m)
}

func TestWriteFile_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "config.yaml")

	data := map[string]any{"output_format": "json"}
	require.NoError(t, WriteFile(path, data))

	assert.FileExists(t, path)

	m, err := LoadRaw(path)
	require.NoError(t, err)
	assert.Equal(t, "json", m["output_format"])
}

func TestWriteFile_Overwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	require.NoError(t, WriteFile(path, map[string]any{"output_format": "json"}))
	require.NoError(t, WriteFile(path, map[string]any{"output_format": "markdown"}))

	m, err := LoadRaw(path)
	require.NoError(t, err)
	assert.Equal(t, "markdown", m["output_format"])
}
