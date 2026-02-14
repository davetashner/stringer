// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"regexp"
	"strings"
)

// gradleTestConfigs are test-scoped Gradle configurations (skipped during parsing).
var gradleTestConfigs = map[string]bool{
	"testimplementation": true,
	"testcompileonly":    true,
	"testruntimeonly":    true,
}

// allGradleConfigs lists all recognized Gradle dependency configurations.
// Test configs come first so that extractConfig matches "testImplementation"
// before "implementation" (longest-prefix-first ordering).
var allGradleConfigs = []string{
	"testimplementation",
	"testcompileonly",
	"testruntimeonly",
	"implementation",
	"compileonly",
	"runtimeonly",
	"classpath",
	"compile",
	"api",
}

// configPattern is the regex alternation of all known Gradle configurations.
var configPattern = "(?:" + strings.Join(allGradleConfigs, "|") + ")"

var (
	// reStringNotation matches Groovy string notation and Kotlin DSL:
	//   implementation 'group:artifact:version'
	//   implementation "group:artifact:version"
	//   implementation("group:artifact:version")
	reStringNotation = regexp.MustCompile(
		`(?im)^\s*` + configPattern + `\s*[\(]?\s*['"]([^'"]+)['"]\s*[\)]?\s*$`,
	)

	// Map-style field extractors.
	reMapGroup = regexp.MustCompile(`(?i)group\s*:\s*['"]([^'"]+)['"]`)
	reMapName  = regexp.MustCompile(`(?i)name\s*:\s*['"]([^'"]+)['"]`)
	reMapVer   = regexp.MustCompile(`(?i)version\s*:\s*['"]([^'"]+)['"]`)

	// reMapNotation matches a line starting with a config followed by map-style args.
	reMapNotation = regexp.MustCompile(
		`(?im)^\s*` + configPattern + `[\s(]+.*group\s*:`,
	)
)

// parseGradleDeps reads a build.gradle or build.gradle.kts file and returns
// PackageQuery entries for OSV lookup.
func parseGradleDeps(data []byte) ([]PackageQuery, error) {
	lines := strings.Split(string(data), "\n")
	var queries []PackageQuery
	seen := make(map[string]bool)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments.
		if trimmed == "" || strings.HasPrefix(trimmed, "//") ||
			strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "/*") {
			continue
		}

		// Try string notation first (covers both Groovy and Kotlin DSL).
		if m := reStringNotation.FindStringSubmatch(line); m != nil {
			config := extractConfig(trimmed)
			if isTestConfig(config) {
				continue
			}
			q := parseCoordinates(m[1])
			if q != nil && !seen[q.Name+"@"+q.Version] {
				seen[q.Name+"@"+q.Version] = true
				queries = append(queries, *q)
			}
			continue
		}

		// Try map-style notation.
		if reMapNotation.MatchString(line) {
			config := extractConfig(trimmed)
			if isTestConfig(config) {
				continue
			}
			q := parseMapNotation(line)
			if q != nil && !seen[q.Name+"@"+q.Version] {
				seen[q.Name+"@"+q.Version] = true
				queries = append(queries, *q)
			}
		}
	}

	return queries, nil
}

// extractConfig returns the lowercased configuration name from a dependency line.
// It checks longer config names first to avoid "testimplementation" matching "implementation".
func extractConfig(line string) string {
	lower := strings.ToLower(strings.TrimSpace(line))
	for _, cfg := range allGradleConfigs {
		if strings.HasPrefix(lower, cfg) {
			return cfg
		}
	}
	return ""
}

// isTestConfig returns true if the configuration is test-scoped.
func isTestConfig(config string) bool {
	return gradleTestConfigs[config]
}

// parseCoordinates parses a "group:artifact:version" string into a PackageQuery.
// Returns nil if the format is invalid or version is missing.
func parseCoordinates(coords string) *PackageQuery {
	parts := strings.Split(coords, ":")
	if len(parts) < 3 || parts[2] == "" {
		return nil
	}
	return &PackageQuery{
		Ecosystem: "Maven",
		Name:      parts[0] + ":" + parts[1],
		Version:   parts[2],
	}
}

// parseMapNotation extracts group, name, version from a map-style dependency declaration.
// Returns nil if any required field is missing.
func parseMapNotation(line string) *PackageQuery {
	groupMatch := reMapGroup.FindStringSubmatch(line)
	nameMatch := reMapName.FindStringSubmatch(line)
	verMatch := reMapVer.FindStringSubmatch(line)

	if groupMatch == nil || nameMatch == nil || verMatch == nil {
		return nil
	}

	return &PackageQuery{
		Ecosystem: "Maven",
		Name:      groupMatch[1] + ":" + nameMatch[1],
		Version:   verMatch[1],
	}
}
