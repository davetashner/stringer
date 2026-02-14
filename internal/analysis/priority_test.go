// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/llm"
	"github.com/davetashner/stringer/internal/signal"
)

// -----------------------------------------------------------------------
// buildPriorityPrompt tests
// -----------------------------------------------------------------------

func TestBuildPriorityPrompt_ContainsSignals(t *testing.T) {
	signals := testSignals()
	prompt := buildPriorityPrompt(signals)

	assert.Contains(t, prompt, "sig-0")
	assert.Contains(t, prompt, "sig-1")
	assert.Contains(t, prompt, "sig-2")
	assert.Contains(t, prompt, "fix auth error")
	assert.Contains(t, prompt, "todo")
	assert.Contains(t, prompt, "internal/auth/handler.go")
	assert.Contains(t, prompt, "P1")
	assert.Contains(t, prompt, "P4")
	assert.Contains(t, prompt, "JSON")
}

func TestBuildPriorityPrompt_IncludesTagsAndConfidence(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "test", Tags: []string{"security", "auth"}, Confidence: 0.85},
	}
	prompt := buildPriorityPrompt(signals)

	assert.Contains(t, prompt, "security, auth")
	assert.Contains(t, prompt, "0.85")
}

func TestBuildPriorityPrompt_TruncatesDescription(t *testing.T) {
	longDesc := ""
	for i := 0; i < 300; i++ {
		longDesc += "x"
	}
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "test", Description: longDesc},
	}
	prompt := buildPriorityPrompt(signals)
	assert.Contains(t, prompt, "...")
}

func TestBuildPriorityPrompt_EmptySignals(t *testing.T) {
	prompt := buildPriorityPrompt(nil)
	// Should still produce a valid prompt structure.
	assert.Contains(t, prompt, "SIGNALS")
	assert.Contains(t, prompt, "JSON")
}

// -----------------------------------------------------------------------
// parsePriorityResponse tests
// -----------------------------------------------------------------------

func TestParsePriorityResponse_ValidWrapper(t *testing.T) {
	input := `{"priorities": [{"id": "sig-0", "priority": 2, "reasoning": "user-facing bug"}]}`
	items, err := parsePriorityResponse(input)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "sig-0", items[0].ID)
	assert.Equal(t, 2, items[0].Priority)
	assert.Equal(t, "user-facing bug", items[0].Reasoning)
}

func TestParsePriorityResponse_ValidArray(t *testing.T) {
	input := `[{"id": "sig-0", "priority": 1, "reasoning": "security"}]`
	items, err := parsePriorityResponse(input)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, 1, items[0].Priority)
}

func TestParsePriorityResponse_WithCodeFences(t *testing.T) {
	input := "```json\n" + `{"priorities": [{"id": "sig-0", "priority": 3, "reasoning": "tech debt"}]}` + "\n```"
	items, err := parsePriorityResponse(input)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, 3, items[0].Priority)
}

func TestParsePriorityResponse_InvalidJSON(t *testing.T) {
	_, err := parsePriorityResponse("not json at all")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse")
}

func TestParsePriorityResponse_EmptyContent(t *testing.T) {
	_, err := parsePriorityResponse("")
	require.Error(t, err)
}

func TestParsePriorityResponse_MultiplePriorities(t *testing.T) {
	input := `{"priorities": [
		{"id": "sig-0", "priority": 1, "reasoning": "security vuln"},
		{"id": "sig-1", "priority": 3, "reasoning": "tech debt"},
		{"id": "sig-2", "priority": 4, "reasoning": "cosmetic"}
	]}`
	items, err := parsePriorityResponse(input)
	require.NoError(t, err)
	require.Len(t, items, 3)
	assert.Equal(t, 1, items[0].Priority)
	assert.Equal(t, 3, items[1].Priority)
	assert.Equal(t, 4, items[2].Priority)
}

// -----------------------------------------------------------------------
// applyOverrides tests
// -----------------------------------------------------------------------

func TestApplyOverrides_MatchesGlob(t *testing.T) {
	signals := []signal.RawSignal{
		{FilePath: "auth/login.go"},
		{FilePath: "db/conn.go"},
	}
	overrides := []PriorityOverride{
		{Pattern: "auth/**", Priority: 1},
	}

	result := applyOverrides(signals, overrides)
	require.NotNil(t, result[0].Priority)
	assert.Equal(t, 1, *result[0].Priority)
	assert.Nil(t, result[1].Priority)
}

