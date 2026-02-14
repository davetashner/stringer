// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package output

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time interface check for MarkdownFormatter.
var _ Formatter = (*MarkdownFormatter)(nil)

func TestMarkdownFormatterName(t *testing.T) {
	f := NewMarkdownFormatter()
	assert.Equal(t, "markdown", f.Name())
}

// --- Registration ---

func TestMarkdownFormatter_RegisteredViaInit(t *testing.T) {
	f, err := GetFormatter("markdown")
	require.NoError(t, err)
	assert.Equal(t, "markdown", f.Name())
}

// --- Empty input ---

func TestMarkdownFormat_EmptySignals(t *testing.T) {
	f := NewMarkdownFormatter()

	t.Run("nil", func(t *testing.T) {
		var buf bytes.Buffer
		err := f.Format(nil, &buf)
		require.NoError(t, err)
		assert.Empty(t, buf.String())
	})

	t.Run("empty_slice", func(t *testing.T) {
		var buf bytes.Buffer
		err := f.Format([]signal.RawSignal{}, &buf)
		require.NoError(t, err)
		assert.Empty(t, buf.String())
	})
}

// --- Header and summary ---

func TestMarkdownFormat_Header(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Fix this", FilePath: "main.go", Line: 1, Confidence: 0.5},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.True(t, strings.HasPrefix(output, "# Stringer Scan Results\n"),
		"output should start with heading, got: %s", output)
}

func TestMarkdownFormat_SummaryLine(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A", FilePath: "a.go", Line: 1, Confidence: 0.5},
		{Source: "todos", Kind: "todo", Title: "B", FilePath: "b.go", Line: 2, Confidence: 0.5},
		{Source: "gitlog", Kind: "churn", Title: "C", FilePath: "c.go", Line: 3, Confidence: 0.7},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "**Total signals:** 3")
	assert.Contains(t, output, "**Collectors:** gitlog, todos")
}

// --- Priority distribution table ---

func TestMarkdownFormat_PriorityTable(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "P1", FilePath: "a.go", Line: 1, Confidence: 0.9},
		{Source: "todos", Kind: "todo", Title: "P1b", FilePath: "a.go", Line: 2, Confidence: 0.85},
		{Source: "todos", Kind: "todo", Title: "P2", FilePath: "b.go", Line: 1, Confidence: 0.7},
		{Source: "todos", Kind: "todo", Title: "P3", FilePath: "c.go", Line: 1, Confidence: 0.5},
		{Source: "todos", Kind: "todo", Title: "P3b", FilePath: "c.go", Line: 2, Confidence: 0.45},
		{Source: "todos", Kind: "todo", Title: "P4", FilePath: "d.go", Line: 1, Confidence: 0.2},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "| Priority | Count |")
	assert.Contains(t, output, "|----------|-------|")
	assert.Contains(t, output, "| P1       | 2     |")
	assert.Contains(t, output, "| P2       | 1     |")
	assert.Contains(t, output, "| P3       | 2     |")
	assert.Contains(t, output, "| P4       | 1     |")
}

func TestMarkdownFormat_PriorityTable_AllZeros(t *testing.T) {
	f := NewMarkdownFormatter()
	// All signals have confidence < 0.4 -> all P4
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Low", FilePath: "a.go", Line: 1, Confidence: 0.1},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "| P1       | 0     |")
	assert.Contains(t, output, "| P2       | 0     |")
	assert.Contains(t, output, "| P3       | 0     |")
	assert.Contains(t, output, "| P4       | 1     |")
}

// --- Collector grouping ---

func TestMarkdownFormat_GroupedByCollector(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A", FilePath: "a.go", Line: 1, Confidence: 0.5},
		{Source: "gitlog", Kind: "churn", Title: "B", FilePath: "b.go", Line: 1, Confidence: 0.7},
		{Source: "todos", Kind: "fixme", Title: "C", FilePath: "c.go", Line: 5, Confidence: 0.6},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "## gitlog (1 signals)")
	assert.Contains(t, output, "## todos (2 signals)")

	// gitlog section should appear before todos (alphabetical)
	gitlogIdx := strings.Index(output, "## gitlog")
	todosIdx := strings.Index(output, "## todos")
	assert.True(t, gitlogIdx < todosIdx,
		"gitlog section should appear before todos (alphabetical order)")
}

