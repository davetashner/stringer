// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package llm_test

import (
	"testing"

	"github.com/davetashner/stringer/internal/llm"
	"github.com/stretchr/testify/assert"
)

// TestProviderInterfaceCompliance verifies that all provider types satisfy
// the Provider interface at compile time. The var _ assignments above each
// type already do this, but this test makes it explicit and documents the
// contract in test output.
func TestProviderInterfaceCompliance(t *testing.T) {
	t.Run("MockProvider implements Provider", func(t *testing.T) {
		var p llm.Provider = llm.NewMockProvider()
		assert.NotNil(t, p)
	})
}

func TestRequestZeroValue(t *testing.T) {
	var req llm.Request
	assert.Empty(t, req.Prompt)
	assert.Empty(t, req.Model)
	assert.Zero(t, req.MaxTokens)
	assert.Nil(t, req.Temperature)
	assert.Empty(t, req.SystemPrompt)
}

func TestResponseZeroValue(t *testing.T) {
	var resp llm.Response
	assert.Empty(t, resp.Content)
	assert.Empty(t, resp.Model)
	assert.Zero(t, resp.Usage.InputTokens)
	assert.Zero(t, resp.Usage.OutputTokens)
}
