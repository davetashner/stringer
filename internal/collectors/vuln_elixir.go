// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"regexp"
	"strings"
)

// mixDepsRe matches {:package_name, "~> version"} entries in mix.exs files.
// Handles various version constraint formats used by Hex.
var mixDepsRe = regexp.MustCompile(
	`\{:(\w+)\s*,\s*"([^"]+)"`,
)

// parseMixDeps parses a mix.exs file and returns PackageQuery entries for OSV lookup.
// It extracts dependencies from {:name, "version"} tuples. Elixir version constraints
// (~>, >=, ==, etc.) are stripped to extract the base version. Dependencies with
// path or git options are skipped.
func parseMixDeps(data []byte) []PackageQuery {
	// First check for path/git dependencies to exclude them.
	content := string(data)
	matches := mixDepsRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var queries []PackageQuery

	for _, match := range matches {
		name := string(match[1])
		versionConstraint := string(match[2])

		if seen[name] {
			continue
		}

		// Check if this dependency has path: or git: options (look ahead in context).
		matchStr := string(match[0])
		matchIdx := strings.Index(content, matchStr)
		if matchIdx >= 0 {
			// Look at the rest of the tuple for path: or git: keywords.
			endIdx := matchIdx + len(matchStr) + 200
			if endIdx > len(content) {
				endIdx = len(content)
			}
			rest := content[matchIdx+len(matchStr) : endIdx]
			// Only look until the closing brace.
			if braceIdx := strings.Index(rest, "}"); braceIdx >= 0 {
				rest = rest[:braceIdx]
			}
			if strings.Contains(rest, "path:") || strings.Contains(rest, "git:") {
				continue
			}
		}

		version := extractMixVersion(versionConstraint)
		if version == "" {
			continue
		}

		seen[name] = true
		queries = append(queries, PackageQuery{
			Ecosystem: "Hex",
			Name:      name,
			Version:   version,
		})
	}

	return queries
}

// extractMixVersion strips Elixir/Hex version constraint prefixes and returns the
// base version. Returns "" for versions that can't be meaningfully queried.
func extractMixVersion(version string) string {
	version = strings.TrimSpace(version)

	if version == "" {
		return ""
	}

	// Strip constraint operators.
	version = strings.TrimLeft(version, "~>=<! ")
	version = strings.TrimSpace(version)

	// Skip if nothing left or starts with non-digit.
	if version == "" || (version[0] < '0' || version[0] > '9') {
		return ""
	}

	return version
}
