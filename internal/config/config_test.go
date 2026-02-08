package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestConfig_YAMLRoundTrip(t *testing.T) {
	enabled := true
	disabled := false
	original := &Config{
		OutputFormat: "json",
		MaxIssues:    50,
		NoLLM:        true,
		Collectors: map[string]CollectorConfig{
			"todos": {
				Enabled:         &enabled,
				ErrorMode:       "fail",
				MinConfidence:   0.5,
				IncludePatterns: []string{"*.go"},
				ExcludePatterns: []string{"vendor/**"},
			},
			"gitlog": {
				Enabled: &disabled,
			},
		},
	}

	data, err := yaml.Marshal(original)
	require.NoError(t, err)

	var decoded Config
	require.NoError(t, yaml.Unmarshal(data, &decoded))

	assert.Equal(t, original.OutputFormat, decoded.OutputFormat)
	assert.Equal(t, original.MaxIssues, decoded.MaxIssues)
	assert.Equal(t, original.NoLLM, decoded.NoLLM)
	assert.Len(t, decoded.Collectors, 2)

	todos := decoded.Collectors["todos"]
	require.NotNil(t, todos.Enabled)
	assert.True(t, *todos.Enabled)
	assert.Equal(t, "fail", todos.ErrorMode)
	assert.InDelta(t, 0.5, todos.MinConfidence, 0.001)
	assert.Equal(t, []string{"*.go"}, todos.IncludePatterns)
	assert.Equal(t, []string{"vendor/**"}, todos.ExcludePatterns)

	gitlog := decoded.Collectors["gitlog"]
	require.NotNil(t, gitlog.Enabled)
	assert.False(t, *gitlog.Enabled)
}

func TestConfig_EnabledNilDistinct(t *testing.T) {
	// When Enabled is not set in YAML, it should unmarshal as nil.
	data := []byte(`
collectors:
  todos:
    min_confidence: 0.3
`)
	var cfg Config
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	assert.Nil(t, cfg.Collectors["todos"].Enabled)
}

func TestConfig_EmptyYAML(t *testing.T) {
	var cfg Config
	require.NoError(t, yaml.Unmarshal([]byte(""), &cfg))
	assert.Empty(t, cfg.OutputFormat)
	assert.Equal(t, 0, cfg.MaxIssues)
	assert.False(t, cfg.NoLLM)
	assert.Nil(t, cfg.Collectors)
}

func TestConfig_OmitEmptyFields(t *testing.T) {
	cfg := &Config{}
	data, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	// Should produce minimal output with omitempty.
	assert.Equal(t, "{}\n", string(data))
}
