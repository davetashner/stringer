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

// --- constraint parsing (stringer-nxx.2) ---

func TestSplitSemverConstraint(t *testing.T) {
	tests := []struct {
		spec      string
		bareRange bool
		want      string
		wantRange bool
	}{
		{"1.2.3", false, "1.2.3", false},
		{"1.2.3", true, "1.2.3", true}, // Cargo: bare is caret
		{"=1.2.3", true, "1.2.3", false},
		{"^1.2.3", false, "1.2.3", true},
		{"~1.2", false, "1.2", true},
		{">=1.0, <2.0", true, "1.0", true},
		{"1.*", true, "1", true},
		{"*", true, "", true},
		{"  ", false, "", false},
		{"beta", false, "", false},
		{"> 1.0.0", false, "1.0.0", true},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, isRange := splitSemverConstraint(tt.spec, tt.bareRange)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantRange, isRange)
		})
	}
}

func TestSplitMavenConstraint(t *testing.T) {
	tests := []struct {
		spec      string
		want      string
		wantRange bool
	}{
		{"1.0", "1.0", false},
		{"[1.0]", "1.0", false},
		{"[1.0,2.0)", "1.0", true},
		{"(1.0,2.0]", "1.0", true},
		{"(,1.0]", "", true},
		{"1.0+", "1.0", true},
		{"1.0.*", "1.0", true},
		{"latest.release", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, isRange := splitMavenConstraint(tt.spec)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantRange, isRange)
		})
	}
}

func TestDeclaredSpecAndDiscount(t *testing.T) {
	assert.Equal(t, "click>=8.1.3", declaredSpec("click", ">=8.1.3"))
	assert.Equal(t, "minimist^1.2.3", declaredSpec("minimist", "^1.2.3"))
	assert.Equal(t, "log4j@[2.0,3.0)", declaredSpec("log4j", "[2.0,3.0)"))
	assert.InDelta(t, 0.57, applyRangeDiscount(0.95), 0.001)
	assert.InDelta(t, 0.51, applyRangeDiscount(0.85), 0.001)
}

// --- per-parser range detection ---

func TestParseRequirementLine_RangeDetection(t *testing.T) {
	tests := []struct {
		line           string
		wantVersion    string
		wantRange      bool
		wantConstraint string
	}{
		{"click>=8.1.3", "8.1.3", true, ">=8.1.3"},
		{"flask==3.0.0", "3.0.0", false, ""},
		{"foo ~= 1.4", "1.4", true, "~=1.4"},
		{"bar>=1.0,<2.0", "1.0", true, ">=1.0,<2.0"},
		{"baz==1.2.*", "1.2", true, "==1.2.*"},
		{"qux===1.0", "1.0", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			q := parseRequirementLine(tt.line)
			require.NotNil(t, q)
			assert.Equal(t, tt.wantVersion, q.Version)
			assert.Equal(t, tt.wantRange, q.IsRange)
			assert.Equal(t, tt.wantConstraint, q.Constraint)
		})
	}
}

func TestParseCargoDeps_RangeDetection(t *testing.T) {
	queries, err := parseCargoDeps([]byte(`[dependencies]
tokio = { version = "1.2.0", features = ["rt"] }
serde = "=1.0.100"
anyhow = "^1.0"
rand = "0.*"
log = "*"
`))
	require.NoError(t, err)
	byName := map[string]PackageQuery{}
	for _, q := range queries {
		byName[q.Name] = q
	}
	require.Len(t, byName, 4, "wildcard-only requirement is skipped: %v", byName)

	assert.Equal(t, "1.2.0", byName["tokio"].Version)
	assert.True(t, byName["tokio"].IsRange, "a bare Cargo requirement is a caret range")
	assert.Equal(t, "^1.2.0", byName["tokio"].Constraint)

	assert.False(t, byName["serde"].IsRange, "=1.0.100 pins")
	assert.Equal(t, "1.0.100", byName["serde"].Version)

	assert.True(t, byName["anyhow"].IsRange)
	assert.Equal(t, "^1.0", byName["anyhow"].Constraint)

	assert.Equal(t, "0", byName["rand"].Version)
	assert.True(t, byName["rand"].IsRange)
}

