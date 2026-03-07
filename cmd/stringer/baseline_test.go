// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/baseline"
)

func TestBaselineCmd_IsRegistered(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "baseline" {
			found = true
			break
		}
	}
	assert.True(t, found, "baseline command should be registered on rootCmd")
}

func TestBaselineSubcommands_AreRegistered(t *testing.T) {
	subs := map[string]bool{}
	for _, cmd := range baselineCmd.Commands() {
		subs[cmd.Name()] = true
	}
	assert.True(t, subs["create"], "create subcommand should be registered")
	assert.True(t, subs["suppress"], "suppress subcommand should be registered")
	assert.True(t, subs["list"], "list subcommand should be registered")
	assert.True(t, subs["remove"], "remove subcommand should be registered")
	assert.True(t, subs["status"], "status subcommand should be registered")
}

// --- suppress tests ---

func TestBaselineSuppress_ValidID(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "suppress", "str-abcd1234", "--reason", "won't-fix", "--comment", "test comment"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Suppressed str-abcd1234")
	assert.Contains(t, stdout.String(), "won't-fix")

	// Verify baseline file was created.
	state, err := baseline.Load(dir)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Len(t, state.Suppressions, 1)
	assert.Equal(t, "str-abcd1234", state.Suppressions[0].SignalID)
	assert.Equal(t, baseline.ReasonWontFix, state.Suppressions[0].Reason)
	assert.Equal(t, "test comment", state.Suppressions[0].Comment)
}

func TestBaselineSuppress_InvalidID(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(new(bytes.Buffer))

	tests := []string{
		"invalid",
		"str-xyz",
		"str-abcd12345", // too long
		"str-ABCD1234",  // uppercase
		"abc-12345678",  // wrong prefix
	}
	for _, id := range tests {
		resetBaselineFlags()
		rootCmd.SetArgs([]string{"baseline", "suppress", id})
		err := rootCmd.Execute()
		assert.Error(t, err, "expected error for ID %q", id)
	}
}

func TestBaselineSuppress_UpdateExisting(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	// Pre-create a baseline with an existing suppression.
	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{
				SignalID:     "str-abcd1234",
				Reason:       baseline.ReasonAcknowledged,
				Comment:      "old comment",
				SuppressedAt: time.Now().Add(-24 * time.Hour),
			},
		},
	}
	require.NoError(t, baseline.Save(dir, state))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "suppress", "str-abcd1234", "--reason", "false-positive", "--comment", "updated"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	// Should still have exactly 1 suppression (updated, not duplicated).
	updated, err := baseline.Load(dir)
	require.NoError(t, err)
	assert.Len(t, updated.Suppressions, 1)
	assert.Equal(t, baseline.ReasonFalsePositive, updated.Suppressions[0].Reason)
	assert.Equal(t, "updated", updated.Suppressions[0].Comment)
}

func TestBaselineSuppress_WithExpiry(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "suppress", "str-abcd1234", "--expires", "90d"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	state, err := baseline.Load(dir)
	require.NoError(t, err)
	require.NotNil(t, state.Suppressions[0].ExpiresAt)
	// Expiry should be roughly 90 days from now.
	expected := time.Now().Add(90 * 24 * time.Hour)
	assert.WithinDuration(t, expected, *state.Suppressions[0].ExpiresAt, time.Minute)
}

func TestBaselineSuppress_InvalidExpiry(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"baseline", "suppress", "str-abcd1234", "--expires", "invalid"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

func TestBaselineSuppress_InvalidReason(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"baseline", "suppress", "str-abcd1234", "--reason", "bad-reason"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- list tests ---

func TestBaselineList_Empty(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "list"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "No suppressions")
}

func TestBaselineList_WithSuppressions(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	now := time.Now()
	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{
				SignalID:     "str-aaaaaaaa",
				Reason:       baseline.ReasonAcknowledged,
				Comment:      "reviewed this one",
				SuppressedBy: "alice",
				SuppressedAt: now,
			},
			{
				SignalID:     "str-bbbbbbbb",
				Reason:       baseline.ReasonWontFix,
				Comment:      "this comment is really long and should be truncated to forty characters maximum",
				SuppressedBy: "bob",
				SuppressedAt: now,
			},
		},
	}
	require.NoError(t, baseline.Save(dir, state))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "list"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "str-aaaaaaaa")
	assert.Contains(t, out, "str-bbbbbbbb")
	assert.Contains(t, out, "acknowledged")
	assert.Contains(t, out, "won't-fix")
	assert.Contains(t, out, "alice")
	assert.Contains(t, out, "bob")
	// Long comment should be truncated.
	assert.Contains(t, out, "...")
}

