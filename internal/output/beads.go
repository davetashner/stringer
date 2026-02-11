package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/davetashner/stringer/internal/beads"
	"github.com/davetashner/stringer/internal/signal"
)

// beadRecord is the JSON structure written for each bead in JSONL output.
// Fields are tagged to match the schema expected by `bd import`.
type beadRecord struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Type        string   `json:"type"`
	Priority    int      `json:"priority"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"created_at,omitempty"`
	CreatedBy   string   `json:"created_by"`
	Labels      []string `json:"labels,omitempty"`
	ClosedAt    string   `json:"closed_at,omitempty"`
	CloseReason string   `json:"close_reason,omitempty"`
}

func init() {
	RegisterFormatter(NewBeadsFormatter())
}

// BeadsFormatter writes signals as Beads-compatible JSONL.
type BeadsFormatter struct {
	conventions *beads.Conventions
}

// Compile-time interface check.
var _ Formatter = (*BeadsFormatter)(nil)

// NewBeadsFormatter returns a new BeadsFormatter.
func NewBeadsFormatter() *BeadsFormatter {
	return &BeadsFormatter{}
}

// SetConventions configures the formatter to adopt existing beads conventions.
// Passing nil resets to default behavior.
func (b *BeadsFormatter) SetConventions(c *beads.Conventions) {
	b.conventions = c
}

// Name returns the format name.
func (b *BeadsFormatter) Name() string {
	return "beads"
}

// Format writes each signal as a single-line JSON object to w.
// Each line is valid JSON parseable by `bd import`.
func (b *BeadsFormatter) Format(signals []signal.RawSignal, w io.Writer) error {
	for i, sig := range signals {
		rec := b.signalToBead(sig)
		data, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("marshal signal %d: %w", i, err)
		}
		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("write signal %d: %w", i, err)
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			return fmt.Errorf("write newline %d: %w", i, err)
		}
	}
	return nil
}

// signalToBead converts a RawSignal into a beadRecord.
func (b *BeadsFormatter) signalToBead(sig signal.RawSignal) beadRecord {
	priority := mapConfidenceToPriority(sig.Confidence)
	if sig.Priority != nil {
		priority = *sig.Priority
	}

	rec := beadRecord{
		ID:          b.generateID(sig),
		Title:       sig.Title,
		Description: buildDescription(sig),
		Type:        mapKindToType(sig.Kind),
		Priority:    priority,
		Status:      "open",
		CreatedAt:   formatTimestamp(sig.Timestamp),
		CreatedBy:   resolveAuthor(sig.Author),
		Labels:      b.buildLabels(sig),
	}

	if hasTag(sig.Tags, "pre-closed") {
		rec.Status = "closed"
		rec.ClosedAt = formatTimestamp(sig.ClosedAt)
		rec.CloseReason = deriveCloseReason(sig.Kind)
	}

	return rec
}

// hasTag returns true if tags contains the given tag.
func hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// deriveCloseReason maps a signal Kind to a human-readable close reason.
func deriveCloseReason(kind string) string {
	switch kind {
	case "github-merged-pr":
		return "merged"
	case "github-closed-pr":
		return "closed"
	case "github-closed-issue":
		return "completed"
	default:
		return "resolved"
	}
}

// generateID produces a deterministic ID from signal content.
// It delegates to the shared signalID helper and applies convention overrides.
func (b *BeadsFormatter) generateID(sig signal.RawSignal) string {
	prefix := "str-"
	if b.conventions != nil && b.conventions.IDPrefix != "" {
		prefix = b.conventions.IDPrefix
	}
	return signalID(sig, prefix)
}

// mapKindToType maps a signal Kind to a bead type.
func mapKindToType(kind string) string {
	switch strings.ToLower(kind) {
	case "bug", "fixme":
		return "bug"
	case "github-bug":
		return "bug"
	case "todo":
		return "task"
	case "hack", "xxx", "optimize", "low-lottery-risk":
		return "chore"
	case "github-feature", "github-issue", "github-pr-changes", "github-pr-approved", "github-pr-pending", "github-review-todo",
		"github-closed-issue", "github-merged-pr", "github-closed-pr":
		return "task"
	default:
		return "task"
	}
}

// mapConfidenceToPriority derives bead priority from signal confidence.
// >=0.8 -> P1, >=0.6 -> P2, >=0.4 -> P3, else P4.
func mapConfidenceToPriority(confidence float64) int {
	switch {
	case confidence >= 0.8:
		return 1
	case confidence >= 0.6:
		return 2
	case confidence >= 0.4:
		return 3
	default:
		return 4
	}
}

// formatTimestamp formats a time.Time as ISO 8601.
// Returns empty string for zero time.
func formatTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

// resolveAuthor returns the signal author, falling back to "stringer".
func resolveAuthor(author string) string {
	if author == "" {
		return "stringer"
	}
	return author
}

// buildDescription combines the signal description with file location context.
func buildDescription(sig signal.RawSignal) string {
	var parts []string

	if sig.Description != "" {
		parts = append(parts, sig.Description)
	}

	if sig.FilePath != "" {
		loc := sig.FilePath
		if sig.Line > 0 {
			loc = fmt.Sprintf("%s:%d", sig.FilePath, sig.Line)
		}
		parts = append(parts, fmt.Sprintf("Location: %s", loc))
	}

	return strings.Join(parts, "\n\n")
}

// buildLabels combines signal tags with standard stringer labels.
func (b *BeadsFormatter) buildLabels(sig signal.RawSignal) []string {
	labels := make([]string, 0, len(sig.Tags)+2)
	labels = append(labels, sig.Tags...)
	generatedLabel := "stringer-generated"
	if b.conventions != nil && b.conventions.LabelStyle == "snake_case" {
		generatedLabel = "stringer_generated"
	}
	labels = append(labels, generatedLabel)
	if sig.Source != "" {
		labels = append(labels, sig.Source)
	}
	return labels
}
