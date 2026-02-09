package context

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/docs"
	"github.com/davetashner/stringer/internal/state"
)

func TestRenderJSON_Basic(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "test-project",
		Language: "Go",
		TechStack: []docs.TechComponent{
			{Name: "Go", Version: "1.22", Source: "go.mod"},
		},
		BuildCommands: []docs.BuildCommand{
			{Name: "build", Command: "go build ./...", Source: "go.mod"},
		},
		Patterns: []docs.CodePattern{
			{Name: "Internal Packages", Description: "Uses internal/ directory"},
		},
	}

	var buf bytes.Buffer
	err := RenderJSON(analysis, nil, nil, &buf)
	require.NoError(t, err)

	var result ContextJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))
	assert.Equal(t, "test-project", result.Name)
	assert.Equal(t, "Go", result.Language)
	assert.Len(t, result.TechStack, 1)
	assert.Len(t, result.BuildCmds, 1)
	assert.Len(t, result.Patterns, 1)
	assert.Nil(t, result.History)
	assert.Nil(t, result.TechDebt)
}

func TestRenderJSON_WithScanState(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "test",
		Language: "Go",
	}

	scanState := &state.ScanState{
		SignalCount: 3,
		SignalMetas: []state.SignalMeta{
			{Kind: "todo", Title: "Fix this"},
			{Kind: "todo", Title: "Fix that"},
			{Kind: "fixme", Title: "Broken"},
		},
	}

	var buf bytes.Buffer
	err := RenderJSON(analysis, nil, scanState, &buf)
	require.NoError(t, err)

	var result ContextJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))
	require.NotNil(t, result.TechDebt)
	assert.Equal(t, 3, result.TechDebt.SignalCount)
	assert.Equal(t, 2, result.TechDebt.ByKind["todo"])
	assert.Equal(t, 1, result.TechDebt.ByKind["fixme"])
}

func TestRenderJSON_EmptyScanState(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "test",
		Language: "Go",
	}

	scanState := &state.ScanState{
		SignalCount: 0,
		SignalMetas: nil,
	}

	var buf bytes.Buffer
	err := RenderJSON(analysis, nil, scanState, &buf)
	require.NoError(t, err)

	var result ContextJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))
	assert.Nil(t, result.TechDebt, "empty SignalMetas should not create TechDebt")
}

func TestRenderJSON_WithHistory(t *testing.T) {
	analysis := &docs.RepoAnalysis{
		Name:     "test",
		Language: "Go",
	}

	history := &GitHistory{
		TotalCommits: 42,
		TopAuthors: []AuthorStats{
			{Name: "Alice", Commits: 30},
			{Name: "Bob", Commits: 12},
		},
	}

	var buf bytes.Buffer
	err := RenderJSON(analysis, history, nil, &buf)
	require.NoError(t, err)

	var result ContextJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))
	require.NotNil(t, result.History)
	assert.Equal(t, 42, result.History.TotalCommits)
	assert.Len(t, result.History.TopAuthors, 2)
}
