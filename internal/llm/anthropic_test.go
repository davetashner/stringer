package llm_test

import (
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
