package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/davetashner/stringer/internal/beads"
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
	rec := NewBeadsFormatter().signalToBead(sig)

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

	t.Run("status_open_without_pre_closed_tag", func(t *testing.T) {
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
		{"github-bug", "bug"},
		{"todo", "task"},
		{"TODO", "task"},
		{"hack", "chore"},
		{"HACK", "chore"},
		{"xxx", "chore"},
		{"XXX", "chore"},
		{"optimize", "chore"},
		{"OPTIMIZE", "chore"},
		{"low-lottery-risk", "chore"},
		{"github-feature", "task"},
		{"github-issue", "task"},
		{"github-pr-changes", "task"},
		{"github-pr-approved", "task"},
		{"github-pr-pending", "task"},
		{"github-review-todo", "task"},
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

	bf := NewBeadsFormatter()
	id1 := bf.generateID(sig)
	id2 := bf.generateID(sig)

	if id1 != id2 {
		t.Errorf("same input produced different IDs: %q vs %q", id1, id2)
	}
}

func TestIDFormat(t *testing.T) {
	sig := testSignal()
	id := NewBeadsFormatter().generateID(sig)

	if !strings.HasPrefix(id, "str-") {
		t.Errorf("ID %q does not start with 'str-'", id)
	}

	// "str-" (4 chars) + 8 hex chars = 12 total
	if len(id) != 12 {
		t.Errorf("ID %q has length %d, want 12", id, len(id))
	}
}

func TestIDUniqueness(t *testing.T) {
	bf := NewBeadsFormatter()
	sig1 := testSignal()
	sig2 := testSignal()
	sig2.Title = "Different title"

	id1 := bf.generateID(sig1)
	id2 := bf.generateID(sig2)

	if id1 == id2 {
		t.Errorf("different signals produced same ID: %q", id1)
	}
}

func TestIDDiffersByField(t *testing.T) {
	bf := NewBeadsFormatter()
	base := testSignal()
	baseID := bf.generateID(base)

	t.Run("source", func(t *testing.T) {
		s := base
		s.Source = "gitlog"
		if bf.generateID(s) == baseID {
			t.Error("changing Source did not change ID")
		}
	})

	t.Run("kind", func(t *testing.T) {
		s := base
		s.Kind = "fixme"
		if bf.generateID(s) == baseID {
			t.Error("changing Kind did not change ID")
		}
	})

	t.Run("filepath", func(t *testing.T) {
		s := base
		s.FilePath = "other/file.go"
		if bf.generateID(s) == baseID {
			t.Error("changing FilePath did not change ID")
		}
	})

	t.Run("line", func(t *testing.T) {
		s := base
		s.Line = 99
		if bf.generateID(s) == baseID {
			t.Error("changing Line did not change ID")
		}
	})

	t.Run("title", func(t *testing.T) {
		s := base
		s.Title = "Something else"
		if bf.generateID(s) == baseID {
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

	rec := NewBeadsFormatter().signalToBead(sig)

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
		rec := NewBeadsFormatter().signalToBead(sig)
		if rec.Description != "Some context" {
			t.Errorf("Description = %q, want %q", rec.Description, "Some context")
		}
	})

	t.Run("filepath_only", func(t *testing.T) {
		sig := signal.RawSignal{
			FilePath: "main.go",
		}
		rec := NewBeadsFormatter().signalToBead(sig)
		if rec.Description != "Location: main.go" {
			t.Errorf("Description = %q, want %q", rec.Description, "Location: main.go")
		}
	})

	t.Run("filepath_with_line", func(t *testing.T) {
		sig := signal.RawSignal{
			FilePath: "main.go",
			Line:     5,
		}
		rec := NewBeadsFormatter().signalToBead(sig)
		if rec.Description != "Location: main.go:5" {
			t.Errorf("Description = %q, want %q", rec.Description, "Location: main.go:5")
		}
	})

	t.Run("filepath_line_zero_omits_line", func(t *testing.T) {
		sig := signal.RawSignal{
			FilePath: "main.go",
			Line:     0,
		}
		rec := NewBeadsFormatter().signalToBead(sig)
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
		rec := NewBeadsFormatter().signalToBead(sig)
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
	rec := NewBeadsFormatter().signalToBead(sig)
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
	rec := NewBeadsFormatter().signalToBead(sig)
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
	rec := NewBeadsFormatter().signalToBead(sig)
	want := "2026-03-15T22:00:00Z"
	if rec.CreatedAt != want {
		t.Errorf("CreatedAt = %q, want %q", rec.CreatedAt, want)
	}
}

// -----------------------------------------------------------------------
// Conventions tests
// -----------------------------------------------------------------------

func TestSetConventions_CustomPrefix(t *testing.T) {
	bf := NewBeadsFormatter()
	bf.SetConventions(&beads.Conventions{
		IDPrefix: "myapp-",
	})

	sig := testSignal()
	rec := bf.signalToBead(sig)

	if !strings.HasPrefix(rec.ID, "myapp-") {
		t.Errorf("ID %q should start with 'myapp-'", rec.ID)
	}
}

func TestSetConventions_SnakeCaseLabels(t *testing.T) {
	bf := NewBeadsFormatter()
	bf.SetConventions(&beads.Conventions{
		LabelStyle: "snake_case",
	})

	sig := testSignal()
	rec := bf.signalToBead(sig)

	found := false
	for _, label := range rec.Labels {
		if label == "stringer_generated" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Labels %v should contain 'stringer_generated' with snake_case convention", rec.Labels)
	}

	// Verify "stringer-generated" is NOT present.
	for _, label := range rec.Labels {
		if label == "stringer-generated" {
			t.Errorf("Labels %v should NOT contain 'stringer-generated' with snake_case convention", rec.Labels)
		}
	}
}

func TestSetConventions_NilConventionsDefaultBehavior(t *testing.T) {
	bf := NewBeadsFormatter()
	// Do not call SetConventions — nil conventions.

	sig := testSignal()
	rec := bf.signalToBead(sig)

	if !strings.HasPrefix(rec.ID, "str-") {
		t.Errorf("ID %q should start with 'str-' by default", rec.ID)
	}

	found := false
	for _, label := range rec.Labels {
		if label == "stringer-generated" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Labels %v should contain 'stringer-generated' by default", rec.Labels)
	}
}

func TestSetConventions_ResetToNil(t *testing.T) {
	bf := NewBeadsFormatter()

	// Set conventions.
	bf.SetConventions(&beads.Conventions{
		IDPrefix:   "custom-",
		LabelStyle: "snake_case",
	})

	// Reset to nil.
	bf.SetConventions(nil)

	sig := testSignal()
	rec := bf.signalToBead(sig)

	if !strings.HasPrefix(rec.ID, "str-") {
		t.Errorf("ID %q should start with 'str-' after reset", rec.ID)
	}
}

func TestSetConventions_KebabCaseLabelsExplicit(t *testing.T) {
	bf := NewBeadsFormatter()
	bf.SetConventions(&beads.Conventions{
		LabelStyle: "kebab-case",
	})

	sig := testSignal()
	rec := bf.signalToBead(sig)

	found := false
	for _, label := range rec.Labels {
		if label == "stringer-generated" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Labels %v should contain 'stringer-generated' with explicit kebab-case convention", rec.Labels)
	}
}

func TestSetConventions_EmptyPrefix(t *testing.T) {
	bf := NewBeadsFormatter()
	bf.SetConventions(&beads.Conventions{
		IDPrefix: "",
	})

	sig := testSignal()
	rec := bf.signalToBead(sig)

	// Empty prefix should fall back to default "str-".
	if !strings.HasPrefix(rec.ID, "str-") {
		t.Errorf("ID %q should start with 'str-' when conventions IDPrefix is empty", rec.ID)
	}
}

func TestFormat_WithConventions(t *testing.T) {
	bf := NewBeadsFormatter()
	bf.SetConventions(&beads.Conventions{
		IDPrefix:   "proj-",
		LabelStyle: "snake_case",
	})

	signals := []signal.RawSignal{testSignal()}
	var buf bytes.Buffer
	if err := bf.Format(signals, &buf); err != nil {
		t.Fatalf("Format() error: %v", err)
	}

	var rec map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	id, ok := rec["id"].(string)
	if !ok {
		t.Fatal("id field not a string")
	}
	if !strings.HasPrefix(id, "proj-") {
		t.Errorf("ID %q should start with 'proj-'", id)
	}

	labels, ok := rec["labels"].([]interface{})
	if !ok {
		t.Fatal("labels field not an array")
	}
	found := false
	for _, l := range labels {
		if l == "stringer_generated" {
			found = true
		}
	}
	if !found {
		t.Errorf("labels %v should contain 'stringer_generated'", labels)
	}
}

// -----------------------------------------------------------------------
// Pre-closed bead tests
// -----------------------------------------------------------------------

func TestPreClosedSignal_StatusClosed(t *testing.T) {
	closedAt := time.Date(2026, 1, 20, 14, 0, 0, 0, time.UTC)
	sig := signal.RawSignal{
		Source:      "github",
		Kind:        "github-closed-issue",
		FilePath:    "github/issues/42",
		Title:       "Fix login bug",
		Description: "Closed at: 2026-01-20, Reason: completed",
		Author:      "bob",
		Timestamp:   time.Date(2026, 1, 10, 10, 0, 0, 0, time.UTC),
		Confidence:  0.3,
		Tags:        []string{"github-closed-issue", "pre-closed"},
		ClosedAt:    closedAt,
	}

	rec := NewBeadsFormatter().signalToBead(sig)

	if rec.Status != "closed" {
		t.Errorf("Status = %q, want %q", rec.Status, "closed")
	}
	if rec.ClosedAt != "2026-01-20T14:00:00Z" {
		t.Errorf("ClosedAt = %q, want %q", rec.ClosedAt, "2026-01-20T14:00:00Z")
	}
	if rec.CloseReason != "completed" {
		t.Errorf("CloseReason = %q, want %q", rec.CloseReason, "completed")
	}
}

func TestPreClosedSignal_MergedPR(t *testing.T) {
	sig := signal.RawSignal{
		Source:     "github",
		Kind:       "github-merged-pr",
		FilePath:   "github/prs/99",
		Title:      "Add feature X",
		Author:     "alice",
		Timestamp:  time.Date(2026, 1, 5, 8, 0, 0, 0, time.UTC),
		Confidence: 0.3,
		Tags:       []string{"github-merged-pr", "pre-closed"},
		ClosedAt:   time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
	}

	rec := NewBeadsFormatter().signalToBead(sig)

	if rec.Status != "closed" {
		t.Errorf("Status = %q, want %q", rec.Status, "closed")
	}
	if rec.CloseReason != "merged" {
		t.Errorf("CloseReason = %q, want %q", rec.CloseReason, "merged")
	}
}

func TestPreClosedSignal_ClosedPR(t *testing.T) {
	sig := signal.RawSignal{
		Source:     "github",
		Kind:       "github-closed-pr",
		FilePath:   "github/prs/50",
		Title:      "Abandoned PR",
		Confidence: 0.2,
		Tags:       []string{"github-closed-pr", "pre-closed"},
		ClosedAt:   time.Date(2026, 1, 18, 16, 0, 0, 0, time.UTC),
	}

	rec := NewBeadsFormatter().signalToBead(sig)

	if rec.Status != "closed" {
		t.Errorf("Status = %q, want %q", rec.Status, "closed")
	}
	if rec.CloseReason != "closed" {
		t.Errorf("CloseReason = %q, want %q", rec.CloseReason, "closed")
	}
}

func TestSignalWithoutPreClosedTag_StaysOpen(t *testing.T) {
	sig := signal.RawSignal{
		Source:     "github",
		Kind:       "github-issue",
		Title:      "Open issue",
		Confidence: 0.5,
		Tags:       []string{"github-issue"},
	}

	rec := NewBeadsFormatter().signalToBead(sig)

	if rec.Status != "open" {
		t.Errorf("Status = %q, want %q", rec.Status, "open")
	}
	if rec.ClosedAt != "" {
		t.Errorf("ClosedAt = %q, want empty for open signal", rec.ClosedAt)
	}
	if rec.CloseReason != "" {
		t.Errorf("CloseReason = %q, want empty for open signal", rec.CloseReason)
	}
}

func TestPreClosedSignal_ZeroClosedAt(t *testing.T) {
	sig := signal.RawSignal{
		Source:     "github",
		Kind:       "github-closed-issue",
		Title:      "Closed with no timestamp",
		Confidence: 0.3,
		Tags:       []string{"github-closed-issue", "pre-closed"},
		// ClosedAt is zero value
	}

	rec := NewBeadsFormatter().signalToBead(sig)

	if rec.Status != "closed" {
		t.Errorf("Status = %q, want %q", rec.Status, "closed")
	}
	if rec.ClosedAt != "" {
		t.Errorf("ClosedAt = %q, want empty for zero time", rec.ClosedAt)
	}
}

func TestPreClosedSignal_JSONL(t *testing.T) {
	signals := []signal.RawSignal{
		{
			Source:     "github",
			Kind:       "github-closed-issue",
			FilePath:   "github/issues/10",
			Title:      "Closed bug",
			Author:     "dev",
			Timestamp:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Confidence: 0.3,
			Tags:       []string{"github-closed-issue", "pre-closed"},
			ClosedAt:   time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
		},
		testSignal(), // open signal for comparison
	}

	var buf bytes.Buffer
	f := NewBeadsFormatter()
	if err := f.Format(signals, &buf); err != nil {
		t.Fatalf("Format() error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	// Line 0: closed signal.
	var closed map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &closed); err != nil {
		t.Fatalf("line 0 is not valid JSON: %v", err)
	}
	if closed["status"] != "closed" {
		t.Errorf("line 0: status = %v, want 'closed'", closed["status"])
	}
	if closed["closed_at"] != "2026-01-10T00:00:00Z" {
		t.Errorf("line 0: closed_at = %v, want '2026-01-10T00:00:00Z'", closed["closed_at"])
	}
	if closed["close_reason"] != "completed" {
		t.Errorf("line 0: close_reason = %v, want 'completed'", closed["close_reason"])
	}

	// Line 1: open signal — no closed_at or close_reason.
	var open map[string]interface{}
	if err := json.Unmarshal([]byte(lines[1]), &open); err != nil {
		t.Fatalf("line 1 is not valid JSON: %v", err)
	}
	if open["status"] != "open" {
		t.Errorf("line 1: status = %v, want 'open'", open["status"])
	}
	if _, ok := open["closed_at"]; ok {
		t.Errorf("line 1: closed_at should be omitted for open signals")
	}
	if _, ok := open["close_reason"]; ok {
		t.Errorf("line 1: close_reason should be omitted for open signals")
	}
}

func TestClosedKindToTypeMapping(t *testing.T) {
	cases := []struct {
		kind     string
		wantType string
	}{
		{"github-closed-issue", "task"},
		{"github-merged-pr", "task"},
		{"github-closed-pr", "task"},
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

func TestBeadsFormatter_WriteFailure(t *testing.T) {
	f := NewBeadsFormatter()
	signals := []signal.RawSignal{
		{Source: "test", Kind: "todo", Title: "Task", FilePath: "a.go", Confidence: 0.5},
	}

	t.Run("fail_on_data_write", func(t *testing.T) {
		w := &failWriter{failAfter: 0}
		err := f.Format(signals, w)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "write signal") {
			t.Errorf("expected 'write signal' in error, got: %s", err.Error())
		}
	})

	t.Run("fail_on_newline_write", func(t *testing.T) {
		w := &failWriter{failAfter: 1}
		err := f.Format(signals, w)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "write newline") {
			t.Errorf("expected 'write newline' in error, got: %s", err.Error())
		}
	})
}

func TestDeriveCloseReason(t *testing.T) {
	cases := []struct {
		kind string
		want string
	}{
		{"github-merged-pr", "merged"},
		{"github-closed-pr", "closed"},
		{"github-closed-issue", "completed"},
		{"unknown-kind", "resolved"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			got := deriveCloseReason(tc.kind)
			if got != tc.want {
				t.Errorf("deriveCloseReason(%q) = %q, want %q", tc.kind, got, tc.want)
			}
		})
	}
}

func TestBeadsFormatter_WorkspaceScopedID(t *testing.T) {
	f := NewBeadsFormatter()
	sig := testSignal()
	sig.Workspace = "core"

	var buf bytes.Buffer
	if err := f.Format([]signal.RawSignal{sig}, &buf); err != nil {
		t.Fatalf("Format() error: %v", err)
	}

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	id, ok := rec["id"].(string)
	if !ok {
		t.Fatal("id field missing")
	}
	if !strings.HasPrefix(id, "str-core-") {
		t.Errorf("workspace-scoped ID should start with 'str-core-', got %q", id)
	}
}

func TestBeadsFormatter_WorkspaceLabel(t *testing.T) {
	f := NewBeadsFormatter()
	sig := testSignal()
	sig.Workspace = "api"

	var buf bytes.Buffer
	if err := f.Format([]signal.RawSignal{sig}, &buf); err != nil {
		t.Fatalf("Format() error: %v", err)
	}

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	labels, ok := rec["labels"].([]any)
	if !ok {
		t.Fatal("labels field missing or wrong type")
	}

	found := false
	for _, l := range labels {
		if l == "workspace:api" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'workspace:api' in labels, got %v", labels)
	}
}

func TestBeadsFormatter_NoWorkspaceNoScopeInID(t *testing.T) {
	f := NewBeadsFormatter()
	sig := testSignal()
	sig.Workspace = "" // non-monorepo

	var buf bytes.Buffer
	if err := f.Format([]signal.RawSignal{sig}, &buf); err != nil {
		t.Fatalf("Format() error: %v", err)
	}

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	id := rec["id"].(string)
	if !strings.HasPrefix(id, "str-") {
		t.Errorf("non-workspace ID should start with 'str-', got %q", id)
	}
	// Should NOT have a workspace scope.
	parts := strings.SplitN(id, "-", 3)
	if len(parts) >= 3 {
		// For non-workspace, the hash directly follows "str-".
		// The hash is 8 hex chars. Check it doesn't have a workspace name embedded.
		if len(parts[1]) > 8 {
			t.Errorf("non-workspace ID should not have workspace in prefix, got %q", id)
		}
	}
}
