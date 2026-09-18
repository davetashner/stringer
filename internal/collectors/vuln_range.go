// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"math"
	"strings"
)

// rangeConfidenceFactor scales the confidence of a finding whose declared
// version is a floor or range rather than an exact pin. The queried version
// is only the minimum the manifest allows; without a lockfile the installed
// version is unknown, so the finding is a policy problem ("raise the floor")
// rather than a confirmed vulnerable install (DR-023 amendment, stringer-nxx.2).
const rangeConfidenceFactor = 0.6

// applyRangeDiscount reduces confidence for a range/floor finding.
func applyRangeDiscount(confidence float64) float64 {
	return math.Round(confidence*rangeConfidenceFactor*100) / 100
}

// declaredSpec renders a package with its declared constraint for titles:
// "click>=8.1.3", "minimist^1.2.3", "log4j@[2.0,3.0)".
func declaredSpec(name, constraint string) string {
	if constraint != "" && strings.ContainsRune("^~><=!*", rune(constraint[0])) {
		return name + constraint
	}
	return name + "@" + constraint
}

// splitSemverConstraint reduces a semver-style constraint (npm, Composer,
// Cargo) to a concrete query version and reports whether the constraint is a
// range rather than an exact pin. Compound constraints (||, commas, space
// separated bounds, hyphen ranges) yield their first lower bound. Wildcard
// components (1.x, 1.2.*) are truncated to their fixed prefix. When
// bareIsRange is set, an operator-less version is treated as a caret range
// (Cargo semantics). Returns "" when no concrete version can be derived.
func splitSemverConstraint(spec string, bareIsRange bool) (string, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", false
	}

	isRange := false
	// OR and AND constraints: the first segment carries the lower bound.
	if idx := strings.Index(spec, "||"); idx >= 0 {
		spec = spec[:idx]
		isRange = true
	}
	if idx := strings.Index(spec, ","); idx >= 0 {
		spec = spec[:idx]
		isRange = true
	}

	tokens := strings.Fields(spec)
	if len(tokens) > 1 {
		isRange = true
	}
	for _, tok := range tokens {
		if tok == "-" {
			continue
		}
		sawRangeOp, sawEq := false, false
		for tok != "" && strings.ContainsRune("^~><!=", rune(tok[0])) {
			if tok[0] == '=' {
				sawEq = true
			} else {
				sawRangeOp = true
			}
			tok = tok[1:]
		}
		// "=1.2.3" pins; any other operator (or a bare Cargo version) floats.
		exact := !sawRangeOp && (sawEq || !bareIsRange)
		tok = strings.TrimPrefix(strings.TrimPrefix(tok, "v"), "V")
		if tok == "" {
			continue // bare operator; the version is the next token
		}
		if tok == "*" || tok == "x" || tok == "X" {
			return "", true
		}
		for _, wild := range []string{".*", ".x", ".X"} {
			if idx := strings.Index(tok, wild); idx >= 0 {
				tok = tok[:idx]
				exact = false
			}
		}
		if tok == "" || tok[0] < '0' || tok[0] > '9' {
			return "", false
		}
		return tok, isRange || !exact
	}
	return "", false
}

// mavenStyleQuery builds a query for a Maven-syntax version spec (Maven,
// Gradle, sbt, NuGet). Returns nil when the spec has no concrete lower bound.
func mavenStyleQuery(ecosystem, name, spec string) *PackageQuery {
	floor, isRange := splitMavenConstraint(spec)
	if floor == "" {
		return nil
	}
	q := &PackageQuery{Ecosystem: ecosystem, Name: name, Version: floor}
	if isRange {
		q.IsRange = true
		q.Constraint = strings.TrimSpace(spec)
	}
	return q
}

// splitMavenConstraint handles Maven/Gradle/NuGet/Ivy version syntax: bracket
// ranges "[1.0,2.0)" and "(,1.0]", the exact form "[1.0]", Gradle dynamic
// versions "1.0+", NuGet floating versions "1.0.*", and plain "1.0" (which
// resolvers treat as the version actually used, so it counts as exact).
// Returns "" when no concrete lower bound exists.
func splitMavenConstraint(spec string) (string, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", false
	}
	if strings.HasPrefix(strings.ToLower(spec), "latest") {
		return "", false
	}
	if strings.HasPrefix(spec, "[") || strings.HasPrefix(spec, "(") {
		inner := strings.TrimLeft(spec, "[(")
		if end := strings.IndexAny(inner, "])"); end >= 0 {
			inner = inner[:end]
		}
		parts := strings.Split(inner, ",")
		lower := strings.TrimSpace(parts[0])
		return lower, len(parts) > 1
	}
	if strings.HasSuffix(spec, "+") {
		return strings.TrimSuffix(spec, "+"), true
	}
	if idx := strings.Index(spec, ".*"); idx >= 0 {
		return spec[:idx], true
	}
	return spec, false
}