func TestParseComposerDeps_RangeAndDev(t *testing.T) {
	queries, err := parseComposerDeps([]byte(`{
		"require": {"symfony/http-foundation": "^7.4.0 || ^8.0.0", "vendor/pinned": "1.2.3"},
		"require-dev": {"phpunit/phpunit": "^10.0"}
	}`))
	require.NoError(t, err)
	byName := map[string]PackageQuery{}
	for _, q := range queries {
		byName[q.Name] = q
	}
	require.Len(t, byName, 3)
	assert.Equal(t, "7.4.0", byName["symfony/http-foundation"].Version)
	assert.True(t, byName["symfony/http-foundation"].IsRange)
	assert.Equal(t, "^7.4.0 || ^8.0.0", byName["symfony/http-foundation"].Constraint)
	assert.False(t, byName["vendor/pinned"].IsRange)
	assert.False(t, byName["vendor/pinned"].Dev)
	assert.True(t, byName["phpunit/phpunit"].Dev)
	assert.True(t, byName["phpunit/phpunit"].IsRange)
}

func TestParseNpmDeps_RangeFlag(t *testing.T) {
	queries, err := parseNpmDeps([]byte(`{"dependencies": {"minimist": "^1.2.3", "pinned": "1.0.0"}}`))
	require.NoError(t, err)
	byName := map[string]PackageQuery{}
	for _, q := range queries {
		byName[q.Name] = q
	}
	assert.True(t, byName["minimist"].IsRange)
	assert.Equal(t, "^1.2.3", byName["minimist"].Constraint)
	assert.Equal(t, "1.2.3", byName["minimist"].Version)
	assert.False(t, byName["pinned"].IsRange)
	assert.Empty(t, byName["pinned"].Constraint)
}

func TestParseMavenDeps_Ranges(t *testing.T) {
	queries, err := parseMavenDeps([]byte(`<project>
  <dependencies>
    <dependency><groupId>g</groupId><artifactId>ranged</artifactId><version>[1.0,2.0)</version></dependency>
    <dependency><groupId>g</groupId><artifactId>exact</artifactId><version>[1.0]</version></dependency>
    <dependency><groupId>g</groupId><artifactId>plain</artifactId><version>1.0</version></dependency>
    <dependency><groupId>g</groupId><artifactId>nofloor</artifactId><version>(,1.0]</version></dependency>
  </dependencies>
</project>`))
	require.NoError(t, err)
	byName := map[string]PackageQuery{}
	for _, q := range queries {
		byName[q.Name] = q
	}
	require.Len(t, byName, 3, "an upper-bound-only range has no floor to query")
	assert.Equal(t, "1.0", byName["g:ranged"].Version)
	assert.True(t, byName["g:ranged"].IsRange)
	assert.Equal(t, "[1.0,2.0)", byName["g:ranged"].Constraint)
	assert.False(t, byName["g:exact"].IsRange)
	assert.Equal(t, "1.0", byName["g:exact"].Version)
	assert.False(t, byName["g:plain"].IsRange)
}

func TestParseGradleDeps_DynamicVersions(t *testing.T) {
	queries, err := parseGradleDeps([]byte(`dependencies {
    implementation 'com.example:dyn:1.0+'
    implementation 'com.example:latest:latest.release'
    implementation group: 'com.example', name: 'mapped', version: '[2.0,3.0)'
    implementation 'com.example:exact:1.2.3'
}`))
	require.NoError(t, err)
	byName := map[string]PackageQuery{}
	for _, q := range queries {
		byName[q.Name] = q
	}
	require.Len(t, byName, 3, "latest.release cannot be queried: %v", byName)
	assert.True(t, byName["com.example:dyn"].IsRange)
	assert.Equal(t, "1.0", byName["com.example:dyn"].Version)
	assert.True(t, byName["com.example:mapped"].IsRange)
	assert.Equal(t, "2.0", byName["com.example:mapped"].Version)
	assert.False(t, byName["com.example:exact"].IsRange)
}

