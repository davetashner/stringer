// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"github.com/BurntSushi/toml"
)

// cargoManifest represents the subset of Cargo.toml we need for dependency extraction.
type cargoManifest struct {
	Dependencies map[string]any `toml:"dependencies"`
}

// parseCargoDeps parses a Cargo.toml file and returns PackageQuery entries for OSV lookup.
// It handles both string notation (serde = "1.0") and table notation (serde = { version = "1.0" }).
// Dependencies with path or git sources are skipped (local/git-only deps).
// Dev-dependencies and build-dependencies are not parsed.
func parseCargoDeps(data []byte) ([]PackageQuery, error) {
	var manifest cargoManifest
	if err := toml.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}

	if len(manifest.Dependencies) == 0 {
		return nil, nil
	}

	var queries []PackageQuery
	for name, val := range manifest.Dependencies {
		var version string
		switch v := val.(type) {
		case string:
			version = v
		case map[string]any:
			// Skip path-only or git-only dependencies.
			if _, hasPath := v["path"]; hasPath {
				continue
			}
			if _, hasGit := v["git"]; hasGit {
				continue
			}
			if ver, ok := v["version"].(string); ok {
				version = ver
			}
		}

		if version == "" {
			continue
		}

		queries = append(queries, PackageQuery{
			Ecosystem: "crates.io",
			Name:      name,
			Version:   version,
		})
	}

	return queries, nil
}
