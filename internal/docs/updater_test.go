// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdate_PreservesManualSections(t *testing.T) {
	dir := t.TempDir()

	existing := `# AGENTS.md — myproject

This is a manually written intro.

<!-- stringer:auto:start:architecture -->
## Architecture

Old architecture content
<!-- stringer:auto:end:architecture -->

## My Custom Section

This content was manually added and should be preserved.

<!-- stringer:auto:start:techstack -->
## Tech Stack

- **OldTech** (this should be replaced)
<!-- stringer:auto:end:techstack -->

<!-- stringer:auto:start:build -->
## Build & Test

Old build commands
<!-- stringer:auto:end:build -->

## Another Manual Section

More custom content here.
`
	existingPath := filepath.Join(dir, "AGENTS.md")
	require.NoError(t, os.WriteFile(existingPath, []byte(existing), 0o600))

	analysis := &RepoAnalysis{
		Name:     "myproject",
		Language: "Go",
		TechStack: []TechComponent{
			{Name: "Go", Version: "1.24", Source: "go.mod"},
		},
		BuildCommands: []BuildCommand{
			{Name: "build", Command: "go build ./...", Source: "go.mod"},
		},
	}

	var buf strings.Builder
	err := Update(existingPath, analysis, &buf)
	require.NoError(t, err)

	result := buf.String()

	// Manual sections preserved.
	assert.Contains(t, result, "This is a manually written intro.")
	assert.Contains(t, result, "## My Custom Section")
	assert.Contains(t, result, "This content was manually added and should be preserved.")
	assert.Contains(t, result, "## Another Manual Section")
	assert.Contains(t, result, "More custom content here.")

	// Auto sections updated.
	assert.Contains(t, result, "**Go** 1.24")
	assert.NotContains(t, result, "OldTech")
	assert.NotContains(t, result, "Old architecture content")
	assert.NotContains(t, result, "Old build commands")
	assert.Contains(t, result, "go build ./...")
}

func TestParseAutoSections(t *testing.T) {
	content := `Some preamble

<!-- stringer:auto:start:architecture -->
## Architecture

content here
<!-- stringer:auto:end:architecture -->

middle content

<!-- stringer:auto:start:techstack -->
## Tech Stack

- Go 1.22
<!-- stringer:auto:end:techstack -->

footer
`

	sections := parseAutoSections(content)

	require.Len(t, sections, 2)
	assert.Contains(t, sections, "architecture")
	assert.Contains(t, sections, "techstack")
	assert.Contains(t, sections["architecture"], "content here")
	assert.Contains(t, sections["techstack"], "Go 1.22")
}

func TestParseAutoSections_Empty(t *testing.T) {
	sections := parseAutoSections("no markers here")
	assert.Empty(t, sections)
}

func TestParseAutoSections_SingleSection(t *testing.T) {
	content := `<!-- stringer:auto:start:build -->
## Build
go build
<!-- stringer:auto:end:build -->
`
	sections := parseAutoSections(content)
	require.Len(t, sections, 1)
	assert.Contains(t, sections["build"], "go build")
}

func TestReplaceAutoSections(t *testing.T) {
	existing := `Header

<!-- stringer:auto:start:tech -->
Old tech content
<!-- stringer:auto:end:tech -->

Footer
`

	freshSections := map[string]string{
		"tech": "<!-- stringer:auto:start:tech -->\nNew tech content\n<!-- stringer:auto:end:tech -->\n",
	}

	result := replaceAutoSections(existing, freshSections)

	assert.Contains(t, result, "Header")
	assert.Contains(t, result, "Footer")
	assert.Contains(t, result, "New tech content")
	assert.NotContains(t, result, "Old tech content")
}

func TestReplaceAutoSections_NoMatchingSections(t *testing.T) {
	existing := "Just plain content\nNo markers\n"
	freshSections := map[string]string{
		"tech": "<!-- stringer:auto:start:tech -->\nNew\n<!-- stringer:auto:end:tech -->\n",
	}

	result := replaceAutoSections(existing, freshSections)
	assert.Contains(t, result, "Just plain content")
	assert.Contains(t, result, "No markers")
}

func TestReplaceAutoSections_MultipleSections(t *testing.T) {
	existing := `Intro
<!-- stringer:auto:start:a -->
Old A
<!-- stringer:auto:end:a -->
Middle
<!-- stringer:auto:start:b -->
Old B
<!-- stringer:auto:end:b -->
End
`

	freshSections := map[string]string{
		"a": "<!-- stringer:auto:start:a -->\nNew A\n<!-- stringer:auto:end:a -->\n",
		"b": "<!-- stringer:auto:start:b -->\nNew B\n<!-- stringer:auto:end:b -->\n",
	}

	result := replaceAutoSections(existing, freshSections)

	assert.Contains(t, result, "Intro")
	assert.Contains(t, result, "Middle")
	assert.Contains(t, result, "End")
	assert.Contains(t, result, "New A")
	assert.Contains(t, result, "New B")
	assert.NotContains(t, result, "Old A")
	assert.NotContains(t, result, "Old B")
}

func TestUpdate_NoExistingFile(t *testing.T) {
	dir := t.TempDir()
	nonexistentPath := filepath.Join(dir, "AGENTS.md")

	analysis := &RepoAnalysis{Name: "test"}
	var buf strings.Builder
	err := Update(nonexistentPath, analysis, &buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read existing file")
}

func TestUpdate_EmptyExistingFile(t *testing.T) {
	dir := t.TempDir()
	existingPath := filepath.Join(dir, "AGENTS.md")
	require.NoError(t, os.WriteFile(existingPath, []byte(""), 0o600))

	analysis := &RepoAnalysis{
		Name: "test",
		TechStack: []TechComponent{
			{Name: "Go", Version: "1.22", Source: "go.mod"},
		},
	}

	var buf strings.Builder
	err := Update(existingPath, analysis, &buf)
	require.NoError(t, err)

	// With no markers in the existing file, nothing gets replaced.
	// The result should be the empty file content (just a newline from scanner).
	assert.NotContains(t, buf.String(), "Go")
}
