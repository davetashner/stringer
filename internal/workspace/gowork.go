// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package workspace

import (
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// detectGoWork detects a Go workspace defined by a go.work file.
func detectGoWork(rootPath string) (*Layout, error) {
	workFile := filepath.Join(rootPath, "go.work")
	if !fileExists(workFile) {
		return nil, nil
	}

	data, err := os.ReadFile(workFile) //nolint:gosec // trusted path from caller
	if err != nil {
		return nil, err
	}

	wf, err := modfile.ParseWork(workFile, data, nil)
	if err != nil {
		return nil, err
	}

	var workspaces []Workspace
	for _, use := range wf.Use {
		rel := filepath.Clean(use.Path)
		abs := rel
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(rootPath, rel)
		}
		if !dirExists(abs) {
			continue
		}
		workspaces = append(workspaces, Workspace{
			Name: filepath.Base(rel),
			Path: abs,
			Rel:  rel,
		})
	}

	if len(workspaces) == 0 {
		return nil, nil
	}

	return &Layout{
		Kind:       KindGoWork,
		Root:       rootPath,
		Workspaces: workspaces,
	}, nil
}
