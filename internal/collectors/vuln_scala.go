// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"regexp"
	"strings"
)

// sbtDepRe matches libraryDependencies entries in build.sbt files.
// Handles both %% (Scala-suffixed) and % (Java) artifact separators.
// Pattern: "groupId" %% "artifactId" % "version"
var sbtDepRe = regexp.MustCompile(
	`"([^"]+)"\s+%%?\s+"([^"]+)"\s+%\s+"([^"]+)"`,
)

// parseSbtDeps parses a build.sbt file and returns PackageQuery entries for OSV lookup.
// It extracts library dependencies using the standard sbt notation:
//
//	"org.typelevel" %% "cats-core" % "2.9.0"
//
// Dependencies with %% get a _2.13 suffix appended (Scala convention for Maven).
// Test-scoped dependencies (% Test, % "test") are included since they still affect builds.
func parseSbtDeps(data []byte) []PackageQuery {
	matches := sbtDepRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var queries []PackageQuery

	content := string(data)

	for _, match := range matches {
		groupID := string(match[1])
		artifactID := string(match[2])
		version := string(match[3])

		if groupID == "" || artifactID == "" || version == "" {
			continue
		}

		// Check if this uses %% (Scala-suffixed) by looking at the original match context.
		// The regex matches both % and %%, so check the original text.
		matchStr := string(match[0])
		matchIdx := strings.Index(content, matchStr)
		if matchIdx >= 0 {
			// Find the separator between groupId and artifactId.
			afterGroup := strings.Index(matchStr, "\""+groupID+"\"") + len("\""+groupID+"\"")
			sep := strings.TrimSpace(matchStr[afterGroup:strings.Index(matchStr, "\""+artifactID+"\"")])
			if strings.HasPrefix(sep, "%%") {
				// Scala cross-built: append default Scala version suffix.
				artifactID += "_2.13"
			}
		}

		key := groupID + ":" + artifactID
		if seen[key] {
			continue
		}
		seen[key] = true

		queries = append(queries, PackageQuery{
			Ecosystem: "Maven",
			Name:      groupID + ":" + artifactID,
			Version:   version,
		})
	}

	return queries
}
