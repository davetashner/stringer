// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package docs

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/davetashner/stringer/internal/testable"
)

// FS is the file system implementation used by this package.
// Override in tests with a testable.MockFileSystem.
var FS testable.FileSystem = testable.DefaultFS

// DirEntry represents a directory or file in the repo tree.
type DirEntry struct {
	Path  string
	IsDir bool
	Depth int
}

// TechComponent represents a detected technology.
type TechComponent struct {
	Name    string // e.g., "Go", "Node.js", "Python"
	Version string // e.g., "1.24", "18", "3.11"
	Source  string // file that detected it, e.g., "go.mod"
}

// BuildCommand is a detected build/test command.
type BuildCommand struct {
	Name    string // e.g., "build", "test", "lint"
	Command string // e.g., "go build ./..."
	Source  string // which file suggested it
}

// CodePattern is a detected code pattern or convention.
type CodePattern struct {
	Name        string // e.g., "Cobra CLI", "Registry Pattern"
	Description string
}

// RepoAnalysis holds the results of analyzing a repository.
type RepoAnalysis struct {
	Name          string
	Language      string
	Description   string
	DirectoryTree []DirEntry
	TechStack     []TechComponent
	BuildCommands []BuildCommand
	Patterns      []CodePattern
	HasREADME     bool
	HasAGENTSMD   bool
}

// Analyze scans a repository path and returns analysis results.
func Analyze(repoPath string) (*RepoAnalysis, error) {
	analysis := &RepoAnalysis{
		Name: filepath.Base(repoPath),
	}

	// Check for existing files.
	if _, err := FS.Stat(filepath.Join(repoPath, "README.md")); err == nil {
		analysis.HasREADME = true
	}
	if _, err := FS.Stat(filepath.Join(repoPath, "AGENTS.md")); err == nil {
		analysis.HasAGENTSMD = true
	}

	// Detect tech stack from build files.
	detections := DetectAll(repoPath)
	for _, d := range detections {
		analysis.TechStack = append(analysis.TechStack, d.Components...)
		analysis.BuildCommands = append(analysis.BuildCommands, d.Commands...)
	}

	// Set primary language from first detected component.
	if len(analysis.TechStack) > 0 {
		analysis.Language = analysis.TechStack[0].Name
	}

	// Build directory tree (max depth 3, skip hidden dirs and common noise).
	analysis.DirectoryTree = buildDirectoryTree(repoPath, 3)

	// Detect code patterns.
	analysis.Patterns = detectPatterns(repoPath, detections)

	return analysis, nil
}

// buildDirectoryTree walks the repo up to maxDepth, skipping hidden/vendor dirs.
func buildDirectoryTree(repoPath string, maxDepth int) []DirEntry {
	var entries []DirEntry
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "vendor": true,
		".idea": true, ".vscode": true, "__pycache__": true,
		".stringer": true, ".beads": true, "dist": true, "build": true,
	}

	_ = FS.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error { //nolint:errcheck // best-effort directory scan; empty result on failure is acceptable
		if err != nil {
			return nil // skip errors
		}

		rel, _ := filepath.Rel(repoPath, path) //nolint:errcheck // best-effort relative path; falls back to absolute
		if rel == "." {
			return nil
		}

		depth := len(strings.Split(rel, string(filepath.Separator)))
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		name := filepath.Base(path)
		// Skip hidden files/dirs (except at root level for important ones).
		if strings.HasPrefix(name, ".") && depth > 1 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			if skipDirs[name] {
				return filepath.SkipDir
			}
			entries = append(entries, DirEntry{Path: rel, IsDir: true, Depth: depth})
		} else {
			entries = append(entries, DirEntry{Path: rel, IsDir: false, Depth: depth})
		}

		return nil
	})

	return entries
}

// detectPatterns identifies code patterns based on detected tech and file structure.
func detectPatterns(repoPath string, detections []Detection) []CodePattern {
	var patterns []CodePattern

	for _, d := range detections {
		for _, c := range d.Components {
			switch c.Name {
			case "Cobra":
				patterns = append(patterns, CodePattern{
					Name:        "Cobra CLI",
					Description: "Uses spf13/cobra for command-line interface",
				})
			case "React":
				patterns = append(patterns, CodePattern{
					Name:        "React",
					Description: "React frontend framework",
				})
			}
		}
	}

	if _, err := FS.Stat(filepath.Join(repoPath, "internal")); err == nil {
		patterns = append(patterns, CodePattern{
			Name:        "Go Internal Packages",
			Description: "Uses Go internal/ directory for private packages",
		})
	}
	if _, err := FS.Stat(filepath.Join(repoPath, "docs", "decisions")); err == nil {
		patterns = append(patterns, CodePattern{
			Name:        "Decision Records",
			Description: "Uses architectural decision records in docs/decisions/",
		})
	}
	if _, err := FS.Stat(filepath.Join(repoPath, ".github", "workflows")); err == nil {
		patterns = append(patterns, CodePattern{
			Name:        "GitHub Actions CI",
			Description: "Uses GitHub Actions for CI/CD",
		})
	}

	return patterns
}
