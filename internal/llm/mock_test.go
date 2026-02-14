// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package llm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/davetashner/stringer/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockProvider_EmptyResponses(t *testing.T) {
	m := llm.NewMockProvider()
	resp, err := m.Complete(context.Background(), llm.Request{Prompt: "hello"})
	require.NoError(t, err)
	assert.Empty(t, resp.Content)
	assert.Equal(t, "mock", resp.Model)
}

func TestMockProvider_SequentialResponses(t *testing.T) {
	m := llm.NewMockProvider(
		llm.MockResponse{Content: "first"},
		llm.MockResponse{Content: "second"},
		llm.MockResponse{Content: "third"},
	)

	ctx := context.Background()

	resp1, err := m.Complete(ctx, llm.Request{Prompt: "a"})
	require.NoError(t, err)
	assert.Equal(t, "first", resp1.Content)

	resp2, err := m.Complete(ctx, llm.Request{Prompt: "b"})
	require.NoError(t, err)
	assert.Equal(t, "second", resp2.Content)

	resp3, err := m.Complete(ctx, llm.Request{Prompt: "c"})
	require.NoError(t, err)
	assert.Equal(t, "third", resp3.Content)
}

func TestMockProvider_StaysOnLastResponse(t *testing.T) {
	m := llm.NewMockProvider(
		llm.MockResponse{Content: "only"},
	)

	ctx := context.Background()

	for range 5 {
		resp, err := m.Complete(ctx, llm.Request{Prompt: "x"})
		require.NoError(t, err)
		assert.Equal(t, "only", resp.Content)
	}
}

func TestMockProvider_ErrorResponse(t *testing.T) {
	expectedErr := errors.New("api failure")
	m := llm.NewMockProvider(
		llm.MockResponse{Err: expectedErr},
	)

	resp, err := m.Complete(context.Background(), llm.Request{Prompt: "fail"})
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, expectedErr)
}

func TestMockProvider_MixedResponses(t *testing.T) {
	expectedErr := errors.New("temporary error")
	m := llm.NewMockProvider(
		llm.MockResponse{Content: "ok"},
		llm.MockResponse{Err: expectedErr},
		llm.MockResponse{Content: "recovered"},
	)

	ctx := context.Background()

	resp1, err := m.Complete(ctx, llm.Request{Prompt: "1"})
	require.NoError(t, err)
	assert.Equal(t, "ok", resp1.Content)

	resp2, err := m.Complete(ctx, llm.Request{Prompt: "2"})
	assert.Nil(t, resp2)
	assert.ErrorIs(t, err, expectedErr)

	resp3, err := m.Complete(ctx, llm.Request{Prompt: "3"})
	require.NoError(t, err)
	assert.Equal(t, "recovered", resp3.Content)
}

func TestMockProvider_CallHistory(t *testing.T) {
	m := llm.NewMockProvider(
		llm.MockResponse{Content: "r1"},
		llm.MockResponse{Content: "r2"},
	)

	ctx := context.Background()

	_, _ = m.Complete(ctx, llm.Request{
		Prompt:       "first prompt",
		Model:        "test-model",
		MaxTokens:    100,
		SystemPrompt: "be helpful",
	})
	_, _ = m.Complete(ctx, llm.Request{
		Prompt: "second prompt",
	})

	calls := m.Calls()
	require.Len(t, calls, 2)

	assert.Equal(t, "first prompt", calls[0].Prompt)
	assert.Equal(t, "test-model", calls[0].Model)
	assert.Equal(t, 100, calls[0].MaxTokens)
	assert.Equal(t, "be helpful", calls[0].SystemPrompt)

	assert.Equal(t, "second prompt", calls[1].Prompt)
	assert.Empty(t, calls[1].Model)
}

func TestMockProvider_CancelledContext(t *testing.T) {
	m := llm.NewMockProvider(
		llm.MockResponse{Content: "should not get this"},
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	resp, err := m.Complete(ctx, llm.Request{Prompt: "cancelled"})
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, context.Canceled)

	// Cancelled requests should not be recorded.
	assert.Empty(t, m.Calls())
}

func TestMockProvider_Reset(t *testing.T) {
	m := llm.NewMockProvider(
		llm.MockResponse{Content: "first"},
		llm.MockResponse{Content: "second"},
	)

	ctx := context.Background()

	resp, err := m.Complete(ctx, llm.Request{Prompt: "a"})
	require.NoError(t, err)
	assert.Equal(t, "first", resp.Content)
	assert.Len(t, m.Calls(), 1)

	m.Reset()
	assert.Empty(t, m.Calls())

	// After reset, responses start from the beginning again.
	resp, err = m.Complete(ctx, llm.Request{Prompt: "b"})
	require.NoError(t, err)
	assert.Equal(t, "first", resp.Content)
}

func TestMockProvider_UsageReported(t *testing.T) {
	m := llm.NewMockProvider(
		llm.MockResponse{Content: "hello"},
	)

	resp, err := m.Complete(context.Background(), llm.Request{Prompt: "x"})
	require.NoError(t, err)
	assert.Greater(t, resp.Usage.InputTokens, 0)
	assert.Greater(t, resp.Usage.OutputTokens, 0)
}

func TestMockProvider_ConcurrentAccess(t *testing.T) {
	m := llm.NewMockProvider(
		llm.MockResponse{Content: "safe"},
	)

	ctx := context.Background()
	done := make(chan struct{})

	for range 10 {
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = m.Complete(ctx, llm.Request{Prompt: "concurrent"})
		}()
	}

	for range 10 {
		<-done
	}

	assert.Len(t, m.Calls(), 10)
}
