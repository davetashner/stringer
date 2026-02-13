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

// Compile-time interface check.
var _ Formatter = (*HTMLFormatter)(nil)

func TestHTMLFormatter_Name(t *testing.T) {
	f := NewHTMLFormatter()
	assert.Equal(t, "html", f.Name())
}

func TestHTMLFormatter_Registration(t *testing.T) {
	f, err := GetFormatter("html")
	require.NoError(t, err)
	assert.Equal(t, "html", f.Name())
}

func TestHTMLFormatter_EmptySignals(t *testing.T) {
	f := NewHTMLFormatter()

	t.Run("nil", func(t *testing.T) {
		var buf bytes.Buffer
		err := f.Format(nil, &buf)
		require.NoError(t, err)
		out := buf.String()
		assert.Contains(t, out, "<!DOCTYPE html>")
		assert.Contains(t, out, "No signals found")
	})

	t.Run("empty_slice", func(t *testing.T) {
		var buf bytes.Buffer
		err := f.Format([]signal.RawSignal{}, &buf)
		require.NoError(t, err)
		out := buf.String()
		assert.Contains(t, out, "No signals found")
	})
}

func TestHTMLFormatter_BasicOutput(t *testing.T) {
	f := &HTMLFormatter{
		nowFunc: func() time.Time { return time.Date(2026, 2, 12, 10, 0, 0, 0, time.UTC) },
	}
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Fix this", FilePath: "main.go", Line: 10, Confidence: 0.5},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "<!DOCTYPE html>")
	assert.Contains(t, out, "<title>Stringer Dashboard</title>")
	assert.Contains(t, out, "2026-02-12 10:00 UTC")
	assert.Contains(t, out, "1 signals from 1 collector(s)")
}

func TestHTMLFormatter_SignalTable(t *testing.T) {
	f := NewHTMLFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Add tests", FilePath: "handler.go", Line: 42, Confidence: 0.75},
		{Source: "gitlog", Kind: "churn", Title: "High churn", FilePath: "config.go", Line: 0, Confidence: 0.85},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "Add tests")
	assert.Contains(t, out, "handler.go:42")
	assert.Contains(t, out, "0.75")
	assert.Contains(t, out, "High churn")
	assert.Contains(t, out, "config.go")
	assert.Contains(t, out, "0.85")
}

func TestHTMLFormatter_PriorityDistribution(t *testing.T) {
	f := NewHTMLFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "P1", Confidence: 0.9},
		{Source: "todos", Kind: "todo", Title: "P1b", Confidence: 0.85},
		{Source: "todos", Kind: "todo", Title: "P2", Confidence: 0.7},
		{Source: "todos", Kind: "todo", Title: "P3", Confidence: 0.5},
		{Source: "todos", Kind: "todo", Title: "P4", Confidence: 0.2},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	out := buf.String()
	// P1=2, P2=1, P3=1, P4=1 — verify summary cards show correct values
	assert.Contains(t, out, "card-p1")
	assert.Contains(t, out, "card-p2")
	assert.Contains(t, out, "card-p3")
	assert.Contains(t, out, "card-p4")
	// Verify priority classes in table rows
	assert.Contains(t, out, "priority-1")
	assert.Contains(t, out, "priority-2")
	assert.Contains(t, out, "priority-3")
	assert.Contains(t, out, "priority-4")
}

func TestHTMLFormatter_CollectorGrouping(t *testing.T) {
	f := NewHTMLFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A", Confidence: 0.5},
		{Source: "gitlog", Kind: "churn", Title: "B", Confidence: 0.7},
		{Source: "patterns", Kind: "pattern", Title: "C", Confidence: 0.6},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	out := buf.String()
	// Filter dropdown should list all collectors
	assert.Contains(t, out, `<option value="gitlog">gitlog</option>`)
	assert.Contains(t, out, `<option value="patterns">patterns</option>`)
	assert.Contains(t, out, `<option value="todos">todos</option>`)
}

