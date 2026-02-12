package analysis

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/llm"
	"github.com/davetashner/stringer/internal/output"
	"github.com/davetashner/stringer/internal/signal"
)

// -----------------------------------------------------------------------
// buildDependencyPrompt tests
// -----------------------------------------------------------------------

func TestBuildDependencyPrompt_ContainsSignals(t *testing.T) {
	signals := testSignals()
	prompt := buildDependencyPrompt(signals)

	assert.Contains(t, prompt, "sig-0")
	assert.Contains(t, prompt, "sig-1")
	assert.Contains(t, prompt, "sig-2")
	assert.Contains(t, prompt, "fix auth error")
	assert.Contains(t, prompt, "blocks")
	assert.Contains(t, prompt, "parent")
	assert.Contains(t, prompt, "relates-to")
	assert.Contains(t, prompt, "JSON")
}

func TestBuildDependencyPrompt_TruncatesDescription(t *testing.T) {
	longDesc := ""
	for i := 0; i < 300; i++ {
		longDesc += "y"
	}
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "test", Description: longDesc},
		{Source: "todos", Kind: "todo", Title: "test2"},
	}
	prompt := buildDependencyPrompt(signals)
	assert.Contains(t, prompt, "...")
}

// -----------------------------------------------------------------------
// parseDependencyResponse tests
// -----------------------------------------------------------------------

func TestParseDependencyResponse_ValidWrapper(t *testing.T) {
	input := `{"dependencies": [{"from": "sig-0", "to": "sig-1", "type": "blocks", "confidence": 0.9}]}`
	items, err := parseDependencyResponse(input)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "sig-0", items[0].From)
	assert.Equal(t, "sig-1", items[0].To)
	assert.Equal(t, "blocks", items[0].Type)
	assert.InDelta(t, 0.9, items[0].Confidence, 0.01)
}

func TestParseDependencyResponse_ValidArray(t *testing.T) {
	input := `[{"from": "sig-0", "to": "sig-1", "type": "blocks", "confidence": 0.8}]`
	items, err := parseDependencyResponse(input)
	require.NoError(t, err)
	require.Len(t, items, 1)
}

