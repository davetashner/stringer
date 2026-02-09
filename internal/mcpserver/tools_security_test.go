package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Security tests for MCP tool handlers (DX1.8).

func TestHandleScan_SecurityCollectorSpecialChars(t *testing.T) {
	dir := initTestRepo(t)

	tests := []struct {
		name       string
		collectors string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
		{"command injection", "todos;rm -rf /"},
		{"null byte", "todos\x00evil"},
		{"newline injection", "todos\nevil"},
		{"pipe injection", "todos|cat /etc/passwd"},
		{"backtick injection", "todos`whoami`"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ScanInput{
				Path:       dir,
				Collectors: tt.collectors,
			}

			result, _, err := handleScan(context.Background(), nil, input)
			if err != nil {
				// Invalid collector names should be rejected by the registry.
				assert.Contains(t, err.Error(), "unknown collector",
					"malicious collector name should be rejected")
				return
			}
			// Empty/whitespace collectors fall through to defaults — that's fine.
			require.NotNil(t, result)
			text := result.Content[0].(*mcp.TextContent).Text
			assert.True(t, json.Valid([]byte(text)), "output should be valid JSON")
		})
	}
}

func TestHandleScan_SecurityFormatSpecialChars(t *testing.T) {
	dir := initTestRepo(t)

	tests := []struct {
		name   string
		format string
	}{
		{"newline", "json\nevil"},
		{"null byte", "json\x00evil"},
		{"template injection", "{{.}}"},
		{"html script", "<script>alert(1)</script>"},
		{"command injection", "json;rm -rf /"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ScanInput{
				Path:       dir,
				Collectors: "todos",
				Format:     tt.format,
			}

			_, _, err := handleScan(context.Background(), nil, input)
			require.Error(t, err, "malicious format %q should be rejected", tt.format)
			assert.Contains(t, err.Error(), "unsupported format")
		})
	}
}

func TestHandleScan_SecurityEmptyCollectorString(t *testing.T) {
	dir := initTestRepo(t)

	// ",,,," should result in all empty strings being filtered out by splitAndTrim,
	// leaving an empty slice, which means "run all defaults".
	input := ScanInput{
		Path:       dir,
		Collectors: ",,,,",
	}

	result, _, err := handleScan(context.Background(), nil, input)
	require.NoError(t, err, "empty collector entries should fall through to defaults")

	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, json.Valid([]byte(text)))
}

func TestHandleScan_SecurityStderrIsolation(t *testing.T) {
	dir := initTestRepo(t)

	// Create corrupted .beads/ to trigger slog.Warn.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".beads"), 0o750))
	writeTestFile(t, dir, ".beads/issues.jsonl", "not valid json\n")

	input := ScanInput{
		Path:       dir,
		Collectors: "todos",
	}

	result, _, err := handleScan(context.Background(), nil, input)
	require.NoError(t, err)

	text := result.Content[0].(*mcp.TextContent).Text
	// The MCP response content must not contain warning/error text from slog.
	assert.NotContains(t, text, "failed to load")
	assert.NotContains(t, text, "slog")
	assert.NotContains(t, text, "WARN")
	assert.True(t, json.Valid([]byte(text)), "output should be clean JSON")
}

func TestHandleContext_SecurityStderrIsolation(t *testing.T) {
	dir := initTestRepo(t)

	// Create corrupted state file to trigger slog.Warn.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".stringer"), 0o750))
	writeTestFile(t, dir, ".stringer/last-scan.json", "corrupted{{{")

	input := ContextInput{
		Path: dir,
	}

	result, _, err := handleContext(context.Background(), nil, input)
	require.NoError(t, err)

	text := result.Content[0].(*mcp.TextContent).Text
	// MCP content must not leak warning text.
	assert.NotContains(t, text, "failed to load scan state")
	assert.NotContains(t, text, "corrupted{{{")
	assert.True(t, json.Valid([]byte(text)), "output should be clean JSON")
}

func TestHandleScan_SecurityNoEnvVarsExposed(t *testing.T) {
	dir := initTestRepo(t)

	marker := "STRINGER_SECURITY_TEST_MARKER_12345"
	t.Setenv("STRINGER_SECRET", marker)

	input := ScanInput{
		Path:       dir,
		Collectors: "todos",
	}

	result, _, err := handleScan(context.Background(), nil, input)
	require.NoError(t, err)

	text := result.Content[0].(*mcp.TextContent).Text
	assert.NotContains(t, text, marker, "scan output must not expose env vars")
}

func TestHandleReport_SecurityNoEnvVarsExposed(t *testing.T) {
	dir := initTestRepo(t)

	marker := "STRINGER_SECURITY_TEST_MARKER_67890"
	t.Setenv("STRINGER_SECRET", marker)

	input := ReportInput{
		Path:       dir,
		Collectors: "todos",
	}

	result, _, err := handleReport(context.Background(), nil, input)
	require.NoError(t, err)

	text := result.Content[0].(*mcp.TextContent).Text
	assert.NotContains(t, text, marker, "report output must not expose env vars")
}

func TestHandleScan_SecurityPathTraversalAttempts(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"parent traversal", "../../../etc"},
		{"absolute etc", "/etc/passwd"},
		{"null in path", "/tmp\x00/evil"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ScanInput{
				Path:       tt.path,
				Collectors: "todos",
			}

			_, _, err := handleScan(context.Background(), nil, input)
			if err == nil {
				t.Fatal("expected error for traversal path")
			}
			// Should get a resolution error, not a scan of a sensitive directory.
		})
	}
}

func TestHandleScan_SecurityUnicodeCollectorNames(t *testing.T) {
	dir := initTestRepo(t)

	tests := []struct {
		name       string
		collectors string
	}{
		{"emoji", "\U0001f4a3"},
		{"chinese chars", "\u4e2d\u6587"},
		{"rtl override", "\u202etodos"},
		{"zero width space", "todos\u200b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ScanInput{
				Path:       dir,
				Collectors: tt.collectors,
			}

			_, _, err := handleScan(context.Background(), nil, input)
			require.Error(t, err, "non-ASCII collector name %q should be rejected", tt.collectors)
			assert.Contains(t, err.Error(), "unknown collector")
		})
	}
}

func TestHandleScan_SecurityKindFilterEdgeCases(t *testing.T) {
	dir := initTestRepo(t)

	tests := []struct {
		name string
		kind string
	}{
		{"nonexistent kind", "nonexistent_kind_xyz"},
		{"empty after trim", "   "},
		{"special chars", "todo<script>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ScanInput{
				Path:       dir,
				Collectors: "todos",
				Kind:       tt.kind,
			}

			result, _, err := handleScan(context.Background(), nil, input)
			require.NoError(t, err, "unknown kinds should produce empty results, not errors")

			text := result.Content[0].(*mcp.TextContent).Text
			assert.True(t, json.Valid([]byte(text)), "output should be valid JSON")

			// Unknown kind should filter out all signals.
			// The kind filter in handleScan lowercases the kind and matches against signal.Kind.
			// Non-matching kinds should produce empty (but valid) output.
			if strings.TrimSpace(tt.kind) != "" {
				// Parse the JSON to verify no signals leaked through.
				var signals []map[string]any
				if err := json.Unmarshal([]byte(text), &signals); err == nil {
					assert.Empty(t, signals,
						"unknown kind %q should not match any signals", tt.kind)
				}
			}
		})
	}
}
