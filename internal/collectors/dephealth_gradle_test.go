// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

func staleMavenClient(coords, group, artifact string) *mockMavenRegistryClient {
	staleTimestamp := time.Now().Add(-5 * 365 * 24 * time.Hour).UnixMilli()
	info := &mavenArtifactInfo{}
	info.Response.NumFound = 1
	info.Response.Docs = []mavenArtifact{{GroupID: group, ArtifactID: artifact, Version: "1.0.0", Timestamp: staleTimestamp}}
	return &mockMavenRegistryClient{results: map[string]*mavenArtifactInfo{coords: info}}
}

func TestDepHealthCollector_GradleOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "gradle"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gradle", "libs.versions.toml"), []byte(`[versions]
old = "1.0.0"
[libraries]
old-lib = { module = "com.old:stale-lib", version.ref = "old" }
fresh = "com.fresh:lib:2.0.0"
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle.kts"), []byte(`dependencies {
    implementation(libs.old.lib)
    implementation(libs.fresh)
    testImplementation("junit:junit:4.13.2")
}
`), 0o600))

	c := &DepHealthCollector{mavenClient: staleMavenClient("com.old:stale-lib", "com.old", "stale-lib")}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	require.Len(t, signals, 1)
	assert.Equal(t, "stale-dependency", signals[0].Kind)
	assert.Equal(t, "build.gradle.kts", signals[0].FilePath)
	assert.Equal(t, "Stale Maven artifact: com.old:stale-lib", signals[0].Title)
	assert.Contains(t, signals[0].Tags, "maven")

	require.NotNil(t, c.metrics)
	assert.Equal(t, []string{"gradle"}, c.metrics.Ecosystems)
	assert.Equal(t, []string{"Stale Maven artifact: com.old:stale-lib"}, c.metrics.Stale)
}

func TestDepHealthCollector_GradleSubprojectsAndGroovyCatalog(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "gradle"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "clients"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gradle", "dependencies.gradle"), []byte(`versions += [
  validator: "1.9.0"
]
libs += [
  commonsValidator: "commons-validator:commons-validator:$versions.validator"
]
`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.gradle"), []byte("include 'clients'\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle"), []byte("apply from: \"$rootDir/gradle/dependencies.gradle\"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clients", "build.gradle"), []byte("dependencies {\n  implementation libs.commonsValidator\n}\n"), 0o600))

	c := &DepHealthCollector{mavenClient: staleMavenClient("commons-validator:commons-validator", "commons-validator", "commons-validator")}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	require.Len(t, signals, 1)
	assert.Equal(t, "build.gradle", signals[0].FilePath, "signals are attributed to the root build file")
	assert.Equal(t, "Stale Maven artifact: commons-validator:commons-validator", signals[0].Title)
	assert.Equal(t, []string{"gradle"}, c.metrics.Ecosystems)
}

func TestDepHealthCollector_GradleWithoutDependencies(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle"), []byte("plugins { id 'java' }\n"), 0o600))

	c := &DepHealthCollector{mavenClient: &mockMavenRegistryClient{}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Nil(t, signals)
	assert.Nil(t, c.metrics, "a build file with no dependencies registers no ecosystem")
}

func TestDepHealthCollector_MavenAndGradle(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte(`<project><dependencies><dependency>
<groupId>com.old</groupId><artifactId>lib</artifactId><version>1.0.0</version>
</dependency></dependencies></project>`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle"), []byte("dependencies {\n  implementation 'com.old:lib:1.0.0'\n}\n"), 0o600))

	c := &DepHealthCollector{mavenClient: staleMavenClient("com.old:lib", "com.old", "lib")}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	assert.Equal(t, []string{"maven", "gradle"}, c.metrics.Ecosystems)
	require.Len(t, signals, 2)
	assert.Equal(t, "pom.xml", signals[0].FilePath)
	assert.Equal(t, "build.gradle", signals[1].FilePath)
}
