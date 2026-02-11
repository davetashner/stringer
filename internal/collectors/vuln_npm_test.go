package collectors

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNpmDeps_Basic(t *testing.T) {
	data := []byte(`{
		"dependencies": {
			"express": "4.18.2",
			"lodash": "4.17.21"
		}
	}`)
	queries, err := parseNpmDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 2)

	names := make(map[string]string)
	for _, q := range queries {
		assert.Equal(t, "npm", q.Ecosystem)
		names[q.Name] = q.Version
	}
	assert.Equal(t, "4.18.2", names["express"])
	assert.Equal(t, "4.17.21", names["lodash"])
}

func TestParseNpmDeps_CaretPrefix(t *testing.T) {
	data := []byte(`{
		"dependencies": {
			"react": "^18.2.0"
		}
	}`)
	queries, err := parseNpmDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "react", queries[0].Name)
	assert.Equal(t, "18.2.0", queries[0].Version)
}

func TestParseNpmDeps_TildePrefix(t *testing.T) {
	data := []byte(`{
		"dependencies": {
			"debug": "~4.3.4"
		}
	}`)
	queries, err := parseNpmDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "debug", queries[0].Name)
	assert.Equal(t, "4.3.4", queries[0].Version)
}

func TestParseNpmDeps_GtePrefix(t *testing.T) {
	data := []byte(`{
		"dependencies": {
			"node-fetch": ">=2.6.7"
		}
	}`)
	queries, err := parseNpmDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "node-fetch", queries[0].Name)
	assert.Equal(t, "2.6.7", queries[0].Version)
}

func TestParseNpmDeps_DevDependencies(t *testing.T) {
	data := []byte(`{
		"dependencies": {
			"express": "4.18.2"
		},
		"devDependencies": {
			"jest": "^29.7.0",
			"typescript": "~5.3.3"
		}
	}`)
	queries, err := parseNpmDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 3)

	names := make(map[string]string)
	for _, q := range queries {
		names[q.Name] = q.Version
	}
	assert.Equal(t, "4.18.2", names["express"])
	assert.Equal(t, "29.7.0", names["jest"])
	assert.Equal(t, "5.3.3", names["typescript"])
}

func TestParseNpmDeps_DuplicateDedup(t *testing.T) {
	// Same package in both deps and devDeps — should appear once.
	data := []byte(`{
		"dependencies": {
			"lodash": "^4.17.21"
		},
		"devDependencies": {
			"lodash": "^4.17.21"
		}
	}`)
	queries, err := parseNpmDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "lodash", queries[0].Name)
}

func TestParseNpmDeps_WildcardSkipped(t *testing.T) {
	data := []byte(`{
		"dependencies": {
			"express": "4.18.2",
			"custom-lib": "*"
		}
	}`)
	queries, err := parseNpmDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "express", queries[0].Name)
}

func TestParseNpmDeps_LatestSkipped(t *testing.T) {
	data := []byte(`{
		"dependencies": {
			"express": "4.18.2",
			"some-pkg": "latest"
		}
	}`)
	queries, err := parseNpmDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "express", queries[0].Name)
}

func TestParseNpmDeps_URLSkipped(t *testing.T) {
	data := []byte(`{
		"dependencies": {
			"express": "4.18.2",
			"my-fork": "git+https://github.com/user/repo.git",
			"local-pkg": "file:../local-pkg"
		}
	}`)
	queries, err := parseNpmDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "express", queries[0].Name)
}

func TestParseNpmDeps_WorkspaceSkipped(t *testing.T) {
	data := []byte(`{
		"dependencies": {
			"express": "4.18.2",
			"shared-utils": "workspace:*"
		}
	}`)
	queries, err := parseNpmDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "express", queries[0].Name)
}

func TestParseNpmDeps_RangeExpression(t *testing.T) {
	data := []byte(`{
		"dependencies": {
			"semver": ">=7.0.0 <8.0.0"
		}
	}`)
	queries, err := parseNpmDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "semver", queries[0].Name)
	assert.Equal(t, "7.0.0", queries[0].Version)
}

