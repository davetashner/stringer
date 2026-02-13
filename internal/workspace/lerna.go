package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// lernaConfig represents the subset of lerna.json fields we need.
type lernaConfig struct {
	Packages []string `json:"packages"`
}

// detectLerna detects a Lerna monorepo defined by lerna.json with a
// "packages" array of glob patterns.
func detectLerna(rootPath string) (*Layout, error) {
	lernaFile := filepath.Join(rootPath, "lerna.json")
	if !fileExists(lernaFile) {
		return nil, nil
	}

	data, err := os.ReadFile(lernaFile) //nolint:gosec // trusted path from caller
	if err != nil {
		return nil, err
	}

	var cfg lernaConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if len(cfg.Packages) == 0 {
		return nil, nil
	}

	dirs, err := expandGlobs(rootPath, cfg.Packages)
	if err != nil {
		return nil, err
	}

	if len(dirs) == 0 {
		return nil, nil
	}

	return &Layout{
		Kind:       KindLerna,
		Root:       rootPath,
		Workspaces: dirsToWorkspaces(rootPath, dirs),
	}, nil
}