func TestBaselineList_JSON(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{
				SignalID:     "str-aaaaaaaa",
				Reason:       baseline.ReasonAcknowledged,
				SuppressedAt: time.Now(),
			},
		},
	}
	require.NoError(t, baseline.Save(dir, state))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "list", "--json"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	var parsed []baseline.Suppression
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed))
	assert.Len(t, parsed, 1)
	assert.Equal(t, "str-aaaaaaaa", parsed[0].SignalID)
}

func TestBaselineList_FilterByReason(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: "str-aaaaaaaa", Reason: baseline.ReasonAcknowledged, SuppressedAt: time.Now()},
			{SignalID: "str-bbbbbbbb", Reason: baseline.ReasonWontFix, SuppressedAt: time.Now()},
		},
	}
	require.NoError(t, baseline.Save(dir, state))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "list", "--reason", "won't-fix"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "str-bbbbbbbb")
	assert.NotContains(t, out, "str-aaaaaaaa")
}

func TestBaselineList_FilterExpired(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: "str-aaaaaaaa", Reason: baseline.ReasonAcknowledged, SuppressedAt: time.Now(), ExpiresAt: &past},
			{SignalID: "str-bbbbbbbb", Reason: baseline.ReasonWontFix, SuppressedAt: time.Now(), ExpiresAt: &future},
		},
	}
	require.NoError(t, baseline.Save(dir, state))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "list", "--expired"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "str-aaaaaaaa")
	assert.NotContains(t, out, "str-bbbbbbbb")
}

// --- remove tests ---

func TestBaselineRemove_ByID(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: "str-aaaaaaaa", Reason: baseline.ReasonAcknowledged, SuppressedAt: time.Now()},
			{SignalID: "str-bbbbbbbb", Reason: baseline.ReasonWontFix, SuppressedAt: time.Now()},
		},
	}
	require.NoError(t, baseline.Save(dir, state))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "remove", "str-aaaaaaaa"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Removed str-aaaaaaaa")

	// Verify only one suppression remains.
	updated, err := baseline.Load(dir)
	require.NoError(t, err)
	assert.Len(t, updated.Suppressions, 1)
	assert.Equal(t, "str-bbbbbbbb", updated.Suppressions[0].SignalID)
}

func TestBaselineRemove_NotFound(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: "str-aaaaaaaa", Reason: baseline.ReasonAcknowledged, SuppressedAt: time.Now()},
		},
	}
	require.NoError(t, baseline.Save(dir, state))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"baseline", "remove", "str-99999999"})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestBaselineRemove_Expired(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: "str-aaaaaaaa", Reason: baseline.ReasonAcknowledged, SuppressedAt: time.Now(), ExpiresAt: &past},
			{SignalID: "str-bbbbbbbb", Reason: baseline.ReasonWontFix, SuppressedAt: time.Now(), ExpiresAt: &future},
			{SignalID: "str-cccccccc", Reason: baseline.ReasonFalsePositive, SuppressedAt: time.Now(), ExpiresAt: &past},
		},
	}
	require.NoError(t, baseline.Save(dir, state))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "remove", "--expired"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Removed 2 expired suppressions")

	updated, err := baseline.Load(dir)
	require.NoError(t, err)
	assert.Len(t, updated.Suppressions, 1)
	assert.Equal(t, "str-bbbbbbbb", updated.Suppressions[0].SignalID)
}

func TestBaselineRemove_NoBaseline(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "remove", "--expired"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "No suppressions")
}

func TestBaselineRemove_NoArgsNoFlags(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	state := &baseline.BaselineState{
		Version:      "1",
		Suppressions: []baseline.Suppression{{SignalID: "str-aaaaaaaa", Reason: baseline.ReasonAcknowledged, SuppressedAt: time.Now()}},
	}
	require.NoError(t, baseline.Save(dir, state))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"baseline", "remove"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- status tests ---

func TestBaselineStatus_NoBaseline(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "status"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "No baseline")
	assert.Contains(t, stdout.String(), "stringer baseline create")
}

