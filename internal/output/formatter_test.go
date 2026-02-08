package output

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time interface check.
var _ Formatter = (*stubFormatter)(nil)

type stubFormatter struct{ name string }

func (s *stubFormatter) Name() string                                   { return s.name }
func (s *stubFormatter) Format(_ []signal.RawSignal, _ io.Writer) error { return nil }

func TestFormatterInterface(t *testing.T) {
	var f Formatter = &stubFormatter{name: "stub"}
	assert.Equal(t, "stub", f.Name())

	var buf bytes.Buffer
	assert.NoError(t, f.Format(nil, &buf))
}

// --- GetFormatter tests ---

// restoreFormatters resets the registry and re-registers all init-registered formatters.
func restoreFormatters() {
	resetFmtForTesting()
	RegisterFormatter(NewBeadsFormatter())
	RegisterFormatter(NewJSONFormatter())
	RegisterFormatter(NewMarkdownFormatter())
	RegisterFormatter(NewTasksFormatter())
}

func TestGetFormatter_Known(t *testing.T) {
	// Save and restore registry state.
	resetFmtForTesting()
	defer restoreFormatters()

	RegisterFormatter(&stubFormatter{name: "json"})
	RegisterFormatter(&stubFormatter{name: "markdown"})

	f, err := GetFormatter("json")
	require.NoError(t, err)
	assert.Equal(t, "json", f.Name())

	f, err = GetFormatter("markdown")
	require.NoError(t, err)
	assert.Equal(t, "markdown", f.Name())
}

func TestGetFormatter_Unknown(t *testing.T) {
	resetFmtForTesting()
	defer restoreFormatters()

	RegisterFormatter(&stubFormatter{name: "beads"})

	f, err := GetFormatter("nonexistent")
	assert.Nil(t, f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown format: "nonexistent"`)
	assert.Contains(t, err.Error(), "beads")
}

func TestGetFormatter_UnknownEmptyRegistry(t *testing.T) {
	resetFmtForTesting()
	defer restoreFormatters()

	f, err := GetFormatter("anything")
	assert.Nil(t, f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown format: "anything"`)
}

// --- formatNames tests ---

func TestFormatNames_Empty(t *testing.T) {
	resetFmtForTesting()
	defer restoreFormatters()

	result := formatNames()
	assert.Equal(t, "", result)
}

func TestFormatNames_Single(t *testing.T) {
	resetFmtForTesting()
	defer restoreFormatters()

	RegisterFormatter(&stubFormatter{name: "beads"})
	result := formatNames()
	assert.Equal(t, "beads", result)
}

func TestFormatNames_MultipleSorted(t *testing.T) {
	resetFmtForTesting()
	defer restoreFormatters()

	RegisterFormatter(&stubFormatter{name: "markdown"})
	RegisterFormatter(&stubFormatter{name: "beads"})
	RegisterFormatter(&stubFormatter{name: "json"})

	result := formatNames()
	assert.Equal(t, "beads, json, markdown", result)
}

// --- Format write-failure error path ---

// failWriter is a writer that always returns an error.
type failWriter struct {
	// failAfter counts successful Write calls before failing.
	failAfter int
	calls     int
}

func (fw *failWriter) Write(p []byte) (int, error) {
	fw.calls++
	if fw.calls > fw.failAfter {
		return 0, errors.New("disk full")
	}
	return len(p), nil
}

func TestFormat_WriteFailure_OnData(t *testing.T) {
	f := NewBeadsFormatter()
	signals := []signal.RawSignal{
		{Source: "test", Kind: "todo", Title: "Task", FilePath: "a.go", Confidence: 0.5},
	}

	// Fail on the very first write (the JSON data).
	w := &failWriter{failAfter: 0}
	err := f.Format(signals, w)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write signal 0")
	assert.Contains(t, err.Error(), "disk full")
}

func TestFormat_WriteFailure_OnNewline(t *testing.T) {
	f := NewBeadsFormatter()
	signals := []signal.RawSignal{
		{Source: "test", Kind: "todo", Title: "Task", FilePath: "a.go", Confidence: 0.5},
	}

	// First write (data) succeeds, second write (newline) fails.
	w := &failWriter{failAfter: 1}
	err := f.Format(signals, w)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write newline 0")
	assert.Contains(t, err.Error(), "disk full")
}

func TestFormat_WriteFailure_SecondSignal(t *testing.T) {
	f := NewBeadsFormatter()
	signals := []signal.RawSignal{
		{Source: "test", Kind: "todo", Title: "First", FilePath: "a.go", Confidence: 0.5},
		{Source: "test", Kind: "todo", Title: "Second", FilePath: "b.go", Confidence: 0.5},
	}

	// First signal writes OK (data + newline = 2 writes), second signal data fails.
	w := &failWriter{failAfter: 2}
	err := f.Format(signals, w)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write signal 1")
}

// --- GetFormatter with beads (init registration) ---

func TestGetFormatter_Beads_ViaInit(t *testing.T) {
	// The beads formatter is registered via init() in beads.go.
	// Verify it's accessible through GetFormatter.
	f, err := GetFormatter("beads")
	require.NoError(t, err)
	assert.Equal(t, "beads", f.Name())
}

// --- GetFormatter error message includes available names ---

func TestGetFormatter_ErrorListsAvailableFormatters(t *testing.T) {
	resetFmtForTesting()
	defer restoreFormatters()

	RegisterFormatter(&stubFormatter{name: "alpha"})
	RegisterFormatter(&stubFormatter{name: "beta"})

	_, err := GetFormatter("missing")
	require.Error(t, err)
	msg := err.Error()
	// Should list available formatters in sorted order.
	assert.True(t, strings.Contains(msg, "alpha") && strings.Contains(msg, "beta"),
		"error should list available formatters, got: %s", msg)
}
