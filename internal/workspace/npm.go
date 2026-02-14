// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// npmPackageJSON is the subset of package.json fields we need.
type npmPackageJSON struct {
	Workspaces json.RawMessage `json:"workspaces"`
}

// detectNpm detects an npm/yarn workspace defined by the "workspaces" field
// in the root package.json. The field can be either an array of globs or an
// object with a "packages" array.
func detectNpm(rootPath string) (*Layout, error) {
	pkgFile := filepath.Join(rootPath, "package.json")
	if !fileExists(pkgFile) {
		return nil, nil
	}

	data, err := os.ReadFile(pkgFile) //nolint:gosec // trusted path from caller
	if err != nil {
		return nil, err
	}

	var pkg npmPackageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}

	if pkg.Workspaces == nil {
		return nil, nil
	}

	patterns, err := parseWorkspacesField(pkg.Workspaces)
	if err != nil {
		return nil, err
	}

	if len(patterns) == 0 {
		return nil, nil
	}

	dirs, err := expandGlobs(rootPath, patterns)
	if err != nil {
		return nil, err
	}

	if len(dirs) == 0 {
		return nil, nil
	}

	return &Layout{
		Kind:       KindNpm,
		Root:       rootPath,
		Workspaces: dirsToWorkspaces(rootPath, dirs),
	}, nil
}

// parseWorkspacesField handles both the array form ["packages/*"] and the
// object form {"packages": ["packages/*"]}.
func parseWorkspacesField(raw json.RawMessage) ([]string, error) {
	// Try array form first.
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}

	// Try object form.
	var obj struct {
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return obj.Packages, nil
}
