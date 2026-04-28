// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"regexp"
	"strings"
)

// swiftPackageRe matches .package(url: "...", from: "...") and similar patterns
// in Package.swift dependency declarations.
var swiftPackageRe = regexp.MustCompile(
	`\.package\(\s*url:\s*"([^"]+)"` + // capture URL
		`\s*,\s*(?:from:\s*"([^"]+)"` + // from: "version"
		`|\.upToNextMajor\(from:\s*"([^"]+)"\)` + // .upToNextMajor(from: "version")
		`|\.upToNextMinor\(from:\s*"([^"]+)"\)` + // .upToNextMinor(from: "version")
		`|"([^"]+)"\s*\.\.\<\s*"[^"]+"` + // "1.0.0"..<"2.0.0"
		`|"([^"]+)"\s*\.\.\.\s*"[^"]+"` + // "1.0.0"..."2.0.0"
		`|exact:\s*"([^"]+)"` + // exact: "version"
		`)`,
)

// parseSwiftPackageDeps parses a Package.swift file and returns PackageQuery entries.
// It extracts dependency URLs and versions from .package() declarations.
// Dependencies without a parseable version are included with an empty version.
func parseSwiftPackageDeps(data []byte) []PackageQuery {
	matches := swiftPackageRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var queries []PackageQuery

	for _, match := range matches {
		url := string(match[1])
		if seen[url] {
			continue
		}
		seen[url] = true

		// Extract version from whichever capture group matched.
		var version string
		for _, group := range match[2:] {
			if len(group) > 0 {
				version = string(group)
				break
			}
		}

		// Normalize the URL: strip .git suffix for consistent naming.
		name := strings.TrimSuffix(url, ".git")

		queries = append(queries, PackageQuery{
			Ecosystem: "SwiftURL",
			Name:      name,
			Version:   version,
		})
	}

	return queries
}
