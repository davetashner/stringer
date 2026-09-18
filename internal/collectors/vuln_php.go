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

	// require first, require-dev second: a package in both is production.
	for _, group := range []struct {
		deps map[string]string
		dev  bool
	}{{pkg.Require, false}, {pkg.RequireDev, true}} {
		for name, version := range group.deps {
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

			v, isRange := extractComposerVersion(version)
			if v == "" {
				continue
			}

			seen[name] = true
			q := PackageQuery{
				Ecosystem: "Packagist",
				Name:      name,
				Version:   v,
				Dev:       group.dev,
			}
			if isRange {
				q.IsRange = true
				q.Constraint = strings.TrimSpace(version)
			}
			queries = append(queries, q)
		}
	}

	return queries, nil
}

// extractComposerVersion reduces a Composer constraint to a concrete query
// version and reports whether the constraint is a range (^, ~, >=, ||, *)
// rather than an exact pin. Returns "" for versions that can't be
// meaningfully queried (wildcards, aliases, branches, dev versions).
func extractComposerVersion(version string) (string, bool) {
	version = strings.TrimSpace(version)

	// Skip branch aliases ("dev-main"), inline aliases ("1.0.x-dev as 1.0.0")
	// and dev branches ("1.0.x-dev").
	if version == "" || strings.HasPrefix(version, "dev-") ||
		strings.Contains(version, " as ") || strings.Contains(version, "-dev") {
		return "", false
	}

	// Drop stability flags ("^1.0@beta").
	if idx := strings.Index(version, "@"); idx >= 0 {
		version = version[:idx]
	}

	return splitSemverConstraint(version, false)
}
