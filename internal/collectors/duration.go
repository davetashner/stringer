// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"fmt"
	"strconv"
	"time"
)

// ParseDuration parses duration strings like "90d", "6m", "1y" into time.Duration.
// Supported units: d (days), w (weeks), m (months/30d), y (years/365d).
func ParseDuration(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}
	numStr := s[:len(s)-1]
	unit := s[len(s)-1]

	n, err := strconv.Atoi(numStr)
	if err != nil {
		return 0, fmt.Errorf("invalid duration number: %q", s)
	}

	switch unit {
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	case 'm':
		return time.Duration(n) * 30 * 24 * time.Hour, nil
	case 'y':
		return time.Duration(n) * 365 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid duration unit %q in %q (use d/w/m/y)", string(unit), s)
	}
}

// gitSinceArg converts a --git-since shorthand ("90d", "6m", "1y") into an
// absolute RFC 3339 timestamp for `git log --since`. The raw shorthand must
// never reach git: git's approxidate parser misreads it ("1y" becomes the
// first day of the current month, "90d" a date in 1990). Values that are not
// valid shorthand yield "" (no time filter), matching the documented flag
// contract.
func gitSinceArg(spec string, now time.Time) string {
	if spec == "" {
		return ""
	}
	d, err := ParseDuration(spec)
	if err != nil {
		return ""
	}
	return now.Add(-d).UTC().Format(time.RFC3339)
}
