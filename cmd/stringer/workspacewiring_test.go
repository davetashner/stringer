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
	assert.Equal(t, filepath.Join("svc-a", "main.go"), signals[0].FilePath)

	assert.Equal(t, "svc-a", signals[1].Workspace)
	assert.Equal(t, filepath.Join("svc-a", "lib/util.go"), signals[1].FilePath)
}

func TestStampWorkspace_NestedRel(t *testing.T) {
	signals := []signal.RawSignal{
		{FilePath: "handler.go", Title: "add handler"},
	}
	ws := workspaceEntry{Name: "api", Path: "/root/packages/api", Rel: filepath.Join("packages", "api")}
	stampWorkspace(ws, signals)

	assert.Equal(t, "api", signals[0].Workspace)
	assert.Equal(t, filepath.Join("packages", "api", "handler.go"), signals[0].FilePath)
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