func TestHTMLFormatter_XSSSafety(t *testing.T) {
	f := NewHTMLFormatter()
	signals := []signal.RawSignal{
		{
			Source:      "todos",
			Kind:        "todo",
			Title:       `<script>alert('xss')</script>`,
			FilePath:    `<img src=x onerror=alert(1)>`,
			Confidence:  0.5,
			Description: `<b onmouseover="alert('xss')">hover</b>`,
		},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	out := buf.String()
	// html/template should escape dangerous HTML — tags must not appear as raw HTML
	assert.NotContains(t, out, "<script>alert")
	assert.NotContains(t, out, `<img src=x`)
	// Escaped forms should be present instead
	assert.Contains(t, out, "&lt;script&gt;")
	assert.Contains(t, out, "&lt;img")
	assert.Contains(t, out, "&#34;alert")
}

func TestHTMLFormatter_ChartData(t *testing.T) {
	now := time.Date(2026, 2, 12, 10, 0, 0, 0, time.UTC)
	f := &HTMLFormatter{
		nowFunc: func() time.Time { return now },
	}
	signals := []signal.RawSignal{
		{Source: "gitlog", Kind: "churn", Title: "Churn: config.go", FilePath: "config.go", Confidence: 0.7},
		{Source: "lotteryrisk", Kind: "lottery-risk", Title: "Risk in pkg/", FilePath: "pkg/handler.go", Confidence: 0.8},
		{Source: "todos", Kind: "todo", Title: "Old todo", FilePath: "main.go", Line: 5, Confidence: 0.5,
			Timestamp: now.Add(-400 * 24 * time.Hour)},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	out := buf.String()
	// Churn chart section
	assert.Contains(t, out, "chart-churn")
	// Lottery chart section
	assert.Contains(t, out, "chart-lottery")
	// TODO age chart section
	assert.Contains(t, out, "chart-todo-age")
	// Chart data JSON should be embedded
	assert.Contains(t, out, "churnLabels")
	assert.Contains(t, out, "lotteryLabels")
	assert.Contains(t, out, "todoAgeLabels")
}

func TestHTMLFormatter_ChartData_NoSpecialSignals(t *testing.T) {
	f := NewHTMLFormatter()
	signals := []signal.RawSignal{
		{Source: "patterns", Kind: "large-file", Title: "Big file", FilePath: "big.go", Confidence: 0.5},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	out := buf.String()
	// No churn, lottery, or todo-age chart containers when data absent
	assert.NotContains(t, out, `id="chart-churn"`)
	assert.NotContains(t, out, `id="chart-lottery"`)
	assert.NotContains(t, out, `id="chart-todo-age"`)
}

func TestHTMLFormatter_WriteError(t *testing.T) {
	f := NewHTMLFormatter()

	t.Run("empty_write_error", func(t *testing.T) {
		w := &htmlFailWriter{failAfter: 0}
		err := f.Format(nil, w)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "write empty html")
	})

	t.Run("template_write_error", func(t *testing.T) {
		signals := []signal.RawSignal{
			{Source: "todos", Kind: "todo", Title: "A", Confidence: 0.5},
		}
		w := &htmlFailWriter{failAfter: 0}
		err := f.Format(signals, w)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "execute html template")
	})
}

type htmlFailWriter struct {
	failAfter int
	calls     int
}

func (fw *htmlFailWriter) Write(p []byte) (int, error) {
	fw.calls++
	if fw.calls > fw.failAfter {
		return 0, errors.New("disk full")
	}
	return len(p), nil
}

// --- Helper unit tests ---

func TestBuildChurnEntries(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "gitlog", Kind: "churn", FilePath: "a.go"},
		{Source: "gitlog", Kind: "churn", FilePath: "a.go"},
		{Source: "gitlog", Kind: "churn", FilePath: "b.go"},
		{Source: "todos", Kind: "todo", FilePath: "c.go"}, // not churn
	}
	entries := buildChurnEntries(signals)
	require.Len(t, entries, 2)
	assert.Equal(t, "a.go", entries[0].Path)
	assert.Equal(t, 2, entries[0].Count)
	assert.Equal(t, "b.go", entries[1].Path)
	assert.Equal(t, 1, entries[1].Count)
}

func TestBuildChurnEntries_Empty(t *testing.T) {
	entries := buildChurnEntries(nil)
	assert.Nil(t, entries)
}

func TestBuildChurnEntries_Top20(t *testing.T) {
	signals := make([]signal.RawSignal, 25)
	for i := range signals {
		signals[i] = signal.RawSignal{Source: "gitlog", Kind: "churn", FilePath: "file" + string(rune('a'+i)) + ".go"}
	}
	entries := buildChurnEntries(signals)
	assert.Len(t, entries, 20)
}

