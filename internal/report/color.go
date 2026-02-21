// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package report

import (
	"fmt"

	"github.com/fatih/color"
)

// Shared color printers for report sections.
var (
	colorRed    = color.New(color.FgRed)
	colorYellow = color.New(color.FgYellow)
	colorGreen  = color.New(color.FgGreen)
	colorBold   = color.New(color.Bold)
)

// ColorRiskLevel colors CRITICAL/WARNING/ok risk labels.
func ColorRiskLevel(val string) string {
	switch val {
	case "CRITICAL", "NO TESTS":
		return colorRed.Sprint(val)
	case "WARNING", "LOW":
		return colorYellow.Sprint(val)
	case "ok", "GOOD":
		return colorGreen.Sprint(val)
	default:
		return val
	}
}

// ColorStability colors stability labels.
func ColorStability(val string) string {
	switch val {
	case "unstable":
		return colorRed.Sprint(val)
	case "moderate":
		return colorYellow.Sprint(val)
	case "stable":
		return colorGreen.Sprint(val)
	default:
		return val
	}
}

// ColorAssessment colors test coverage assessments.
func ColorAssessment(val string) string {
	switch val {
	case "NO TESTS", "CRITICAL":
		return colorRed.Sprint(val)
	case "LOW":
		return colorYellow.Sprint(val)
	case "MODERATE":
		return val
	case "GOOD":
		return colorGreen.Sprint(val)
	default:
		return val
	}
}

// SectionTitle renders a bold section title.
func SectionTitle(title string) string {
	return colorBold.Sprint(title)
}

// ColorDirection colors trend direction labels.
func ColorDirection(val string) string {
	switch val {
	case "improving":
		return colorGreen.Sprint(val)
	case "degrading":
		return colorRed.Sprint(val)
	default:
		return val
	}
}

// colorCount colors a count: 0 is green, >0 is yellow.
func colorCount(n int) string {
	s := fmt.Sprintf("%d", n)
	if n == 0 {
		return colorGreen.Sprint(s)
	}
	return colorYellow.Sprint(s)
}
