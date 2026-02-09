// Package mcpserver implements an MCP (Model Context Protocol) server
// that exposes stringer's core operations as tools over stdio transport.
package mcpserver

import (
	"fmt"
	"os"
	"path/filepath"
)

// PathInfo holds the resolved path information for a repository.
type PathInfo struct {
	// AbsPath is the absolute, symlink-resolved path.
	AbsPath string
	// GitRoot is the .git root, which may differ from AbsPath for subdirectories.
	GitRoot string
}

// ResolvePath resolves a repository path to an absolute path with git root detection.
// It returns an error if the path does not exist or is not a directory.
func ResolvePath(path string) (*PathInfo, error) {
	if path == "" {
		path = "."
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve path %q: %w", path, err)
	}

	absPath, err = filepath.EvalSymlinks(absPath)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve path %q: %w", path, err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("path %q does not exist", path)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", path)
	}

	// Walk up to find .git root.
	gitRoot := absPath
	for {
		if _, err := os.Stat(filepath.Join(gitRoot, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(gitRoot)
		if parent == gitRoot {
			gitRoot = absPath
			break
		}
		gitRoot = parent
	}

	return &PathInfo{
		AbsPath: absPath,
		GitRoot: gitRoot,
	}, nil
}