func TestApplyOverrides_ExactMatch(t *testing.T) {
	signals := []signal.RawSignal{
		{FilePath: "main.go"},
	}
	overrides := []PriorityOverride{
		{Pattern: "main.go", Priority: 2},
	}

	result := applyOverrides(signals, overrides)
	require.NotNil(t, result[0].Priority)
	assert.Equal(t, 2, *result[0].Priority)
}

func TestApplyOverrides_FirstMatchWins(t *testing.T) {
	signals := []signal.RawSignal{
		{FilePath: "auth/handler.go"},
	}
	overrides := []PriorityOverride{
		{Pattern: "auth/**", Priority: 1},
		{Pattern: "auth/**", Priority: 4},
	}

	result := applyOverrides(signals, overrides)
	require.NotNil(t, result[0].Priority)
	assert.Equal(t, 1, *result[0].Priority)
}

func TestApplyOverrides_NoOverrides(t *testing.T) {
	signals := []signal.RawSignal{
		{FilePath: "main.go"},
	}
	result := applyOverrides(signals, nil)
	assert.Nil(t, result[0].Priority)
}

func TestApplyOverrides_NoMatch(t *testing.T) {
	signals := []signal.RawSignal{
		{FilePath: "main.go"},
	}
	overrides := []PriorityOverride{
		{Pattern: "auth/**", Priority: 1},
	}

	result := applyOverrides(signals, overrides)
	assert.Nil(t, result[0].Priority)
}

func TestApplyOverrides_OverwritesExistingPriority(t *testing.T) {
	p := 3
	signals := []signal.RawSignal{
		{FilePath: "auth/login.go", Priority: &p},
	}
	overrides := []PriorityOverride{
		{Pattern: "auth/**", Priority: 1},
	}

	result := applyOverrides(signals, overrides)
	require.NotNil(t, result[0].Priority)
	assert.Equal(t, 1, *result[0].Priority)
}

// -----------------------------------------------------------------------
// validateDistribution tests
// -----------------------------------------------------------------------

func TestValidateDistribution_NoPanic(t *testing.T) {
	// Just verify it doesn't panic on edge cases.
	validateDistribution(nil)
	validateDistribution([]signal.RawSignal{{}, {}})

	p1 := 1
	validateDistribution([]signal.RawSignal{{Priority: &p1}, {Priority: &p1}})

	p2, p3 := 2, 3
	validateDistribution([]signal.RawSignal{{Priority: &p1}, {Priority: &p2}, {Priority: &p3}})
}

// -----------------------------------------------------------------------
// InferPriorities end-to-end tests
// -----------------------------------------------------------------------

