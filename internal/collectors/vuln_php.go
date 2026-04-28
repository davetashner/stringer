// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"encoding/json"
	"strings"
)

// composerJSON represents the subset of composer.json we need for dependency extraction.
type composerJSON struct {
	Require    map[string]string `json:"require"`
	RequireDev map[string]string `json:"require-dev"`
}

// parseComposerDeps parses a composer.json file and returns PackageQuery entries for OSV lookup.
// It extracts both require and require-dev sections. PHP platform requirements (php, ext-*)
// are skipped. Semver range prefixes (^, ~, >=, etc.) are stripped to extract the base version.
func parseComposerDeps(data []byte) ([]PackageQuery, error) {
	var pkg composerJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var queries []PackageQuery

	for _, deps := range []map[string]string{pkg.Require, pkg.RequireDev} {
		for name, version := range deps {
			if seen[name] {
				continue
			}

			// Skip PHP platform requirements.
			if name == "php" || strings.HasPrefix(name, "ext-") || strings.HasPrefix(name, "lib-") {
				continue
			}

			// Must be vendor/package format.
			if !strings.Contains(name, "/") {
				continue
			}

			v := extractComposerVersion(version)
			if v == "" {
				continue
			}

			seen[name] = true
			queries = append(queries, PackageQuery{
				Ecosystem: "Packagist",
				Name:      name,
				Version:   v,
			})
		}
	}

	return queries, nil
}

// extractComposerVersion strips Composer semver constraint prefixes and returns the base version.
// Returns "" for versions that can't be meaningfully queried (wildcards, aliases, branches).
func extractComposerVersion(version string) string {
	version = strings.TrimSpace(version)

	if version == "" || version == "*" {
		return ""
	}

	// Skip branch aliases (e.g., "dev-main", "dev-master").
	if strings.HasPrefix(version, "dev-") {
		return ""
	}

	// Skip inline aliases (e.g., "1.0.x-dev as 1.0.0").
	if strings.Contains(version, " as ") {
		return ""
	}

	// For OR constraints (e.g., "^1.0 || ^2.0"), take the first segment.
	if idx := strings.Index(version, "||"); idx >= 0 {
		version = strings.TrimSpace(version[:idx])
	}

	// For AND constraints with comma (e.g., ">=1.0,<2.0"), take the first part.
	if idx := strings.Index(version, ","); idx >= 0 {
		version = version[:idx]
	}

	// For space-separated bounds (e.g., ">=1.0 <2.0"), take the first part.
	if idx := strings.Index(version, " "); idx >= 0 {
		version = version[:idx]
	}

	// Strip semver constraint prefixes.
	version = strings.TrimLeft(version, "^~>=<!=v")
	version = strings.TrimSpace(version)

	// Skip if nothing left or starts with non-digit.
	if version == "" || (version[0] < '0' || version[0] > '9') {
		return ""
	}

	return version
}
