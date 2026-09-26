// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import "path/filepath"

// relSlash returns target relative to base in slash-separated form.
//
// Collectors key signals, exclude/include globs, path heuristics (test dirs,
// docs, internal/) and metrics on repo-relative paths, all of which are
// written with "/". filepath.Rel uses the OS separator, so on Windows its
// output must be converted before any of that logic sees it. Code that goes
// back to the filesystem should use the absolute walk path, or
// filepath.Join(root, rel), which accepts "/" on every OS.
func relSlash(base, target string) (string, error) {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}
