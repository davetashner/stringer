// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time interface check for JSONFormatter.
var _ Formatter = (*JSONFormatter)(nil)

func TestJSONFormatterName(t *testing.T) {
	f := NewJSONFormatter()
	assert.Equal(t, "json", f.Name())
}

func TestJSONFormatter_Registration(t *testing.T) {
	// Ensure json formatter can be registered and retrieved.
	resetFmtForTesting()
	defer restoreFormatters()

	RegisterFormatter(NewJSONFormatter())
	f, err := GetFormatter("json")
	require.NoError(t, err)
	assert.Equal(t, "json", f.Name())
}

func TestJSONFormatter_EmptySignals(t *testing.T) {
	f := newTestJSONFormatter()

	t.Run("nil_signals", func(t *testing.T) {
		var buf bytes.Buffer
		err := f.Format(nil, &buf)
		require.NoError(t, err)

		var envelope JSONEnvelope
		require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))

		assert.Empty(t, envelope.Signals)
		assert.Equal(t, 0, envelope.Metadata.TotalCount)
		assert.Empty(t, envelope.Metadata.Collectors)
	})

	t.Run("empty_slice", func(t *testing.T) {
		var buf bytes.Buffer
		err := f.Format([]signal.RawSignal{}, &buf)
		require.NoError(t, err)

		var envelope JSONEnvelope
		require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))

		assert.Empty(t, envelope.Signals)
		assert.Equal(t, 0, envelope.Metadata.TotalCount)
	})
}

func TestJSONFormatter_SingleSignal(t *testing.T) {
	f := newTestJSONFormatter()
	sig := testSignal()

	var buf bytes.Buffer
	err := f.Format([]signal.RawSignal{sig}, &buf)
	require.NoError(t, err)

	var envelope JSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))

	require.Len(t, envelope.Signals, 1)
	assert.Equal(t, "Add rate limiting", envelope.Signals[0].Title)
	assert.Equal(t, "todos", envelope.Signals[0].Source)
	assert.Equal(t, "todo", envelope.Signals[0].Kind)
	assert.Equal(t, "internal/server/handler.go", envelope.Signals[0].FilePath)
	assert.Equal(t, 42, envelope.Signals[0].Line)
	assert.Equal(t, "alice", envelope.Signals[0].Author)
	assert.InDelta(t, 0.85, envelope.Signals[0].Confidence, 0.001)
	assert.Equal(t, []string{"security", "performance"}, envelope.Signals[0].Tags)
}

func TestJSONFormatter_MultipleSignals(t *testing.T) {
	f := newTestJSONFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Task A", FilePath: "a.go", Line: 1, Confidence: 0.9},
		{Source: "gitlog", Kind: "fixme", Title: "Task B", FilePath: "b.go", Line: 2, Confidence: 0.7},
		{Source: "todos", Kind: "hack", Title: "Task C", FilePath: "c.go", Line: 3, Confidence: 0.5},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	var envelope JSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))

	assert.Len(t, envelope.Signals, 3)
	assert.Equal(t, "Task A", envelope.Signals[0].Title)
	assert.Equal(t, "Task B", envelope.Signals[1].Title)
	assert.Equal(t, "Task C", envelope.Signals[2].Title)
}

func TestJSONFormatter_Metadata(t *testing.T) {
	fixedTime := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	f := &JSONFormatter{
		nowFunc: func() time.Time { return fixedTime },
	}

	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Task A"},
		{Source: "gitlog", Kind: "fixme", Title: "Task B"},
		{Source: "todos", Kind: "hack", Title: "Task C"},
		{Source: "patterns", Kind: "todo", Title: "Task D"},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	var envelope JSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))

	assert.Equal(t, 4, envelope.Metadata.TotalCount)
	assert.Equal(t, "2026-02-07T12:00:00Z", envelope.Metadata.GeneratedAt)

	// Collectors should be sorted and deduplicated.
	assert.Equal(t, []string{"gitlog", "patterns", "todos"}, envelope.Metadata.Collectors)
}

func TestJSONFormatter_MetadataNoSource(t *testing.T) {
	f := newTestJSONFormatter()

	signals := []signal.RawSignal{
		{Kind: "todo", Title: "No source signal"},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	var envelope JSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))

	assert.Equal(t, 1, envelope.Metadata.TotalCount)
	assert.Empty(t, envelope.Metadata.Collectors)
}

func TestJSONFormatter_GeneratedAtIsUTC(t *testing.T) {
	eastern := time.FixedZone("EST", -5*60*60)
	f := &JSONFormatter{
		nowFunc: func() time.Time {
			return time.Date(2026, 3, 15, 17, 0, 0, 0, eastern)
		},
	}

	var buf bytes.Buffer
	err := f.Format([]signal.RawSignal{}, &buf)
	require.NoError(t, err)

	var envelope JSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))

	assert.Equal(t, "2026-03-15T22:00:00Z", envelope.Metadata.GeneratedAt)
}

