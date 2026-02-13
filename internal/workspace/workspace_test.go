package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetect_NoMatch(t *testing.T) {
	dir := t.TempDir()
	layout, err := Detect(dir)
	require.NoError(t, err)
	assert.Nil(t, layout, "empty dir should not detect any workspace")
}

func TestDetect_GoWork(t *testing.T) {
	dir := t.TempDir()

	// Create go.work file.
	writeFile(t, filepath.Join(dir, "go.work"), `go 1.24

use (
	./svc-a
	./svc-b
)
`)
	mkdirAll(t, filepath.Join(dir, "svc-a"))
	mkdirAll(t, filepath.Join(dir, "svc-b"))

	layout, err := Detect(dir)
	require.NoError(t, err)
	require.NotNil(t, layout)
	assert.Equal(t, KindGoWork, layout.Kind)
	assert.Equal(t, dir, layout.Root)
	require.Len(t, layout.Workspaces, 2)

	assert.Equal(t, "svc-a", layout.Workspaces[0].Name)
	assert.Equal(t, filepath.Join(dir, "svc-a"), layout.Workspaces[0].Path)
	assert.Equal(t, "svc-a", layout.Workspaces[0].Rel)

	assert.Equal(t, "svc-b", layout.Workspaces[1].Name)
	assert.Equal(t, filepath.Join(dir, "svc-b"), layout.Workspaces[1].Path)
	assert.Equal(t, "svc-b", layout.Workspaces[1].Rel)
}

func TestDetect_GoWork_MissingDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.work"), `go 1.24

use ./nonexistent
`)

	layout, err := Detect(dir)
	require.NoError(t, err)
	assert.Nil(t, layout, "go.work referencing nonexistent dir should return nil")
}

func TestDetect_Pnpm(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "pnpm-workspace.yaml"), `packages:
  - "packages/*"
`)
	mkdirAll(t, filepath.Join(dir, "packages", "core"))
	mkdirAll(t, filepath.Join(dir, "packages", "utils"))

	layout, err := Detect(dir)
	require.NoError(t, err)
	require.NotNil(t, layout)
	assert.Equal(t, KindPnpm, layout.Kind)
	require.Len(t, layout.Workspaces, 2)

	names := []string{layout.Workspaces[0].Name, layout.Workspaces[1].Name}
	assert.Contains(t, names, "core")
	assert.Contains(t, names, "utils")
}

func TestDetect_Npm_ArrayForm(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{
  "name": "my-monorepo",
  "workspaces": ["packages/*"]
}`)
	mkdirAll(t, filepath.Join(dir, "packages", "web"))

	layout, err := Detect(dir)
	require.NoError(t, err)
	require.NotNil(t, layout)
	assert.Equal(t, KindNpm, layout.Kind)
	require.Len(t, layout.Workspaces, 1)
	assert.Equal(t, "web", layout.Workspaces[0].Name)
}

