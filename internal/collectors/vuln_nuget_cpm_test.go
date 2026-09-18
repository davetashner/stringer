// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

func TestResolveCsprojDeps_CentralPackageManagement(t *testing.T) {
	central := &nugetCentralVersions{
		central: map[string]string{"newtonsoft.json": "13.0.3", "serilog": "3.1.1"},
		global:  []csprojPackageRef{{Include: "StyleCop.Analyzers", Version: "1.2.0"}},
		locked:  map[string]string{},
	}
	data := []byte(`<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" />
    <PackageReference Include="Serilog" VersionOverride="4.0.0" />
    <PackageReference Include="Unknown" />
    <PackageReference Include="Explicit" Version="1.0.0" />
    <PackageReference Include="Newtonsoft.Json" />
  </ItemGroup>
</Project>`)

	queries, err := resolveCsprojDeps(data, central)
	require.NoError(t, err)

	var got []string
	for _, q := range queries {
		got = append(got, q.Name+"@"+q.Version)
		assert.Equal(t, "NuGet", q.Ecosystem)
		assert.False(t, q.IsRange)
	}
	assert.Equal(t, []string{"Newtonsoft.Json@13.0.3", "Serilog@4.0.0", "Explicit@1.0.0", "StyleCop.Analyzers@1.2.0"}, got)
}

func TestNuGetCentralVersions_VersionFor(t *testing.T) {
	var nilVersions *nugetCentralVersions
	assert.Equal(t, "1.0", nilVersions.versionFor(csprojPackageRef{Include: "A", Version: "1.0"}))
	assert.Equal(t, "", nilVersions.versionFor(csprojPackageRef{Include: "A"}))

	n := &nugetCentralVersions{
		central: map[string]string{"a": "1.0", "b": "2.0"},
		locked:  map[string]string{"a": "1.5"},
	}
	assert.Equal(t, "1.5", n.versionFor(csprojPackageRef{Include: "A", Version: "1.1", VersionOverride: "1.2"}), "lockfile wins")
	assert.Equal(t, "2.1", n.versionFor(csprojPackageRef{Include: "B", Version: "2.1", VersionOverride: "2.2"}), "declared wins over override")
	assert.Equal(t, "2.2", n.versionFor(csprojPackageRef{Include: "b", VersionOverride: "2.2"}), "override wins over central")
	assert.Equal(t, "2.0", n.versionFor(csprojPackageRef{Include: "B"}), "central fallback, case-insensitive")
	assert.Equal(t, "", n.versionFor(csprojPackageRef{Include: "C"}))
}

func TestParseNuGetLock(t *testing.T) {
	locked := map[string]string{}
	ok := parseNuGetLock([]byte(`{
  "version": 1,
  "dependencies": {
    "net8.0": {
      "Newtonsoft.Json": { "type": "Direct", "requested": "[13.0.1, )", "resolved": "13.0.1" },
      "System.Buffers": { "type": "Transitive", "resolved": "4.5.1" },
      "My.Lib": { "type": "Project" }
    }
  }
}`), locked)
	assert.True(t, ok)
	assert.Equal(t, map[string]string{"newtonsoft.json": "13.0.1"}, locked, "only direct dependencies are recorded")

	assert.False(t, parseNuGetLock([]byte("{not json"), map[string]string{}))
	assert.False(t, parseNuGetLock([]byte(`{"dependencies": {}}`), map[string]string{}))
}

// writeCPMRepo lays out a Central Package Management repo:
//
//	Directory.Packages.props        (root: PackageVersion + GlobalPackageReference, $(Prop) interpolation)
//	Directory.Build.props           (root: implicit PackageReference)
//	src/App/App.csproj              (version-less references)
//	src/Legacy/Directory.Packages.props + Legacy.csproj (nearer props override the root)
//	src/Locked/Locked.csproj + packages.lock.json       (lockfile wins)
func writeCPMRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o600))
	}
	write("Directory.Packages.props", `<Project>
  <PropertyGroup>
    <ManagePackageVersionsCentrally>true</ManagePackageVersionsCentrally>
    <SerilogVersion>3.1.1</SerilogVersion>
  </PropertyGroup>
  <ItemGroup>
    <PackageVersion Include="Newtonsoft.Json" Version="13.0.3" />
    <PackageVersion Include="Serilog" Version="$(SerilogVersion)" />
    <PackageVersion Include="Floating" Version="2.*" />
    <PackageVersion Include="" Version="1.0" />
    <GlobalPackageReference Include="Nerdbank.GitVersioning" Version="3.6.133" />
  </ItemGroup>
</Project>`)
	write("Directory.Build.props", `<Project>
  <ItemGroup>
    <PackageReference Include="StyleCop.Analyzers" Version="1.2.0" PrivateAssets="All" />
  </ItemGroup>
</Project>`)
	write("src/App/App.csproj", `<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" />
    <PackageReference Include="Serilog" />
    <PackageReference Include="Floating" />
  </ItemGroup>
</Project>`)
	write("src/Legacy/Directory.Packages.props", `<Project>
  <ItemGroup>
    <PackageVersion Include="Newtonsoft.Json" Version="12.0.1" />
  </ItemGroup>
</Project>`)
	write("src/Legacy/Legacy.csproj", `<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" />
  </ItemGroup>
</Project>`)
	write("src/Locked/Locked.csproj", `<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" />
  </ItemGroup>
</Project>`)
	write("src/Locked/packages.lock.json", `{"version": 1, "dependencies": {"net8.0": {
  "Newtonsoft.Json": {"type": "Direct", "requested": "[13.0.3, )", "resolved": "13.0.2"}
}}}`)
	return dir
}

