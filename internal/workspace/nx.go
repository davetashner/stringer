// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// nxDefaultPatterns are the conventional workspace directories in an Nx monorepo.
var nxDefaultPatterns = []string{"packages/*", "apps/*", "libs/*"}

// nxConfig represents the subset of nx.json fields we need.
type nxConfig struct {
	WorkspaceLayout *nxWorkspaceLayout `json:"workspaceLayout"`
}

type nxWorkspaceLayout struct {
	AppsDir string `json:"appsDir"`
	LibsDir string `json:"libsDir"`
}

// detectNx detects an Nx monorepo by the presence of nx.json.
// If workspaceLayout is defined, it uses those directories; otherwise
// it falls back to the conventional packages/*, apps/*, libs/* pattern.
func detectNx(rootPath string) (*Layout, error) {
	nxFile := filepath.Join(rootPath, "nx.json")
	if !fileExists(nxFile) {
		return nil, nil
	}

	data, err := os.ReadFile(nxFile) //nolint:gosec // trusted path from caller
	if err != nil {
		return nil, err
	}

	var cfg nxConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	patterns := nxDefaultPatterns
	if cfg.WorkspaceLayout != nil {
		patterns = nil
		if cfg.WorkspaceLayout.AppsDir != "" {
			patterns = append(patterns, cfg.WorkspaceLayout.AppsDir+"/*")
		}
		if cfg.WorkspaceLayout.LibsDir != "" {
			patterns = append(patterns, cfg.WorkspaceLayout.LibsDir+"/*")
		}
		if len(patterns) == 0 {
			patterns = nxDefaultPatterns
		}
	}

	dirs, err := expandGlobs(rootPath, patterns)
	if err != nil {
		return nil, err
	}

	if len(dirs) == 0 {
		return nil, nil
	}

	return &Layout{
		Kind:       KindNx,
		Root:       rootPath,
		Workspaces: dirsToWorkspaces(rootPath, dirs),
	}, nil
}