func TestMarkdownFormat_SingleCollector(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Only one collector", FilePath: "a.go", Line: 1, Confidence: 0.5},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "## todos (1 signals)")
	assert.Contains(t, output, "**Collectors:** todos")
}

// --- Signal formatting ---

func TestMarkdownFormat_SignalLine(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{
			Source:     "todos",
			Kind:       "todo",
			Title:      "Add rate limiting",
			FilePath:   "internal/server/handler.go",
			Line:       42,
			Confidence: 0.85,
		},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "- **Add rate limiting** — `internal/server/handler.go:42` (confidence: 0.85)")
}

func TestMarkdownFormat_SignalLine_NoLine(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{
			Source:     "patterns",
			Kind:       "churn",
			Title:      "Large file detected",
			FilePath:   "config.go",
			Line:       0,
			Confidence: 0.70,
		},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "- **Large file detected** — `config.go` (confidence: 0.70)")
}

func TestMarkdownFormat_SignalLine_NoFilePath(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{
			Source:     "todos",
			Kind:       "todo",
			Title:      "No file info",
			Confidence: 0.50,
		},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "- **No file info** — `unknown` (confidence: 0.50)")
}

// --- Location formatting ---

func TestFormatLocation(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		line     int
		want     string
	}{
		{"file_and_line", "main.go", 42, "main.go:42"},
		{"file_only", "main.go", 0, "main.go"},
		{"no_file", "", 0, "unknown"},
		{"no_file_with_line", "", 10, "unknown"},
		{"nested_path", "internal/server/handler.go", 100, "internal/server/handler.go:100"},
		{"line_one", "main.go", 1, "main.go:1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatLocation(tc.filePath, tc.line)
			assert.Equal(t, tc.want, got)
		})
	}
}

// --- Empty source falls back to "unknown" ---

func TestMarkdownFormat_EmptySourceFallback(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "", Kind: "todo", Title: "No source", FilePath: "a.go", Line: 1, Confidence: 0.5},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "## unknown (1 signals)")
}

// --- groupByCollector ---

func TestGroupByCollector(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Title: "A"},
		{Source: "gitlog", Title: "B"},
		{Source: "todos", Title: "C"},
		{Source: "patterns", Title: "D"},
		{Source: "gitlog", Title: "E"},
	}

	groups := groupByCollector(signals)
	assert.Len(t, groups, 3)
	assert.Len(t, groups["todos"], 2)
	assert.Len(t, groups["gitlog"], 2)
	assert.Len(t, groups["patterns"], 1)
}

func TestGroupByCollector_EmptySource(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "", Title: "A"},
		{Source: "", Title: "B"},
	}

	groups := groupByCollector(signals)
	assert.Len(t, groups, 1)
	assert.Len(t, groups["unknown"], 2)
}

// --- sortedCollectorNames ---

func TestSortedCollectorNames(t *testing.T) {
	groups := map[string][]signal.RawSignal{
		"todos":    {{Title: "A"}},
		"gitlog":   {{Title: "B"}},
		"patterns": {{Title: "C"}},
	}

	names := sortedCollectorNames(groups)
	assert.Equal(t, []string{"gitlog", "patterns", "todos"}, names)
}

// --- priorityDistribution ---

func TestPriorityDistribution(t *testing.T) {
	signals := []signal.RawSignal{
		{Confidence: 0.9},  // P1
		{Confidence: 0.85}, // P1
		{Confidence: 0.8},  // P1
		{Confidence: 0.7},  // P2
		{Confidence: 0.6},  // P2
		{Confidence: 0.5},  // P3
		{Confidence: 0.4},  // P3
		{Confidence: 0.3},  // P4
		{Confidence: 0.0},  // P4
	}

	dist := priorityDistribution(signals)
	assert.Equal(t, [4]int{3, 2, 2, 2}, dist)
}

func TestPriorityDistribution_Empty(t *testing.T) {
	dist := priorityDistribution(nil)
	assert.Equal(t, [4]int{0, 0, 0, 0}, dist)
}

// --- Full output structure test ---

