// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package workspace

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// cargoConfig represents the subset of Cargo.toml fields we need.
type cargoConfig struct {
	Workspace *cargoWorkspace `toml:"workspace"`
}

type cargoWorkspace struct {
	Members []string `toml:"members"`
	Exclude []string `toml:"exclude"`
}

// detectCargo detects a Rust workspace defined by a [workspace] section
// in Cargo.toml with a "members" array of glob patterns.
func detectCargo(rootPath string) (*Layout, error) {
	cargoFile := filepath.Join(rootPath, "Cargo.toml")
	if !fileExists(cargoFile) {
		return nil, nil
	}

	data, err := os.ReadFile(cargoFile) //nolint:gosec // trusted path from caller
	if err != nil {
		return nil, err
	}

	var cfg cargoConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Workspace == nil || len(cfg.Workspace.Members) == 0 {
		return nil, nil
	}

	dirs, err := expandGlobs(rootPath, cfg.Workspace.Members)
	if err != nil {
		return nil, err
	}

	// Remove excluded directories.
	if len(cfg.Workspace.Exclude) > 0 {
		excludeDirs, err := expandGlobs(rootPath, cfg.Workspace.Exclude)
		if err != nil {
			return nil, err
		}
		excludeSet := make(map[string]bool, len(excludeDirs))
		for _, d := range excludeDirs {
			excludeSet[d] = true
		}
		var filtered []string
		for _, d := range dirs {
			if !excludeSet[d] {
				filtered = append(filtered, d)
			}
		}
		dirs = filtered
	}

	if len(dirs) == 0 {
		return nil, nil
	}

	return &Layout{
		Kind:       KindCargo,
		Root:       rootPath,
		Workspaces: dirsToWorkspaces(rootPath, dirs),
	}, nil
}