func TestNuGetCentralResolver_ForProject(t *testing.T) {
	dir := writeCPMRepo(t)
	r := newNuGetCentralResolver(dir)

	app := r.forProject("src/App/App.csproj")
	require.NotNil(t, app)
	assert.Equal(t, "13.0.3", app.central["newtonsoft.json"])
	assert.Equal(t, "3.1.1", app.central["serilog"], "$(Prop) interpolated from the PropertyGroup")
	assert.NotContains(t, app.central, "")
	var globals []string
	for _, g := range app.global {
		globals = append(globals, g.Include+"@"+g.Version)
	}
	assert.ElementsMatch(t, []string{"StyleCop.Analyzers@1.2.0", "Nerdbank.GitVersioning@3.6.133"}, globals)
	assert.Same(t, app, r.forProject("src/App/App.csproj"), "cached per directory")

	legacy := r.forProject("src/Legacy/Legacy.csproj")
	require.NotNil(t, legacy)
	assert.Equal(t, "12.0.1", legacy.central["newtonsoft.json"], "nearest props win")

	locked := r.forProject("src/Locked/Locked.csproj")
	require.NotNil(t, locked)
	assert.Equal(t, "13.0.2", locked.versionFor(csprojPackageRef{Include: "Newtonsoft.Json"}))

	assert.Nil(t, newNuGetCentralResolver(t.TempDir()).forProject("App.csproj"), "no props anywhere")
}

func TestNuGetCentralResolver_MalformedProps(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Directory.Packages.props"), []byte("<Project><ItemGroup>"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Directory.Build.props"), []byte(`<Project><ItemGroup><PackageReference Include="X" Version="1" /></ItemGroup></Project>`), 0o600))

	n := newNuGetCentralResolver(dir).forProject("App.csproj")
	require.NotNil(t, n, "the readable props file still applies")
	assert.Len(t, n.global, 1)
	assert.Empty(t, n.central)
}

func TestParseCsprojQueries_CentralPackageManagement(t *testing.T) {
	dir := writeCPMRepo(t)

	file, queries := parseCsprojQueries(dir)
	assert.Equal(t, "*.csproj", file)

	got := map[string]PackageQuery{}
	for _, q := range queries {
		got[q.Name] = q
	}
	assert.Len(t, got, 5)
	assert.Equal(t, "13.0.3", got["Newtonsoft.Json"].Version, "first project wins the cross-project dedup")
	assert.Equal(t, "3.1.1", got["Serilog"].Version)
	assert.Equal(t, "3.6.133", got["Nerdbank.GitVersioning"].Version)
	assert.Equal(t, "1.2.0", got["StyleCop.Analyzers"].Version)
	assert.True(t, got["Floating"].IsRange)
	assert.Equal(t, "2.*", got["Floating"].Constraint)
}

func TestVulnCollector_CsprojCentralPackageManagement(t *testing.T) {
	dir := writeCPMRepo(t)
	c := &VulnCollector{osv: &mockOSVClient{results: []VulnDetail{{
		ID: "GHSA-cpm1", Aliases: []string{"CVE-2025-0002"}, Summary: "bad json",
		Ecosystem: "NuGet", PackageName: "Newtonsoft.Json", Version: "13.0.3", FixedVersion: "13.0.4",
	}}}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "*.csproj", signals[0].FilePath)
	assert.Contains(t, signals[0].Tags, "csharp")
}

func TestDepHealthCollector_NuGetCentralPackageManagement(t *testing.T) {
	dir := writeCPMRepo(t)
	client := &mockNuGetRegistryClient{results: map[string]*nugetRegistrationInfo{
		"Newtonsoft.Json": {Items: []nugetRegistrationPage{{Items: []nugetRegistrationLeaf{{
			CatalogEntry: nugetCatalogEntry{ID: "Newtonsoft.Json", Version: "13.0.3", Deprecation: &nugetDeprecation{Reasons: []string{"Legacy"}}},
		}}}}},
	}}

	c := &DepHealthCollector{nugetClient: client}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "Deprecated NuGet package: Newtonsoft.Json", signals[0].Title)
	assert.Equal(t, "*.csproj", signals[0].FilePath)
	assert.Contains(t, c.metrics.Ecosystems, "nuget")
}