func TestParseSwiftPackageDeps_RangeDetection(t *testing.T) {
	queries := parseSwiftPackageDeps([]byte(`
.package(url: "https://github.com/a/from", from: "1.0.0"),
.package(url: "https://github.com/a/major", .upToNextMajor(from: "2.0.0")),
.package(url: "https://github.com/a/exact", exact: "3.0.0"),
`))
	byName := map[string]PackageQuery{}
	for _, q := range queries {
		byName[q.Name] = q
	}
	require.Len(t, byName, 3)
	assert.True(t, byName["https://github.com/a/from"].IsRange)
	assert.Equal(t, ">=1.0.0", byName["https://github.com/a/from"].Constraint)
	assert.True(t, byName["https://github.com/a/major"].IsRange)
	assert.False(t, byName["https://github.com/a/exact"].IsRange)
	assert.Equal(t, "3.0.0", byName["https://github.com/a/exact"].Version)
}

func TestParseSbtDeps_IvyRange(t *testing.T) {
	queries := parseSbtDeps([]byte(`libraryDependencies += "org.example" % "ranged" % "[1.0,2.0)"`))
	require.Len(t, queries, 1)
	assert.True(t, queries[0].IsRange)
	assert.Equal(t, "1.0", queries[0].Version)
}

func TestParseMixDeps_RangeDetection(t *testing.T) {
	queries := parseMixDeps([]byte(`defp deps do
  [
    {:plug, "~> 1.14"},
    {:jason, "== 1.4.0"}
  ]
end`))
	byName := map[string]PackageQuery{}
	for _, q := range queries {
		byName[q.Name] = q
	}
	require.Len(t, byName, 2)
	assert.True(t, byName["plug"].IsRange)
	assert.Equal(t, "~>1.14", byName["plug"].Constraint)
	assert.False(t, byName["jason"].IsRange)
}

func TestParseCsprojDeps_FloatingVersions(t *testing.T) {
	queries, err := parseCsprojDeps([]byte(`<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <PackageReference Include="Floating" Version="1.0.*" />
    <PackageReference Include="Bracket" Version="[2.0,3.0)" />
    <PackageReference Include="Plain" Version="4.0.0" />
  </ItemGroup>
</Project>`))
	require.NoError(t, err)
	byName := map[string]PackageQuery{}
	for _, q := range queries {
		byName[q.Name] = q
	}
	require.Len(t, byName, 3)
	assert.True(t, byName["Floating"].IsRange)
	assert.Equal(t, "1.0", byName["Floating"].Version)
	assert.True(t, byName["Bracket"].IsRange)
	assert.False(t, byName["Plain"].IsRange)
}

// --- reporting ---

// TestVulnCollector_RangeFloorTitle verifies a range finding is titled by
// its declared constraint, says the floor (not an installed version) is
// vulnerable, and is discounted after the dev adjustment.
func TestVulnCollector_RangeFloorTitle(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(`[project]
dependencies = ["click>=8.1.3", "pytest>=7.0"]
`), 0o600))

	critical := "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H" // 9.8
	high := "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H"     // 7.5
	c := &VulnCollector{osv: &mockOSVClient{results: []VulnDetail{
		{
			ID: "GHSA-click", Aliases: []string{"CVE-2026-7246"}, Summary: "click bug", Ecosystem: "PyPI",
			PackageName: "click", Version: "8.1.3", FixedVersion: "8.3.3", Severity: critical,
			IsRange: true, Constraint: ">=8.1.3",
		},
		{
			ID: "GHSA-pytest", Summary: "pytest bug", Ecosystem: "PyPI",
			PackageName: "pytest", Version: "7.0", Severity: high, Dev: true,
			IsRange: true, Constraint: ">=7.0",
		},
	}}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 2)

	byPkg := map[string]signal.RawSignal{"click": signals[0], "pytest": signals[1]}
	click, pytest := byPkg["click"], byPkg["pytest"]

	assert.Equal(t, "Vulnerable dependency floor: click>=8.1.3 allows CVE-2026-7246", click.Title)
	assert.Contains(t, click.Description, "The declared minimum 8.1.3 of click is vulnerable; the installed version is not known without a lockfile. Raise the floor to 8.3.3.")
	assert.InDelta(t, 0.57, click.Confidence, 0.001, "critical 0.95 × 0.6")
	assert.Contains(t, click.Tags, "version-floor")
	assert.Equal(t, "pyproject.toml", click.FilePath)

	assert.Equal(t, "Vulnerable dependency floor: pytest>=7.0 allows GHSA-pytest", pytest.Title)
	assert.Contains(t, pytest.Description, "No fixed version is available yet.")
	assert.InDelta(t, 0.39, pytest.Confidence, 0.001, "(high 0.85 − dev 0.2) × 0.6")

	metrics := c.Metrics().(*VulnMetrics)
	require.Len(t, metrics.Vulns, 2)
	assert.Equal(t, ">=8.1.3", metrics.Vulns[0].Constraint)
}