func TestJSONFormatter_PrettyPrintDefault(t *testing.T) {
	f := newTestJSONFormatter()

	var buf bytes.Buffer
	err := f.Format([]signal.RawSignal{}, &buf)
	require.NoError(t, err)

	output := buf.String()
	// Pretty-printed JSON should contain newlines and indentation.
	assert.Contains(t, output, "\n")
	assert.Contains(t, output, "  ")
}

func TestJSONFormatter_CompactMode(t *testing.T) {
	f := &JSONFormatter{
		Compact: true,
		nowFunc: fixedNow,
	}

	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Task A"},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	// Compact JSON should be a single line plus trailing newline.
	lines := countLines(output)
	assert.Equal(t, 1, lines, "compact output should be a single line (plus trailing newline)")

	// Should still be valid JSON.
	var envelope JSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))
	assert.Len(t, envelope.Signals, 1)
}

func TestJSONFormatter_PrettyVsCompactContent(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Compare", FilePath: "main.go", Line: 1, Confidence: 0.8},
	}

	prettyFmt := &JSONFormatter{Compact: false, nowFunc: fixedNow}
	compactFmt := &JSONFormatter{Compact: true, nowFunc: fixedNow}

	var prettyBuf, compactBuf bytes.Buffer
	require.NoError(t, prettyFmt.Format(signals, &prettyBuf))
	require.NoError(t, compactFmt.Format(signals, &compactBuf))

	// Both should parse to the same structure.
	var prettyEnv, compactEnv JSONEnvelope
	require.NoError(t, json.Unmarshal(prettyBuf.Bytes(), &prettyEnv))
	require.NoError(t, json.Unmarshal(compactBuf.Bytes(), &compactEnv))

	assert.Equal(t, prettyEnv.Signals, compactEnv.Signals)
	assert.Equal(t, prettyEnv.Metadata, compactEnv.Metadata)

	// Pretty should be longer than compact.
	assert.Greater(t, prettyBuf.Len(), compactBuf.Len())
}

func TestJSONFormatter_ValidJSON(t *testing.T) {
	f := newTestJSONFormatter()
	signals := []signal.RawSignal{
		testSignal(),
		{
			Source:     "gitlog",
			Kind:       "fixme",
			FilePath:   "main.go",
			Line:       10,
			Title:      "Fix broken test",
			Confidence: 0.5,
			Timestamp:  time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)
	assert.True(t, json.Valid(buf.Bytes()), "output should be valid JSON")
}

func TestJSONFormatter_OutputIsJSONArray(t *testing.T) {
	f := newTestJSONFormatter()
	signals := []signal.RawSignal{testSignal()}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	// Output should start with '{' (envelope object), not '[' (array).
	output := buf.String()
	assert.True(t, len(output) > 0 && output[0] == '{', "output should start with '{'")
}

func TestJSONFormatter_TrailingNewline(t *testing.T) {
	f := newTestJSONFormatter()

	var buf bytes.Buffer
	err := f.Format([]signal.RawSignal{}, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.True(t, len(output) > 0 && output[len(output)-1] == '\n',
		"output should end with a trailing newline")
}

func TestJSONFormatter_InjectionSafe(t *testing.T) {
	f := newTestJSONFormatter()

	injectionSignals := []signal.RawSignal{
		{
			Source:     "todos",
			Kind:       "todo",
			Title:      `Evil","injected":"true`,
			FilePath:   "main.go",
			Line:       1,
			Confidence: 0.5,
		},
		{
			Source:      "todos",
			Kind:        "todo",
			Title:       "Normal title",
			Description: "Description with\nnewlines\nand \"quotes\" and \\backslashes",
			FilePath:    "main.go",
			Line:        2,
			Confidence:  0.5,
		},
		{
			Source:     "todos",
			Kind:       "todo",
			Title:      `<script>alert("xss")</script>`,
			FilePath:   "index.html",
			Line:       3,
			Confidence: 0.5,
		},
		{
			Source:     "todos",
			Kind:       "todo",
			Title:      "Null bytes: \x00\x01\x02",
			FilePath:   "binary.go",
			Line:       4,
			Confidence: 0.5,
		},
	}

	var buf bytes.Buffer
	err := f.Format(injectionSignals, &buf)
	require.NoError(t, err)

	// Must be valid JSON.
	var envelope JSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))

	assert.Len(t, envelope.Signals, 4)

	// The injection attempt should be a literal string value, not parsed as JSON structure.
	assert.Equal(t, `Evil","injected":"true`, envelope.Signals[0].Title)
}

