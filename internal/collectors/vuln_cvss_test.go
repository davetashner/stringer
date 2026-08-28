// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCVSSBaseScore(t *testing.T) {
	tests := []struct {
		name   string
		vector string
		want   float64
		ok     bool
	}{
		// Canonical vectors with published base scores.
		{"critical full compromise", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 9.8, true},
		{"network DoS (image-size CVE-2025-71329 shape)", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H", 7.5, true},
		{"medium info leak", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N", 5.3, true},
		{"scope changed XSS shape", "CVSS:3.1/AV:N/AC:L/PR:N/UI:R/S:C/C:L/I:L/A:N", 6.1, true},
		{"local low priv", "CVSS:3.1/AV:L/AC:L/PR:L/UI:N/S:U/C:H/I:N/A:N", 5.5, true},
		{"zero impact", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N", 0, true},
		{"v3.0 vector uses same formula", "CVSS:3.0/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:H/A:H", 8.1, true},
		{"empty", "", 0, false},
		{"not a vector", "not-a-cvss-string", 0, false},
		{"missing metrics", "CVSS:3.1/AV:N/AC:L", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := cvssBaseScore(tt.vector)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.InDelta(t, tt.want, got, 0.001)
			}
		})
	}
}

// TestCVSSBaseScore_DoSDoesNotOutrankFullCompromise pins the ordering bug
// from stringer-g9b: under the old CIA-flag heuristic, a DoS-only vector and
// a full-compromise vector both mapped to "high". With real base scores the
// full compromise is critical and sorts first.
func TestCVSSBaseScore_DoSDoesNotOutrankFullCompromise(t *testing.T) {
	dos, ok1 := cvssBaseScore("CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H")
	full, ok2 := cvssBaseScore("CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H")
	assert.True(t, ok1)
	assert.True(t, ok2)
	assert.Less(t, dos, full)
	assert.Equal(t, "high", severityFromScore(dos))
	assert.Equal(t, "critical", severityFromScore(full))
}

func TestSeverityFromScore(t *testing.T) {
	tests := []struct {
		score float64
		want  string
	}{
		{10.0, "critical"},
		{9.0, "critical"},
		{8.9, "high"},
		{7.0, "high"},
		{6.9, "medium"},
		{4.0, "medium"},
		{3.9, "low"},
		{0.1, "low"},
		{0, "none"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, severityFromScore(tt.score), "score %.1f", tt.score)
	}
}

func TestCVSSRoundup(t *testing.T) {
	// Examples from the CVSS v3.1 spec Appendix A.
	assert.InDelta(t, 4.0, cvssRoundup(4.0), 0.0001)
	assert.InDelta(t, 4.1, cvssRoundup(4.02), 0.0001)
	assert.InDelta(t, 4.5, cvssRoundup(4.45), 0.0001)
}