func TestBaselineStatus_WithSuppressions(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	past := time.Now().Add(-time.Hour)
	now := time.Now()
	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: "str-aaaaaaaa", Reason: baseline.ReasonAcknowledged, SuppressedAt: now},
			{SignalID: "str-bbbbbbbb", Reason: baseline.ReasonWontFix, SuppressedAt: now},
			{SignalID: "str-cccccccc", Reason: baseline.ReasonAcknowledged, SuppressedAt: now.Add(-48 * time.Hour), ExpiresAt: &past},
		},
	}
	require.NoError(t, baseline.Save(dir, state))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "status"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	out := stdout.String()
	assert.Contains(t, out, "Total suppressions: 3")
	assert.Contains(t, out, "acknowledged:")
	assert.Contains(t, out, "won't-fix:")
	assert.Contains(t, out, "Expired: 1")
	assert.Contains(t, out, "Oldest:")
	assert.Contains(t, out, "Newest:")
}

func TestBaselineStatus_JSON(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	now := time.Now()
	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: "str-aaaaaaaa", Reason: baseline.ReasonAcknowledged, SuppressedAt: now},
		},
	}
	require.NoError(t, baseline.Save(dir, state))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "status", "--json"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &parsed))
	assert.Equal(t, float64(1), parsed["total"])
	assert.Equal(t, float64(0), parsed["expired"])
}

func TestBaselineStatus_ExpiredWarning(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	past := time.Now().Add(-time.Hour)
	now := time.Now()
	// 3 out of 4 expired (75%) - should trigger the >20% warning.
	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: "str-aaaaaaaa", Reason: baseline.ReasonAcknowledged, SuppressedAt: now, ExpiresAt: &past},
			{SignalID: "str-bbbbbbbb", Reason: baseline.ReasonAcknowledged, SuppressedAt: now, ExpiresAt: &past},
			{SignalID: "str-cccccccc", Reason: baseline.ReasonAcknowledged, SuppressedAt: now, ExpiresAt: &past},
			{SignalID: "str-dddddddd", Reason: baseline.ReasonAcknowledged, SuppressedAt: now},
		},
	}
	require.NoError(t, baseline.Save(dir, state))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "status"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "Warning")
	assert.Contains(t, stdout.String(), "baseline remove --expired")
}

// --- create tests ---

func TestBaselineCreate_ExistingNoForce(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	// Create a .git directory so resolveScanPath finds it.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))

	// Pre-create baseline.
	state := &baseline.BaselineState{Version: "1", Suppressions: []baseline.Suppression{{SignalID: "str-aaaaaaaa"}}}
	require.NoError(t, baseline.Save(dir, state))

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"baseline", "create", dir})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--force")
}

func TestBaselineCreate_InvalidReason(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o750))

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"baseline", "create", dir, "--reason", "bad-reason"})

	err := rootCmd.Execute()
	assert.Error(t, err)
}

// --- helper tests ---

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input   string
		want    time.Duration
		wantErr bool
	}{
		{"90d", 90 * 24 * time.Hour, false},
		{"6m", 6 * 30 * 24 * time.Hour, false},
		{"1y", 365 * 24 * time.Hour, false},
		{"2w", 14 * 24 * time.Hour, false},
		{"x", 0, true},
		{"", 0, true},
		{"abc", 0, true},
		{"90z", 0, true},
	}

	for _, tc := range tests {
		got, err := parseDuration(tc.input)
		if tc.wantErr {
			assert.Error(t, err, "parseDuration(%q) should error", tc.input)
		} else {
			require.NoError(t, err, "parseDuration(%q)", tc.input)
			assert.Equal(t, tc.want, got, "parseDuration(%q)", tc.input)
		}
	}
}

func TestFormatAge(t *testing.T) {
	now := time.Now()
	tests := []struct {
		t    time.Time
		want string
	}{
		{now.Add(-30 * time.Minute), "30m"},
		{now.Add(-5 * time.Hour), "5h"},
		{now.Add(-3 * 24 * time.Hour), "3d"},
		{now.Add(-60 * 24 * time.Hour), "2mo"},
		{now.Add(-400 * 24 * time.Hour), "1y"},
	}

	for _, tc := range tests {
		got := formatAge(tc.t)
		assert.Equal(t, tc.want, got)
	}
}

