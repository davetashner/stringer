// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/davetashner/stringer/internal/testable"
	"github.com/stretchr/testify/assert"
)

func TestIsSymlinkOutsideRepo_InsideRepo(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "sub", "file.go")
	require := assert.New(t)

	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		EvalSymlinksFn: func(_ string) (string, error) {
			return child, nil
		},
	}

	require.False(isSymlinkOutsideRepo(child, dir))
}

func TestIsSymlinkOutsideRepo_OutsideRepo(t *testing.T) {
	repoRoot := filepath.Join(os.TempDir(), "repo")
	outsidePath := filepath.Join(os.TempDir(), "outside", "secret.go")

	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		EvalSymlinksFn: func(_ string) (string, error) {
			return outsidePath, nil
		},
	}

	assert.True(t, isSymlinkOutsideRepo("/some/link", repoRoot))
}

func TestIsSymlinkOutsideRepo_NonSymlink(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "real.go")

	oldFS := FS
	defer func() { FS = oldFS }()

	// EvalSymlinks on a real file returns the same path.
	FS = &testable.MockFileSystem{
		EvalSymlinksFn: func(_ string) (string, error) {
			return file, nil
		},
	}

	assert.False(t, isSymlinkOutsideRepo(file, dir))
}

func TestIsSymlinkOutsideRepo_EvalError(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		EvalSymlinksFn: func(_ string) (string, error) {
			return "", fmt.Errorf("permission denied")
		},
	}

	assert.True(t, isSymlinkOutsideRepo("/any/path", "/repo"))
}

func TestIsSymlinkOutsideRepo_ExactRepoRoot(t *testing.T) {
	repoRoot := filepath.Join(os.TempDir(), "repo")

	oldFS := FS
	defer func() { FS = oldFS }()

	// A symlink that resolves to exactly the repo root should not be
	// considered outside.
	FS = &testable.MockFileSystem{
		EvalSymlinksFn: func(_ string) (string, error) {
			return repoRoot, nil
		},
	}

	assert.False(t, isSymlinkOutsideRepo("/some/link", repoRoot))
}