func TestMarkdownFormat_FullOutput(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{
			Source:     "todos",
			Kind:       "todo",
			Title:      "Add proper CLI argument parsing",
			FilePath:   "main.go",
			Line:       6,
			Confidence: 0.50,
			Timestamp:  time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		},
		{
			Source:     "todos",
			Kind:       "fixme",
			Title:      "This will panic on nil input",
			FilePath:   "main.go",
			Line:       9,
			Confidence: 0.60,
			Timestamp:  time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC),
		},
		{
			Source:     "gitlog",
			Kind:       "churn",
			Title:      "High churn: config.go",
			FilePath:   "config.go",
			Line:       1,
			Confidence: 0.70,
		},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()

	// Heading
	assert.Contains(t, output, "# Stringer Scan Results")

	// Summary
	assert.Contains(t, output, "**Total signals:** 3")
	assert.Contains(t, output, "**Collectors:** gitlog, todos")

	// Priority table
	assert.Contains(t, output, "| P1       | 0     |")
	assert.Contains(t, output, "| P2       | 2     |")
	assert.Contains(t, output, "| P3       | 1     |")
	assert.Contains(t, output, "| P4       | 0     |")

	// Collector sections in alphabetical order
	assert.Contains(t, output, "## gitlog (1 signals)")
	assert.Contains(t, output, "## todos (2 signals)")

	// Signal lines
	assert.Contains(t, output, "- **Add proper CLI argument parsing** — `main.go:6` (confidence: 0.50)")
	assert.Contains(t, output, "- **This will panic on nil input** — `main.go:9` (confidence: 0.60)")
	assert.Contains(t, output, "- **High churn: config.go** — `config.go:1` (confidence: 0.70)")
}

// --- Many collectors ---

func TestMarkdownFormat_ManyCollectors(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A", FilePath: "a.go", Line: 1, Confidence: 0.9},
		{Source: "gitlog", Kind: "churn", Title: "B", FilePath: "b.go", Line: 1, Confidence: 0.7},
		{Source: "patterns", Kind: "pattern", Title: "C", FilePath: "c.go", Line: 1, Confidence: 0.5},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "**Collectors:** gitlog, patterns, todos")
	assert.Contains(t, output, "## gitlog (1 signals)")
	assert.Contains(t, output, "## patterns (1 signals)")
	assert.Contains(t, output, "## todos (1 signals)")
}

// --- Write failure handling ---

// mdFailWriter is a writer that fails after a specified number of writes.
type mdFailWriter struct {
	failAfter int
	calls     int
}

func (fw *mdFailWriter) Write(p []byte) (int, error) {
	fw.calls++
	if fw.calls > fw.failAfter {
		return 0, errors.New("write failed")
	}
	return len(p), nil
}

func TestMarkdownFormat_WriteFailure_Header(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A", FilePath: "a.go", Line: 1, Confidence: 0.5},
	}

	// Fail on the very first write (the heading).
	w := &mdFailWriter{failAfter: 0}
	err := f.Format(signals, w)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

func TestMarkdownFormat_WriteFailure_Summary(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A", FilePath: "a.go", Line: 1, Confidence: 0.5},
	}

	// First write (heading) succeeds, second write (summary) fails.
	w := &mdFailWriter{failAfter: 1}
	err := f.Format(signals, w)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

func TestMarkdownFormat_WriteFailure_PriorityTable(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A", FilePath: "a.go", Line: 1, Confidence: 0.5},
	}

	// Heading (1) + summary (1) + priority table header (1) = fail on 3rd
	w := &mdFailWriter{failAfter: 2}
	err := f.Format(signals, w)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

func TestMarkdownFormat_WriteFailure_SignalLine(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A", FilePath: "a.go", Line: 1, Confidence: 0.5},
	}

	// heading(1) + summary(1) + table_header(1) + table_separator(1) + 4 prio rows(4) + table_trailing_newline(1) + section_heading(1) = fail on 11th
	w := &mdFailWriter{failAfter: 10}
	err := f.Format(signals, w)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

func TestMarkdownFormat_WriteFailure_PriorityTableSeparator(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A", FilePath: "a.go", Line: 1, Confidence: 0.5},
	}

	// heading(1) + summary(1) + table_header(1) + separator(1) = fail on 4th
	w := &mdFailWriter{failAfter: 3}
	err := f.Format(signals, w)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

func TestMarkdownFormat_WriteFailure_PriorityTableRow(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A", FilePath: "a.go", Line: 1, Confidence: 0.5},
	}

	// heading(1) + summary(1) + table_header(1) + separator(1) + row1(1) = fail on 5th
	w := &mdFailWriter{failAfter: 4}
	err := f.Format(signals, w)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

func TestMarkdownFormat_WriteFailure_PriorityTableTrailingNewline(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A", FilePath: "a.go", Line: 1, Confidence: 0.5},
	}

	// heading(1) + summary(1) + table_header(1) + separator(1) + 4 rows(4) + trailing_newline(1) = fail on 9th
	w := &mdFailWriter{failAfter: 8}
	err := f.Format(signals, w)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