func TestParseDependencyResponse_EmptyDeps(t *testing.T) {
	input := `{"dependencies": []}`
	items, err := parseDependencyResponse(input)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestParseDependencyResponse_WithCodeFences(t *testing.T) {
	input := "```json\n" + `{"dependencies": [{"from": "sig-0", "to": "sig-1", "type": "blocks", "confidence": 0.7}]}` + "\n```"
	items, err := parseDependencyResponse(input)
	require.NoError(t, err)
	require.Len(t, items, 1)
}

func TestParseDependencyResponse_InvalidJSON(t *testing.T) {
	_, err := parseDependencyResponse("not json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse")
}

func TestParseDependencyResponse_EmptyContent(t *testing.T) {
	_, err := parseDependencyResponse("")
	require.Error(t, err)
}

func TestParseDependencyResponse_MultipleDeps(t *testing.T) {
	input := `{"dependencies": [
		{"from": "sig-0", "to": "sig-1", "type": "blocks", "confidence": 0.9},
		{"from": "sig-1", "to": "sig-2", "type": "relates-to", "confidence": 0.7},
		{"from": "sig-0", "to": "sig-2", "type": "parent", "confidence": 0.8}
	]}`
	items, err := parseDependencyResponse(input)
	require.NoError(t, err)
	require.Len(t, items, 3)
}

// -----------------------------------------------------------------------
// hasCycle tests
// -----------------------------------------------------------------------

func TestHasCycle_NoCycle(t *testing.T) {
	graph := adjacencyList{
		"sig-0": {"sig-1"},
		"sig-1": {"sig-2"},
		"sig-2": nil,
	}
	assert.False(t, hasCycle(graph))
}

func TestHasCycle_WithCycle(t *testing.T) {
	graph := adjacencyList{
		"sig-0": {"sig-1"},
		"sig-1": {"sig-2"},
		"sig-2": {"sig-0"},
	}
	assert.True(t, hasCycle(graph))
}

func TestHasCycle_SelfLoop(t *testing.T) {
	graph := adjacencyList{
		"sig-0": {"sig-0"},
	}
	assert.True(t, hasCycle(graph))
}

func TestHasCycle_Empty(t *testing.T) {
	assert.False(t, hasCycle(adjacencyList{}))
}

func TestHasCycle_DiamondNoCycle(t *testing.T) {
	graph := adjacencyList{
		"sig-0": {"sig-1", "sig-2"},
		"sig-1": {"sig-3"},
		"sig-2": {"sig-3"},
		"sig-3": nil,
	}
	assert.False(t, hasCycle(graph))
}

// -----------------------------------------------------------------------
// validateDAG tests
// -----------------------------------------------------------------------

func TestValidateDAG_NoCycle(t *testing.T) {
	deps := []BeadDependency{
		{FromID: "sig-0", ToID: "sig-1", Type: "blocks", Confidence: 0.9},
		{FromID: "sig-1", ToID: "sig-2", Type: "blocks", Confidence: 0.8},
	}
	result := validateDAG(deps)
	assert.Len(t, result, 2)
}

func TestValidateDAG_CycleBreaksLowestConfidence(t *testing.T) {
	deps := []BeadDependency{
		{FromID: "sig-0", ToID: "sig-1", Type: "blocks", Confidence: 0.9},
		{FromID: "sig-1", ToID: "sig-2", Type: "blocks", Confidence: 0.5}, // Lowest confidence.
		{FromID: "sig-2", ToID: "sig-0", Type: "blocks", Confidence: 0.7},
	}
	result := validateDAG(deps)

	// The cycle should be broken by removing the edge with confidence 0.5.
	assert.Less(t, len(result), 3)

	// Verify remaining edges form a DAG.
	var blockDeps []BeadDependency
	for _, d := range result {
		if d.Type == "blocks" {
			blockDeps = append(blockDeps, d)
		}
	}
	graph := buildDependencyGraph(blockDeps)
	assert.False(t, hasCycle(graph))
}

func TestValidateDAG_PreservesNonBlockEdges(t *testing.T) {
	deps := []BeadDependency{
		{FromID: "sig-0", ToID: "sig-1", Type: "blocks", Confidence: 0.9},
		{FromID: "sig-1", ToID: "sig-0", Type: "blocks", Confidence: 0.5}, // Creates cycle.
		{FromID: "sig-0", ToID: "sig-1", Type: "relates-to", Confidence: 0.7},
	}
	result := validateDAG(deps)

	// "relates-to" should always be preserved.
	var relatesToCount int
	for _, d := range result {
		if d.Type == "relates-to" {
			relatesToCount++
		}
	}
	assert.Equal(t, 1, relatesToCount)
}

func TestValidateDAG_NoDeps(t *testing.T) {
	result := validateDAG(nil)
	assert.Nil(t, result)
}

// -----------------------------------------------------------------------
// mapToBeadIDs tests
// -----------------------------------------------------------------------

func TestMapToBeadIDs_ConvertsIDs(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "fix auth", FilePath: "auth.go", Line: 1},
		{Source: "todos", Kind: "todo", Title: "fix db", FilePath: "db.go", Line: 1},
	}
	deps := []BeadDependency{
		{FromID: "sig-0", ToID: "sig-1", Type: "blocks", Confidence: 0.9},
	}

	result := mapToBeadIDs(deps, signals, "str-")
	require.Len(t, result, 1)

	expectedFrom := output.SignalID(signals[0], "str-")
	expectedTo := output.SignalID(signals[1], "str-")
	assert.Equal(t, expectedFrom, result[0].FromID)
	assert.Equal(t, expectedTo, result[0].ToID)
	assert.Equal(t, "blocks", result[0].Type)
}

func TestMapToBeadIDs_InvalidIDsDropped(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "test"},
	}
	deps := []BeadDependency{
		{FromID: "sig-0", ToID: "sig-999", Type: "blocks", Confidence: 0.9},
	}

	result := mapToBeadIDs(deps, signals, "str-")
	assert.Empty(t, result)
}

