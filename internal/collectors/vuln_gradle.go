// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"log/slog"
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

	// gradleAccessor matches a version-catalog accessor such as libs.foo.bar.
	gradleAccessor = `([A-Za-z]\w*(?:\.\w+)+)`

	// reCatalogNotation matches "implementation libs.foo.bar", optionally
	// parenthesised, wrapped in (enforced)platform(), followed by a closure
	// ("{ exclude ... }") or a trailing comma opening a multi-line list.
	reCatalogNotation = regexp.MustCompile(
		`(?im)^\s*` + configPattern + `\s*\(?\s*(?:(?:enforced)?platform\s*\(\s*)?` + gradleAccessor + `\s*\)*\s*(?:\{.*|,)?$`,
	)

	// reCatalogContinuation matches a bare accessor on a list continuation line.
	reCatalogContinuation = regexp.MustCompile(`(?i)^\s*` + gradleAccessor + `\s*,?$`)
)

// parseGradleDeps reads a build.gradle(.kts) file and returns PackageQuery
// entries for OSV lookup from literal "g:a:v" coordinates only.
func parseGradleDeps(data []byte) ([]PackageQuery, error) {
	return parseGradleDepsWithCatalogs(data, nil)
}

// parseGradleDepsWithCatalogs is parseGradleDeps plus catalog accessors
// (libs.foo.bar); comma-continued lists inherit the opening line's config.
func parseGradleDepsWithCatalogs(data []byte, catalogs gradleCatalogs) ([]PackageQuery, error) {
	lines := strings.Split(string(data), "\n")
	var queries []PackageQuery
	seen := make(map[string]bool)
	pending := "" // config of an open comma-continued list

	add := func(q *PackageQuery) {
		if q != nil && !seen[q.Name+"@"+q.Version] {
			seen[q.Name+"@"+q.Version] = true
			queries = append(queries, *q)
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments.
		if trimmed == "" || strings.HasPrefix(trimmed, "//") ||
			strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "/*") {
			continue
		}

		// Accessor lines carry no string literals; drop a trailing "// comment".
		code := strings.TrimSpace(strings.SplitN(trimmed, "//", 2)[0])

		// Continuation line of a comma-separated list: "libs.foo,".
		if pending != "" {
			if m := reCatalogContinuation.FindStringSubmatch(code); m != nil {
				if !isTestConfig(pending) {
					add(parseCoordinates(catalogs.resolve(m[1])))
				}
				if !strings.HasSuffix(code, ",") {
					pending = ""
				}
				continue
			}
			pending = ""
		}

		// Catalog accessor: implementation libs.foo.bar
		if m := reCatalogNotation.FindStringSubmatch(code); m != nil {
			config := extractConfig(code)
			if strings.HasSuffix(code, ",") {
				pending = config
			}
			if isTestConfig(config) {
				continue
			}
			add(parseCoordinates(catalogs.resolve(m[1])))
			continue
		}

		// Try string notation first (covers both Groovy and Kotlin DSL).
		if m := reStringNotation.FindStringSubmatch(line); m != nil {
			config := extractConfig(trimmed)
			if isTestConfig(config) {
				continue
			}
			// "g:a_$versions.x:$versions.y" literals resolve through the Groovy
			// ext maps; an unresolved placeholder falls through to be dropped.
			add(parseCoordinates(catalogs.interpolateOrKeep(m[1])))
			continue
		}

		// Try map-style notation.
		if reMapNotation.MatchString(line) {
			config := extractConfig(trimmed)
			if isTestConfig(config) {
				continue
			}
			add(parseMapNotation(catalogs.interpolateOrKeep(line)))
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
	if coords == "" {
		return nil
	}
	parts := strings.Split(coords, ":")
	if len(parts) < 3 || parts[2] == "" {
		return nil
	}
	return gradleQuery(parts[0], parts[1], parts[2])
}

// gradleQuery builds a Maven query from resolved coordinates. Any segment that
// still carries a placeholder or a dangling suffix (an interpolation that
// resolved to nothing, e.g. "scala-logging_.") is dropped here so a malformed
// name is never sent to OSV or a registry.
func gradleQuery(group, artifact, version string) *PackageQuery {
	for _, seg := range []string{group, artifact, version} {
		if malformedGradleSegment(seg) {
			slog.Debug("vuln: skipping malformed gradle coordinate", "coordinate", group+":"+artifact+":"+version)
			return nil
		}
	}
	// Dynamic versions ("1.0+", "[1.0,2.0)") query their lower bound as a floor.
	return mavenStyleQuery("Maven", group+":"+artifact, version)
}

// malformedGradleSegment reports an empty segment, an unresolved $var / ${var}
// placeholder, or a trailing "_." / "_" / "-" left by an empty interpolation.
func malformedGradleSegment(s string) bool {
	return s == "" || strings.ContainsAny(s, "${}") ||
		strings.HasSuffix(s, "_.") || strings.HasSuffix(s, "_") || strings.HasSuffix(s, "-")
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

	return gradleQuery(groupMatch[1], nameMatch[1], verMatch[1])
}
