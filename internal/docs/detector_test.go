package docs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectGoMod(t *testing.T) {
	dir := t.TempDir()
	goMod := "module example.com/myapp\n\ngo 1.24\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o600))

	det := detectGoMod(dir)
	require.NotNil(t, det)
	assert.Equal(t, "go.mod", det.File)

	// Should detect Go version.
	require.NotEmpty(t, det.Components)
	assert.Equal(t, "Go", det.Components[0].Name)
	assert.Equal(t, "1.24", det.Components[0].Version)
	assert.Equal(t, "go.mod", det.Components[0].Source)

	// Should have standard build commands.
	require.Len(t, det.Commands, 3)
	assert.Equal(t, "build", det.Commands[0].Name)
	assert.Equal(t, "go build ./...", det.Commands[0].Command)
	assert.Equal(t, "test", det.Commands[1].Name)
	assert.Equal(t, "vet", det.Commands[2].Name)
}

func TestDetectGoMod_WithDeps(t *testing.T) {
	dir := t.TempDir()
	goMod := `module example.com/myapp

go 1.24

require (
	github.com/spf13/cobra v1.8.0
	github.com/stretchr/testify v1.9.0
	github.com/go-git/go-git/v5 v5.12.0
)
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o600))

	det := detectGoMod(dir)
	require.NotNil(t, det)

	componentNames := make(map[string]bool)
	for _, c := range det.Components {
		componentNames[c.Name] = true
	}

	assert.True(t, componentNames["Go"], "should detect Go")
	assert.True(t, componentNames["Cobra"], "should detect Cobra")
	assert.True(t, componentNames["Testify"], "should detect Testify")
	assert.True(t, componentNames["go-git"], "should detect go-git")
}

func TestDetectGoMod_SingleRequire(t *testing.T) {
	dir := t.TempDir()
	goMod := `module example.com/myapp

go 1.22

require github.com/spf13/cobra v1.8.0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o600))

	det := detectGoMod(dir)
	require.NotNil(t, det)

	componentNames := make(map[string]bool)
	for _, c := range det.Components {
		componentNames[c.Name] = true
	}

	assert.True(t, componentNames["Cobra"], "should detect single-line require for Cobra")
}

func TestDetectGoMod_NoFile(t *testing.T) {
	dir := t.TempDir()
	det := detectGoMod(dir)
	assert.Nil(t, det)
}

func TestDetectPackageJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o600))

	det := detectPackageJSON(dir)
	require.NotNil(t, det)
	assert.Equal(t, "package.json", det.File)
	require.Len(t, det.Components, 1)
	assert.Equal(t, "Node.js", det.Components[0].Name)
	require.Len(t, det.Commands, 2)
	assert.Equal(t, "install", det.Commands[0].Name)
	assert.Equal(t, "npm install", det.Commands[0].Command)
}

func TestDetectPackageJSON_NoFile(t *testing.T) {
	dir := t.TempDir()
	det := detectPackageJSON(dir)
	assert.Nil(t, det)
}

func TestDetectMakefile(t *testing.T) {
	dir := t.TempDir()
	makefile := `build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run

.PHONY: build test lint
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makefile), 0o600))

	det := detectMakefile(dir)
	require.NotNil(t, det)
	assert.Equal(t, "Makefile", det.File)

	cmdNames := make(map[string]bool)
	for _, c := range det.Commands {
		cmdNames[c.Name] = true
	}

	assert.True(t, cmdNames["build"], "should detect build target")
	assert.True(t, cmdNames["test"], "should detect test target")
	assert.True(t, cmdNames["lint"], "should detect lint target")
}

func TestDetectMakefile_SkipsNonStandardTargets(t *testing.T) {
	dir := t.TempDir()
	makefile := `custom-target:
	echo hello

build:
	go build

internal-thing:
	echo internal
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makefile), 0o600))

	det := detectMakefile(dir)
	require.NotNil(t, det)

	for _, c := range det.Commands {
		assert.NotEqual(t, "custom-target", c.Name, "should skip non-standard targets")
		assert.NotEqual(t, "internal-thing", c.Name, "should skip non-standard targets")
	}
	require.Len(t, det.Commands, 1)
	assert.Equal(t, "build", det.Commands[0].Name)
}

func TestDetectMakefile_NoFile(t *testing.T) {
	dir := t.TempDir()
	det := detectMakefile(dir)
	assert.Nil(t, det)
}

func TestDetectCargoToml(t *testing.T) {
	det := detectCargoToml("")
	require.NotNil(t, det)
	assert.Equal(t, "Cargo.toml", det.File)
	require.Len(t, det.Components, 1)
	assert.Equal(t, "Rust", det.Components[0].Name)
	require.Len(t, det.Commands, 3)
	assert.Equal(t, "build", det.Commands[0].Name)
	assert.Equal(t, "cargo build", det.Commands[0].Command)
}

func TestDetectPyprojectToml(t *testing.T) {
	det := detectPyprojectToml("")
	require.NotNil(t, det)
	assert.Equal(t, "Python", det.Components[0].Name)
	assert.Equal(t, "pytest", det.Commands[0].Command)
}

func TestDetectRequirementsTxt(t *testing.T) {
	det := detectRequirementsTxt("")
	require.NotNil(t, det)
	assert.Equal(t, "Python", det.Components[0].Name)
	assert.Equal(t, "pip install -r requirements.txt", det.Commands[0].Command)
}

func TestDetectDockerfile(t *testing.T) {
	det := detectDockerfile("")
	require.NotNil(t, det)
	assert.Equal(t, "Docker", det.Components[0].Name)
	assert.Equal(t, "docker build .", det.Commands[0].Command)
}

func TestDetectGoReleaser(t *testing.T) {
	det := detectGoReleaser("")
	require.NotNil(t, det)
	assert.Equal(t, "GoReleaser", det.Components[0].Name)
}

func TestDetectAll_MultipleFiles(t *testing.T) {
	dir := t.TempDir()

	// Create go.mod and Makefile.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))
	makefile := "build:\n\tgo build\n\ntest:\n\tgo test\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makefile), 0o600))

	detections := DetectAll(dir)
	require.Len(t, detections, 2)

	files := make(map[string]bool)
	for _, d := range detections {
		files[d.File] = true
	}
	assert.True(t, files["go.mod"])
	assert.True(t, files["Makefile"])
}

func TestDetectAll_NoFiles(t *testing.T) {
	dir := t.TempDir()
	detections := DetectAll(dir)
	assert.Empty(t, detections)
}

func TestDetectAll_GoReleaserYAML(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".goreleaser.yaml"), []byte("builds:\n"), 0o600))

	detections := DetectAll(dir)
	require.Len(t, detections, 1)
	assert.Equal(t, "GoReleaser", detections[0].Components[0].Name)
}
