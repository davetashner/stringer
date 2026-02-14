// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package workspace

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// pnpmWorkspace represents the structure of a pnpm-workspace.yaml file.
type pnpmWorkspace struct {
	Packages []string `yaml:"packages"`
}

// detectPnpm detects a pnpm workspace defined by pnpm-workspace.yaml.
func detectPnpm(rootPath string) (*Layout, error) {
	wsFile := filepath.Join(rootPath, "pnpm-workspace.yaml")
	if !fileExists(wsFile) {
		return nil, nil
	}

	data, err := os.ReadFile(wsFile) //nolint:gosec // trusted path from caller
	if err != nil {
		return nil, err
	}

	var ws pnpmWorkspace
	if err := yaml.Unmarshal(data, &ws); err != nil {
		return nil, err
	}

	dirs, err := expandGlobs(rootPath, ws.Packages)
	if err != nil {
		return nil, err
	}

	if len(dirs) == 0 {
		return nil, nil
	}

	return &Layout{
		Kind:       KindPnpm,
		Root:       rootPath,
		Workspaces: dirsToWorkspaces(rootPath, dirs),
	}, nil
}