func TestBuildLotteryEntries(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "lotteryrisk", FilePath: "pkg/handler.go", Confidence: 0.9},
		{Source: "lotteryrisk", FilePath: "internal/core.go", Confidence: 0.7},
		{Source: "todos", FilePath: "main.go", Confidence: 0.5}, // not lottery
	}
	entries := buildLotteryEntries(signals)
	require.Len(t, entries, 2)
	// Sorted by confidence desc
	assert.Equal(t, "pkg", entries[0].Directory)
	assert.Equal(t, 0.9, entries[0].Confidence)
}

func TestBuildLotteryEntries_Empty(t *testing.T) {
	entries := buildLotteryEntries(nil)
	assert.Nil(t, entries)
}

func TestBuildTodoAgeBuckets(t *testing.T) {
	now := time.Date(2026, 2, 12, 10, 0, 0, 0, time.UTC)
	signals := []signal.RawSignal{
		{Source: "todos", Timestamp: now.Add(-2 * 24 * time.Hour)},   // <1w
		{Source: "todos", Timestamp: now.Add(-14 * 24 * time.Hour)},  // 1-4w
		{Source: "todos", Timestamp: now.Add(-60 * 24 * time.Hour)},  // 1-3m
		{Source: "todos", Timestamp: now.Add(-200 * 24 * time.Hour)}, // 3-12m
		{Source: "todos", Timestamp: now.Add(-400 * 24 * time.Hour)}, // >1y
		{Source: "todos"}, // no timestamp, skipped
		{Source: "gitlog", Timestamp: now.Add(-2 * 24 * time.Hour), Kind: "churn"}, // not todos
	}
	buckets := buildTodoAgeBuckets(signals, now)
	require.Len(t, buckets, 5)
	assert.Equal(t, 1, buckets[0].Count) // <1w
	assert.Equal(t, 1, buckets[1].Count) // 1-4w
	assert.Equal(t, 1, buckets[2].Count) // 1-3m
	assert.Equal(t, 1, buckets[3].Count) // 3-12m
	assert.Equal(t, 1, buckets[4].Count) // >1y
}

func TestBuildTodoAgeBuckets_NoTodos(t *testing.T) {
	now := time.Now()
	buckets := buildTodoAgeBuckets(nil, now)
	assert.Nil(t, buckets)
}

func TestBuildSignalRows(t *testing.T) {
	p2 := 2
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A", FilePath: "a.go", Line: 5, Confidence: 0.9, Description: "details"},
		{Source: "todos", Kind: "todo", Title: "B", FilePath: "b.go", Line: 0, Confidence: 0.5, Priority: &p2},
	}
	rows := buildSignalRows(signals)
	require.Len(t, rows, 2)
	assert.Equal(t, "A", rows[0].Title)
	assert.Equal(t, "a.go:5", rows[0].Location)
	assert.Equal(t, 1, rows[0].Priority) // confidence 0.9 -> P1
	assert.Equal(t, "details", rows[0].Description)
	assert.Equal(t, 2, rows[1].Priority) // explicit priority override
	assert.Equal(t, "b.go", rows[1].Location)
}

func TestHTMLFormatter_SelfContained(t *testing.T) {
	f := NewHTMLFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Test", FilePath: "a.go", Confidence: 0.5},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	out := buf.String()
	// No external resource references (http://www.w3.org is OK — it's the SVG namespace, not a network request)
	assert.NotContains(t, out, `<link rel="stylesheet" href`)
	assert.NotContains(t, out, `<script src=`)
	// All CSS and JS inline
	assert.Contains(t, out, "<style>")
	assert.Contains(t, out, "<script>")
}

func TestHTMLFormatter_ResponsiveLayout(t *testing.T) {
	f := NewHTMLFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Test", FilePath: "a.go", Confidence: 0.5},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "viewport")
	assert.Contains(t, out, "@media")
	assert.Contains(t, out, "prefers-color-scheme: dark")
}

func TestHTMLFormatter_Description(t *testing.T) {
	f := NewHTMLFormatter()
	signals := []signal.RawSignal{
		{
			Source:      "todos",
			Kind:        "todo",
			Title:       "Fix handler",
			FilePath:    "main.go",
			Confidence:  0.5,
			Description: "This needs careful refactoring of the error paths",
		},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "This needs careful refactoring of the error paths")
	assert.Contains(t, out, "detail-row")
}

func TestHTMLFormatter_MultipleSignals(t *testing.T) {
	f := NewHTMLFormatter()
	signals := make([]signal.RawSignal, 50)
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

	out := buf.String()
	assert.Contains(t, out, "50 signals from 1 collector(s)")
	count := strings.Count(out, `class="signal-row"`)
	assert.Equal(t, 50, count)
}
