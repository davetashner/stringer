package output

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time interface check for TasksFormatter.
var _ Formatter = (*TasksFormatter)(nil)

func TestTasksFormatterName(t *testing.T) {
	f := NewTasksFormatter()
	assert.Equal(t, "tasks", f.Name())
}

func TestTasksFormatter_Registration(t *testing.T) {
	resetFmtForTesting()
	defer restoreFormatters()

	RegisterFormatter(NewTasksFormatter())
	f, err := GetFormatter("tasks")
	require.NoError(t, err)
	assert.Equal(t, "tasks", f.Name())
}

func TestTasksFormatter_EmptySignals(t *testing.T) {
	f := newTestTasksFormatter()

	t.Run("nil_signals", func(t *testing.T) {
		var buf bytes.Buffer
		err := f.Format(nil, &buf)
		require.NoError(t, err)

		var envelope TasksEnvelope
		require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))

		assert.Empty(t, envelope.Tasks)
		assert.Equal(t, 0, envelope.Metadata.TotalCount)
		assert.Empty(t, envelope.Metadata.Collectors)
	})

	t.Run("empty_slice", func(t *testing.T) {
		var buf bytes.Buffer
		err := f.Format([]signal.RawSignal{}, &buf)
		require.NoError(t, err)

		var envelope TasksEnvelope
		require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))

		assert.Empty(t, envelope.Tasks)
		assert.Equal(t, 0, envelope.Metadata.TotalCount)
	})
}

