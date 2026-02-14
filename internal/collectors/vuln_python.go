// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"bufio"
	"bytes"
	"strings"

	"github.com/BurntSushi/toml"
)

// parsePythonRequirements parses a requirements.txt file and returns PackageQuery entries
// for OSV lookup. It handles pinned versions (==), compatible release (~=), and minimum
// version (>=) constraints. Editable installs (-e), URL refs, options (--), comments (#),
// and blank lines are skipped. For multi-constraint specs (e.g. >=1.0,<2.0), the first
// version found is used.
func parsePythonRequirements(data []byte) ([]PackageQuery, error) {
	var queries []PackageQuery

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Strip inline comments.
		if idx := strings.Index(line, " #"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Skip pip options and editable/recursive installs.
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "--") {
			continue
		}

		// Skip URL-based dependencies (PEP 440 direct references).
		if strings.Contains(line, "://") {
			continue
		}

		q := parseRequirementLine(line)
		if q != nil {
			queries = append(queries, *q)
		}
	}

	return queries, scanner.Err()
}

// parseRequirementLine parses a single requirements.txt line into a PackageQuery.
// Returns nil if the line has no extractable version.
func parseRequirementLine(line string) *PackageQuery {
	// Strip environment markers (e.g. ; python_version >= "3.8").
	if idx := strings.Index(line, ";"); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}

	// Find the first version operator.
	var name, version string
	for _, op := range []string{"~=", "==", ">=", "<=", "!=", ">", "<"} {
		if idx := strings.Index(line, op); idx >= 0 {
			name = strings.TrimSpace(line[:idx])
			// Take the version after the operator, up to a comma (multi-constraint).
			rest := line[idx+len(op):]
			if comma := strings.Index(rest, ","); comma >= 0 {
				rest = rest[:comma]
			}
			version = strings.TrimSpace(rest)
			break
		}
	}

	if name == "" || version == "" {
		return nil
	}

	// Strip extras from package name (e.g. requests[security] → requests).
	if idx := strings.Index(name, "["); idx >= 0 {
		name = name[:idx]
	}

	return &PackageQuery{
		Ecosystem: "PyPI",
		Name:      name,
		Version:   version,
	}
}

// pyprojectFile represents the subset of pyproject.toml we need for dependency extraction.
type pyprojectFile struct {
	Project struct {
		Dependencies []string `toml:"dependencies"`
	} `toml:"project"`
}

// parsePyprojectDeps parses a pyproject.toml file (PEP 621) and returns PackageQuery
// entries for OSV lookup. Dependencies use the same PEP 508 format as requirements.txt.
func parsePyprojectDeps(data []byte) ([]PackageQuery, error) {
	var proj pyprojectFile
	if err := toml.Unmarshal(data, &proj); err != nil {
		return nil, err
	}

	if len(proj.Project.Dependencies) == 0 {
		return nil, nil
	}

	var queries []PackageQuery
	for _, dep := range proj.Project.Dependencies {
		q := parseRequirementLine(dep)
		if q != nil {
			queries = append(queries, *q)
		}
	}

	return queries, nil
}
