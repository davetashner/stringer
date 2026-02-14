// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/config"
)

// Security tests for the init CLI command (DX1.7).

func TestInitCmd_SecuritySymlinkToFile_Rejected(t *testing.T) {
	resetInitFlags()
	dir := t.TempDir()
	filePath := filepath.Join(dir, "somefile.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("hello"), 0o600))

	// Create symlink to a file.
	linkPath := filepath.Join(dir, "link-to-file")
	require.NoError(t, os.Symlink(filePath, linkPath))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"init", linkPath})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestInitCmd_SecuritySymlinkToValidDir_Works(t *testing.T) {
	resetInitFlags()

	// Create real target directory.
	realDir := t.TempDir()
	realDir, err := filepath.EvalSymlinks(realDir)
	require.NoError(t, err)

	// Create a symlink to the real directory.
	linkParent := t.TempDir()
	linkPath := filepath.Join(linkParent, "linked-repo")
	require.NoError(t, os.Symlink(realDir, linkPath))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"init", linkPath, "--quiet"})

	err = cmd.Execute()
	require.NoError(t, err)

	// Files must be created in the real target, not the symlink parent.
	assert.FileExists(t, filepath.Join(realDir, config.FileName))
	assert.FileExists(t, filepath.Join(realDir, "AGENTS.md"))
	assert.NoFileExists(t, filepath.Join(linkParent, config.FileName))
}

func TestInitCmd_SecurityChainedSymlinks(t *testing.T) {
	resetInitFlags()

	// Create real directory.
	realDir := t.TempDir()
	realDir, err := filepath.EvalSymlinks(realDir)
	require.NoError(t, err)

	// Create A -> B -> realDir.
	linkParent := t.TempDir()
	linkB := filepath.Join(linkParent, "linkB")
	linkA := filepath.Join(linkParent, "linkA")
	require.NoError(t, os.Symlink(realDir, linkB))
	require.NoError(t, os.Symlink(linkB, linkA))

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"init", linkA, "--quiet"})

	err = cmd.Execute()
	require.NoError(t, err)

	// Files end up in the real directory.
	assert.FileExists(t, filepath.Join(realDir, config.FileName))
	assert.FileExists(t, filepath.Join(realDir, "AGENTS.md"))
}

func TestInitCmd_SecurityOutputFilesAre0644(t *testing.T) {
	resetInitFlags()
	dir := t.TempDir()

	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"init", dir, "--quiet"})

	err := cmd.Execute()
	require.NoError(t, err)

	for _, name := range []string{config.FileName, "AGENTS.md"} {
		info, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err, "stat %s", name)

		perm := info.Mode().Perm()
		assert.Equal(t, fs.FileMode(0o644), perm, "%s should be 0644", name)
		assert.Zero(t, perm&0o002, "%s must not be world-writable", name)
	}
}

func TestInitCmd_SecurityNonexistentParentTraversal(t *testing.T) {
	resetInitFlags()

	// Path like /nonexistent/../tmp — the parent component doesn't exist
	// so this should fail gracefully.
	cmd, _, _ := newTestCmd()
	cmd.SetArgs([]string{"init", "/nonexistent/../tmp/stringer-test"})

	err := cmd.Execute()
	require.Error(t, err)
	// Should get a path resolution or "does not exist" error, not a panic.
	assert.True(t,
		assert.ObjectsAreEqual(true, err != nil),
		"should return an error, not panic")
}
