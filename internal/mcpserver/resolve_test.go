// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package mcpserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePath_ValidDirectory(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	info, err := ResolvePath(dir)
	require.NoError(t, err)
	assert.Equal(t, dir, info.AbsPath)
	// No .git, so GitRoot should equal AbsPath.
	assert.Equal(t, dir, info.GitRoot)
}

func TestResolvePath_EmptyDefaultsToCwd(t *testing.T) {
	info, err := ResolvePath("")
	require.NoError(t, err)
	assert.NotEmpty(t, info.AbsPath)
}

func TestResolvePath_NonexistentPath(t *testing.T) {
	_, err := ResolvePath("/nonexistent/path/that/does/not/exist")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot resolve path")
}

func TestResolvePath_NotADirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(file, []byte("hello"), 0o600))

	_, err := ResolvePath(file)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestResolvePath_DetectsGitRoot(t *testing.T) {
	dir := t.TempDir()
	dir, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)

	// Create .git directory at root.
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".git"), 0o750))

	// Create a subdirectory.
	subdir := filepath.Join(dir, "sub", "deep")
	require.NoError(t, os.MkdirAll(subdir, 0o750))

	// Resolve the subdirectory — should find git root at parent.
	info, err := ResolvePath(subdir)
	require.NoError(t, err)
	assert.Equal(t, subdir, info.AbsPath)
	assert.Equal(t, dir, info.GitRoot)
}
