package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/davetashner/stringer/internal/signal"
)

// Compile-time interface check for BeadsFormatter.
var _ Formatter = (*BeadsFormatter)(nil)

func TestBeadsFormatterName(t *testing.T) {
	f := NewBeadsFormatter()
	if got := f.Name(); got != "beads" {
		t.Errorf("Name() = %q, want %q", got, "beads")
	}
}

// testSignal returns a fully-populated signal for testing.
func testSignal() signal.RawSignal {
	return signal.RawSignal{
		Source:      "todos",
		Kind:        "todo",
		FilePath:    "internal/server/handler.go",
		Line:        42,
		Title:       "Add rate limiting",
		Description: "This endpoint needs rate limiting before production",
		Author:      "alice",
		Timestamp:   time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
		Confidence:  0.85,
		Tags:        []string{"security", "performance"},
	}
}

func TestFieldMapping(t *testing.T) {
	sig := testSignal()
	rec := signalToBead(sig)

	t.Run("title", func(t *testing.T) {
		if rec.Title != "Add rate limiting" {
			t.Errorf("Title = %q, want %q", rec.Title, "Add rate limiting")
		}
	})

	t.Run("description_includes_original", func(t *testing.T) {
		if !strings.Contains(rec.Description, "This endpoint needs rate limiting before production") {
			t.Errorf("Description missing original text: %q", rec.Description)
		}
	})

	t.Run("description_includes_location", func(t *testing.T) {
		if !strings.Contains(rec.Description, "internal/server/handler.go:42") {
			t.Errorf("Description missing location: %q", rec.Description)
		}
	})

	t.Run("type_todo_maps_to_task", func(t *testing.T) {
		if rec.Type != "task" {
			t.Errorf("Type = %q, want %q", rec.Type, "task")
		}
	})

	t.Run("priority_high_confidence", func(t *testing.T) {
		if rec.Priority != 1 {
			t.Errorf("Priority = %d, want 1 (confidence 0.85 >= 0.8)", rec.Priority)
		}
	})

	t.Run("status_always_open", func(t *testing.T) {
		if rec.Status != "open" {
			t.Errorf("Status = %q, want %q", rec.Status, "open")
		}
	})

	t.Run("created_at_iso8601", func(t *testing.T) {
		want := "2026-01-15T10:30:00Z"
		if rec.CreatedAt != want {
			t.Errorf("CreatedAt = %q, want %q", rec.CreatedAt, want)
		}
	})

	t.Run("created_by", func(t *testing.T) {
		if rec.CreatedBy != "alice" {
			t.Errorf("CreatedBy = %q, want %q", rec.CreatedBy, "alice")
		}
	})

	t.Run("labels_include_tags_and_stringer", func(t *testing.T) {
		want := []string{"security", "performance", "stringer-generated", "todos"}
		if len(rec.Labels) != len(want) {
			t.Fatalf("Labels = %v, want %v", rec.Labels, want)
		}
		for i, label := range want {
			if rec.Labels[i] != label {
				t.Errorf("Labels[%d] = %q, want %q", i, rec.Labels[i], label)
			}
		}
	})
}

func TestKindToTypeMapping(t *testing.T) {
	cases := []struct {
		kind     string
		wantType string
	}{
		{"bug", "bug"},
		{"BUG", "bug"},
		{"fixme", "bug"},
		{"FIXME", "bug"},
		{"todo", "task"},
		{"TODO", "task"},
		{"hack", "chore"},
		{"HACK", "chore"},
		{"xxx", "chore"},
		{"XXX", "chore"},
		{"optimize", "chore"},
		{"OPTIMIZE", "chore"},
		{"unknown", "task"},
		{"", "task"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			got := mapKindToType(tc.kind)
			if got != tc.wantType {
				t.Errorf("mapKindToType(%q) = %q, want %q", tc.kind, got, tc.wantType)
			}
		})
	}
}

func TestConfidenceToPriority(t *testing.T) {
	cases := []struct {
		confidence float64
		wantP      int
	}{
		{1.0, 1},
		{0.9, 1},
		{0.8, 1},
		{0.79, 2},
		{0.7, 2},
		{0.6, 2},
		{0.59, 3},
		{0.5, 3},
		{0.4, 3},
		{0.39, 4},
		{0.2, 4},
		{0.0, 4},
	}
	for _, tc := range cases {
		t.Run("", func(t *testing.T) {
			got := mapConfidenceToPriority(tc.confidence)
			if got != tc.wantP {
				t.Errorf("mapConfidenceToPriority(%v) = %d, want %d", tc.confidence, got, tc.wantP)
			}
		})
	}
}

func TestIDDeterminism(t *testing.T) {
	sig := testSignal()

	id1 := generateID(sig)
	id2 := generateID(sig)

	if id1 != id2 {
		t.Errorf("same input produced different IDs: %q vs %q", id1, id2)
	}
}

func TestIDFormat(t *testing.T) {
	sig := testSignal()
	id := generateID(sig)

	if !strings.HasPrefix(id, "str-") {
		t.Errorf("ID %q does not start with 'str-'", id)
	}

	// "str-" (4 chars) + 8 hex chars = 12 total
	if len(id) != 12 {
		t.Errorf("ID %q has length %d, want 12", id, len(id))
	}
}