// -----------------------------------------------------------------------
// ApplyDepsToSignals tests
// -----------------------------------------------------------------------

func TestApplyDepsToSignals_SetsBlocksAndDependsOn(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "fix auth", FilePath: "auth.go", Line: 1},
		{Source: "todos", Kind: "todo", Title: "fix db", FilePath: "db.go", Line: 1},
	}

	id0 := output.SignalID(signals[0], "str-")
	id1 := output.SignalID(signals[1], "str-")

	deps := []BeadDependency{
		{FromID: id0, ToID: id1, Type: "blocks", Confidence: 0.9},
	}

	ApplyDepsToSignals(signals, deps, "str-")

	assert.Contains(t, signals[0].Blocks, id1)
	assert.Contains(t, signals[1].DependsOn, id0)
}

func TestApplyDepsToSignals_IgnoresNonBlockDeps(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "a"},
		{Source: "todos", Kind: "todo", Title: "b"},
	}

	id0 := output.SignalID(signals[0], "str-")
	id1 := output.SignalID(signals[1], "str-")

	deps := []BeadDependency{
		{FromID: id0, ToID: id1, Type: "relates-to", Confidence: 0.7},
	}

	ApplyDepsToSignals(signals, deps, "str-")

	assert.Empty(t, signals[0].Blocks)
	assert.Empty(t, signals[1].DependsOn)
}

func TestApplyDepsToSignals_NoDuplicates(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "fix auth", FilePath: "auth.go", Line: 1},
		{Source: "todos", Kind: "todo", Title: "fix db", FilePath: "db.go", Line: 1},
	}

	id0 := output.SignalID(signals[0], "str-")
	id1 := output.SignalID(signals[1], "str-")

	deps := []BeadDependency{
		{FromID: id0, ToID: id1, Type: "blocks", Confidence: 0.9},
		{FromID: id0, ToID: id1, Type: "blocks", Confidence: 0.8}, // Duplicate.
	}

	ApplyDepsToSignals(signals, deps, "str-")

	assert.Len(t, signals[0].Blocks, 1)
	assert.Len(t, signals[1].DependsOn, 1)
}

// -----------------------------------------------------------------------
// InferDependencies end-to-end tests
// -----------------------------------------------------------------------