func TestTasksFormatter_SubjectPrefixing(t *testing.T) {
	tests := []struct {
		name  string
		kind  string
		title string
		want  string
	}{
		{"todo_no_prefix", "todo", "fix parser", "TODO: fix parser"},
		{"fixme_no_prefix", "fixme", "broken auth", "BUG: broken auth"},
		{"bug_no_prefix", "bug", "null pointer", "BUG: null pointer"},
		{"hack_no_prefix", "hack", "temp workaround", "HACK: temp workaround"},
		{"xxx_no_prefix", "xxx", "needs rewrite", "HACK: needs rewrite"},
		{"churn", "churn", "high churn in main.go", "high churn in main.go"},
		{"large_file", "large_file", "config.json is 5MB", "config.json is 5MB"},
		{"revert", "revert", "reverted commit abc", "reverted commit abc"},
		// Dedup: title already starts with keyword — no double prefix.
		{"todo_already_prefixed", "todo", "TODO: fix parser", "TODO: fix parser"},
		{"bug_already_prefixed", "bug", "BUG: null pointer", "BUG: null pointer"},
		{"hack_already_prefixed", "hack", "HACK: temp workaround", "HACK: temp workaround"},
		// Word-boundary: "TODOIST" should NOT be treated as having TODO prefix.
		{"todo_false_positive", "todo", "TODOIST integration broken", "TODO: TODOIST integration broken"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := signal.RawSignal{Kind: tt.kind, Title: tt.title}
			got := subjectForSignal(s)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTasksFormatter_ActiveForm(t *testing.T) {
	tests := []struct {
		kind  string
		title string
		want  string
	}{
		{"fixme", "broken auth", "Fixing broken auth"},
		{"bug", "null pointer", "Fixing null pointer"},
		{"todo", "add tests", "Addressing add tests"},
		{"hack", "temp fix", "Addressing temp fix"},
		{"xxx", "needs rewrite", "Addressing needs rewrite"},
		{"churn", "high churn", "Investigating high churn"},
		{"revert", "reverted commit", "Investigating reverted commit"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			s := signal.RawSignal{Kind: tt.kind, Title: tt.title}
			got := activeFormForSignal(s)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTasksFormatter_ActiveFormCamelCase(t *testing.T) {
	f := newTestTasksFormatter()
	sig := signal.RawSignal{
		Source:     "todos",
		Kind:       "todo",
		Title:      "Add tests",
		FilePath:   "main.go",
		Line:       1,
		Confidence: 0.5,
	}

	var buf bytes.Buffer
	err := f.Format([]signal.RawSignal{sig}, &buf)
	require.NoError(t, err)

	// Verify the JSON uses camelCase "activeForm", not snake_case "active_form".
	output := buf.String()
	assert.Contains(t, output, `"activeForm"`)
	assert.NotContains(t, output, `"active_form"`)
}

func TestTasksFormatter_Description(t *testing.T) {
	t.Run("full_signal", func(t *testing.T) {
		s := signal.RawSignal{
			Description: "This needs fixing urgently",
			Source:      "todos",
			FilePath:    "main.go",
			Line:        42,
			Author:      "alice",
			Confidence:  0.85,
			Tags:        []string{"security", "performance"},
		}
		got := descriptionForSignal(s)
		assert.Contains(t, got, "This needs fixing urgently")
		assert.Contains(t, got, "Source: todos collector")
		assert.Contains(t, got, "File: main.go:42")
		assert.Contains(t, got, "Author: alice")
		assert.Contains(t, got, "Confidence: 85%")
		assert.Contains(t, got, "Priority: P1")
		assert.Contains(t, got, "Tags: security, performance")
	})

	t.Run("no_description", func(t *testing.T) {
		s := signal.RawSignal{
			Source:   "gitlog",
			FilePath: "handler.go",
		}
		got := descriptionForSignal(s)
		assert.Contains(t, got, "Source: gitlog collector")
		assert.Contains(t, got, "File: handler.go")
		assert.NotContains(t, got, "Confidence:")
		assert.NotContains(t, got, "Author:")
		assert.NotContains(t, got, "Tags:")
	})

	t.Run("file_without_line", func(t *testing.T) {
		s := signal.RawSignal{
			FilePath: "README.md",
		}
		got := descriptionForSignal(s)
		assert.Contains(t, got, "File: README.md")
		assert.NotContains(t, got, "README.md:")
	})

	t.Run("minimal_signal", func(t *testing.T) {
		s := signal.RawSignal{}
		// Should not panic — empty signal produces empty description.
		got := descriptionForSignal(s)
		assert.Empty(t, got)
	})

	t.Run("priority_mapping", func(t *testing.T) {
		tests := []struct {
			confidence float64
			wantP      string
		}{
			{0.85, "Priority: P1"},
			{0.65, "Priority: P2"},
			{0.45, "Priority: P3"},
			{0.2, "Priority: P4"},
		}
		for _, tt := range tests {
			s := signal.RawSignal{Confidence: tt.confidence}
			got := descriptionForSignal(s)
			assert.Contains(t, got, tt.wantP, "confidence %.2f should map to %s", tt.confidence, tt.wantP)
		}
	})
}

func TestTasksFormatter_Metadata(t *testing.T) {
	s := signal.RawSignal{
		Source:     "todos",
		Kind:       "todo",
		FilePath:   "main.go",
		Line:       10,
		Confidence: 0.9,
		Tags:       []string{"security", "perf"},
		Author:     "alice",
		Timestamp:  time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	m := metadataForSignal(s)
	assert.Equal(t, "todo", m["kind"])
	assert.Equal(t, "todos", m["collector"])
	assert.Equal(t, "main.go", m["file_path"])
	assert.Equal(t, "10", m["line"])
	assert.Equal(t, "0.90", m["confidence"])
	assert.Equal(t, "security,perf", m["tags"])
	assert.Equal(t, "alice", m["author"])
	assert.Equal(t, "2026-01-15T10:30:00Z", m["timestamp"])
}

func TestTasksFormatter_MetadataMinimal(t *testing.T) {
	s := signal.RawSignal{Kind: "churn"}
	m := metadataForSignal(s)
	assert.Equal(t, "churn", m["kind"])
	_, hasCollector := m["collector"]
	assert.False(t, hasCollector)
	_, hasFile := m["file_path"]
	assert.False(t, hasFile)
	_, hasLine := m["line"]
	assert.False(t, hasLine)
	_, hasTags := m["tags"]
	assert.False(t, hasTags)
	_, hasAuthor := m["author"]
	assert.False(t, hasAuthor)
	_, hasTimestamp := m["timestamp"]
	assert.False(t, hasTimestamp)
	_, hasClosedAt := m["closed_at"]
	assert.False(t, hasClosedAt)
}

func TestTasksFormatter_MetadataClosedAt(t *testing.T) {
	closedTime := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	s := signal.RawSignal{
		Kind:     "github-closed-issue",
		ClosedAt: closedTime,
	}

	m := metadataForSignal(s)
	assert.Equal(t, "2026-02-01T12:00:00Z", m["closed_at"])
}

func TestTasksFormatter_ID(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		sig := testSignal()
		task1 := signalToTask(sig)
		task2 := signalToTask(sig)
		assert.Equal(t, task1.ID, task2.ID, "same signal should produce the same task ID")
	})

	t.Run("format", func(t *testing.T) {
		sig := testSignal()
		task := signalToTask(sig)
		assert.Regexp(t, `^str-[0-9a-f]{8}$`, task.ID, "task ID should be str- prefix + 8 hex chars")
	})

	t.Run("different_signals_different_ids", func(t *testing.T) {
		sig1 := signal.RawSignal{Source: "todos", Kind: "todo", Title: "A", FilePath: "a.go", Line: 1}
		sig2 := signal.RawSignal{Source: "todos", Kind: "todo", Title: "B", FilePath: "b.go", Line: 2}
		task1 := signalToTask(sig1)
		task2 := signalToTask(sig2)
		assert.NotEqual(t, task1.ID, task2.ID)
	})
}

func TestTasksFormatter_Status(t *testing.T) {
	t.Run("pending_when_open", func(t *testing.T) {
		sig := signal.RawSignal{
			Source: "todos",
			Kind:   "todo",
			Title:  "Open task",
		}
		task := signalToTask(sig)
		assert.Equal(t, "pending", task.Status)
	})

	t.Run("completed_when_closed", func(t *testing.T) {
		sig := signal.RawSignal{
			Source:   "github",
			Kind:     "github-closed-issue",
			Title:    "Closed issue",
			ClosedAt: time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC),
		}
		task := signalToTask(sig)
		assert.Equal(t, "completed", task.Status)
	})
}

func TestTasksFormatter_SingleSignal(t *testing.T) {
	f := newTestTasksFormatter()
	sig := testSignal()

	var buf bytes.Buffer
	err := f.Format([]signal.RawSignal{sig}, &buf)
	require.NoError(t, err)

	var envelope TasksEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))

	require.Len(t, envelope.Tasks, 1)
	task := envelope.Tasks[0]

	assert.Regexp(t, `^str-[0-9a-f]{8}$`, task.ID)
	assert.Equal(t, "TODO: Add rate limiting", task.Subject)
	assert.Equal(t, "Addressing Add rate limiting", task.ActiveForm)
	assert.Equal(t, "pending", task.Status)
	assert.Contains(t, task.Description, "This endpoint needs rate limiting before production")
	assert.Contains(t, task.Description, "Source: todos collector")
	assert.Contains(t, task.Description, "File: internal/server/handler.go:42")
	assert.Contains(t, task.Description, "Author: alice")
	assert.Contains(t, task.Description, "Confidence: 85%")
	assert.Contains(t, task.Description, "Priority: P1")
	assert.Contains(t, task.Description, "Tags: security, performance")

	assert.Equal(t, "todo", task.Metadata["kind"])
	assert.Equal(t, "todos", task.Metadata["collector"])
	assert.Equal(t, "internal/server/handler.go", task.Metadata["file_path"])
	assert.Equal(t, "42", task.Metadata["line"])
	assert.Equal(t, "security,performance", task.Metadata["tags"])
	assert.Equal(t, "alice", task.Metadata["author"])
	assert.Equal(t, "2026-01-15T10:30:00Z", task.Metadata["timestamp"])
}

func TestTasksFormatter_MultipleSignals(t *testing.T) {
	f := newTestTasksFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Task A", FilePath: "a.go", Line: 1, Confidence: 0.9},
		{Source: "gitlog", Kind: "fixme", Title: "Task B", FilePath: "b.go", Line: 2, Confidence: 0.7},
		{Source: "todos", Kind: "hack", Title: "Task C", FilePath: "c.go", Line: 3, Confidence: 0.5},
	}

	var buf bytes.Buffer
	err := f.Format(signals, &buf)
	require.NoError(t, err)

	var envelope TasksEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))

	require.Len(t, envelope.Tasks, 3)
	assert.Equal(t, "TODO: Task A", envelope.Tasks[0].Subject)
	assert.Equal(t, "BUG: Task B", envelope.Tasks[1].Subject)
	assert.Equal(t, "HACK: Task C", envelope.Tasks[2].Subject)

	// Each task should have a unique ID.
	ids := map[string]bool{}
	for _, task := range envelope.Tasks {
		assert.NotEmpty(t, task.ID)
		assert.False(t, ids[task.ID], "duplicate task ID: %s", task.ID)
		ids[task.ID] = true
	}
}