func TestIDUniqueness(t *testing.T) {
	sig1 := testSignal()
	sig2 := testSignal()
	sig2.Title = "Different title"

	id1 := generateID(sig1)
	id2 := generateID(sig2)

	if id1 == id2 {
		t.Errorf("different signals produced same ID: %q", id1)
	}
}

func TestIDDiffersByField(t *testing.T) {
	base := testSignal()
	baseID := generateID(base)

	t.Run("source", func(t *testing.T) {
		s := base
		s.Source = "gitlog"
		if generateID(s) == baseID {
			t.Error("changing Source did not change ID")
		}
	})

	t.Run("kind", func(t *testing.T) {
		s := base
		s.Kind = "fixme"
		if generateID(s) == baseID {
			t.Error("changing Kind did not change ID")
		}
	})

	t.Run("filepath", func(t *testing.T) {
		s := base
		s.FilePath = "other/file.go"
		if generateID(s) == baseID {
			t.Error("changing FilePath did not change ID")
		}
	})

	t.Run("line", func(t *testing.T) {
		s := base
		s.Line = 99
		if generateID(s) == baseID {
			t.Error("changing Line did not change ID")
		}
	})

	t.Run("title", func(t *testing.T) {
		s := base
		s.Title = "Something else"
		if generateID(s) == baseID {
			t.Error("changing Title did not change ID")
		}
	})
}

func TestJSONLValidity(t *testing.T) {
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
	f := NewBeadsFormatter()
	if err := f.Format(signals, &buf); err != nil {
		t.Fatalf("Format() error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}

	for i, line := range lines {
		var rec map[string]interface{}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Errorf("line %d is not valid JSON: %v\nline: %s", i, err, line)
		}

		for _, field := range []string{"id", "title", "type", "priority", "status", "created_at", "created_by"} {
			if _, ok := rec[field]; !ok {
				t.Errorf("line %d missing required field %q", i, field)
			}
		}
	}
}

func TestJSONLNoTrailingCommaOrArray(t *testing.T) {
	signals := []signal.RawSignal{testSignal()}

	var buf bytes.Buffer
	f := NewBeadsFormatter()
	if err := f.Format(signals, &buf); err != nil {
		t.Fatalf("Format() error: %v", err)
	}

	output := buf.String()
	if strings.HasPrefix(output, "[") {
		t.Error("output starts with '[' - should be JSONL, not a JSON array")
	}
	if strings.Contains(output, ",\n") {
		t.Error("output contains trailing commas between lines")
	}
}

func TestEmptySignals(t *testing.T) {
	var buf bytes.Buffer
	f := NewBeadsFormatter()
	if err := f.Format(nil, &buf); err != nil {
		t.Fatalf("Format(nil) error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("Format(nil) produced output: %q", buf.String())
	}

	buf.Reset()
	if err := f.Format([]signal.RawSignal{}, &buf); err != nil {
		t.Fatalf("Format([]) error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("Format([]) produced output: %q", buf.String())
	}
}

func TestMissingOptionalFields(t *testing.T) {
	sig := signal.RawSignal{
		Kind:  "todo",
		Title: "Minimal signal",
	}

	rec := signalToBead(sig)

	t.Run("author_defaults_to_stringer", func(t *testing.T) {
		if rec.CreatedBy != "stringer" {
			t.Errorf("CreatedBy = %q, want %q", rec.CreatedBy, "stringer")
		}
	})

	t.Run("empty_timestamp_is_empty_string", func(t *testing.T) {
		if rec.CreatedAt != "" {
			t.Errorf("CreatedAt = %q, want empty string for zero time", rec.CreatedAt)
		}
	})

	t.Run("description_empty_when_no_description_no_filepath", func(t *testing.T) {
		if rec.Description != "" {
			t.Errorf("Description = %q, want empty", rec.Description)
		}
	})

	t.Run("labels_contain_stringer_generated", func(t *testing.T) {
		found := false
		for _, l := range rec.Labels {
			if l == "stringer-generated" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Labels %v missing 'stringer-generated'", rec.Labels)
		}
	})
}

func TestDescriptionVariants(t *testing.T) {
	t.Run("description_only", func(t *testing.T) {
		sig := signal.RawSignal{
			Description: "Some context",
		}
		rec := signalToBead(sig)
		if rec.Description != "Some context" {
			t.Errorf("Description = %q, want %q", rec.Description, "Some context")
		}
	})

	t.Run("filepath_only", func(t *testing.T) {
		sig := signal.RawSignal{
			FilePath: "main.go",
		}
		rec := signalToBead(sig)
		if rec.Description != "Location: main.go" {
			t.Errorf("Description = %q, want %q", rec.Description, "Location: main.go")
		}
	})

	t.Run("filepath_with_line", func(t *testing.T) {
		sig := signal.RawSignal{
			FilePath: "main.go",
			Line:     5,
		}
		rec := signalToBead(sig)
		if rec.Description != "Location: main.go:5" {
			t.Errorf("Description = %q, want %q", rec.Description, "Location: main.go:5")
		}
	})

	t.Run("filepath_line_zero_omits_line", func(t *testing.T) {
		sig := signal.RawSignal{
			FilePath: "main.go",
			Line:     0,
		}
		rec := signalToBead(sig)
		if rec.Description != "Location: main.go" {
			t.Errorf("Description = %q, want %q", rec.Description, "Location: main.go")
		}
	})

	t.Run("description_and_filepath", func(t *testing.T) {
		sig := signal.RawSignal{
			Description: "Needs work",
			FilePath:    "api.go",
			Line:        100,
		}
		rec := signalToBead(sig)
		want := "Needs work\n\nLocation: api.go:100"
		if rec.Description != want {
			t.Errorf("Description = %q, want %q", rec.Description, want)
		}
	})
}

func TestLabelsWithNoSource(t *testing.T) {
	sig := signal.RawSignal{
		Kind:  "todo",
		Title: "Test",
		Tags:  []string{"tag1"},
	}
	rec := signalToBead(sig)
	want := []string{"tag1", "stringer-generated"}
	if len(rec.Labels) != len(want) {
		t.Fatalf("Labels = %v, want %v", rec.Labels, want)
	}
	for i, label := range want {
		if rec.Labels[i] != label {
			t.Errorf("Labels[%d] = %q, want %q", i, rec.Labels[i], label)
		}
	}
}

func TestLabelsWithNoTags(t *testing.T) {
	sig := signal.RawSignal{
		Source: "todos",
		Kind:   "todo",
		Title:  "Test",
	}
	rec := signalToBead(sig)
	want := []string{"stringer-generated", "todos"}
	if len(rec.Labels) != len(want) {
		t.Fatalf("Labels = %v, want %v", rec.Labels, want)
	}
	for i, label := range want {
		if rec.Labels[i] != label {
			t.Errorf("Labels[%d] = %q, want %q", i, rec.Labels[i], label)
		}
	}
}

func TestFormatRoundTrip(t *testing.T) {
	sig := testSignal()
	var buf bytes.Buffer
	f := NewBeadsFormatter()
	if err := f.Format([]signal.RawSignal{sig}, &buf); err != nil {
		t.Fatalf("Format() error: %v", err)
	}

	var rec beadRecord
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("Unmarshal error: %v\ndata: %s", err, buf.String())
	}

	if rec.ID == "" {
		t.Error("round-trip: ID is empty")
	}
	if rec.Title != "Add rate limiting" {
		t.Errorf("round-trip: Title = %q", rec.Title)
	}
	if rec.Type != "task" {
		t.Errorf("round-trip: Type = %q", rec.Type)
	}
	if rec.Status != "open" {
		t.Errorf("round-trip: Status = %q", rec.Status)
	}
}

func TestMultipleSignalsProduceMultipleLines(t *testing.T) {
	signals := make([]signal.RawSignal, 5)
	for i := range signals {
		signals[i] = signal.RawSignal{
			Source: "todos",
			Kind:   "todo",
			Title:  "Task " + string(rune('A'+i)),
			Line:   i + 1,
		}
	}

	var buf bytes.Buffer
	f := NewBeadsFormatter()
	if err := f.Format(signals, &buf); err != nil {
		t.Fatalf("Format() error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d", len(lines))
	}

	for i, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Errorf("line %d is not valid JSON: %s", i, line)
		}
	}
}