func TestInferDependencies_Success(t *testing.T) {
	signals := testSignals()

	responseJSON := mustJSON(t, depResponseWrapper{
		Dependencies: []depResponseItem{
			{From: "sig-0", To: "sig-1", Type: "blocks", Confidence: 0.9},
			{From: "sig-1", To: "sig-2", Type: "relates-to", Confidence: 0.7},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: responseJSON})
	deps, err := InferDependencies(context.Background(), signals, mock, "str-")
	require.NoError(t, err)
	require.Len(t, deps, 2)

	// IDs should be mapped to bead IDs (not sig-N).
	assert.NotContains(t, deps[0].FromID, "sig-")
	assert.NotContains(t, deps[0].ToID, "sig-")

	// Verify LLM was called.
	calls := mock.Calls()
	require.Len(t, calls, 1)
	assert.Contains(t, calls[0].SystemPrompt, "dependency analysis expert")
}

func TestInferDependencies_LLMError_Fallback(t *testing.T) {
	signals := testSignals()
	mock := llm.NewMockProvider(llm.MockResponse{Err: errors.New("API down")})

	deps, err := InferDependencies(context.Background(), signals, mock, "str-")
	require.NoError(t, err)
	assert.Empty(t, deps)
}

func TestInferDependencies_BadJSON_Fallback(t *testing.T) {
	signals := testSignals()
	mock := llm.NewMockProvider(llm.MockResponse{Content: "not json"})

	deps, err := InferDependencies(context.Background(), signals, mock, "str-")
	require.NoError(t, err)
	assert.Empty(t, deps)
}

func TestInferDependencies_SingleSignal(t *testing.T) {
	signals := []signal.RawSignal{{Source: "todos", Title: "lonely"}}
	mock := llm.NewMockProvider()

	deps, err := InferDependencies(context.Background(), signals, mock, "str-")
	require.NoError(t, err)
	assert.Nil(t, deps)
	assert.Empty(t, mock.Calls(), "LLM should not be called for <2 signals")
}

func TestInferDependencies_EmptySignals(t *testing.T) {
	mock := llm.NewMockProvider()
	deps, err := InferDependencies(context.Background(), nil, mock, "str-")
	require.NoError(t, err)
	assert.Nil(t, deps)
}

func TestInferDependencies_InvalidSignalIDsIgnored(t *testing.T) {
	signals := testSignals()

	responseJSON := mustJSON(t, depResponseWrapper{
		Dependencies: []depResponseItem{
			{From: "sig-999", To: "sig-0", Type: "blocks", Confidence: 0.9},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: responseJSON})
	deps, err := InferDependencies(context.Background(), signals, mock, "str-")
	require.NoError(t, err)
	assert.Empty(t, deps)
}

func TestInferDependencies_SelfDependencyIgnored(t *testing.T) {
	signals := testSignals()

	responseJSON := mustJSON(t, depResponseWrapper{
		Dependencies: []depResponseItem{
			{From: "sig-0", To: "sig-0", Type: "blocks", Confidence: 0.9},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: responseJSON})
	deps, err := InferDependencies(context.Background(), signals, mock, "str-")
	require.NoError(t, err)
	assert.Empty(t, deps)
}

func TestInferDependencies_InvalidTypeIgnored(t *testing.T) {
	signals := testSignals()

	responseJSON := mustJSON(t, depResponseWrapper{
		Dependencies: []depResponseItem{
			{From: "sig-0", To: "sig-1", Type: "invalid-type", Confidence: 0.9},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: responseJSON})
	deps, err := InferDependencies(context.Background(), signals, mock, "str-")
	require.NoError(t, err)
	assert.Empty(t, deps)
}

func TestInferDependencies_CycleDetectionAndBreaking(t *testing.T) {
	signals := testSignals()

	responseJSON := mustJSON(t, depResponseWrapper{
		Dependencies: []depResponseItem{
			{From: "sig-0", To: "sig-1", Type: "blocks", Confidence: 0.9},
			{From: "sig-1", To: "sig-2", Type: "blocks", Confidence: 0.5}, // Lowest confidence.
			{From: "sig-2", To: "sig-0", Type: "blocks", Confidence: 0.7},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: responseJSON})
	deps, err := InferDependencies(context.Background(), signals, mock, "str-")
	require.NoError(t, err)

	// Cycle should be broken — fewer than 3 "blocks" edges remain.
	var blockCount int
	for _, d := range deps {
		if d.Type == "blocks" {
			blockCount++
		}
	}
	assert.Less(t, blockCount, 3)
}

func TestInferDependencies_ContextCancelled(t *testing.T) {
	signals := testSignals()
	mock := llm.NewMockProvider(llm.MockResponse{Content: "{}"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	deps, err := InferDependencies(ctx, signals, mock, "str-")
	require.NoError(t, err)
	assert.Empty(t, deps)
}

// -----------------------------------------------------------------------
// isValidDepType tests
// -----------------------------------------------------------------------

func TestIsValidDepType(t *testing.T) {
	assert.True(t, isValidDepType("blocks"))
	assert.True(t, isValidDepType("parent"))
	assert.True(t, isValidDepType("relates-to"))
	assert.False(t, isValidDepType("unknown"))
	assert.False(t, isValidDepType(""))
}

// -----------------------------------------------------------------------
// appendUnique tests
// -----------------------------------------------------------------------

func TestAppendUnique_AddsNew(t *testing.T) {
	result := appendUnique([]string{"a", "b"}, "c")
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

func TestAppendUnique_SkipsDuplicate(t *testing.T) {
	result := appendUnique([]string{"a", "b"}, "a")
	assert.Equal(t, []string{"a", "b"}, result)
}

func TestAppendUnique_NilSlice(t *testing.T) {
	result := appendUnique(nil, "a")
	assert.Equal(t, []string{"a"}, result)
}