func TestTasksFormatter_EnvelopeMetadata(t *testing.T) {
	fixedTime := time.Date(2026, 2, 7, 12, 0, 0, 0, time.UTC)
	f := &TasksFormatter{
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

	var envelope TasksEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))

	assert.Equal(t, 4, envelope.Metadata.TotalCount)
	assert.Equal(t, "2026-02-07T12:00:00Z", envelope.Metadata.GeneratedAt)
	assert.Equal(t, []string{"gitlog", "patterns", "todos"}, envelope.Metadata.Collectors)
}

func TestTasksFormatter_PrettyPrintDefault(t *testing.T) {
	f := newTestTasksFormatter()

	var buf bytes.Buffer
	err := f.Format([]signal.RawSignal{}, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "\n")
	assert.Contains(t, output, "  ")
}

func TestTasksFormatter_CompactMode(t *testing.T) {
	f := &TasksFormatter{
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
	lines := countLines(output)
	assert.Equal(t, 1, lines, "compact output should be a single line (plus trailing newline)")

	var envelope TasksEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))
	assert.Len(t, envelope.Tasks, 1)
}

func TestTasksFormatter_ValidJSON(t *testing.T) {
	f := newTestTasksFormatter()
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

func TestTasksFormatter_TrailingNewline(t *testing.T) {
	f := newTestTasksFormatter()

	var buf bytes.Buffer
	err := f.Format([]signal.RawSignal{}, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.True(t, len(output) > 0 && output[len(output)-1] == '\n',
		"output should end with a trailing newline")
}

func TestTasksFormatter_InjectionSafe(t *testing.T) {
	f := newTestTasksFormatter()

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
	}

	var buf bytes.Buffer
	err := f.Format(injectionSignals, &buf)
	require.NoError(t, err)

	var envelope TasksEnvelope
	require.NoError(t, json.Unmarshal(buf.Bytes(), &envelope))
	assert.Len(t, envelope.Tasks, 2)

	// The injection attempt should be escaped properly.
	assert.Contains(t, envelope.Tasks[0].Subject, `Evil","injected":"true`)
}

func TestTasksFormatter_WriteFailure(t *testing.T) {
	f := newTestTasksFormatter()
	signals := []signal.RawSignal{
		{Source: "test", Kind: "todo", Title: "Task", FilePath: "a.go", Confidence: 0.5},
	}

	t.Run("fail_on_data_write", func(t *testing.T) {
		w := &failWriter{failAfter: 0}
		err := f.Format(signals, w)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "write tasks json")
	})

	t.Run("fail_on_newline_write", func(t *testing.T) {
		w := &failWriter{failAfter: 1}
		err := f.Format(signals, w)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "write tasks json trailing newline")
	})
}

func TestTasksFormatter_ShouldCompact(t *testing.T) {
	t.Run("compact_true_always_compact", func(t *testing.T) {
		f := &TasksFormatter{Compact: true}
		var buf bytes.Buffer
		assert.True(t, f.shouldCompact(&buf))
	})

	t.Run("non_file_writer_defaults_pretty", func(t *testing.T) {
		f := &TasksFormatter{Compact: false}
		var buf bytes.Buffer
		assert.False(t, f.shouldCompact(&buf))
	})
}

func TestTasksFormatter_AutoDetectPipe(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()

	f := &TasksFormatter{Compact: false, nowFunc: fixedNow}
	assert.True(t, f.shouldCompact(w))
}

// --- Helpers ---

func newTestTasksFormatter() *TasksFormatter {
	return &TasksFormatter{nowFunc: fixedNow}
}