func TestInferPriorities_Success(t *testing.T) {
	signals := testSignals()

	responseJSON := mustJSON(t, priorityResponseWrapper{
		Priorities: []priorityResponseItem{
			{ID: "sig-0", Priority: 2, Reasoning: "user-facing auth bug"},
			{ID: "sig-1", Priority: 1, Reasoning: "login security issue"},
			{ID: "sig-2", Priority: 3, Reasoning: "tech debt in DB layer"},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: responseJSON})
	result, err := InferPriorities(context.Background(), signals, mock, nil)
	require.NoError(t, err)
	require.Len(t, result, 3)

	require.NotNil(t, result[0].Priority)
	assert.Equal(t, 2, *result[0].Priority)

	require.NotNil(t, result[1].Priority)
	assert.Equal(t, 1, *result[1].Priority)

	require.NotNil(t, result[2].Priority)
	assert.Equal(t, 3, *result[2].Priority)

	// Verify LLM was called with correct system prompt.
	calls := mock.Calls()
	require.Len(t, calls, 1)
	assert.Contains(t, calls[0].SystemPrompt, "prioritization expert")
}

func TestInferPriorities_LLMError_Fallback(t *testing.T) {
	signals := testSignals()
	mock := llm.NewMockProvider(llm.MockResponse{Err: errors.New("API down")})

	result, err := InferPriorities(context.Background(), signals, mock, nil)
	require.NoError(t, err)
	// All priorities should remain nil (fallback to confidence-based mapping).
	for _, sig := range result {
		assert.Nil(t, sig.Priority)
	}
}

func TestInferPriorities_BadJSON_Fallback(t *testing.T) {
	signals := testSignals()
	mock := llm.NewMockProvider(llm.MockResponse{Content: "not valid json"})

	result, err := InferPriorities(context.Background(), signals, mock, nil)
	require.NoError(t, err)
	for _, sig := range result {
		assert.Nil(t, sig.Priority)
	}
}

func TestInferPriorities_EmptySignals(t *testing.T) {
	mock := llm.NewMockProvider()
	result, err := InferPriorities(context.Background(), nil, mock, nil)
	require.NoError(t, err)
	assert.Nil(t, result)
	assert.Empty(t, mock.Calls(), "LLM should not be called for empty signals")
}

func TestInferPriorities_InvalidPriorityIgnored(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "test"},
	}

	responseJSON := mustJSON(t, priorityResponseWrapper{
		Priorities: []priorityResponseItem{
			{ID: "sig-0", Priority: 5, Reasoning: "out of range"},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: responseJSON})
	result, err := InferPriorities(context.Background(), signals, mock, nil)
	require.NoError(t, err)
	assert.Nil(t, result[0].Priority, "priority 5 should be ignored")
}

func TestInferPriorities_InvalidSignalIDIgnored(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "test"},
	}

	responseJSON := mustJSON(t, priorityResponseWrapper{
		Priorities: []priorityResponseItem{
			{ID: "sig-999", Priority: 2, Reasoning: "unknown signal"},
		},
	})

	mock := llm.NewMockProvider(llm.MockResponse{Content: responseJSON})
	result, err := InferPriorities(context.Background(), signals, mock, nil)
	require.NoError(t, err)
	assert.Nil(t, result[0].Priority, "unknown signal ID should be ignored")
}

func TestInferPriorities_OverridesAppliedAfterLLM(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "auth fix", FilePath: "auth/login.go"},
		{Source: "todos", Kind: "todo", Title: "db fix", FilePath: "db/conn.go"},
	}

	responseJSON := mustJSON(t, priorityResponseWrapper{
		Priorities: []priorityResponseItem{
			{ID: "sig-0", Priority: 3, Reasoning: "tech debt"},
			{ID: "sig-1", Priority: 3, Reasoning: "tech debt"},
		},
	})

	overrides := []PriorityOverride{
		{Pattern: "auth/**", Priority: 1}, // Override auth to P1.
	}

	mock := llm.NewMockProvider(llm.MockResponse{Content: responseJSON})
	result, err := InferPriorities(context.Background(), signals, mock, overrides)
	require.NoError(t, err)

	// auth/login.go should be P1 (override wins over LLM's P3).
	require.NotNil(t, result[0].Priority)
	assert.Equal(t, 1, *result[0].Priority)

	// db/conn.go should keep LLM's P3.
	require.NotNil(t, result[1].Priority)
	assert.Equal(t, 3, *result[1].Priority)
}

func TestInferPriorities_OverridesAppliedOnLLMFailure(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "auth fix", FilePath: "auth/login.go"},
		{Source: "todos", Kind: "todo", Title: "db fix", FilePath: "db/conn.go"},
	}

	overrides := []PriorityOverride{
		{Pattern: "auth/**", Priority: 1},
	}

	mock := llm.NewMockProvider(llm.MockResponse{Err: errors.New("API down")})
	result, err := InferPriorities(context.Background(), signals, mock, overrides)
	require.NoError(t, err)

	// Override should still be applied even on LLM failure.
	require.NotNil(t, result[0].Priority)
	assert.Equal(t, 1, *result[0].Priority)

	// No override for db.
	assert.Nil(t, result[1].Priority)
}

func TestInferPriorities_ContextCancelled(t *testing.T) {
	signals := testSignals()
	mock := llm.NewMockProvider(llm.MockResponse{Content: "{}"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Should fall back gracefully on context cancellation.
	result, err := InferPriorities(ctx, signals, mock, nil)
	require.NoError(t, err)
	assert.Len(t, result, 3)
}