func TestJSONFormatter_RoundTrip(t *testing.T) {
	f := newTestJSONFormatter()
	original := testSignal()

	var buf bytes.Buffer
	require.NoError(t, f.Format([]signal.RawSignal{original}, &buf))

	var envelope JSONEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))

	require.Len(t, envelope.Signals, 1)
	got := envelope.Signals[0]

	assert.Equal(t, original.Source, got.Source)
	assert.Equal(t, original.Kind, got.Kind)
	assert.Equal(t, original.FilePath, got.FilePath)
	assert.Equal(t, original.Line, got.Line)
	assert.Equal(t, original.Title, got.Title)
	assert.Equal(t, original.Description, got.Description)
	assert.Equal(t, original.Author, got.Author)
	assert.True(t, original.Timestamp.Equal(got.Timestamp))
	assert.InDelta(t, original.Confidence, got.Confidence, 0.001)
	assert.Equal(t, original.Tags, got.Tags)
}

func TestJSONFormatter_WriteFailure(t *testing.T) {
	f := newTestJSONFormatter()
	signals := []signal.RawSignal{
		{Source: "test", Kind: "todo", Title: "Task", FilePath: "a.go", Confidence: 0.5},
	}

	t.Run("fail_on_data_write", func(t *testing.T) {
		w := &failWriter{failAfter: 0}
		err := f.Format(signals, w)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "write json")
	})

	t.Run("fail_on_newline_write", func(t *testing.T) {
		w := &failWriter{failAfter: 1}
		err := f.Format(signals, w)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "write json trailing newline")
	})
}

func TestJSONFormatter_ShouldCompact(t *testing.T) {
	t.Run("compact_true_always_compact", func(t *testing.T) {
		f := &JSONFormatter{Compact: true}
		var buf bytes.Buffer
		assert.True(t, f.shouldCompact(&buf))
	})

	t.Run("non_file_writer_defaults_pretty", func(t *testing.T) {
		f := &JSONFormatter{Compact: false}
		var buf bytes.Buffer
		assert.False(t, f.shouldCompact(&buf))
	})
}

func TestJSONFormatter_AutoDetectPipe(t *testing.T) {
	// Create a pipe — the write end is not a TTY.
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()

	f := &JSONFormatter{Compact: false, nowFunc: fixedNow}
	// Pipe should be detected as non-TTY -> compact.
	assert.True(t, f.shouldCompact(w))
}

func TestExtractCollectors(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		result := extractCollectors(nil)
		assert.Empty(t, result)
	})

	t.Run("single_source", func(t *testing.T) {
		signals := []signal.RawSignal{{Source: "todos"}}
		result := extractCollectors(signals)
		assert.Equal(t, []string{"todos"}, result)
	})

	t.Run("deduplication", func(t *testing.T) {
		signals := []signal.RawSignal{
			{Source: "todos"},
			{Source: "todos"},
			{Source: "todos"},
		}
		result := extractCollectors(signals)
		assert.Equal(t, []string{"todos"}, result)
	})

	t.Run("sorted_output", func(t *testing.T) {
		signals := []signal.RawSignal{
			{Source: "zebra"},
			{Source: "alpha"},
			{Source: "middle"},
		}
		result := extractCollectors(signals)
		assert.Equal(t, []string{"alpha", "middle", "zebra"}, result)
	})

	t.Run("empty_source_excluded", func(t *testing.T) {
		signals := []signal.RawSignal{
			{Source: "todos"},
			{Source: ""},
			{Source: "gitlog"},
		}
		result := extractCollectors(signals)
		assert.Equal(t, []string{"gitlog", "todos"}, result)
	})
}

// --- Helpers ---

// fixedNow returns a deterministic time for testing.
func fixedNow() time.Time {
	return time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
}

// newTestJSONFormatter creates a JSONFormatter with a fixed time for deterministic tests.
func newTestJSONFormatter() *JSONFormatter {
	return &JSONFormatter{nowFunc: fixedNow}
}

// countLines counts the number of non-empty lines in a string.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	count := 0
	for i := range s {
		if s[i] == '\n' {
			count++
		}
	}
	// If the string doesn't end with newline, the last line still counts.
	if s[len(s)-1] != '\n' {
		count++
	}
	return count
}

// errWriter always returns an error on Write.
type errWriter struct{}

func (e *errWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write error")
}

func TestJSONFormatter_WriteError(t *testing.T) {
	f := newTestJSONFormatter()
	err := f.Format([]signal.RawSignal{}, &errWriter{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write json")
}
