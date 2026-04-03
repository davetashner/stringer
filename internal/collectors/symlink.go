// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"path/filepath"
	"strings"
)

// isSymlinkOutsideRepo returns true if path resolves to a location outside repoRoot.
// It also returns true when the symlink cannot be resolved, treating unresolvable
// symlinks as outside the repo for safety.
func isSymlinkOutsideRepo(path, repoRoot string) bool {
	resolved, err := FS.EvalSymlinks(path)
	if err != nil {
		return true
	}
	return !strings.HasPrefix(resolved, repoRoot+string(filepath.Separator)) && resolved != repoRoot
}
