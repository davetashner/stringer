// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/davetashner/stringer/internal/state"
	"github.com/davetashner/stringer/internal/workspace"
)

// workspaceEntry describes a single target to scan. For non-monorepos or when
// workspace detection is disabled, a single entry with empty Name is returned,
// preserving the existing single-directory behavior.
type workspaceEntry struct {
	Name string // workspace name (empty for non-monorepo)
	Path string // absolute path to scan
	Rel  string // relative to monorepo root ("." for single-dir)
}

// resolveWorkspaces determines the list of workspace entries to scan based on
// auto-detection results and CLI flags. When noWorkspaces is true or no layout
// is detected, it returns a single entry for rootPath.
func resolveWorkspaces(rootPath string, noWorkspaces bool, workspaceFilter string) []workspaceEntry {
	if noWorkspaces {
		return []workspaceEntry{{Path: rootPath, Rel: "."}}
	}

	layout, err := workspace.Detect(rootPath)
	if err != nil {
		slog.Warn("workspace detection failed, scanning root", "error", err)
		return []workspaceEntry{{Path: rootPath, Rel: "."}}
	}
	if layout == nil {
		return []workspaceEntry{{Path: rootPath, Rel: "."}}
	}

	slog.Info("monorepo detected", "kind", layout.Kind, "workspaces", len(layout.Workspaces))

	entries := make([]workspaceEntry, 0, len(layout.Workspaces))
	for _, ws := range layout.Workspaces {
		entries = append(entries, workspaceEntry{
			Name: ws.Name,
			Path: ws.Path,
			Rel:  ws.Rel,
		})
	}

	// Apply --workspace filter if set.
	if workspaceFilter != "" {
		entries = filterWorkspaceEntries(entries, workspaceFilter)
	}

	if len(entries) == 0 {
		slog.Warn("no matching workspaces, falling back to root scan")
		return []workspaceEntry{{Path: rootPath, Rel: "."}}
	}

	return entries
}

// filterWorkspaceEntries keeps only entries whose Name matches one of the
// comma-separated names in filter.
func filterWorkspaceEntries(entries []workspaceEntry, filter string) []workspaceEntry {
	want := make(map[string]bool)
	for _, name := range strings.Split(filter, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			want[name] = true
		}
	}

	var result []workspaceEntry
	for _, e := range entries {
		if want[e.Name] {
			result = append(result, e)
		}
	}
	return result
}

// stampWorkspace annotates signals with the workspace name and adjusts
// FilePath to be relative to the monorepo root. When ws.Name is empty
// (non-monorepo), signals are returned unchanged.
func stampWorkspace(ws workspaceEntry, signals []signal.RawSignal) {
	if ws.Name == "" {
		return
	}
	for i := range signals {
		signals[i].Workspace = ws.Name
		if ws.Rel != "." {
			signals[i].FilePath = filepath.Join(ws.Rel, signals[i].FilePath)
		}
	}
}

// saveDeltaState saves delta state, scoping per-workspace when signals span
// multiple workspaces. For single-workspace or non-monorepo scans, it saves
// to the default location.
func saveDeltaState(absPath string, collectorNames []string, allSignals []signal.RawSignal, workspaces []workspaceEntry) error {
	hasWorkspaces := false
	for _, ws := range workspaces {
		if ws.Name != "" {
			hasWorkspaces = true
			break
		}
	}

	if !hasWorkspaces {
		newState := state.Build(absPath, collectorNames, allSignals)
		if err := state.Save(absPath, newState); err != nil {
			return err
		}
		slog.Info("delta state saved", "hashes", newState.SignalCount)
		return nil
	}

	// Group signals by workspace and save per-workspace state files.
	byWS := make(map[string][]signal.RawSignal)
	for _, sig := range allSignals {
		ws := sig.Workspace
		if ws == "" {
			ws = "_root"
		}
		byWS[ws] = append(byWS[ws], sig)
	}

	for _, ws := range workspaces {
		wsName := ws.Name
		if wsName == "" {
			wsName = "_root"
		}
		wsSigs := byWS[wsName]
		newState := state.Build(absPath, collectorNames, wsSigs)
		if err := state.SaveWorkspace(absPath, wsName, newState); err != nil {
			return err
		}
		slog.Info("delta state saved", "workspace", wsName, "hashes", newState.SignalCount)
	}
	return nil
}
