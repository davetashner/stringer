// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDuration_Days(t *testing.T) {
	d, err := ParseDuration("90d")
	require.NoError(t, err)
	assert.Equal(t, 90*24*time.Hour, d)
}

func TestParseDuration_Weeks(t *testing.T) {
	d, err := ParseDuration("2w")
	require.NoError(t, err)
	assert.Equal(t, 14*24*time.Hour, d)
}

func TestParseDuration_Months(t *testing.T) {
	d, err := ParseDuration("6m")
	require.NoError(t, err)
	assert.Equal(t, 180*24*time.Hour, d)
}

func TestParseDuration_Years(t *testing.T) {
	d, err := ParseDuration("1y")
	require.NoError(t, err)
	assert.Equal(t, 365*24*time.Hour, d)
}

func TestParseDuration_InvalidTooShort(t *testing.T) {
	_, err := ParseDuration("d")
	assert.Error(t, err)
}

func TestParseDuration_InvalidEmpty(t *testing.T) {
	_, err := ParseDuration("")
	assert.Error(t, err)
}

func TestParseDuration_InvalidUnit(t *testing.T) {
	_, err := ParseDuration("10x")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid duration unit")
}

func TestParseDuration_InvalidNumber(t *testing.T) {
	_, err := ParseDuration("abcd")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid duration number")
}

func TestGitSinceArg(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		spec, want string
	}{
		{"", ""},
		{"90d", "2026-06-21T12:00:00Z"},
		{"1y", "2025-09-19T12:00:00Z"},
		{"2w", "2026-09-05T12:00:00Z"},
		{"6m", "2026-03-23T12:00:00Z"},
		{"yesterday", ""},
		{"1x", ""},
	}
	for _, tc := range cases {
		if got := gitSinceArg(tc.spec, now); got != tc.want {
			t.Errorf("gitSinceArg(%q) = %q, want %q", tc.spec, got, tc.want)
		}
	}
}
