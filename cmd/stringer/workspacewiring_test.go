// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

func TestResolveWorkspaces_NoWorkspacesFlag(t *testing.T) {
	dir := t.TempDir()
	entries := resolveWorkspaces(dir, true, "")
	require.Len(t, entries, 1)
	assert.Equal(t, "", entries[0].Name)
	assert.Equal(t, dir, entries[0].Path)
	assert.Equal(t, ".", entries[0].Rel)
}

func TestResolveWorkspaces_NoLayout(t *testing.T) {
	dir := t.TempDir()
	entries := resolveWorkspaces(dir, false, "")
	require.Len(t, entries, 1)
	assert.Equal(t, "", entries[0].Name)
	assert.Equal(t, dir, entries[0].Path)
}

func TestResolveWorkspaces_DetectsGoWork(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.work"), []byte("go 1.24\n\nuse (\n\t./svc-a\n\t./svc-b\n)\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "svc-a"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "svc-b"), 0o750))

	entries := resolveWorkspaces(dir, false, "")
	require.Len(t, entries, 2)
	assert.Equal(t, "svc-a", entries[0].Name)
	assert.Equal(t, "svc-b", entries[1].Name)
}

func TestResolveWorkspaces_FilterByName(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.work"), []byte("go 1.24\n\nuse (\n\t./svc-a\n\t./svc-b\n)\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "svc-a"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "svc-b"), 0o750))

	entries := resolveWorkspaces(dir, false, "svc-a")
	require.Len(t, entries, 1)
	assert.Equal(t, "svc-a", entries[0].Name)
}

func TestResolveWorkspaces_FilterNoMatch(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.work"), []byte("go 1.24\n\nuse ./svc-a\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "svc-a"), 0o750))

	// Non-matching filter falls back to root.
	entries := resolveWorkspaces(dir, false, "nonexistent")
	require.Len(t, entries, 1)
	assert.Equal(t, "", entries[0].Name)
	assert.Equal(t, dir, entries[0].Path)
}

func TestFilterWorkspaceEntries(t *testing.T) {
	entries := []workspaceEntry{
		{Name: "core", Path: "/a/core", Rel: "core"},
		{Name: "api", Path: "/a/api", Rel: "api"},
		{Name: "web", Path: "/a/web", Rel: "web"},
	}

	result := filterWorkspaceEntries(entries, "core,web")
	require.Len(t, result, 2)
	assert.Equal(t, "core", result[0].Name)
	assert.Equal(t, "web", result[1].Name)
}

func TestFilterWorkspaceEntries_TrimWhitespace(t *testing.T) {
	entries := []workspaceEntry{
		{Name: "alpha", Path: "/a/alpha", Rel: "alpha"},
	}
	result := filterWorkspaceEntries(entries, " alpha , ")
	require.Len(t, result, 1)
	assert.Equal(t, "alpha", result[0].Name)
}

func TestStampWorkspace_Empty(t *testing.T) {
	signals := []signal.RawSignal{
		{FilePath: "main.go", Title: "fix"},
	}
	ws := workspaceEntry{Name: "", Path: "/root", Rel: "."}
	stampWorkspace(ws, signals)

	assert.Equal(t, "", signals[0].Workspace)
	assert.Equal(t, "main.go", signals[0].FilePath)
}

func TestStampWorkspace_Named(t *testing.T) {
	signals := []signal.RawSignal{
		{FilePath: "main.go", Title: "fix"},
		{FilePath: "lib/util.go", Title: "refactor"},
	}
	ws := workspaceEntry{Name: "svc-a", Path: "/root/svc-a", Rel: "svc-a"}
	stampWorkspace(ws, signals)

	assert.Equal(t, "svc-a", signals[0].Workspace)
	assert.Equal(t, "svc-a/main.go", signals[0].FilePath)

	assert.Equal(t, "svc-a", signals[1].Workspace)
	assert.Equal(t, "svc-a/lib/util.go", signals[1].FilePath)
}

