package config

import (
	"testing"

	// Register collectors and formatters so validation lookups work.
	_ "github.com/davetashner/stringer/internal/collectors"
	_ "github.com/davetashner/stringer/internal/output"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_ValidConfig(t *testing.T) {
	enabled := true
	cfg := &Config{
		OutputFormat: "beads",
		MaxIssues:    10,
		Collectors: map[string]CollectorConfig{
			"todos": {
				Enabled:       &enabled,
				ErrorMode:     "warn",
				MinConfidence: 0.5,
			},
		},
	}
	require.NoError(t, Validate(cfg))
}

func TestValidate_EmptyConfig(t *testing.T) {
	cfg := &Config{}
	require.NoError(t, Validate(cfg))
}

func TestValidate_UnknownFormat(t *testing.T) {
	cfg := &Config{OutputFormat: "xml"}
	err := Validate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "output_format")
	assert.Contains(t, err.Error(), "xml")
}

func TestValidate_NegativeMaxIssues(t *testing.T) {
	cfg := &Config{MaxIssues: -1}
	err := Validate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max_issues")
}

func TestValidate_UnknownCollector(t *testing.T) {
	cfg := &Config{
		Collectors: map[string]CollectorConfig{
			"nonexistent": {},
		},
	}
	err := Validate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
	assert.Contains(t, err.Error(), "unknown collector")
}

func TestValidate_InvalidErrorMode(t *testing.T) {
	cfg := &Config{
		Collectors: map[string]CollectorConfig{
			"todos": {ErrorMode: "explode"},
		},
	}
	err := Validate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error_mode")
	assert.Contains(t, err.Error(), "explode")
}

func TestValidate_MinConfidenceOutOfRange(t *testing.T) {
	tests := []struct {
		name string
		val  float64
	}{
		{"negative", -0.1},
		{"over_one", 1.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Collectors: map[string]CollectorConfig{
					"todos": {MinConfidence: tt.val},
				},
			}
			err := Validate(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "min_confidence")
		})
	}
}

func TestValidate_MinConfidenceBoundaryValues(t *testing.T) {
	// 0.0 and 1.0 should be valid.
	for _, val := range []float64{0.0, 1.0} {
		cfg := &Config{
			Collectors: map[string]CollectorConfig{
				"todos": {MinConfidence: val},
			},
		}
		assert.NoError(t, Validate(cfg), "min_confidence=%g should be valid", val)
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := &Config{
		OutputFormat: "xml",
		MaxIssues:    -5,
		Collectors: map[string]CollectorConfig{
			"nonexistent": {ErrorMode: "explode"},
		},
	}
	err := Validate(cfg)
	require.Error(t, err)
	// All errors should be reported.
	assert.Contains(t, err.Error(), "output_format")
	assert.Contains(t, err.Error(), "max_issues")
	assert.Contains(t, err.Error(), "nonexistent")
	assert.Contains(t, err.Error(), "error_mode")
}

func TestValidate_ValidErrorModes(t *testing.T) {
	for _, mode := range []string{"warn", "skip", "fail"} {
		cfg := &Config{
			Collectors: map[string]CollectorConfig{
				"todos": {ErrorMode: mode},
			},
		}
		assert.NoError(t, Validate(cfg), "error_mode=%q should be valid", mode)
	}
}
