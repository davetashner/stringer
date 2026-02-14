// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendAgentSnippet_CreatesNew(t *testing.T) {
	dir := t.TempDir()

	action, err := AppendAgentSnippet(dir)
	require.NoError(t, err)
	assert.Equal(t, "AGENTS.md", action.File)
	assert.Equal(t, "created", action.Operation)

	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md")) //nolint:gosec // test path
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "# AGENTS.md")
	assert.Contains(t, content, markerStart)
	assert.Contains(t, content, markerEnd)
	assert.Contains(t, content, "## Stringer Integration")
	assert.Contains(t, content, "stringer scan .")
}

func TestAppendAgentSnippet_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	existing := "# My Project\n\nExisting content here.\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(existing), 0o600))

	action, err := AppendAgentSnippet(dir)
	require.NoError(t, err)
	assert.Equal(t, "updated", action.Operation)

	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md")) //nolint:gosec // test path
	require.NoError(t, err)

	content := string(data)
	// Original content preserved.
	assert.Contains(t, content, "# My Project")
	assert.Contains(t, content, "Existing content here.")
	// Snippet appended.
	assert.Contains(t, content, markerStart)
	assert.Contains(t, content, "## Stringer Integration")
}

func TestAppendAgentSnippet_SkipsIfMarkersPresent(t *testing.T) {
	dir := t.TempDir()
	existing := "# AGENTS.md\n\n" + markerStart + "\nold snippet\n" + markerEnd + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(existing), 0o600))

	action, err := AppendAgentSnippet(dir)
	require.NoError(t, err)
	assert.Equal(t, "skipped", action.Operation)
	assert.Contains(t, action.Description, "already present")

	// Content unchanged.
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md")) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, existing, string(data))
}

func TestAppendAgentSnippet_Idempotent(t *testing.T) {
	dir := t.TempDir()

	// First call creates.
	_, err := AppendAgentSnippet(dir)
	require.NoError(t, err)

	data1, err := os.ReadFile(filepath.Join(dir, "AGENTS.md")) //nolint:gosec // test path
	require.NoError(t, err)

	// Second call skips.
	action, err := AppendAgentSnippet(dir)
	require.NoError(t, err)
	assert.Equal(t, "skipped", action.Operation)

	data2, err := os.ReadFile(filepath.Join(dir, "AGENTS.md")) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Equal(t, string(data1), string(data2))
}

func TestAppendAgentSnippet_ExistingNoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	existing := "# No newline at end"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte(existing), 0o600))

	_, err := AppendAgentSnippet(dir)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md")) //nolint:gosec // test path
	require.NoError(t, err)

	content := string(data)
	assert.Contains(t, content, "# No newline at end")
	assert.Contains(t, content, markerStart)
}