func TestStampWorkspace_NestedRel(t *testing.T) {
	signals := []signal.RawSignal{
		{FilePath: "handler.go", Title: "add handler"},
	}
	ws := workspaceEntry{Name: "api", Path: "/root/packages/api", Rel: filepath.Join("packages", "api")}
	stampWorkspace(ws, signals)

	assert.Equal(t, "api", signals[0].Workspace)
	// Output paths are slash-separated on every OS, even though ws.Rel uses
	// the OS separator.
	assert.Equal(t, "packages/api/handler.go", signals[0].FilePath)
}

func TestRunScan_NoWorkspacesFlag(t *testing.T) {
	resetScanFlags()
	dir := fixtureDir(t)

	cmd, stdout, _ := newTestCmd()
	cmd.SetArgs([]string{"scan", dir, "--no-workspaces", "--dry-run", "--quiet", "--collectors=todos"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, stdout.String(), "signal(s) found")
}

func TestScanCmd_WorkspaceFlagsRegistered(t *testing.T) {
	f := scanCmd.Flags().Lookup("workspace")
	require.NotNil(t, f, "flag --workspace not registered")
	assert.Equal(t, "", f.DefValue)

	f = scanCmd.Flags().Lookup("no-workspaces")
	require.NotNil(t, f, "flag --no-workspaces not registered")
	assert.Equal(t, "false", f.DefValue)
}

func TestReportCmd_WorkspaceFlagsRegistered(t *testing.T) {
	f := reportCmd.Flags().Lookup("workspace")
	require.NotNil(t, f, "flag --workspace not registered on report")
	assert.Equal(t, "", f.DefValue)

	f = reportCmd.Flags().Lookup("no-workspaces")
	require.NotNil(t, f, "flag --no-workspaces not registered on report")
	assert.Equal(t, "false", f.DefValue)
}

func TestSaveDeltaState_SingleWorkspace(t *testing.T) {
	dir := t.TempDir()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "main.go", Title: "fix"},
	}
	workspaces := []workspaceEntry{{Path: dir, Rel: "."}}

	err := saveDeltaState(dir, []string{"todos"}, signals, workspaces)
	require.NoError(t, err)

	// Verify state saved at root .stringer/
	_, statErr := os.Stat(filepath.Join(dir, ".stringer", "last-scan.json"))
	require.NoError(t, statErr)
}

func TestSaveDeltaState_MultipleWorkspaces(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "svc-a"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "svc-b"), 0o750))

	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "svc-a/main.go", Title: "fix a", Workspace: "svc-a"},
		{Source: "todos", Kind: "todo", FilePath: "svc-b/main.go", Title: "fix b", Workspace: "svc-b"},
	}
	workspaces := []workspaceEntry{
		{Name: "svc-a", Path: filepath.Join(dir, "svc-a"), Rel: "svc-a"},
		{Name: "svc-b", Path: filepath.Join(dir, "svc-b"), Rel: "svc-b"},
	}

	err := saveDeltaState(dir, []string{"todos"}, signals, workspaces)
	require.NoError(t, err)

	// Verify per-workspace state files.
	_, statErr := os.Stat(filepath.Join(dir, ".stringer", "svc-a", "last-scan.json"))
	require.NoError(t, statErr, "svc-a state file should exist")

	_, statErr = os.Stat(filepath.Join(dir, ".stringer", "svc-b", "last-scan.json"))
	require.NoError(t, statErr, "svc-b state file should exist")
}

func TestResolveWorkspaces_MembersListScannedFirst(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.work"), []byte("go 1.24\n\nuse (\n\t.\n\t./svc-a\n\t./svc-b\n)\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "svc-a"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "svc-b"), 0o750))

	all := resolveWorkspaces(dir, false, "")
	require.Len(t, all, 3)
	want := []string{dir, filepath.Join(dir, "svc-a"), filepath.Join(dir, "svc-b")}
	for _, e := range all {
		assert.Equal(t, want, e.Members, e.Name)
	}

	// A --workspace filter keeps every member known, scanned entries first.
	filtered := resolveWorkspaces(dir, false, "svc-b")
	require.Len(t, filtered, 1)
	assert.Equal(t, []string{filepath.Join(dir, "svc-b"), dir, filepath.Join(dir, "svc-a")}, filtered[0].Members)

	// Non-monorepo entries carry no members.
	assert.Nil(t, resolveWorkspaces(t.TempDir(), false, "")[0].Members)
	assert.Nil(t, resolveWorkspaces(dir, true, "")[0].Members)
}

