// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

// Package workspace detects monorepo structures and enumerates their workspaces.
package workspace

import (
	"os"
	"path/filepath"
)

// Kind identifies the monorepo tool or convention that defines the workspace layout.
type Kind string

const (
	KindGoWork Kind = "go-work"
	KindPnpm   Kind = "pnpm"
	KindNpm    Kind = "npm"
	KindLerna  Kind = "lerna"
	KindNx     Kind = "nx"
	KindCargo  Kind = "cargo"
)

// Workspace represents a single workspace within a monorepo.
type Workspace struct {
	Name string // basename or package name
	Path string // absolute path
	Rel  string // relative to monorepo root
}

// Layout describes a detected monorepo structure.
type Layout struct {
	Kind       Kind
	Root       string
	Workspaces []Workspace
}

// detector is a function that attempts to detect a monorepo layout at rootPath.
// It returns nil, nil when the expected manifest file is not present.
type detector func(rootPath string) (*Layout, error)

// detectors is the ordered list of detection functions. First match wins.
var detectors = []detector{
	detectGoWork,
	detectPnpm,
	detectNpm,
	detectLerna,
	detectNx,
	detectCargo,
}

// Detect probes rootPath for known monorepo layouts. It returns the first
// matching Layout, or nil if no monorepo structure is detected.
func Detect(rootPath string) (*Layout, error) {
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}

	for _, fn := range detectors {
		layout, err := fn(abs)
		if err != nil {
			return nil, err
		}
		if layout != nil {
			return layout, nil
		}
	}
	return nil, nil
}

// fileExists returns true if path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// dirExists returns true if path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
