package collectors

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- parsePythonRequirements tests ---

func TestParsePythonRequirements_PinnedVersions(t *testing.T) {
	data := []byte("requests==2.31.0\nflask==3.0.0\n")
	queries, err := parsePythonRequirements(data)
	require.NoError(t, err)
	require.Len(t, queries, 2)

	names := make(map[string]string)
	for _, q := range queries {
		assert.Equal(t, "PyPI", q.Ecosystem)
		names[q.Name] = q.Version
	}
	assert.Equal(t, "2.31.0", names["requests"])
	assert.Equal(t, "3.0.0", names["flask"])
}

func TestParsePythonRequirements_CompatibleRelease(t *testing.T) {
	data := []byte("django~=4.2.0\n")
	queries, err := parsePythonRequirements(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "django", queries[0].Name)
	assert.Equal(t, "4.2.0", queries[0].Version)
}

func TestParsePythonRequirements_MinimumVersion(t *testing.T) {
	data := []byte("numpy>=1.24.0\n")
	queries, err := parsePythonRequirements(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "numpy", queries[0].Name)
	assert.Equal(t, "1.24.0", queries[0].Version)
}

func TestParsePythonRequirements_MultiConstraint(t *testing.T) {
	data := []byte("urllib3>=1.26.0,<2.0.0\n")
	queries, err := parsePythonRequirements(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "urllib3", queries[0].Name)
	assert.Equal(t, "1.26.0", queries[0].Version)
}

func TestParsePythonRequirements_Extras(t *testing.T) {
	data := []byte("requests[security]==2.31.0\n")
	queries, err := parsePythonRequirements(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "requests", queries[0].Name)
	assert.Equal(t, "2.31.0", queries[0].Version)
}

func TestParsePythonRequirements_Comments(t *testing.T) {
	data := []byte(`# This is a comment
requests==2.31.0
# Another comment
flask==3.0.0
`)
	queries, err := parsePythonRequirements(data)
	require.NoError(t, err)
	require.Len(t, queries, 2)
}

func TestParsePythonRequirements_InlineComments(t *testing.T) {
	data := []byte("requests==2.31.0 # HTTP library\n")
	queries, err := parsePythonRequirements(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "requests", queries[0].Name)
	assert.Equal(t, "2.31.0", queries[0].Version)
}

func TestParsePythonRequirements_BlankLines(t *testing.T) {
	data := []byte("\nrequests==2.31.0\n\nflask==3.0.0\n\n")
	queries, err := parsePythonRequirements(data)
	require.NoError(t, err)
	require.Len(t, queries, 2)
}

func TestParsePythonRequirements_EditableSkipped(t *testing.T) {
	data := []byte(`requests==2.31.0
-e git+https://github.com/example/repo.git#egg=example
-e .
flask==3.0.0
`)
	queries, err := parsePythonRequirements(data)
	require.NoError(t, err)
	require.Len(t, queries, 2)
}

func TestParsePythonRequirements_OptionsSkipped(t *testing.T) {
	data := []byte(`--index-url https://pypi.org/simple
--extra-index-url https://private.pypi.org
-r other-requirements.txt
-f https://download.pytorch.org/whl/torch_stable.html
requests==2.31.0
`)
	queries, err := parsePythonRequirements(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "requests", queries[0].Name)
}

func TestParsePythonRequirements_URLSkipped(t *testing.T) {
	data := []byte(`requests==2.31.0
https://example.com/mypackage-1.0.tar.gz
flask==3.0.0
`)
	queries, err := parsePythonRequirements(data)
	require.NoError(t, err)
	require.Len(t, queries, 2)
}

func TestParsePythonRequirements_NoVersion(t *testing.T) {
	data := []byte("requests\nflask\n")
	queries, err := parsePythonRequirements(data)
	require.NoError(t, err)
	assert.Empty(t, queries)
}

func TestParsePythonRequirements_EnvironmentMarkers(t *testing.T) {
	data := []byte("pywin32==306; sys_platform == 'win32'\nrequests==2.31.0\n")
	queries, err := parsePythonRequirements(data)
	require.NoError(t, err)
	require.Len(t, queries, 2)

	names := make(map[string]string)
	for _, q := range queries {
		names[q.Name] = q.Version
	}
	assert.Equal(t, "306", names["pywin32"])
	assert.Equal(t, "2.31.0", names["requests"])
}

func TestParsePythonRequirements_Empty(t *testing.T) {
	data := []byte("")
	queries, err := parsePythonRequirements(data)
	require.NoError(t, err)
	assert.Empty(t, queries)
}

func TestParsePythonRequirements_RealWorld(t *testing.T) {
	data := []byte(`# Production dependencies
Django==4.2.11
djangorestframework>=3.14.0
celery[redis]~=5.3.0
psycopg2-binary>=2.9,<3.0
gunicorn==21.2.0

# Skip these
-e git+https://github.com/example/internal-lib.git#egg=internal-lib
--index-url https://pypi.org/simple
-r dev-requirements.txt

# Platform-specific
pywin32==306; sys_platform == 'win32'
`)
	queries, err := parsePythonRequirements(data)
	require.NoError(t, err)
	require.Len(t, queries, 6)

	names := make(map[string]bool)
	for _, q := range queries {
		assert.Equal(t, "PyPI", q.Ecosystem)
		names[q.Name] = true
	}
	assert.True(t, names["Django"])
	assert.True(t, names["djangorestframework"])
	assert.True(t, names["celery"])
	assert.True(t, names["psycopg2-binary"])
	assert.True(t, names["gunicorn"])
	assert.True(t, names["pywin32"])
}