func TestMarkdownFormat_WriteFailure_SectionEnd(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A", FilePath: "a.go", Line: 1, Confidence: 0.5},
	}

	// heading(1) + summary(1) + 7 prio writes + section_heading(1) + signal(1) + section_end(1) = 12
	// fail on 12th write (section trailing newline)
	w := &mdFailWriter{failAfter: 11}
	err := f.Format(signals, w)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write failed")
}

// --- Confidence formatting ---

func TestMarkdownFormat_ConfidenceFormatting(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A", FilePath: "a.go", Line: 1, Confidence: 1.0},
		{Source: "todos", Kind: "todo", Title: "B", FilePath: "b.go", Line: 1, Confidence: 0.0},
		{Source: "todos", Kind: "todo", Title: "C", FilePath: "c.go", Line: 1, Confidence: 0.123},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "(confidence: 1.00)")
	assert.Contains(t, output, "(confidence: 0.00)")
	assert.Contains(t, output, "(confidence: 0.12)")
}

// --- Special characters in titles ---

func TestMarkdownFormat_SpecialCharsInTitle(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Handle `nil` pointers", FilePath: "a.go", Line: 1, Confidence: 0.5},
		{Source: "todos", Kind: "todo", Title: "Fix **bold** issue", FilePath: "b.go", Line: 1, Confidence: 0.5},
		{Source: "todos", Kind: "todo", Title: "Check <html> escaping", FilePath: "c.go", Line: 1, Confidence: 0.5},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	// Titles are rendered as-is (no escaping for markdown) since they come from source code
	assert.Contains(t, output, "Handle `nil` pointers")
	assert.Contains(t, output, "Fix **bold** issue")
	assert.Contains(t, output, "Check <html> escaping")
}

// --- Multiple signals from same collector preserve order ---

func TestMarkdownFormat_PreservesSignalOrderWithinCollector(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "First", FilePath: "a.go", Line: 1, Confidence: 0.5},
		{Source: "todos", Kind: "todo", Title: "Second", FilePath: "b.go", Line: 2, Confidence: 0.5},
		{Source: "todos", Kind: "todo", Title: "Third", FilePath: "c.go", Line: 3, Confidence: 0.5},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	firstIdx := strings.Index(output, "First")
	secondIdx := strings.Index(output, "Second")
	thirdIdx := strings.Index(output, "Third")
	assert.True(t, firstIdx < secondIdx && secondIdx < thirdIdx,
		"signals should appear in original order within collector")
}

// --- Large number of signals ---

func TestMarkdownFormat_ManySignals(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := make([]signal.RawSignal, 100)
	for i := range signals {
		signals[i] = signal.RawSignal{
			Source:     "todos",
			Kind:       "todo",
			Title:      "Task",
			FilePath:   "file.go",
			Line:       i + 1,
			Confidence: 0.5,
		}
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "**Total signals:** 100")
	assert.Contains(t, output, "## todos (100 signals)")
	// Count the bullet points
	bulletCount := strings.Count(output, "- **Task**")
	assert.Equal(t, 100, bulletCount)
}

// --- Priority boundary values ---

func TestMarkdownFormat_PriorityBoundaries(t *testing.T) {
	f := NewMarkdownFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Exact 0.8", FilePath: "a.go", Line: 1, Confidence: 0.8},   // P1
		{Source: "todos", Kind: "todo", Title: "Just below", FilePath: "b.go", Line: 1, Confidence: 0.79}, // P2
		{Source: "todos", Kind: "todo", Title: "Exact 0.6", FilePath: "c.go", Line: 1, Confidence: 0.6},   // P2
		{Source: "todos", Kind: "todo", Title: "Just below", FilePath: "d.go", Line: 1, Confidence: 0.59}, // P3
		{Source: "todos", Kind: "todo", Title: "Exact 0.4", FilePath: "e.go", Line: 1, Confidence: 0.4},   // P3
		{Source: "todos", Kind: "todo", Title: "Just below", FilePath: "f.go", Line: 1, Confidence: 0.39}, // P4
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "| P1       | 1     |")
	assert.Contains(t, output, "| P2       | 2     |")
	assert.Contains(t, output, "| P3       | 2     |")
	assert.Contains(t, output, "| P4       | 1     |")
}
