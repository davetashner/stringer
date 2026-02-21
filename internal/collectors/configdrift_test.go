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

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

func TestConfigDrift_Registration(t *testing.T) {
	c := collector.Get("configdrift")
	require.NotNil(t, c)
	assert.Equal(t, "configdrift", c.Name())
}

func TestConfigDrift_EnvVarDrift(t *testing.T) {
	dir := initConfigDriftRepo(t)

	// .env.example is missing SECRET_KEY.
	writeFile(t, dir, ".env.example", "DB_HOST=localhost\n")
	writeFile(t, dir, "main.go", `package main

import "os"

func main() {
	_ = os.Getenv("SECRET_KEY")
	_ = os.Getenv("DB_HOST")
}
`)
	gitCommit(t, dir, "add files")

	c := &ConfigDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	drift := filterByKind(signals, "env-var-drift")
	require.Len(t, drift, 1)
	assert.Contains(t, drift[0].Title, "SECRET_KEY")
	assert.Equal(t, "configdrift", drift[0].Source)
	assert.Equal(t, 0.5, drift[0].Confidence)
}

func TestConfigDrift_NoTemplate(t *testing.T) {
	dir := initConfigDriftRepo(t)

	// No .env.example → no env-var-drift signals.
	writeFile(t, dir, "main.go", `package main

import "os"

func main() {
	_ = os.Getenv("SECRET_KEY")
}
`)
	gitCommit(t, dir, "add go file")

	c := &ConfigDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	drift := filterByKind(signals, "env-var-drift")
	assert.Empty(t, drift, "no template means no env-var-drift signals")
}

func TestConfigDrift_DeadConfigKey(t *testing.T) {
	dir := initConfigDriftRepo(t)

	writeFile(t, dir, ".env.example", "OLD_KEY=somevalue\nDB_HOST=localhost\n")
	writeFile(t, dir, "main.go", `package main

import "os"

func main() {
	_ = os.Getenv("DB_HOST")
}
`)
	gitCommit(t, dir, "add files")

	c := &ConfigDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	dead := filterByKind(signals, "dead-config-key")
	require.NotEmpty(t, dead)
	found := false
	for _, s := range dead {
		if assert.ObjectsAreEqual("OLD_KEY", "") || true {
			if contains(s.Title, "OLD_KEY") {
				found = true
			}
		}
	}
	assert.True(t, found, "expected dead-config-key signal for OLD_KEY")
}

func TestConfigDrift_LiveConfigKey(t *testing.T) {
	dir := initConfigDriftRepo(t)

	writeFile(t, dir, ".env.example", "DB_HOST=localhost\n")
	writeFile(t, dir, "main.go", `package main

import "os"

func main() {
	_ = os.Getenv("DB_HOST")
}
`)
	gitCommit(t, dir, "add files")

	c := &ConfigDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	dead := filterByKind(signals, "dead-config-key")
	assert.Empty(t, dead, "DB_HOST is referenced in source, should not be dead")
}

func TestConfigDrift_InconsistentDefaults(t *testing.T) {
	dir := initConfigDriftRepo(t)

	writeFile(t, dir, ".env.example", "DB_PORT=5432\n")
	writeFile(t, dir, ".env.dev", "DB_PORT=3306\n")
	gitCommit(t, dir, "add env files")

	c := &ConfigDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	inconsistent := filterByKind(signals, "inconsistent-defaults")
	require.NotEmpty(t, inconsistent)
	assert.Contains(t, inconsistent[0].Title, "DB_PORT")
}

func TestConfigDrift_ConsistentDefaults(t *testing.T) {
	dir := initConfigDriftRepo(t)

	writeFile(t, dir, ".env.example", "DB_PORT=5432\n")
	writeFile(t, dir, ".env.dev", "DB_PORT=5432\n")
	gitCommit(t, dir, "add env files")

	c := &ConfigDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	inconsistent := filterByKind(signals, "inconsistent-defaults")
	assert.Empty(t, inconsistent, "same values should not trigger inconsistent signal")
}

func TestConfigDrift_WellKnownVarsSkipped(t *testing.T) {
	dir := initConfigDriftRepo(t)

	writeFile(t, dir, ".env.example", "APP_KEY=value\n")
	writeFile(t, dir, "main.go", `package main

import "os"

func main() {
	_ = os.Getenv("HOME")
	_ = os.Getenv("PATH")
	_ = os.Getenv("GITHUB_TOKEN")
	_ = os.Getenv("NODE_ENV")
	_ = os.Getenv("CI")
}
`)
	gitCommit(t, dir, "add files")

	c := &ConfigDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	drift := filterByKind(signals, "env-var-drift")
	for _, s := range drift {
		assert.NotContains(t, s.Title, "HOME")
		assert.NotContains(t, s.Title, "PATH")
		assert.NotContains(t, s.Title, "GITHUB_TOKEN")
		assert.NotContains(t, s.Title, "NODE_ENV")
		assert.NotContains(t, s.Title, "CI")
	}
}

func TestConfigDrift_NodeEnvVars(t *testing.T) {
	dir := initConfigDriftRepo(t)

	writeFile(t, dir, ".env.example", "OTHER=x\n")
	writeFile(t, dir, "app.js", `const key = process.env.API_KEY;
const host = process.env["DB_HOST"];
const port = process.env['DB_PORT'];
`)
	gitCommit(t, dir, "add files")

	c := &ConfigDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	drift := filterByKind(signals, "env-var-drift")
	titles := make(map[string]bool)
	for _, s := range drift {
		titles[s.Title] = true
	}
	assert.True(t, containsKey(titles, "API_KEY"), "expected drift signal for API_KEY")
	assert.True(t, containsKey(titles, "DB_HOST"), "expected drift signal for DB_HOST")
	assert.True(t, containsKey(titles, "DB_PORT"), "expected drift signal for DB_PORT")
}

