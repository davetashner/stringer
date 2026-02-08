package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/davetashner/stringer/internal/signal"
)

func init() {
	RegisterFormatter(NewTasksFormatter())
}

// taskRecord represents a single Claude Code task.
type taskRecord struct {
	Subject     string            `json:"subject"`
	Description string            `json:"description"`
	ActiveForm  string            `json:"active_form"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// TasksEnvelope wraps tasks with metadata for the tasks output format.
type TasksEnvelope struct {
	Tasks    []taskRecord `json:"tasks"`
	Metadata JSONMetadata `json:"metadata"`
}

// TasksFormatter writes signals as Claude Code TaskCreate-compatible JSON.
type TasksFormatter struct {
	// Compact controls whether output is compact or pretty-printed.
	Compact bool

	// nowFunc is used for testing to override the current time.
	nowFunc func() time.Time
}

// Compile-time interface check.
var _ Formatter = (*TasksFormatter)(nil)

// NewTasksFormatter returns a new TasksFormatter with default settings.
func NewTasksFormatter() *TasksFormatter {
	return &TasksFormatter{}
}

// Name returns the format name.
func (f *TasksFormatter) Name() string {
	return "tasks"
}

// Format writes all signals as a tasks JSON document to w.
func (f *TasksFormatter) Format(signals []signal.RawSignal, w io.Writer) error {
	if signals == nil {
		signals = []signal.RawSignal{}
	}

	collectors := extractCollectors(signals)

	now := time.Now()
	if f.nowFunc != nil {
		now = f.nowFunc()
	}

	tasks := make([]taskRecord, 0, len(signals))
	for _, s := range signals {
		tasks = append(tasks, signalToTask(s))
	}

	envelope := TasksEnvelope{
		Tasks: tasks,
		Metadata: JSONMetadata{
			TotalCount:  len(signals),
			Collectors:  collectors,
			GeneratedAt: now.UTC().Format("2006-01-02T15:04:05Z"),
		},
	}

	compact := f.shouldCompact(w)

	var data []byte
	var err error
	if compact {
		data, err = json.Marshal(envelope)
	} else {
		data, err = json.MarshalIndent(envelope, "", "  ")
	}
	if err != nil {
		return fmt.Errorf("marshal tasks json: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write tasks json: %w", err)
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write tasks json trailing newline: %w", err)
	}

	return nil
}

// shouldCompact determines whether to use compact mode.
func (f *TasksFormatter) shouldCompact(w io.Writer) bool {
	if f.Compact {
		return true
	}

	if file, ok := w.(*os.File); ok {
		fi, err := file.Stat()
		if err != nil {
			return false
		}
		if fi.Mode()&os.ModeCharDevice != 0 {
			return false // TTY -> pretty
		}
		return true // pipe/file -> compact
	}

	return false
}

// signalToTask converts a RawSignal to a taskRecord.
func signalToTask(s signal.RawSignal) taskRecord {
	subject := subjectForSignal(s)
	activeForm := activeFormForSignal(s)
	description := descriptionForSignal(s)
	metadata := metadataForSignal(s)

	return taskRecord{
		Subject:     subject,
		Description: description,
		ActiveForm:  activeForm,
		Metadata:    metadata,
	}
}

// subjectForSignal generates a subject line with kind-based prefix.
func subjectForSignal(s signal.RawSignal) string {
	switch s.Kind {
	case "todo":
		return "TODO: " + s.Title
	case "fixme", "bug":
		return "BUG: " + s.Title
	case "hack", "xxx":
		return "HACK: " + s.Title
	default:
		return s.Title
	}
}

// activeFormForSignal generates a present-continuous spinner label.
func activeFormForSignal(s signal.RawSignal) string {
	title := s.Title
	switch s.Kind {
	case "fixme", "bug":
		return "Fixing " + title
	case "todo", "hack", "xxx":
		return "Addressing " + title
	default:
		return "Investigating " + title
	}
}

// descriptionForSignal builds a rich description block.
func descriptionForSignal(s signal.RawSignal) string {
	var b strings.Builder

	if s.Description != "" {
		b.WriteString(s.Description)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if s.Source != "" {
		fmt.Fprintf(&b, "Source: %s collector\n", s.Source)
	}
	if s.FilePath != "" {
		if s.Line > 0 {
			fmt.Fprintf(&b, "File: %s:%d\n", s.FilePath, s.Line)
		} else {
			fmt.Fprintf(&b, "File: %s\n", s.FilePath)
		}
	}
	if s.Confidence > 0 {
		fmt.Fprintf(&b, "Confidence: %.0f%%\n", s.Confidence*100)
	}

	return strings.TrimRight(b.String(), "\n")
}

// metadataForSignal populates the metadata map from signal fields.
func metadataForSignal(s signal.RawSignal) map[string]string {
	m := map[string]string{
		"kind": s.Kind,
	}

	if s.Source != "" {
		m["collector"] = s.Source
	}
	if s.FilePath != "" {
		m["file_path"] = s.FilePath
	}
	if s.Line > 0 {
		m["line"] = fmt.Sprintf("%d", s.Line)
	}
	if s.Confidence > 0 {
		m["confidence"] = fmt.Sprintf("%.2f", s.Confidence)
	}
	if len(s.Tags) > 0 {
		m["tags"] = strings.Join(s.Tags, ",")
	}

	return m
}
