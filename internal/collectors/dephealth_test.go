package collectors

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/davetashner/stringer/internal/testable"
)

func TestDepHealthCollector_Name(t *testing.T) {
	c := &DepHealthCollector{}
	assert.Equal(t, "dephealth", c.Name())
}

func TestDepHealthCollector_NoGoMod(t *testing.T) {
	dir := t.TempDir()
	c := &DepHealthCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	assert.NoError(t, err)
	assert.Nil(t, signals)
}

func TestDepHealthCollector_BasicParse(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/mymod

go 1.22

require (
	github.com/foo/bar v1.2.3
	github.com/baz/qux v0.1.0 // indirect
)
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	c := &DepHealthCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals) // no replaces or retracts → no signals

	m := c.Metrics()
	require.NotNil(t, m)
	metrics, ok := m.(*DepHealthMetrics)
	require.True(t, ok)

	assert.Equal(t, "example.com/mymod", metrics.ModulePath)
	assert.Equal(t, "1.22", metrics.GoVersion)
	require.Len(t, metrics.Dependencies, 2)

	assert.Equal(t, "github.com/foo/bar", metrics.Dependencies[0].Path)
	assert.Equal(t, "v1.2.3", metrics.Dependencies[0].Version)
	assert.False(t, metrics.Dependencies[0].Indirect)

	assert.Equal(t, "github.com/baz/qux", metrics.Dependencies[1].Path)
	assert.Equal(t, "v0.1.0", metrics.Dependencies[1].Version)
	assert.True(t, metrics.Dependencies[1].Indirect)
}

func TestDepHealthCollector_LocalReplace(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/mymod

go 1.22

require github.com/foo/bar v1.2.3

replace github.com/foo/bar => ../local-bar
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	c := &DepHealthCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	sig := signals[0]
	assert.Equal(t, "dephealth", sig.Source)
	assert.Equal(t, "local-replace", sig.Kind)
	assert.Equal(t, "go.mod", sig.FilePath)
	assert.Contains(t, sig.Title, "github.com/foo/bar")
	assert.Contains(t, sig.Title, "../local-bar")
	assert.Contains(t, sig.Description, "non-portable")
	assert.Equal(t, 0.5, sig.Confidence)
	assert.Contains(t, sig.Tags, "local-replace")
	assert.Greater(t, sig.Line, 0)

	// Metrics should reflect IsLocal.
	metrics := c.Metrics().(*DepHealthMetrics)
	require.Len(t, metrics.Replaces, 1)
	assert.True(t, metrics.Replaces[0].IsLocal)
}

func TestDepHealthCollector_RemoteReplace(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/mymod

go 1.22

require github.com/foo/bar v1.2.3

replace github.com/foo/bar => github.com/fork/bar v1.2.4
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	c := &DepHealthCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals) // remote replace → no signal

	metrics := c.Metrics().(*DepHealthMetrics)
	require.Len(t, metrics.Replaces, 1)
	assert.False(t, metrics.Replaces[0].IsLocal)
	assert.Equal(t, "github.com/fork/bar", metrics.Replaces[0].NewPath)
	assert.Equal(t, "v1.2.4", metrics.Replaces[0].NewVersion)
}

func TestDepHealthCollector_RetractDirective(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/mymod

go 1.22

retract v1.0.0 // security vulnerability
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	c := &DepHealthCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	sig := signals[0]
	assert.Equal(t, "dephealth", sig.Source)
	assert.Equal(t, "retracted-version", sig.Kind)
	assert.Equal(t, "go.mod", sig.FilePath)
	assert.Contains(t, sig.Title, "v1.0.0")
	assert.Contains(t, sig.Description, "security vulnerability")
	assert.Equal(t, 0.3, sig.Confidence)
	assert.Contains(t, sig.Tags, "retracted-version")
	assert.Greater(t, sig.Line, 0)

	metrics := c.Metrics().(*DepHealthMetrics)
	require.Len(t, metrics.Retracts, 1)
	assert.Equal(t, "v1.0.0", metrics.Retracts[0].Low)
	assert.Equal(t, "v1.0.0", metrics.Retracts[0].High)
	assert.Equal(t, "security vulnerability", metrics.Retracts[0].Rationale)
}

func TestDepHealthCollector_RetractRange(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/mymod

go 1.22

retract [v1.0.0, v1.2.0] // broken API
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	c := &DepHealthCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	sig := signals[0]
	assert.Contains(t, sig.Title, "[v1.0.0, v1.2.0]")
	assert.Contains(t, sig.Description, "[v1.0.0, v1.2.0]")

	metrics := c.Metrics().(*DepHealthMetrics)
	require.Len(t, metrics.Retracts, 1)
	assert.Equal(t, "v1.0.0", metrics.Retracts[0].Low)
	assert.Equal(t, "v1.2.0", metrics.Retracts[0].High)
}

func TestDepHealthCollector_MultipleSignals(t *testing.T) {
	dir := t.TempDir()
	gomod := `module example.com/mymod

go 1.22

require (
	github.com/foo/bar v1.2.3
	github.com/baz/qux v0.1.0
)

replace github.com/foo/bar => ./local-foo

replace github.com/baz/qux => ../local-qux

retract v0.9.0 // broken
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	c := &DepHealthCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 3) // 2 local replaces + 1 retract

	kinds := make(map[string]int)
	for _, s := range signals {
		kinds[s.Kind]++
	}
	assert.Equal(t, 2, kinds["local-replace"])
	assert.Equal(t, 1, kinds["retracted-version"])
}

func TestDepHealthCollector_MalformedGoMod(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("this is not valid go.mod syntax!!!"), 0o600))

	c := &DepHealthCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	assert.Error(t, err)
	assert.Nil(t, signals)
	assert.Contains(t, err.Error(), "parsing go.mod")
}

func TestDepHealthCollector_Metrics(t *testing.T) {
	c := &DepHealthCollector{}

	// Before Collect, Metrics returns nil.
	assert.Nil(t, c.Metrics())

	// After Collect with a valid go.mod, Metrics is populated.
	dir := t.TempDir()
	gomod := `module example.com/test

go 1.22

require github.com/x/y v1.0.0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))

	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	m := c.Metrics()
	require.NotNil(t, m)
	metrics, ok := m.(*DepHealthMetrics)
	require.True(t, ok)
	assert.Equal(t, "example.com/test", metrics.ModulePath)
	assert.Len(t, metrics.Dependencies, 1)
}

func TestDepHealthCollector_ReadFileError(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		ReadFileFn: func(name string) ([]byte, error) {
			return nil, os.ErrPermission
		},
	}

	c := &DepHealthCollector{}
	signals, err := c.Collect(context.Background(), "/fake", signal.CollectorOpts{})
	assert.Error(t, err)
	assert.Nil(t, signals)
	assert.Contains(t, err.Error(), "reading go.mod")
}

func TestIsLocalPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"./foo", true},
		{"../bar", true},
		{"/absolute/path", true},
		{"github.com/x/y", false},
		{"example.com/mod", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, isLocalPath(tt.path))
		})
	}
}
