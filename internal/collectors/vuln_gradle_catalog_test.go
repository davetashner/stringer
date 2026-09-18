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

func TestParseTomlCatalog_AllLibraryForms(t *testing.T) {
	data := []byte(`[versions]
commons-validator = "1.9.0"
jackson = { strictly = "[2.15, 3.0[", prefer = "2.15.2" }
guava = { require = "32.1.2-jre" }
weird = 42

[libraries]
commons-validator = { module = "commons-validator:commons-validator", version.ref = "commons-validator" }
jackson-databind = { group = "com.fasterxml.jackson.core", name = "jackson-databind", version.ref = "jackson" }
guava = { group = "com.google.guava", name = "guava", version = "31.0-jre" }
slf4j = "org.slf4j:slf4j-api:2.0.9"
bom_managed = { module = "org.example:managed" }
rich = { module = "org.example:rich", version = { prefer = "1.2" } }
broken = { name = "no-group" }
missing-ref = { module = "org.example:x", version.ref = "nope" }
number = 7

[plugins]
shadow = { id = "com.github.johnrengelman.shadow", version = "8.1.1" }

[bundles]
all = ["guava", "slf4j"]
`)
	cat, err := parseTomlCatalog(data)
	require.NoError(t, err)

	assert.Equal(t, "1.9.0", cat.versions["commons.validator"])
	assert.Equal(t, "2.15.2", cat.versions["jackson"], "prefer wins over strictly")
	assert.Equal(t, "32.1.2-jre", cat.versions["guava"])
	assert.NotContains(t, cat.versions, "weird")

	assert.Equal(t, "commons-validator:commons-validator:1.9.0", cat.libs["commons.validator"])
	assert.Equal(t, "com.fasterxml.jackson.core:jackson-databind:2.15.2", cat.libs["jackson.databind"])
	assert.Equal(t, "com.google.guava:guava:31.0-jre", cat.libs["guava"])
	assert.Equal(t, "org.slf4j:slf4j-api:2.0.9", cat.libs["slf4j"])
	assert.Equal(t, "org.example:managed", cat.libs["bom.managed"], "BOM-managed entries keep no version")
	assert.Equal(t, "org.example:rich:1.2", cat.libs["rich"])
	assert.Equal(t, "org.example:x", cat.libs["missing.ref"], "dangling version.ref yields no version")
	assert.NotContains(t, cat.libs, "broken")
	assert.NotContains(t, cat.libs, "number")
	assert.Len(t, cat.libs, 7)
}

func TestParseTomlCatalog_Malformed(t *testing.T) {
	_, err := parseTomlCatalog([]byte("[versions\nfoo = "))
	assert.Error(t, err)
}

func TestParseGroovyCatalog_MapsAndInterpolation(t *testing.T) {
	data := []byte(`ext {
  versions = [:]
  libs = [:]
}

/* Kafka-style dependency tables */
versions += [
  activation: "1.1.1",
  checkstyle: project.hasProperty('checkstyleVersion') ? checkstyleVersion : "10.12.1",
  jackson: "2.16.0",
  // a comment inside the map
  scala: scalaVersion,
]
versions.slf4j = "1.7.36"
versions["log4j"] = '2.20.0'

libs += [
  activation: "javax.activation:activation:$versions.activation",
  jacksonDatabind: "com.fasterxml.jackson.core:jackson-databind:${versions.jackson}",
  scalaLibrary: "org.scala-lang:scala-library:$versions.scala",
  checkstyle: "com.puppycrawl.tools:checkstyle:$versions.checkstyle"
]
libs.slf4jApi = "org.slf4j:slf4j-api:$slf4j"
ext.libs.log4jCore = "org.apache.logging.log4j:log4j-core:${log4j}"
libs.noLiteral = someVariable
`)
	cat := newGradleCatalog()
	parseGroovyCatalog(data, cat)

	assert.Equal(t, map[string]string{
		"activation": "1.1.1",
		"checkstyle": "10.12.1",
		"jackson":    "2.16.0",
		"slf4j":      "1.7.36",
		"log4j":      "2.20.0",
	}, cat.versions)

	assert.Equal(t, map[string]string{
		"activation":      "javax.activation:activation:1.1.1",
		"jacksondatabind": "com.fasterxml.jackson.core:jackson-databind:2.16.0",
		"checkstyle":      "com.puppycrawl.tools:checkstyle:10.12.1",
		"slf4japi":        "org.slf4j:slf4j-api:1.7.36",
		"log4jcore":       "org.apache.logging.log4j:log4j-core:2.20.0",
	}, cat.libs, "unresolvable $versions.scala entry is dropped")
}

