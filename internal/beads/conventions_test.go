// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package beads

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectConventions_Empty(t *testing.T) {
	c := DetectConventions(nil)
	assert.Nil(t, c, "nil input should return nil conventions")

	c = DetectConventions([]Bead{})
	assert.Nil(t, c, "empty input should return nil conventions")
}

func TestDetectConventions_IDPrefix(t *testing.T) {
	existing := []Bead{
		{ID: "stringer-abc", Priority: 1},
		{ID: "stringer-def", Priority: 2},
		{ID: "stringer-ghi", Priority: 3},
		{ID: "str-12345678", Priority: 2},
	}

	c := DetectConventions(existing)
	require.NotNil(t, c)
	assert.Equal(t, "stringer-", c.IDPrefix, "dominant prefix should be stringer-")
}

func TestDetectConventions_IDPrefix_StrDominant(t *testing.T) {
	existing := []Bead{
		{ID: "str-aaa", Priority: 1},
		{ID: "str-bbb", Priority: 2},
		{ID: "app-ccc", Priority: 3},
	}

	c := DetectConventions(existing)
	require.NotNil(t, c)
	assert.Equal(t, "str-", c.IDPrefix)
}

func TestDetectConventions_LabelStyle_Kebab(t *testing.T) {
	existing := []Bead{
		{ID: "a", Priority: 1, Labels: []string{"my-label", "another-label"}},
		{ID: "b", Priority: 2, Labels: []string{"third-label"}},
	}

	c := DetectConventions(existing)
	require.NotNil(t, c)
	assert.Equal(t, "kebab-case", c.LabelStyle)
}

func TestDetectConventions_LabelStyle_Snake(t *testing.T) {
	existing := []Bead{
		{ID: "a", Priority: 1, Labels: []string{"my_label", "another_label"}},
		{ID: "b", Priority: 2, Labels: []string{"third_label", "fourth_label"}},
	}

	c := DetectConventions(existing)
	require.NotNil(t, c)
	assert.Equal(t, "snake_case", c.LabelStyle)
}

func TestDetectConventions_LabelStyle_DefaultsToKebab(t *testing.T) {
	existing := []Bead{
		{ID: "a", Priority: 1, Labels: []string{"nohyphensunderscores"}},
	}

	c := DetectConventions(existing)
	require.NotNil(t, c)
	assert.Equal(t, "kebab-case", c.LabelStyle, "should default to kebab-case when no hyphens or underscores")
}

func TestDetectConventions_UseIssueType(t *testing.T) {
	existing := []Bead{
		{ID: "a", Priority: 1, IssueType: "enhancement"},
		{ID: "b", Priority: 2, Type: "bug"},
	}

	c := DetectConventions(existing)
	require.NotNil(t, c)
	assert.True(t, c.UseIssueType, "should detect issue_type usage")
}

func TestDetectConventions_UseIssueType_NotUsed(t *testing.T) {
	existing := []Bead{
		{ID: "a", Priority: 1, Type: "bug"},
		{ID: "b", Priority: 2, Type: "task"},
	}

	c := DetectConventions(existing)
	require.NotNil(t, c)
	assert.False(t, c.UseIssueType, "should not detect issue_type when not used")
}

func TestDetectConventions_PriorityRange(t *testing.T) {
	existing := []Bead{
		{ID: "a", Priority: 1},
		{ID: "b", Priority: 3},
		{ID: "c", Priority: 5},
	}

	c := DetectConventions(existing)
	require.NotNil(t, c)
	assert.Equal(t, 1, c.MinPriority)
	assert.Equal(t, 5, c.MaxPriority)
}

func TestDetectConventions_PriorityRange_SingleBead(t *testing.T) {
	existing := []Bead{
		{ID: "a", Priority: 2},
	}

	c := DetectConventions(existing)
	require.NotNil(t, c)
	assert.Equal(t, 2, c.MinPriority)
	assert.Equal(t, 2, c.MaxPriority)
}

func TestExtractPrefix(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"stringer-abc", "stringer-"},
		{"str-0e4098f9", "str-"},
		{"app-v2-xyz", "app-v2-"},
		{"nohyphen", ""},
		{"", ""},
		{"x-", "x-"},
		{"-abc", "-"},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := extractPrefix(tt.id)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectLabelStyle_MixedPreferSnake(t *testing.T) {
	existing := []Bead{
		{ID: "a", Priority: 1, Labels: []string{"my-label", "my_label", "another_label"}},
	}

	style := detectLabelStyle(existing)
	assert.Equal(t, "snake_case", style, "more underscores than hyphens means snake_case")
}

func TestDetectConventions_ZeroPriorityNormalization(t *testing.T) {
	// All beads have priority=0. MinPriority will never update from 999 sentinel,
	// and MaxPriority will never update from -1 sentinel, triggering normalization.
	existing := []Bead{
		{ID: "a-1", Priority: 0},
		{ID: "b-2", Priority: 0},
	}

	c := DetectConventions(existing)
	require.NotNil(t, c)
	assert.Equal(t, 0, c.MinPriority, "zero-priority should normalize MinPriority from 999 to 0")
	assert.Equal(t, 0, c.MaxPriority, "zero-priority should track MaxPriority as 0")
}

func TestDetectConventions_NoIDHyphen(t *testing.T) {
	// Beads without hyphens in IDs should yield empty prefix.
	existing := []Bead{
		{ID: "nohyphen", Priority: 1},
	}

	c := DetectConventions(existing)
	require.NotNil(t, c)
	assert.Equal(t, "", c.IDPrefix, "ID without hyphen should yield empty prefix")
}

func TestDetectLabelStyle_Equal(t *testing.T) {
	existing := []Bead{
		{ID: "a", Priority: 1, Labels: []string{"my-label", "my_label"}},
	}

	style := detectLabelStyle(existing)
	assert.Equal(t, "kebab-case", style, "equal counts default to kebab-case")
}