// TestVulnCollector_ExactPinUnchanged pins the pre-existing title, wording and
// confidence for an exact version so range handling is purely additive.
func TestVulnCollector_ExactPinUnchanged(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("click==8.1.3\n"), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{results: []VulnDetail{{
		ID: "GHSA-click", Aliases: []string{"CVE-2026-7246"}, Summary: "click bug", Ecosystem: "PyPI",
		PackageName: "click", Version: "8.1.3", FixedVersion: "8.3.3",
		Severity: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	}}}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "Vulnerable dependency: click@8.1.3 [CVE-2026-7246]", signals[0].Title)
	assert.Contains(t, signals[0].Description, "Upgrade click from 8.1.3 to 8.3.3.")
	assert.Equal(t, 0.95, signals[0].Confidence)
	assert.NotContains(t, signals[0].Tags, "version-floor")
}

// --- lockfiles ---

func TestParseCargoLock(t *testing.T) {
	queries, local, err := parseCargoLock([]byte(`version = 3

[[package]]
name = "my-crate"
version = "0.1.0"
dependencies = ["serde"]

[[package]]
name = "serde"
version = "1.0.200"
source = "registry+https://github.com/rust-lang/crates.io-index"

[[package]]
name = "serde"
version = "1.0.200"
source = "registry+https://github.com/rust-lang/crates.io-index"

[[package]]
name = "gitdep"
version = "0.3.0"
source = "git+https://github.com/x/gitdep#abc"
`))
	require.NoError(t, err)
	assert.True(t, local["my-crate"], "an entry without a source is a workspace crate")
	require.Len(t, queries, 2, "duplicates collapse: %v", queries)
	for _, q := range queries {
		assert.Equal(t, "crates.io", q.Ecosystem)
		assert.False(t, q.IsRange, "lockfile versions are exact")
	}

	_, _, err = parseCargoLock([]byte("[[package"))
	assert.Error(t, err)
}

func TestParseComposerLock(t *testing.T) {
	queries, err := parseComposerLock([]byte(`{
		"packages": [
			{"name": "symfony/routing", "version": "v7.4.2"},
			{"name": "vendor/branch", "version": "dev-main"}
		],
		"packages-dev": [{"name": "phpunit/phpunit", "version": "10.5.0"}]
	}`))
	require.NoError(t, err)
	require.Len(t, queries, 2, "dev-* versions are skipped")
	assert.Equal(t, "symfony/routing", queries[0].Name)
	assert.Equal(t, "7.4.2", queries[0].Version, "leading v is stripped")
	assert.False(t, queries[0].Dev)
	assert.Equal(t, "phpunit/phpunit", queries[1].Name)
	assert.True(t, queries[1].Dev)
	assert.False(t, queries[1].IsRange)

	_, err = parseComposerLock([]byte("{"))
	assert.Error(t, err)
}