func TestApplyWorkspaceMembers(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "repo")
	ws := workspaceEntry{
		Name:    "svc-a",
		Path:    filepath.Join(root, "mono", "svc-a"),
		Rel:     "svc-a",
		Members: []string{filepath.Join(root, "mono", "svc-a"), filepath.Join(root, "mono")},
	}

	// Members are expressed relative to the git root, which may be above
	// the monorepo root, and only the workspace-scoped collectors get them.
	var cfg signal.ScanConfig
	applyWorkspaceMembers(&cfg, ws, root)
	want := []string{filepath.Join("mono", "svc-a"), "mono"}
	assert.Equal(t, want, cfg.CollectorOpts["gitlog"].WorkspaceMembers)
	assert.Equal(t, want, cfg.CollectorOpts["lotteryrisk"].WorkspaceMembers)
	assert.Len(t, cfg.CollectorOpts, 2)

	// Existing options for those collectors are preserved.
	cfg = signal.ScanConfig{CollectorOpts: map[string]signal.CollectorOpts{"gitlog": {GitDepth: 7}}}
	applyWorkspaceMembers(&cfg, ws, filepath.Join(root, "mono"))
	assert.Equal(t, 7, cfg.CollectorOpts["gitlog"].GitDepth)
	assert.Equal(t, []string{"svc-a", "."}, cfg.CollectorOpts["gitlog"].WorkspaceMembers)

	// Non-monorepo entries change nothing.
	cfg = signal.ScanConfig{}
	applyWorkspaceMembers(&cfg, workspaceEntry{Path: root, Rel: "."}, root)
	assert.Nil(t, cfg.CollectorOpts)
}

func TestNestedMemberExcludes(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "repo")
	members := []string{
		root,
		filepath.Join(root, "staging", "a"),
		filepath.Join(root, "staging", "a", "nested"),
		filepath.Join(root, "b"),
	}

	// The root workspace excludes every member below it, anchored so a
	// member named b does not also hide an unrelated internal/b.
	rootWS := workspaceEntry{Name: ".", Path: root, Rel: ".", Members: members}
	assert.Equal(t, []string{"/staging/a/**", "/staging/a/nested/**", "/b/**"}, nestedMemberExcludes(rootWS))

	// A member excludes only the members nested inside it, relative to
	// itself; the root and its siblings are outside its walk anyway.
	aWS := workspaceEntry{Name: "a", Path: filepath.Join(root, "staging", "a"), Rel: "staging/a", Members: members}
	assert.Equal(t, []string{"/nested/**"}, nestedMemberExcludes(aWS))

	// A leaf member and a non-monorepo entry exclude nothing.
	leaf := workspaceEntry{Name: "nested", Path: filepath.Join(root, "staging", "a", "nested"), Rel: "staging/a/nested", Members: members}
	assert.Nil(t, nestedMemberExcludes(leaf))
	assert.Nil(t, nestedMemberExcludes(workspaceEntry{Path: root, Rel: "."}))
}

func TestApplyWorkspaceMembers_ExcludesNestedMembers(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "repo")
	members := []string{root, filepath.Join(root, "staging", "a")}
	rootWS := workspaceEntry{Name: ".", Path: root, Rel: ".", Members: members}

	// Nested member excludes are appended to the global patterns, after
	// the user's own, so the pipeline applies them to every collector.
	cfg := signal.ScanConfig{ExcludePatterns: []string{"docs/**"}}
	applyWorkspaceMembers(&cfg, rootWS, root)
	assert.Equal(t, []string{"docs/**", "/staging/a/**"}, cfg.ExcludePatterns)
	assert.Equal(t, []string{".", filepath.Join("staging", "a")}, cfg.CollectorOpts["gitlog"].WorkspaceMembers)

	// The member's own scan gets no exclude for itself.
	cfg = signal.ScanConfig{}
	applyWorkspaceMembers(&cfg, workspaceEntry{Name: "a", Path: members[1], Rel: "staging/a", Members: members}, root)
	assert.Empty(t, cfg.ExcludePatterns)
}
