// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"math"
	"strings"
)

// cvssBaseScore computes the CVSS v3.x base score from a vector string
// (e.g. "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H"). It returns the
// score and true on success, or 0 and false when the vector is missing
// required metrics. Both v3.0 and v3.1 vectors use the same base formula.
//
// Implemented per the FIRST CVSS v3.1 specification so that severity
// reflects the actual score rather than a coarse impact-flag heuristic
// (stringer-g9b): a network DoS (A:H only) scores 7.5, not the same as a
// full C:H/I:H/A:H compromise at 9.8.
func cvssBaseScore(vector string) (float64, bool) {
	m := parseCVSSMetrics(vector, "AV", "AC", "PR", "UI", "S", "C", "I", "A")
	if m == nil {
		return 0, false
	}

	scopeChanged := m["S"] == "C"

	av, ok1 := cvssWeight(m["AV"], map[string]float64{"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.2})
	ac, ok2 := cvssWeight(m["AC"], map[string]float64{"L": 0.77, "H": 0.44})
	ui, ok3 := cvssWeight(m["UI"], map[string]float64{"N": 0.85, "R": 0.62})
	c, ok4 := cvssWeight(m["C"], map[string]float64{"H": 0.56, "L": 0.22, "N": 0})
	i, ok5 := cvssWeight(m["I"], map[string]float64{"H": 0.56, "L": 0.22, "N": 0})
	a, ok6 := cvssWeight(m["A"], map[string]float64{"H": 0.56, "L": 0.22, "N": 0})

	prWeights := map[string]float64{"N": 0.85, "L": 0.62, "H": 0.27}
	if scopeChanged {
		prWeights = map[string]float64{"N": 0.85, "L": 0.68, "H": 0.5}
	}
	pr, ok7 := cvssWeight(m["PR"], prWeights)

	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 || !ok7 {
		return 0, false
	}

	iss := 1 - (1-c)*(1-i)*(1-a)

	var impact float64
	if scopeChanged {
		impact = 7.52*(iss-0.029) - 3.25*math.Pow(iss-0.02, 15)
	} else {
		impact = 6.42 * iss
	}

	if impact <= 0 {
		return 0, true
	}

	exploitability := 8.22 * av * ac * pr * ui

	var score float64
	if scopeChanged {
		score = math.Min(1.08*(impact+exploitability), 10)
	} else {
		score = math.Min(impact+exploitability, 10)
	}

	return cvssRoundup(score), true
}

// cvssWeight looks up a metric value's weight, returning false for
// unknown values (including the empty string when the metric is absent).
func cvssWeight(value string, weights map[string]float64) (float64, bool) {
	w, ok := weights[strings.ToUpper(value)]
	return w, ok
}

// cvssRoundup implements the Roundup function from the CVSS v3.1
// specification (Appendix A): round up to one decimal place, with integer
// arithmetic to avoid floating-point artifacts (e.g. 4.02 → 4.1, 4.00 → 4.0).
func cvssRoundup(x float64) float64 {
	i := int(math.Round(x * 100_000))
	if i%10_000 == 0 {
		return float64(i) / 100_000
	}
	return (math.Floor(float64(i)/10_000) + 1) / 10
}

// severityFromScore maps a CVSS base score to the FIRST qualitative
// severity rating scale.
func severityFromScore(score float64) string {
	switch {
	case score >= 9.0:
		return "critical"
	case score >= 7.0:
		return "high"
	case score >= 4.0:
		return "medium"
	case score > 0:
		return "low"
	default:
		return "none"
	}
}
