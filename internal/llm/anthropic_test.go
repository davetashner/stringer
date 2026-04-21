// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/davetashner/stringer/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAnthropicProvider_WithAPIKey(t *testing.T) {
	p, err := llm.NewAnthropicProvider(llm.WithAPIKey("test-key-123"))
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewAnthropicProvider_FromEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-test-key")

	p, err := llm.NewAnthropicProvider()
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestNewAnthropicProvider_NoKeyError(t *testing.T) {
	// Clear env to ensure no key is available.
	t.Setenv("ANTHROPIC_API_KEY", "")

	p, err := llm.NewAnthropicProvider()
	assert.Nil(t, p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ANTHROPIC_API_KEY")
}

func TestNewAnthropicProvider_OptionPrecedence(t *testing.T) {
	// Explicit key should be used even when env is set.
	t.Setenv("ANTHROPIC_API_KEY", "env-key")

	p, err := llm.NewAnthropicProvider(llm.WithAPIKey("explicit-key"))
	require.NoError(t, err)
	assert.NotNil(t, p)
}

func TestAnthropicProvider_DefaultModel(t *testing.T) {
	p, err := llm.NewAnthropicProvider(llm.WithAPIKey("test-key"))
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet-4-5-20250929", p.Model())
}

func TestAnthropicProvider_CustomModel(t *testing.T) {
	p, err := llm.NewAnthropicProvider(
		llm.WithAPIKey("test-key"),
		llm.WithModel("claude-haiku-3-5-20241022"),
	)
	require.NoError(t, err)
	assert.Equal(t, "claude-haiku-3-5-20241022", p.Model())
}

func TestAnthropicProvider_DefaultMaxRetries(t *testing.T) {
	p, err := llm.NewAnthropicProvider(llm.WithAPIKey("test-key"))
	require.NoError(t, err)
	assert.Equal(t, 3, p.MaxRetries())
}

func TestAnthropicProvider_CustomMaxRetries(t *testing.T) {
	p, err := llm.NewAnthropicProvider(
		llm.WithAPIKey("test-key"),
		llm.WithMaxRetries(5),
	)
	require.NoError(t, err)
	assert.Equal(t, 5, p.MaxRetries())
}

func TestAnthropicProvider_ImplementsProvider(t *testing.T) {
	// Skip if no API key (we only need construction, not API calls).
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Setenv("ANTHROPIC_API_KEY", "test-key")
	}

	p, err := llm.NewAnthropicProvider()
	require.NoError(t, err)

	var _ llm.Provider = p
}