func TestDetect_Npm_ObjectForm(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{
  "name": "my-monorepo",
  "workspaces": {"packages": ["libs/*"]}
}`)
	mkdirAll(t, filepath.Join(dir, "libs", "shared"))

	layout, err := Detect(dir)
	require.NoError(t, err)
	require.NotNil(t, layout)
	assert.Equal(t, KindNpm, layout.Kind)
	require.Len(t, layout.Workspaces, 1)
	assert.Equal(t, "shared", layout.Workspaces[0].Name)
}

func TestDetect_Npm_NoWorkspacesField(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "package.json"), `{"name": "not-a-monorepo"}`)

	layout, err := Detect(dir)
	require.NoError(t, err)
	assert.Nil(t, layout, "package.json without workspaces should not match")
}

func TestDetect_Lerna(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lerna.json"), `{
  "packages": ["packages/*"],
  "version": "1.0.0"
}`)
	mkdirAll(t, filepath.Join(dir, "packages", "alpha"))
	mkdirAll(t, filepath.Join(dir, "packages", "beta"))

	layout, err := Detect(dir)
	require.NoError(t, err)
	require.NotNil(t, layout)
	assert.Equal(t, KindLerna, layout.Kind)
	require.Len(t, layout.Workspaces, 2)
}

func TestDetect_Lerna_EmptyPackages(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "lerna.json"), `{"packages": [], "version": "1.0.0"}`)

	layout, err := Detect(dir)
	require.NoError(t, err)
	assert.Nil(t, layout, "lerna.json with empty packages should return nil")
}

func TestDetect_Nx_DefaultLayout(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "nx.json"), `{}`)
	mkdirAll(t, filepath.Join(dir, "packages", "ui"))
	mkdirAll(t, filepath.Join(dir, "apps", "web"))

	layout, err := Detect(dir)
	require.NoError(t, err)
	require.NotNil(t, layout)
	assert.Equal(t, KindNx, layout.Kind)
	require.Len(t, layout.Workspaces, 2)

	names := []string{layout.Workspaces[0].Name, layout.Workspaces[1].Name}
	assert.Contains(t, names, "ui")
	assert.Contains(t, names, "web")
}

func TestDetect_Nx_CustomLayout(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "nx.json"), `{
  "workspaceLayout": {
    "appsDir": "projects",
    "libsDir": "shared"
  }
}`)
	mkdirAll(t, filepath.Join(dir, "projects", "app1"))
	mkdirAll(t, filepath.Join(dir, "shared", "common"))

	layout, err := Detect(dir)
	require.NoError(t, err)
	require.NotNil(t, layout)
	assert.Equal(t, KindNx, layout.Kind)
	require.Len(t, layout.Workspaces, 2)
}

func TestDetect_Nx_NoMatchingDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "nx.json"), `{}`)
	// No packages/, apps/, or libs/ directories.

	layout, err := Detect(dir)
	require.NoError(t, err)
	assert.Nil(t, layout, "nx.json with no matching dirs should return nil")
}

func TestDetect_Cargo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Cargo.toml"), `[workspace]
members = ["crates/*"]
`)
	mkdirAll(t, filepath.Join(dir, "crates", "core"))
	mkdirAll(t, filepath.Join(dir, "crates", "cli"))

	layout, err := Detect(dir)
	require.NoError(t, err)
	require.NotNil(t, layout)
	assert.Equal(t, KindCargo, layout.Kind)
	require.Len(t, layout.Workspaces, 2)
}

func TestDetect_Cargo_WithExclude(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Cargo.toml"), `[workspace]
members = ["crates/*"]
exclude = ["crates/experimental"]
`)
	mkdirAll(t, filepath.Join(dir, "crates", "core"))
	mkdirAll(t, filepath.Join(dir, "crates", "experimental"))

	layout, err := Detect(dir)
	require.NoError(t, err)
	require.NotNil(t, layout)
	assert.Equal(t, KindCargo, layout.Kind)
	require.Len(t, layout.Workspaces, 1)
	assert.Equal(t, "core", layout.Workspaces[0].Name)
}

func TestDetect_Cargo_NoWorkspace(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Cargo.toml"), `[package]
name = "single-crate"
version = "0.1.0"
`)

	layout, err := Detect(dir)
	require.NoError(t, err)
	assert.Nil(t, layout, "Cargo.toml without [workspace] should return nil")
}

func TestDetect_PriorityOrder(t *testing.T) {
	// When both go.work and pnpm-workspace.yaml exist, go.work wins.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.work"), `go 1.24

use ./mod-a
`)
	writeFile(t, filepath.Join(dir, "pnpm-workspace.yaml"), `packages:
  - "pkg/*"
`)
	mkdirAll(t, filepath.Join(dir, "mod-a"))
	mkdirAll(t, filepath.Join(dir, "pkg", "x"))

	layout, err := Detect(dir)
	require.NoError(t, err)
	require.NotNil(t, layout)
	assert.Equal(t, KindGoWork, layout.Kind, "go.work should take priority over pnpm")
}

func TestDetect_RelativePaths(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.work"), `go 1.24

use ./nested/deep/module
`)
	mkdirAll(t, filepath.Join(dir, "nested", "deep", "module"))

	layout, err := Detect(dir)
	require.NoError(t, err)
	require.NotNil(t, layout)
	require.Len(t, layout.Workspaces, 1)
	assert.Equal(t, "module", layout.Workspaces[0].Name)
	assert.Equal(t, filepath.Join("nested", "deep", "module"), layout.Workspaces[0].Rel)
}

func TestExpandGlobs_Dedup(t *testing.T) {
	dir := t.TempDir()
	mkdirAll(t, filepath.Join(dir, "packages", "a"))

	// Same directory matched by two patterns.
	dirs, err := expandGlobs(dir, []string{"packages/*", "packages/a"})
	require.NoError(t, err)
	assert.Len(t, dirs, 1, "duplicates should be removed")
}

func TestExpandGlobs_SkipsFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "packages", "readme.txt"), "not a dir")
	mkdirAll(t, filepath.Join(dir, "packages", "real"))

	dirs, err := expandGlobs(dir, []string{"packages/*"})
	require.NoError(t, err)
	assert.Len(t, dirs, 1)
	assert.Equal(t, filepath.Join(dir, "packages", "real"), dirs[0])
}

// --- helpers ---

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(path, 0o750))
}