func TestParseNpmDeps_OrRange(t *testing.T) {
	data := []byte(`{
		"dependencies": {
			"graceful-fs": "^4.2.0 || ^3.0.0"
		}
	}`)
	queries, err := parseNpmDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "graceful-fs", queries[0].Name)
	assert.Equal(t, "4.2.0", queries[0].Version)
}

func TestParseNpmDeps_NoDeps(t *testing.T) {
	data := []byte(`{
		"name": "myapp",
		"version": "1.0.0"
	}`)
	queries, err := parseNpmDeps(data)
	require.NoError(t, err)
	assert.Empty(t, queries)
}

func TestParseNpmDeps_EmptyDeps(t *testing.T) {
	data := []byte(`{
		"dependencies": {},
		"devDependencies": {}
	}`)
	queries, err := parseNpmDeps(data)
	require.NoError(t, err)
	assert.Empty(t, queries)
}

func TestParseNpmDeps_MalformedJSON(t *testing.T) {
	data := []byte(`{"dependencies": {broken`)
	queries, err := parseNpmDeps(data)
	assert.Error(t, err)
	assert.Nil(t, queries)
}

func TestParseNpmDeps_RealWorld(t *testing.T) {
	data := []byte(`{
		"name": "my-web-app",
		"version": "2.0.0",
		"dependencies": {
			"express": "^4.18.2",
			"mongoose": "~7.6.3",
			"dotenv": "16.3.1",
			"jsonwebtoken": ">=9.0.0",
			"internal-lib": "file:../internal-lib",
			"forked-pkg": "git+https://github.com/user/repo.git#v1.0.0",
			"any-version": "*"
		},
		"devDependencies": {
			"jest": "^29.7.0",
			"eslint": "~8.56.0",
			"nodemon": "latest",
			"shared-config": "workspace:*"
		}
	}`)
	queries, err := parseNpmDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 6, "should skip file:, git+, *, latest, workspace: deps")

	names := make(map[string]bool)
	for _, q := range queries {
		assert.Equal(t, "npm", q.Ecosystem)
		names[q.Name] = true
	}
	assert.True(t, names["express"])
	assert.True(t, names["mongoose"])
	assert.True(t, names["dotenv"])
	assert.True(t, names["jsonwebtoken"])
	assert.True(t, names["jest"])
	assert.True(t, names["eslint"])
	assert.False(t, names["internal-lib"], "file: dep should be skipped")
	assert.False(t, names["forked-pkg"], "git+ dep should be skipped")
	assert.False(t, names["any-version"], "wildcard dep should be skipped")
	assert.False(t, names["nodemon"], "latest dep should be skipped")
	assert.False(t, names["shared-config"], "workspace: dep should be skipped")
}

// --- extractNpmVersion tests ---

func TestExtractNpmVersion(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"exact", "4.18.2", "4.18.2"},
		{"caret", "^18.2.0", "18.2.0"},
		{"tilde", "~4.3.4", "4.3.4"},
		{"gte", ">=2.6.7", "2.6.7"},
		{"lte", "<=1.0.0", "1.0.0"},
		{"gt", ">3.0.0", "3.0.0"},
		{"lt", "<5.0.0", "5.0.0"},
		{"wildcard", "*", ""},
		{"latest", "latest", ""},
		{"next", "next", ""},
		{"empty", "", ""},
		{"git url", "git+https://github.com/user/repo.git", ""},
		{"file ref", "file:../local", ""},
		{"link ref", "link:../other", ""},
		{"workspace", "workspace:*", ""},
		{"range with space", ">=1.0.0 <2.0.0", "1.0.0"},
		{"or range", "^4.2.0 || ^3.0.0", "4.2.0"},
		{"tag name", "beta", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractNpmVersion(tt.input))
		})
	}
}