func TestJSONLInjectionSafe(t *testing.T) {
	// Crafted TODO comments should not break JSONL output or inject extra fields.
	injectionSignals := []signal.RawSignal{
		{
			Source:     "todos",
			Kind:       "todo",
			Title:      `Evil","status":"closed","hacked":"true`,
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
	f := NewBeadsFormatter()
	if err := f.Format(injectionSignals, &buf); err != nil {
		t.Fatalf("Format() error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != len(injectionSignals) {
		t.Fatalf("expected %d lines, got %d", len(injectionSignals), len(lines))
	}

	for i, line := range lines {
		// Each line must be valid JSON.
		var rec map[string]interface{}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Errorf("line %d is not valid JSON: %v\nline: %s", i, err, line)
			continue
		}

		// Status must always be "open" — injection attempts should not override it.
		if status, ok := rec["status"]; !ok || status != "open" {
			t.Errorf("line %d: status = %v, want 'open' (possible injection)", i, status)
		}

		// No "hacked" field should exist.
		if _, ok := rec["hacked"]; ok {
			t.Errorf("line %d: unexpected 'hacked' field found (JSON injection succeeded)", i)
		}
	}
}

func TestTimestampUTCConversion(t *testing.T) {
	eastern := time.FixedZone("EST", -5*60*60)
	sig := signal.RawSignal{
		Timestamp: time.Date(2026, 3, 15, 17, 0, 0, 0, eastern),
	}
	rec := signalToBead(sig)
	want := "2026-03-15T22:00:00Z"
	if rec.CreatedAt != want {
		t.Errorf("CreatedAt = %q, want %q", rec.CreatedAt, want)
	}
}