// anthropicResponse is the JSON shape returned by the Messages API.
type anthropicResponse struct {
	ID         string             `json:"id"`
	Type       string             `json:"type"`
	Role       string             `json:"role"`
	Content    []anthropicContent `json:"content"`
	Model      string             `json:"model"`
	StopReason string             `json:"stop_reason"`
	Usage      anthropicUsage     `json:"usage"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// capturedAnthropicRequest mirrors the fields of the outgoing request body
// that tests need to assert on. Using a typed struct instead of
// map[string]interface{} gets compiler-enforced field names and avoids the
// float64 quirk JSON numbers have when decoded into a generic map.
type capturedAnthropicRequest struct {
	Model       string                 `json:"model"`
	MaxTokens   int                    `json:"max_tokens"`
	Temperature *float64               `json:"temperature,omitempty"`
	System      []anthropicSystemBlock `json:"system,omitempty"`
}

type anthropicSystemBlock struct {
	Type string `json:"type,omitempty"`
	Text string `json:"text"`
}

// newTestServer returns an httptest server that responds with the given
// anthropicResponse, and captures the outgoing request body for assertions.
func newTestServer(t *testing.T, resp anthropicResponse, statusCode int, captured *capturedAnthropicRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if captured != nil {
			_ = json.NewDecoder(r.Body).Decode(captured)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestComplete_DefaultModelAndMaxTokens(t *testing.T) {
	var captured capturedAnthropicRequest
	srv := newTestServer(t, anthropicResponse{
		ID:         "msg_test",
		Type:       "message",
		Role:       "assistant",
		Content:    []anthropicContent{{Type: "text", Text: "hello"}},
		Model:      "claude-sonnet-4-5-20250929",
		StopReason: "end_turn",
		Usage:      anthropicUsage{InputTokens: 10, OutputTokens: 5},
	}, http.StatusOK, &captured)
	defer srv.Close()

	p, err := llm.NewAnthropicProvider(
		llm.WithAPIKey("test-key"),
		llm.WithBaseURL(srv.URL),
		llm.WithMaxRetries(0),
	)
	require.NoError(t, err)

	resp, err := p.Complete(context.Background(), llm.Request{Prompt: "hi"})
	require.NoError(t, err)

	assert.Equal(t, "hello", resp.Content)
	assert.Equal(t, "claude-sonnet-4-5-20250929", resp.Model)
	assert.Equal(t, 10, resp.Usage.InputTokens)
	assert.Equal(t, 5, resp.Usage.OutputTokens)

	// Verify defaults sent to API.
	assert.Equal(t, "claude-sonnet-4-5-20250929", captured.Model)
	assert.Equal(t, 4096, captured.MaxTokens)
}

func TestComplete_ModelOverride(t *testing.T) {
	var captured capturedAnthropicRequest
	srv := newTestServer(t, anthropicResponse{
		ID:      "msg_test",
		Type:    "message",
		Role:    "assistant",
		Content: []anthropicContent{{Type: "text", Text: "ok"}},
		Model:   "claude-haiku-3-5-20241022",
		Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 2},
	}, http.StatusOK, &captured)
	defer srv.Close()

	p, err := llm.NewAnthropicProvider(
		llm.WithAPIKey("test-key"),
		llm.WithBaseURL(srv.URL),
		llm.WithMaxRetries(0),
	)
	require.NoError(t, err)

	resp, err := p.Complete(context.Background(), llm.Request{
		Prompt: "hi",
		Model:  "claude-haiku-3-5-20241022",
	})
	require.NoError(t, err)

	assert.Equal(t, "claude-haiku-3-5-20241022", resp.Model)
	assert.Equal(t, "claude-haiku-3-5-20241022", captured.Model)
}

func TestComplete_MaxTokensOverride(t *testing.T) {
	var captured capturedAnthropicRequest
	srv := newTestServer(t, anthropicResponse{
		ID:      "msg_test",
		Type:    "message",
		Role:    "assistant",
		Content: []anthropicContent{{Type: "text", Text: "ok"}},
		Model:   "claude-sonnet-4-5-20250929",
		Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 2},
	}, http.StatusOK, &captured)
	defer srv.Close()

	p, err := llm.NewAnthropicProvider(
		llm.WithAPIKey("test-key"),
		llm.WithBaseURL(srv.URL),
		llm.WithMaxRetries(0),
	)
	require.NoError(t, err)

	_, err = p.Complete(context.Background(), llm.Request{
		Prompt:    "hi",
		MaxTokens: 1024,
	})
	require.NoError(t, err)

	assert.Equal(t, 1024, captured.MaxTokens)
}

func TestComplete_SystemPrompt(t *testing.T) {
	var captured capturedAnthropicRequest
	srv := newTestServer(t, anthropicResponse{
		ID:      "msg_test",
		Type:    "message",
		Role:    "assistant",
		Content: []anthropicContent{{Type: "text", Text: "ok"}},
		Model:   "claude-sonnet-4-5-20250929",
		Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 2},
	}, http.StatusOK, &captured)
	defer srv.Close()

	p, err := llm.NewAnthropicProvider(
		llm.WithAPIKey("test-key"),
		llm.WithBaseURL(srv.URL),
		llm.WithMaxRetries(0),
	)
	require.NoError(t, err)

	_, err = p.Complete(context.Background(), llm.Request{
		Prompt:       "hi",
		SystemPrompt: "You are a helpful assistant.",
	})
	require.NoError(t, err)

	require.Len(t, captured.System, 1, "system field should be present with one block")
	assert.Equal(t, "You are a helpful assistant.", captured.System[0].Text)
}

func TestComplete_Temperature(t *testing.T) {
	var captured capturedAnthropicRequest
	srv := newTestServer(t, anthropicResponse{
		ID:      "msg_test",
		Type:    "message",
		Role:    "assistant",
		Content: []anthropicContent{{Type: "text", Text: "ok"}},
		Model:   "claude-sonnet-4-5-20250929",
		Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 2},
	}, http.StatusOK, &captured)
	defer srv.Close()

	p, err := llm.NewAnthropicProvider(
		llm.WithAPIKey("test-key"),
		llm.WithBaseURL(srv.URL),
		llm.WithMaxRetries(0),
	)
	require.NoError(t, err)

	temp := 0.7
	_, err = p.Complete(context.Background(), llm.Request{
		Prompt:      "hi",
		Temperature: &temp,
	})
	require.NoError(t, err)

	require.NotNil(t, captured.Temperature, "temperature should be present when set")
	assert.Equal(t, 0.7, *captured.Temperature)
}

func TestComplete_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`))
	}))
	defer srv.Close()

	p, err := llm.NewAnthropicProvider(
		llm.WithAPIKey("test-key"),
		llm.WithBaseURL(srv.URL),
		llm.WithMaxRetries(0),
	)
	require.NoError(t, err)

	_, err = p.Complete(context.Background(), llm.Request{Prompt: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "anthropic: completion failed")
}

func TestComplete_EmptyContent(t *testing.T) {
	srv := newTestServer(t, anthropicResponse{
		ID:      "msg_test",
		Type:    "message",
		Role:    "assistant",
		Content: []anthropicContent{},
		Model:   "claude-sonnet-4-5-20250929",
		Usage:   anthropicUsage{InputTokens: 5, OutputTokens: 0},
	}, http.StatusOK, nil)
	defer srv.Close()

	p, err := llm.NewAnthropicProvider(
		llm.WithAPIKey("test-key"),
		llm.WithBaseURL(srv.URL),
		llm.WithMaxRetries(0),
	)
	require.NoError(t, err)

	resp, err := p.Complete(context.Background(), llm.Request{Prompt: "hi"})
	require.NoError(t, err)
	assert.Equal(t, "", resp.Content)
}

func TestComplete_MultipleTextBlocks(t *testing.T) {
	srv := newTestServer(t, anthropicResponse{
		ID:   "msg_test",
		Type: "message",
		Role: "assistant",
		Content: []anthropicContent{
			{Type: "text", Text: "hello "},
			{Type: "text", Text: "world"},
		},
		Model: "claude-sonnet-4-5-20250929",
		Usage: anthropicUsage{InputTokens: 5, OutputTokens: 4},
	}, http.StatusOK, nil)
	defer srv.Close()

	p, err := llm.NewAnthropicProvider(
		llm.WithAPIKey("test-key"),
		llm.WithBaseURL(srv.URL),
		llm.WithMaxRetries(0),
	)
	require.NoError(t, err)

	resp, err := p.Complete(context.Background(), llm.Request{Prompt: "hi"})
	require.NoError(t, err)
	assert.Equal(t, "hello world", resp.Content)
}
