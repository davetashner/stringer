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

// Security tests for ResolvePath (DX1.8).

func TestResolvePath_SecurityTraversalAttempts(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"parent traversal", "../../../etc"},
		{"absolute etc passwd", "/etc/passwd"},
		{"absolute etc shadow", "/etc/shadow"},
		{"dot dot slash", "../../.."},
		{"encoded traversal literal", "..%2f..%2f.."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ResolvePath(tt.path)
			if err == nil {
				// If it resolved, it must NOT point to a sensitive system path.
				assert.NotEqual(t, "/etc/passwd", result.AbsPath)
				assert.NotEqual(t, "/etc/shadow", result.AbsPath)
				// And it must be a directory (ResolvePath validates this).
				info, statErr := os.Stat(result.AbsPath)
				if statErr == nil {
					assert.True(t, info.IsDir(), "resolved path must be a directory")
				}
			}
			// Either returns error or resolves to a safe directory — both acceptable.
		})
	}
}

func TestResolvePath_SecurityNullBytesInPath(t *testing.T) {
	_, err := ResolvePath("some\x00path")
	require.Error(t, err, "paths with null bytes must be rejected")
}

func TestResolvePath_SecuritySymlinkToFile_Rejected(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("data"), 0o600))

	linkPath := filepath.Join(dir, "link-to-file")
	require.NoError(t, os.Symlink(filePath, linkPath))

	_, err := ResolvePath(linkPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestResolvePath_SecuritySymlinkToValidDir(t *testing.T) {
	// Create a real directory.
	realDir := t.TempDir()
	realDir, err := filepath.EvalSymlinks(realDir)
	require.NoError(t, err)

	// Create a symlink pointing to it.
	linkParent := t.TempDir()
	linkPath := filepath.Join(linkParent, "linked-dir")
	require.NoError(t, os.Symlink(realDir, linkPath))

	result, err := ResolvePath(linkPath)
	require.NoError(t, err)

	// AbsPath must be the real directory, not the symlink.
	assert.Equal(t, realDir, result.AbsPath, "should resolve symlink to real path")
}