// --- parsePyprojectDeps tests ---

func TestParsePyprojectDeps_Basic(t *testing.T) {
	data := []byte(`[project]
name = "myapp"
version = "1.0.0"
dependencies = [
    "requests>=2.31.0",
    "flask==3.0.0",
]
`)
	queries, err := parsePyprojectDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 2)

	names := make(map[string]string)
	for _, q := range queries {
		assert.Equal(t, "PyPI", q.Ecosystem)
		names[q.Name] = q.Version
	}
	assert.Equal(t, "2.31.0", names["requests"])
	assert.Equal(t, "3.0.0", names["flask"])
}

func TestParsePyprojectDeps_NoDeps(t *testing.T) {
	data := []byte(`[project]
name = "myapp"
version = "1.0.0"
`)
	queries, err := parsePyprojectDeps(data)
	require.NoError(t, err)
	assert.Nil(t, queries)
}

func TestParsePyprojectDeps_EmptyDeps(t *testing.T) {
	data := []byte(`[project]
name = "myapp"
dependencies = []
`)
	queries, err := parsePyprojectDeps(data)
	require.NoError(t, err)
	assert.Nil(t, queries)
}

func TestParsePyprojectDeps_WithExtras(t *testing.T) {
	data := []byte(`[project]
name = "myapp"
dependencies = [
    "celery[redis]~=5.3.0",
]
`)
	queries, err := parsePyprojectDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 1)
	assert.Equal(t, "celery", queries[0].Name)
	assert.Equal(t, "5.3.0", queries[0].Version)
}

func TestParsePyprojectDeps_WithMarkers(t *testing.T) {
	data := []byte(`[project]
name = "myapp"
dependencies = [
    "pywin32==306; sys_platform == 'win32'",
    "requests>=2.31.0",
]
`)
	queries, err := parsePyprojectDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 2)
}

func TestParsePyprojectDeps_NoVersion(t *testing.T) {
	data := []byte(`[project]
name = "myapp"
dependencies = [
    "requests",
    "flask",
]
`)
	queries, err := parsePyprojectDeps(data)
	require.NoError(t, err)
	assert.Empty(t, queries)
}

func TestParsePyprojectDeps_MalformedTOML(t *testing.T) {
	data := []byte(`[project
broken toml
`)
	queries, err := parsePyprojectDeps(data)
	assert.Error(t, err)
	assert.Nil(t, queries)
}

func TestParsePyprojectDeps_RealWorld(t *testing.T) {
	data := []byte(`[build-system]
requires = ["setuptools>=68.0", "wheel"]
build-backend = "setuptools.backends._legacy:_Backend"

[project]
name = "my-web-app"
version = "2.0.0"
requires-python = ">=3.10"
dependencies = [
    "Django>=4.2,<5.0",
    "djangorestframework~=3.14.0",
    "celery[redis]==5.3.6",
    "psycopg2-binary>=2.9.9",
    "gunicorn==21.2.0",
]

[project.optional-dependencies]
dev = [
    "pytest>=7.0",
    "ruff>=0.1.0",
]
`)
	queries, err := parsePyprojectDeps(data)
	require.NoError(t, err)
	require.Len(t, queries, 5, "should parse only [project].dependencies, not optional-dependencies")

	names := make(map[string]bool)
	for _, q := range queries {
		assert.Equal(t, "PyPI", q.Ecosystem)
		names[q.Name] = true
	}
	assert.True(t, names["Django"])
	assert.True(t, names["djangorestframework"])
	assert.True(t, names["celery"])
	assert.True(t, names["psycopg2-binary"])
	assert.True(t, names["gunicorn"])
	assert.False(t, names["pytest"], "optional dep should not be included")
	assert.False(t, names["ruff"], "optional dep should not be included")
}

// --- parseRequirementLine tests ---

func TestParseRequirementLine_AllOperators(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantPkg string
		wantVer string
	}{
		{"pinned ==", "requests==2.31.0", "requests", "2.31.0"},
		{"compatible ~=", "django~=4.2.0", "django", "4.2.0"},
		{"minimum >=", "numpy>=1.24.0", "numpy", "1.24.0"},
		{"maximum <=", "flask<=2.3.0", "flask", "2.3.0"},
		{"not equal !=", "six!=1.0", "six", "1.0"},
		{"greater >", "pip>21.0", "pip", "21.0"},
		{"less <", "setuptools<70.0", "setuptools", "70.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := parseRequirementLine(tt.line)
			require.NotNil(t, q)
			assert.Equal(t, tt.wantPkg, q.Name)
			assert.Equal(t, tt.wantVer, q.Version)
			assert.Equal(t, "PyPI", q.Ecosystem)
		})
	}
}

func TestParseRequirementLine_NoOperator(t *testing.T) {
	q := parseRequirementLine("requests")
	assert.Nil(t, q)
}

func TestParseRequirementLine_EmptyVersion(t *testing.T) {
	q := parseRequirementLine("requests==")
	assert.Nil(t, q)
}
