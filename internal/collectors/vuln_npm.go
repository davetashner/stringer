// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"encoding/json"
	"strings"
)

// packageJSON represents the subset of package.json we need for dependency extraction.
type packageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// parseNpmDeps parses a package.json file and returns PackageQuery entries for OSV lookup.
// It extracts both dependencies and devDependencies. Semver range prefixes (^, ~, >=, etc.)
// are stripped to extract the base version. Entries with wildcard (*), latest, or URL-based
// versions are skipped.
func parseNpmDeps(data []byte) ([]PackageQuery, error) {
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var queries []PackageQuery

	for _, deps := range []map[string]string{pkg.Dependencies, pkg.DevDependencies} {
		for name, version := range deps {
			if seen[name] {
				continue
			}

			v := extractNpmVersion(version)
			if v == "" {
				continue
			}

			seen[name] = true
			queries = append(queries, PackageQuery{
				Ecosystem: "npm",
				Name:      name,
				Version:   v,
			})
		}
	}

	return queries, nil
}

// packageLock represents the subset of package-lock.json (v2/v3) we need for dependency extraction.
type packageLock struct {
	Packages map[string]packageLockEntry `json:"packages"`
}

// packageLockEntry represents a single resolved package in the lockfile.
type packageLockEntry struct {
	Version string `json:"version"`
	Dev     bool   `json:"dev"`
}

// parseNpmLockDeps parses a package-lock.json (v2/v3) file and returns PackageQuery entries
// with resolved versions. Entries under "node_modules/" keys are included; the root "" entry
// is skipped. Nested node_modules paths are handled by extracting the final package name.
func parseNpmLockDeps(data []byte) ([]PackageQuery, error) {
	var lock packageLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var queries []PackageQuery

	for path, entry := range lock.Packages {
		// Skip root entry.
		if path == "" {
			continue
		}

		// Only process node_modules entries.
		if !strings.HasPrefix(path, "node_modules/") {
			continue
		}

		if entry.Version == "" {
			continue
		}

		// Extract package name: take the portion after the last "node_modules/".
		// This handles nested deps like "node_modules/a/node_modules/@scope/pkg".
		name := path
		if idx := strings.LastIndex(path, "node_modules/"); idx >= 0 {
			name = path[idx+len("node_modules/"):]
		}

		if seen[name] {
			continue
		}
		seen[name] = true

		queries = append(queries, PackageQuery{
			Ecosystem: "npm",
			Name:      name,
			Version:   entry.Version,
		})
	}

	return queries, nil
}

// extractNpmVersion strips semver range prefixes and returns the base version string.
// Returns "" for versions that can't be meaningfully queried (wildcards, URLs, tags).
func extractNpmVersion(version string) string {
	version = strings.TrimSpace(version)

	if version == "" || version == "*" || version == "latest" || version == "next" {
		return ""
	}

	// Skip URL-based versions (git, http, file, etc.).
	if strings.Contains(version, "://") || strings.HasPrefix(version, "git+") ||
		strings.HasPrefix(version, "file:") || strings.HasPrefix(version, "link:") {
		return ""
	}

	// Skip workspace references.
	if strings.HasPrefix(version, "workspace:") {
		return ""
	}

	// For range expressions with ||, take the first segment.
	if idx := strings.Index(version, "||"); idx >= 0 {
		version = strings.TrimSpace(version[:idx])
	}

	// For range expressions with space-separated bounds (e.g. ">=1.0.0 <2.0.0"),
	// take the first part.
	if idx := strings.Index(version, " "); idx >= 0 {
		version = version[:idx]
	}

	// Strip semver range prefixes.
	version = strings.TrimLeft(version, "^~>=<!")
	version = strings.TrimSpace(version)

	// Skip if nothing left or starts with non-digit (tag names like "beta").
	if version == "" || (version[0] < '0' || version[0] > '9') {
		return ""
	}

	return version
}