func TestGradleCatalogs_Resolve(t *testing.T) {
	cats := gradleCatalogs{"libs": {
		versions: map[string]string{"slf4j": "1.7.36"},
		libs:     map[string]string{"commons.validator": "a:b:1", "camelcase": "c:d:2"},
	}}

	assert.Equal(t, "a:b:1", cats.resolve("libs.commons.validator"))
	assert.Equal(t, "a:b:1", cats.resolve("libs.commons_validator"))
	assert.Equal(t, "c:d:2", cats.resolve("libs.camelCase"))
	assert.Equal(t, "", cats.resolve("other.commons.validator"), "unknown catalog")
	assert.Equal(t, "", cats.resolve("libs.versions.slf4j"))
	assert.Equal(t, "", cats.resolve("libs.bundles.all"))
	assert.Equal(t, "", cats.resolve("libs.plugins.shadow"))
	assert.Equal(t, "", cats.resolve("libs"))
	assert.Equal(t, "", cats.resolve("libs.nope"))

	assert.Equal(t, "g:a:1", cats.interpolate("g:a:1"))
	assert.Equal(t, "g:a:1.7.36", cats.interpolate("g:a:$versions.slf4j"))
	assert.Equal(t, "", cats.interpolate("g:a:$versions.missing"))
	assert.Equal(t, "", gradleCatalogs(nil).interpolate("g:a:$versions.slf4j"), "no catalog at all")
	assert.Equal(t, "", gradleCatalogs(nil).resolve("libs.x"))
}

func TestParseGradleDepsWithCatalogs(t *testing.T) {
	cats := gradleCatalogs{"libs": {
		versions: map[string]string{"slf4j": "1.7.36"},
		libs: map[string]string{
			"commons.validator": "commons-validator:commons-validator:1.9.0",
			"jackson.databind":  "com.fasterxml.jackson.core:jackson-databind:2.16.0",
			"bom":               "org.example:bom:1.0",
			"jose4j":            "org.bitbucket.b_c:jose4j:0.9.4",
			"a":                 "g:a:1", "b": "g:b:2", "c": "g:c:3",
			"junit": "org.junit:junit:5", "t1": "g:t1:1", "t2": "g:t2:2",
			"nover": "g:nover",
		},
	}}
	data := []byte(`dependencies {
    implementation libs.commons.validator
    implementation(libs.jackson.databind) {
        exclude group: 'x'
    }
    api platform(libs.bom)
    compileOnly libs.jose4j // for JWT validation
    runtimeOnly libs.a,
        libs.b, // trailing comment
        libs.c
    testImplementation libs.junit
    testRuntimeOnly libs.t1,
        libs.t2
    implementation libs.unknown
    implementation libs.bundles.all
    implementation libs.nover
    implementation "org.slf4j:slf4j-api:$versions.slf4j"
    implementation "org.example:unresolved:$versions.missing"
    implementation 'org.springframework:spring-core:5.3.0'
    implementation project(':core')
}
`)
	queries, err := parseGradleDepsWithCatalogs(data, cats)
	require.NoError(t, err)

	var got []string
	for _, q := range queries {
		got = append(got, q.Name+"@"+q.Version)
		assert.Equal(t, "Maven", q.Ecosystem)
	}
	assert.Equal(t, []string{
		"commons-validator:commons-validator@1.9.0",
		"com.fasterxml.jackson.core:jackson-databind@2.16.0",
		"org.example:bom@1.0",
		"org.bitbucket.b_c:jose4j@0.9.4",
		"g:a@1", "g:b@2", "g:c@3",
		"org.slf4j:slf4j-api@1.7.36",
		"org.springframework:spring-core@5.3.0",
	}, got)
}

func TestParseGradleDeps_DropsUnresolvedLiteralInterpolation(t *testing.T) {
	queries, err := parseGradleDeps([]byte(`classpath "org.ajoberstar.grgit:grgit-core:$versions.grgit"`))
	require.NoError(t, err)
	assert.Empty(t, queries, "a $-version is never a real version")
}

