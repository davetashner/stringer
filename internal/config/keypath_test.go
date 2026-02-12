package config

import (
	"testing"

	// Register collectors so ValidateKeyPath lookups work.
	_ "github.com/davetashner/stringer/internal/collectors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetValue_TopLevel(t *testing.T) {
	cfg := &Config{
		OutputFormat: "json",
		MaxIssues:    42,
		NoLLM:        true,
	}

	val, err := GetValue(cfg, "output_format")
	require.NoError(t, err)
	assert.Equal(t, "json", val)

	val, err = GetValue(cfg, "max_issues")
	require.NoError(t, err)
	assert.Equal(t, 42, val)

	val, err = GetValue(cfg, "no_llm")
	require.NoError(t, err)
	assert.Equal(t, true, val)
}

func TestGetValue_Nested(t *testing.T) {
	cfg := &Config{
		Collectors: map[string]CollectorConfig{
			"todos": {MinConfidence: 0.8},
		},
	}

	val, err := GetValue(cfg, "collectors.todos.min_confidence")
	require.NoError(t, err)
	assert.Equal(t, 0.8, val)
}

func TestGetValue_CollectorBlock(t *testing.T) {
	cfg := &Config{
		Collectors: map[string]CollectorConfig{
			"todos": {MinConfidence: 0.5},
		},
	}

	val, err := GetValue(cfg, "collectors.todos")
	require.NoError(t, err)
	m, ok := val.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 0.5, m["min_confidence"])
}

func TestGetValue_NotFound(t *testing.T) {
	cfg := &Config{}

	_, err := GetValue(cfg, "output_format")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetValue_EmptyConfig(t *testing.T) {
	cfg := &Config{}
	_, err := GetValue(cfg, "collectors.todos")
	assert.Error(t, err)
}

func TestSetValue_Simple(t *testing.T) {
	data := make(map[string]any)
	require.NoError(t, SetValue(data, "output_format", "json"))
	assert.Equal(t, "json", data["output_format"])
}

func TestSetValue_Nested(t *testing.T) {
	data := make(map[string]any)
	require.NoError(t, SetValue(data, "collectors.todos.min_confidence", "0.8"))

	collectors, ok := data["collectors"].(map[string]any)
	require.True(t, ok)
	todos, ok := collectors["todos"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 0.8, todos["min_confidence"])
}

func TestSetValue_OverwriteExisting(t *testing.T) {
	data := map[string]any{
		"output_format": "markdown",
	}
	require.NoError(t, SetValue(data, "output_format", "json"))
	assert.Equal(t, "json", data["output_format"])
}

func TestSetValue_CreateIntermediateMaps(t *testing.T) {
	data := make(map[string]any)
	require.NoError(t, SetValue(data, "collectors.patterns.large_file_threshold", "500"))

	collectors := data["collectors"].(map[string]any)
	patterns := collectors["patterns"].(map[string]any)
	assert.Equal(t, 500, patterns["large_file_threshold"])
}

func TestSetValue_NonMapParent(t *testing.T) {
	data := map[string]any{
		"output_format": "json",
	}
	err := SetValue(data, "output_format.nested", "val")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a map")
}

func TestFlattenMap_Simple(t *testing.T) {
	m := map[string]any{
		"output_format": "json",
		"max_issues":    50,
	}
	flat := FlattenMap(m, "")
	assert.Equal(t, "json", flat["output_format"])
	assert.Equal(t, 50, flat["max_issues"])
}

func TestFlattenMap_Nested(t *testing.T) {
	m := map[string]any{
		"collectors": map[string]any{
			"todos": map[string]any{
				"min_confidence": 0.5,
				"enabled":        true,
			},
		},
	}
	flat := FlattenMap(m, "")
	assert.Equal(t, 0.5, flat["collectors.todos.min_confidence"])
	assert.Equal(t, true, flat["collectors.todos.enabled"])
	assert.Len(t, flat, 2)
}

func TestFlattenMap_WithPrefix(t *testing.T) {
	m := map[string]any{
		"enabled": true,
	}
	flat := FlattenMap(m, "collectors.todos")
	assert.Equal(t, true, flat["collectors.todos.enabled"])
}

func TestFlattenMap_Empty(t *testing.T) {
	flat := FlattenMap(map[string]any{}, "")
	assert.Empty(t, flat)
}

func TestCoerceValue(t *testing.T) {
	tests := []struct {
		input string
		want  any
	}{
		{"true", true},
		{"false", false},
		{"42", 42},
		{"0", 0},
		{"-1", -1},
		{"3.14", 3.14},
		{"0.5", 0.5},
		{"hello", "hello"},
		{"json", "json"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := coerceValue(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateKeyPath_TopLevelKeys(t *testing.T) {
	assert.NoError(t, ValidateKeyPath("output_format"))
	assert.NoError(t, ValidateKeyPath("max_issues"))
	assert.NoError(t, ValidateKeyPath("no_llm"))
	assert.NoError(t, ValidateKeyPath("beads_aware"))
}

func TestValidateKeyPath_UnknownKey(t *testing.T) {
	err := ValidateKeyPath("unknown_key")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown key")
}

func TestValidateKeyPath_PriorityOverrides(t *testing.T) {
	err := ValidateKeyPath("priority_overrides")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "edit")
}

func TestValidateKeyPath_CollectorsNoName(t *testing.T) {
	err := ValidateKeyPath("collectors")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "requires a collector name")
}

func TestValidateKeyPath_ScalarSubkey(t *testing.T) {
	err := ValidateKeyPath("output_format.nested")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scalar")
}

func TestValidateKeyPath_TooDeep(t *testing.T) {
	err := ValidateKeyPath("collectors.todos.min_confidence.too_deep")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "too deep")
}

func TestValidateKeyPath_ValidCollectorField(t *testing.T) {
	assert.NoError(t, ValidateKeyPath("collectors.todos.min_confidence"))
	assert.NoError(t, ValidateKeyPath("collectors.todos.enabled"))
	assert.NoError(t, ValidateKeyPath("collectors.todos.error_mode"))
}

func TestValidateKeyPath_UnknownCollector(t *testing.T) {
	err := ValidateKeyPath("collectors.nonexistent.min_confidence")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown collector")
}

func TestValidateKeyPath_UnknownCollectorField(t *testing.T) {
	err := ValidateKeyPath("collectors.todos.unknown_field")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown collector field")
}

func TestValidateKeyPath_CollectorNameOnly(t *testing.T) {
	assert.NoError(t, ValidateKeyPath("collectors.todos"))
}

func TestNavigateMap_NotAMap(t *testing.T) {
	m := map[string]any{
		"foo": "bar",
	}
	_, err := navigateMap(m, "foo.baz")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a map")
}

func TestSortedKeys(t *testing.T) {
	m := map[string]bool{"z": true, "a": true, "m": true}
	result := sortedKeys(m)
	assert.Equal(t, "a, m, z", result)
}