func TestSignalIDPattern(t *testing.T) {
	valid := []string{"str-abcd1234", "str-00000000", "str-ffffffff", "str-12345678"}
	invalid := []string{"str-ABCD1234", "str-abc", "str-abcd12345", "abc-12345678", "invalid", "str-ghij1234"}

	for _, id := range valid {
		assert.True(t, signalIDPattern.MatchString(id), "expected %q to be valid", id)
	}
	for _, id := range invalid {
		assert.False(t, signalIDPattern.MatchString(id), "expected %q to be invalid", id)
	}
}

func TestBaselineRemove_InvalidID(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	state := &baseline.BaselineState{
		Version:      "1",
		Suppressions: []baseline.Suppression{{SignalID: "str-aaaaaaaa", Reason: baseline.ReasonAcknowledged, SuppressedAt: time.Now()}},
	}
	require.NoError(t, baseline.Save(dir, state))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs([]string{"baseline", "remove", "bad-id"})

	err := rootCmd.Execute()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid signal ID")
}

func TestBaselineSuppress_DefaultReason(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "suppress", "str-abcd1234"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	state, err := baseline.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, baseline.ReasonAcknowledged, state.Suppressions[0].Reason)
}

func TestFilterSuppressions(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	suppressions := []baseline.Suppression{
		{SignalID: "a", Reason: baseline.ReasonAcknowledged, ExpiresAt: &past},
		{SignalID: "b", Reason: baseline.ReasonWontFix, ExpiresAt: &future},
		{SignalID: "c", Reason: baseline.ReasonAcknowledged},
	}

	// No filters.
	baselineListReason = ""
	baselineExpired = false
	result := filterSuppressions(suppressions)
	assert.Len(t, result, 3)

	// Filter by reason.
	baselineListReason = "acknowledged"
	baselineExpired = false
	result = filterSuppressions(suppressions)
	assert.Len(t, result, 2)

	// Filter expired.
	baselineListReason = ""
	baselineExpired = true
	result = filterSuppressions(suppressions)
	assert.Len(t, result, 1)
	assert.Equal(t, "a", result[0].SignalID)

	// Both filters.
	baselineListReason = "acknowledged"
	baselineExpired = true
	result = filterSuppressions(suppressions)
	assert.Len(t, result, 1)
	assert.Equal(t, "a", result[0].SignalID)

	// Reset.
	baselineListReason = ""
	baselineExpired = false
}

func TestBaselineList_NoBaselineFile(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "list"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "No suppressions")
}

func TestBaselineList_AllFilteredOut(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: "str-aaaaaaaa", Reason: baseline.ReasonAcknowledged, SuppressedAt: time.Now()},
		},
	}
	require.NoError(t, baseline.Save(dir, state))

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	rootCmd.SetArgs([]string{"baseline", "list", "--reason", "won't-fix"})

	err := rootCmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "No suppressions")
}

// Ensure the suppress command works with default reason when --reason is not explicitly passed.
// This tests that the flag detection via Changed() works correctly.
func TestBaselineSuppress_ChangedFlagDetection(t *testing.T) {
	resetBaselineFlags()
	dir := t.TempDir()

	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	stdout := new(bytes.Buffer)
	rootCmd.SetOut(stdout)
	// No --reason flag provided; should default to "acknowledged".
	rootCmd.SetArgs([]string{"baseline", "suppress", "str-12345678"})

	err := rootCmd.Execute()
	require.NoError(t, err)

	state, err := baseline.Load(dir)
	require.NoError(t, err)
	require.Len(t, state.Suppressions, 1)
	assert.Equal(t, baseline.ReasonAcknowledged, state.Suppressions[0].Reason)
	assert.Contains(t, stdout.String(), "acknowledged")
}

func TestGitUserName(t *testing.T) {
	// Just verify it returns a non-empty string (it will be either the git
	// user name or "unknown").
	name := gitUserName()
	assert.NotEmpty(t, name)
	assert.False(t, strings.ContainsAny(name, "\n\r"))
}
