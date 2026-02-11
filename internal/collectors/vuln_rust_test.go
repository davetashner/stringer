package collectors

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCargoDeps_StringNotation(t *testing.T) {
	data := []byte(`[dependencies]
serde = "1.0.100"
`)
	queries, err := parseCargoDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "crates.io", queries[0].Ecosystem)
	assert.Equal(t, "serde", queries[0].Name)
	assert.Equal(t, "1.0.100", queries[0].Version)
}

func TestParseCargoDeps_TableNotation(t *testing.T) {
	data := []byte(`[dependencies]
serde = { version = "1.0" }
`)
	queries, err := parseCargoDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "serde", queries[0].Name)
	assert.Equal(t, "1.0", queries[0].Version)
}

func TestParseCargoDeps_MultipleDeps(t *testing.T) {
	data := []byte(`[dependencies]
serde = "1.0.100"
tokio = "1.28.0"
rand = "0.8.5"
`)
	queries, err := parseCargoDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 3)

	names := make(map[string]string)
	for _, q := range queries {
		names[q.Name] = q.Version
	}
	assert.Equal(t, "1.0.100", names["serde"])
	assert.Equal(t, "1.28.0", names["tokio"])
	assert.Equal(t, "0.8.5", names["rand"])
}

func TestParseCargoDeps_PathSkipped(t *testing.T) {
	data := []byte(`[dependencies]
mylib = { path = "../mylib" }
serde = "1.0"
`)
	queries, err := parseCargoDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "serde", queries[0].Name)
}

func TestParseCargoDeps_GitSkipped(t *testing.T) {
	data := []byte(`[dependencies]
mylib = { git = "https://github.com/example/mylib" }
serde = "1.0"
`)
	queries, err := parseCargoDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "serde", queries[0].Name)
}

func TestParseCargoDeps_NoVersion(t *testing.T) {
	data := []byte(`[dependencies]
mylib = { features = ["derive"] }
`)
	queries, err := parseCargoDeps(data)
	require.NoError(t, err)
	assert.Empty(t, queries)
}

func TestParseCargoDeps_DevDepsIgnored(t *testing.T) {
	data := []byte(`[dependencies]
serde = "1.0"

[dev-dependencies]
tempfile = "3.5"
`)
	queries, err := parseCargoDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "serde", queries[0].Name)
}

func TestParseCargoDeps_BuildDepsIgnored(t *testing.T) {
	data := []byte(`[dependencies]
serde = "1.0"

[build-dependencies]
cc = "1.0"
`)
	queries, err := parseCargoDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "serde", queries[0].Name)
}

func TestParseCargoDeps_EmptyDeps(t *testing.T) {
	data := []byte(`[dependencies]
`)
	queries, err := parseCargoDeps(data)
	require.NoError(t, err)
	assert.Nil(t, queries)
}

func TestParseCargoDeps_NoDepsSection(t *testing.T) {
	data := []byte(`[package]
name = "myapp"
version = "0.1.0"
`)
	queries, err := parseCargoDeps(data)
	require.NoError(t, err)
	assert.Nil(t, queries)
}

func TestParseCargoDeps_MalformedTOML(t *testing.T) {
	data := []byte(`[dependencies
serde = "broken
`)
	queries, err := parseCargoDeps(data)
	assert.Error(t, err)
	assert.Nil(t, queries)
}

func TestParseCargoDeps_MixedNotations(t *testing.T) {
	data := []byte(`[dependencies]
serde = "1.0.100"
tokio = { version = "1.28.0", features = ["full"] }
`)
	queries, err := parseCargoDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 2)

	names := make(map[string]string)
	for _, q := range queries {
		names[q.Name] = q.Version
	}
	assert.Equal(t, "1.0.100", names["serde"])
	assert.Equal(t, "1.28.0", names["tokio"])
}

func TestParseCargoDeps_RealWorld(t *testing.T) {
	data := []byte(`[package]
name = "my-app"
version = "0.1.0"
edition = "2021"

[dependencies]
serde = { version = "1.0.197", features = ["derive"] }
serde_json = "1.0.114"
tokio = { version = "1.36.0", features = ["full"] }
reqwest = { version = "0.11.24", features = ["json"] }
local-lib = { path = "../local-lib" }
git-dep = { git = "https://github.com/example/repo", branch = "main" }

[dev-dependencies]
tempfile = "3.10"
criterion = { version = "0.5", features = ["html_reports"] }

[build-dependencies]
cc = "1.0"
`)
	queries, err := parseCargoDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 4, "should have serde, serde_json, tokio, reqwest (skip path/git deps)")

	names := make(map[string]bool)
	for _, q := range queries {
		assert.Equal(t, "crates.io", q.Ecosystem)
		names[q.Name] = true
	}
	assert.True(t, names["serde"])
	assert.True(t, names["serde_json"])
	assert.True(t, names["tokio"])
	assert.True(t, names["reqwest"])
	assert.False(t, names["local-lib"], "path dep should be skipped")
	assert.False(t, names["git-dep"], "git dep should be skipped")
	assert.False(t, names["tempfile"], "dev dep should be skipped")
	assert.False(t, names["criterion"], "dev dep should be skipped")
	assert.False(t, names["cc"], "build dep should be skipped")
}