// TestParseCargoQueries_PrefersLock verifies the resolved lockfile version
// replaces the manifest floor and is reported as exact.
func TestParseCargoQueries_PrefersLock(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[package]
name = "app"
version = "0.1.0"

[dependencies]
tokio = "1.2.0"
`), 0o600))

	file, queries := parseCargoQueries(dir)
	assert.Equal(t, "Cargo.toml", file)
	require.Len(t, queries, 1)
	assert.True(t, queries[0].IsRange)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.lock"), []byte(`[[package]]
name = "app"
version = "0.1.0"

[[package]]
name = "tokio"
version = "1.38.0"
source = "registry+https://github.com/rust-lang/crates.io-index"
`), 0o600))

	file, queries = parseCargoQueries(dir)
	assert.Equal(t, "Cargo.lock", file)
	require.Len(t, queries, 1)
	assert.Equal(t, "tokio", queries[0].Name)
	assert.Equal(t, "1.38.0", queries[0].Version)
	assert.False(t, queries[0].IsRange)

	// A malformed lockfile is non-fatal but yields nothing.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.lock"), []byte("[[package"), 0o600))
	_, queries = parseCargoQueries(dir)
	assert.Empty(t, queries)
}

func TestParseComposerQueries_PrefersLock(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{
		"require": {"symfony/routing": "^7.4.0 || ^8.0.0"}
	}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "composer.lock"), []byte(`{
		"packages": [{"name": "symfony/routing", "version": "v7.4.9"}]
	}`), 0o600))

	file, queries := parseComposerQueries(dir)
	assert.Equal(t, "composer.lock", file)
	require.Len(t, queries, 1)
	assert.Equal(t, "7.4.9", queries[0].Version)
	assert.False(t, queries[0].IsRange)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "composer.lock"), []byte("{"), 0o600))
	_, queries = parseComposerQueries(dir)
	assert.Empty(t, queries)
}

// --- workspaces ---

func TestExpandMemberDirs(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"crates/a", "crates/b", "tools/c", "crates/a/nested"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, d), 0o750))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "crates", "file.txt"), []byte("x"), 0o600))

	got := expandMemberDirs(dir, []string{"crates/*", "./tools/c/", "!crates/b", "", "deep/**/x"})
	assert.ElementsMatch(t, []string{
		filepath.Join(dir, "crates", "a"),
		filepath.Join(dir, "crates", "b"),
		filepath.Join(dir, "tools", "c"),
	}, got)
}

func TestParseCargoQueries_SkipsWorkspaceMembers(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tokio-test"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(`[workspace]
members = ["tokio-test", "missing"]

[package]
name = "tokio"
version = "1.0.0"

[dependencies]
tokio-test = "0.4.0"
bytes = "1.0"
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tokio-test", "Cargo.toml"), []byte(`[package]
name = "tokio-test"
version = "0.4.0"
`), 0o600))

	_, queries := parseCargoQueries(dir)
	require.Len(t, queries, 1)
	assert.Equal(t, "bytes", queries[0].Name)
}

func TestParseNpmQueries_SkipsWorkspaceMembers(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "packages", "ui"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "packages", "ui", "package.json"),
		[]byte(`{"name": "@acme/ui"}`), 0o600))

	for _, workspaces := range []string{`["packages/*"]`, `{"packages": ["packages/*"]}`} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{
			"workspaces": `+workspaces+`,
			"dependencies": {"@acme/ui": "^1.0.0", "minimist": "^1.2.3"}
		}`), 0o600))

		file, queries := parseNpmQueries(dir)
		assert.Equal(t, "package.json", file)
		require.Len(t, queries, 1, "workspaces=%s", workspaces)
		assert.Equal(t, "minimist", queries[0].Name)
	}

	// Unparseable workspaces field: nothing is dropped.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{
		"workspaces": 42,
		"dependencies": {"@acme/ui": "^1.0.0"}
	}`), 0o600))
	_, queries := parseNpmQueries(dir)
	assert.Len(t, queries, 1)
}

func TestParseGoModQueries_SkipsWorkMembersAndLocalReplaces(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "lib"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib", "go.mod"), []byte("module example.com/lib\n\ngo 1.22\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.work"), []byte("go 1.22\n\nuse (\n\t.\n\t./lib\n\t./absent\n)\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(`module example.com/app

go 1.22

require (
	example.com/lib v0.0.0
	example.com/sibling v1.0.0
	github.com/foo/bar v1.0.0
)

replace example.com/sibling => ../sibling
`), 0o600))

	queries, err := parseGoModQueries(dir)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "github.com/foo/bar", queries[0].Name)

	// A malformed go.work is ignored rather than fatal.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.work"), []byte("use (\n"), 0o600))
	queries, err = parseGoModQueries(dir)
	require.NoError(t, err)
	assert.Len(t, queries, 2)
}