func TestLoadGradleCatalogs(t *testing.T) {
	dir := t.TempDir()
	gradleDir := filepath.Join(dir, "gradle")
	require.NoError(t, os.MkdirAll(filepath.Join(gradleDir, "wrapper"), 0o750))

	write := func(rel, content string) {
		require.NoError(t, os.WriteFile(filepath.Join(gradleDir, rel), []byte(content), 0o600))
	}
	write("libs.versions.toml", "[libraries]\nguava = \"com.google.guava:guava:32.0.0-jre\"\n")
	write("deps.versions.toml", "[libraries]\nslf4j = \"org.slf4j:slf4j-api:2.0.9\"\n")
	write("bad.versions.toml", "[libraries\n")
	write("dependencies.gradle", "versions += [\n  jackson: \"2.16.0\"\n]\nlibs += [\n  jackson: \"com.fasterxml.jackson.core:jackson-databind:$versions.jackson\"\n]\n")
	write("wrapper/skipped.gradle", "libs.skipped = \"g:skipped:1\"\n")

	cats := loadGradleCatalogs(dir)
	assert.Equal(t, "com.google.guava:guava:32.0.0-jre", cats.resolve("libs.guava"))
	assert.Equal(t, "com.fasterxml.jackson.core:jackson-databind:2.16.0", cats.resolve("libs.jackson"), "Groovy maps layer onto the libs catalog")
	assert.Equal(t, "org.slf4j:slf4j-api:2.0.9", cats.resolve("deps.slf4j"), "named catalogs use their file prefix")
	assert.NotContains(t, cats, "bad")
	assert.Equal(t, "", cats.resolve("libs.skipped"), "nested directories are not scanned")

	assert.Empty(t, loadGradleCatalogs(t.TempDir()), "no gradle/ dir")
}

func TestGradleSubprojectDirs(t *testing.T) {
	dir := t.TempDir()
	settings := `rootProject.name = 'demo'
include 'clients',
    'connect:api',
    ':streams'
include(":app", ":lib:core")
includeBuild 'other'
project(':streams').projectDir = file('streams-dir')
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.gradle"), []byte(settings), 0o600))
	assert.Equal(t, []string{"clients", "connect/api", "streams", "app", "lib/core"}, gradleSubprojectDirs(dir))

	kts := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(kts, "settings.gradle.kts"), []byte("include(\":app\")\ninclude(\":app\")\n"), 0o600))
	assert.Equal(t, []string{"app"}, gradleSubprojectDirs(kts))

	assert.Nil(t, gradleSubprojectDirs(t.TempDir()))
}

func TestFindGradleBuildFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.gradle"), []byte("include 'clients', 'connect:api', 'missing'\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle"), []byte(""), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "clients"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clients", "build.gradle"), []byte(""), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "connect", "api"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "connect", "api", "build.gradle.kts"), []byte(""), 0o600))

	assert.Equal(t, []string{"build.gradle", "clients/build.gradle", "connect/api/build.gradle.kts"}, findGradleBuildFiles(dir))

	assert.Nil(t, findGradleBuildFiles(t.TempDir()))
}

func TestParseGradleQueries_CatalogAndSubprojects(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "gradle"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "app"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gradle", "libs.versions.toml"), []byte(`[versions]
validator = "1.9.0"
[libraries]
commons-validator = { module = "commons-validator:commons-validator", version.ref = "validator" }
slf4j = "org.slf4j:slf4j-api:2.0.9"
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.gradle"), []byte("include ':app'\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle"), []byte(`ext {
  versions = [:]
}
versions += [
  guava: "32.0.0-jre"
]
dependencies {
  implementation "com.google.guava:guava:$versions.guava"
  implementation libs.commons.validator
}
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app", "build.gradle.kts"), []byte(`dependencies {
  implementation(libs.commons.validator)
  implementation(libs.slf4j)
}
`), 0o600))

	file, queries := parseGradleQueries(dir)
	assert.Equal(t, "build.gradle", file)

	var got []string
	for _, q := range queries {
		got = append(got, q.Name+"@"+q.Version)
	}
	assert.Equal(t, []string{
		"com.google.guava:guava@32.0.0-jre",
		"commons-validator:commons-validator@1.9.0",
		"org.slf4j:slf4j-api@2.0.9",
	}, got, "root ext maps resolve, catalog refs resolve, duplicates collapse")
}

func TestVulnCollector_GradleCatalog(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "gradle"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gradle", "libs.versions.toml"), []byte(`[libraries]
commons-validator = "commons-validator:commons-validator:1.9.0"
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle.kts"), []byte("dependencies {\n    implementation(libs.commons.validator)\n}\n"), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{results: []VulnDetail{{
		ID: "GHSA-xxxx", Aliases: []string{"CVE-2025-0001"}, Summary: "bad",
		Ecosystem: "Maven", PackageName: "commons-validator:commons-validator", Version: "1.9.0", FixedVersion: "1.10.0",
	}}}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "build.gradle.kts", signals[0].FilePath)
	assert.Contains(t, signals[0].Title, "commons-validator:commons-validator@1.9.0")
	assert.Contains(t, signals[0].Tags, "java")
}
