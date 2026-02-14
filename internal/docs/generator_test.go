// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package docs

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerate_BasicAnalysis(t *testing.T) {
	analysis := &RepoAnalysis{
		Name:     "myproject",
		Language: "Go",
		TechStack: []TechComponent{
			{Name: "Go", Version: "1.22", Source: "go.mod"},
		},
		BuildCommands: []BuildCommand{
			{Name: "build", Command: "go build ./...", Source: "go.mod"},
			{Name: "test", Command: "go test -race ./...", Source: "go.mod"},
		},
		DirectoryTree: []DirEntry{
			{Path: "cmd", IsDir: true, Depth: 1},
			{Path: "internal", IsDir: true, Depth: 1},
			{Path: "go.mod", IsDir: false, Depth: 1},
		},
	}

	var buf strings.Builder
	err := Generate(analysis, &buf)
	require.NoError(t, err)

	out := buf.String()

	// Verify header.
	assert.Contains(t, out, "# AGENTS.md — myproject")

	// Verify architecture section with markers.
	assert.Contains(t, out, "<!-- stringer:auto:start:architecture -->")
	assert.Contains(t, out, "<!-- stringer:auto:end:architecture -->")
	assert.Contains(t, out, "myproject/")

	// Verify tech stack section.
	assert.Contains(t, out, "<!-- stringer:auto:start:techstack -->")
	assert.Contains(t, out, "<!-- stringer:auto:end:techstack -->")
	assert.Contains(t, out, "**Go** 1.22")

	// Verify build section.
	assert.Contains(t, out, "<!-- stringer:auto:start:build -->")
	assert.Contains(t, out, "<!-- stringer:auto:end:build -->")
	assert.Contains(t, out, "go build ./...")
	assert.Contains(t, out, "go test -race ./...")

	// Verify static sections.
	assert.Contains(t, out, "## Decision Records")
	assert.Contains(t, out, "## Working on This Project")
}

func TestGenerate_EmptyAnalysis(t *testing.T) {
	analysis := &RepoAnalysis{
		Name: "empty",
	}

	var buf strings.Builder
	err := Generate(analysis, &buf)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "# AGENTS.md — empty")
	assert.Contains(t, out, "empty/")
	assert.Contains(t, out, "<!-- Add your key design decisions here -->")
}

func TestGenerate_WithPatterns(t *testing.T) {
	analysis := &RepoAnalysis{
		Name: "patterned",
		Patterns: []CodePattern{
			{Name: "Cobra CLI", Description: "Uses spf13/cobra for CLI"},
			{Name: "Go Internal Packages", Description: "Uses internal/ for private packages"},
		},
	}

	var buf strings.Builder
	err := Generate(analysis, &buf)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "**Cobra CLI**")
	assert.Contains(t, out, "Uses spf13/cobra for CLI")
	assert.Contains(t, out, "**Go Internal Packages**")
	assert.NotContains(t, out, "<!-- Add your key design decisions here -->")
}

func TestGenerate_WithDescription(t *testing.T) {
	analysis := &RepoAnalysis{
		Name:        "described",
		Description: "A tool for doing things",
	}

	var buf strings.Builder
	err := Generate(analysis, &buf)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "A tool for doing things")
}

func TestRenderDirectoryTree_Empty(t *testing.T) {
	result := renderDirectoryTree(nil, "myrepo")
	assert.Equal(t, "myrepo/\n", result)
}

func TestRenderDirectoryTree_Simple(t *testing.T) {
	entries := []DirEntry{
		{Path: "cmd", IsDir: true, Depth: 1},
		{Path: "internal", IsDir: true, Depth: 1},
		{Path: "go.mod", IsDir: false, Depth: 1},
	}

	result := renderDirectoryTree(entries, "myrepo")

	assert.Contains(t, result, "myrepo/")
	assert.Contains(t, result, "cmd/")
	assert.Contains(t, result, "internal/")
	assert.Contains(t, result, "go.mod")
}

func TestRenderDirectoryTree_Nested(t *testing.T) {
	entries := []DirEntry{
		{Path: "cmd", IsDir: true, Depth: 1},
		{Path: "cmd/app", IsDir: true, Depth: 2},
		{Path: "cmd/app/main.go", IsDir: false, Depth: 3},
		{Path: "internal", IsDir: true, Depth: 1},
	}

	result := renderDirectoryTree(entries, "project")

	assert.Contains(t, result, "project/")
	assert.Contains(t, result, "cmd/")
	assert.Contains(t, result, "app/")
	assert.Contains(t, result, "main.go")
	assert.Contains(t, result, "internal/")
}

func TestGenerate_MultipleTechStack(t *testing.T) {
	analysis := &RepoAnalysis{
		Name: "multi",
		TechStack: []TechComponent{
			{Name: "Go", Version: "1.22", Source: "go.mod"},
			{Name: "Docker", Version: "", Source: "Dockerfile"},
			{Name: "GoReleaser", Version: "", Source: ".goreleaser.yml"},
		},
		BuildCommands: []BuildCommand{
			{Name: "build", Command: "go build ./...", Source: "go.mod"},
		},
	}

	var buf strings.Builder
	err := Generate(analysis, &buf)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "**Go** 1.22")
	assert.Contains(t, out, "**Docker**")
	assert.Contains(t, out, "**GoReleaser**")
}