func TestConfigDrift_PythonEnvVars(t *testing.T) {
	dir := initConfigDriftRepo(t)

	writeFile(t, dir, ".env.example", "OTHER=x\n")
	writeFile(t, dir, "app.py", `import os
key = os.getenv("API_KEY")
host = os.environ["DB_HOST"]
port = os.environ.get("DB_PORT")
`)
	gitCommit(t, dir, "add files")

	c := &ConfigDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	drift := filterByKind(signals, "env-var-drift")
	titles := make(map[string]bool)
	for _, s := range drift {
		titles[s.Title] = true
	}
	assert.True(t, containsKey(titles, "API_KEY"), "expected drift signal for API_KEY")
	assert.True(t, containsKey(titles, "DB_HOST"), "expected drift signal for DB_HOST")
	assert.True(t, containsKey(titles, "DB_PORT"), "expected drift signal for DB_PORT")
}

func TestConfigDrift_RubyEnvVars(t *testing.T) {
	dir := initConfigDriftRepo(t)

	writeFile(t, dir, ".env.example", "OTHER=x\n")
	writeFile(t, dir, "app.rb", `key = ENV["API_KEY"]
host = ENV.fetch("DB_HOST")
`)
	gitCommit(t, dir, "add files")

	c := &ConfigDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	drift := filterByKind(signals, "env-var-drift")
	titles := make(map[string]bool)
	for _, s := range drift {
		titles[s.Title] = true
	}
	assert.True(t, containsKey(titles, "API_KEY"), "expected drift signal for API_KEY")
	assert.True(t, containsKey(titles, "DB_HOST"), "expected drift signal for DB_HOST")
}

func TestConfigDrift_Metrics(t *testing.T) {
	dir := initConfigDriftRepo(t)

	writeFile(t, dir, ".env.example", "DB_HOST=localhost\nOLD_KEY=val\n")
	writeFile(t, dir, ".env.dev", "DB_HOST=remotehost\n")
	writeFile(t, dir, "main.go", `package main

import "os"

func main() {
	_ = os.Getenv("DB_HOST")
	_ = os.Getenv("SECRET")
}
`)
	gitCommit(t, dir, "add files")

	c := &ConfigDriftCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	m := c.Metrics()
	require.NotNil(t, m)
	metrics, ok := m.(*ConfigDriftMetrics)
	require.True(t, ok)

	assert.GreaterOrEqual(t, metrics.EnvTemplatesFound, 1)
	assert.GreaterOrEqual(t, metrics.EnvVarsInCode, 1)
	assert.GreaterOrEqual(t, metrics.EnvVarsInTemplates, 1)
}

func TestConfigDrift_PlaceholderValuesIgnored(t *testing.T) {
	dir := initConfigDriftRepo(t)

	writeFile(t, dir, ".env.example", "API_KEY=changeme\n")
	writeFile(t, dir, ".env.dev", "API_KEY=real-key-123\n")
	gitCommit(t, dir, "add env files")

	c := &ConfigDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{GitRoot: dir})
	require.NoError(t, err)

	// changeme is a placeholder, so .env.example's value is skipped.
	// Only one file has a non-placeholder value, so no inconsistency.
	inconsistent := filterByKind(signals, "inconsistent-defaults")
	assert.Empty(t, inconsistent, "placeholder values should be ignored for inconsistency check")
}

func TestConfigDrift_ContextCancellation(t *testing.T) {
	dir := initConfigDriftRepo(t)

	writeFile(t, dir, ".env.example", "KEY=val\n")
	writeFile(t, dir, "main.go", "package main\n")
	gitCommit(t, dir, "add files")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &ConfigDriftCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{GitRoot: dir})
	assert.Error(t, err)
}

func TestConfigDrift_MinConfidenceFilter(t *testing.T) {
	dir := initConfigDriftRepo(t)

	writeFile(t, dir, ".env.example", "OLD_KEY=val\n")
	writeFile(t, dir, ".env.dev", "DB_PORT=3306\n")
	writeFile(t, dir, ".env.example", "DB_PORT=5432\nOLD_KEY=val\n")
	writeFile(t, dir, "main.go", `package main

import "os"

func main() {
	_ = os.Getenv("SECRET")
}
`)
	gitCommit(t, dir, "add files")

	c := &ConfigDriftCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		GitRoot:       dir,
		MinConfidence: 0.9,
	})
	require.NoError(t, err)

	// All config drift signals have confidence <= 0.5, so they should all be filtered.
	assert.Empty(t, signals, "all signals should be filtered at min confidence 0.9")
}

// Test helpers.

// initConfigDriftRepo creates a temporary git repo with an initial commit.
func initConfigDriftRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runDocGit(t, dir, "init")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0o600))
	runDocGit(t, dir, "add", ".")
	runDocGit(t, dir, "-c", "user.name=Test", "-c", "user.email=test@test.com", "commit", "-m", "init")
	return dir
}

// writeFile creates a file with the given content, creating parent dirs as needed.
func writeFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	absPath := filepath.Join(dir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(absPath), 0o750))
	require.NoError(t, os.WriteFile(absPath, []byte(content), 0o600))
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

// containsStr is a simple string contains check.
func containsStr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// containsKey checks if any key in a title map contains the given substring.
func containsKey(titles map[string]bool, key string) bool {
	for title := range titles {
		if containsStr(title, key) {
			return true
		}
	}
	return false
}
