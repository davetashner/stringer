package output

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

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
	CreatedAt   string   `json:"created_at"`
	CreatedBy   string   `json:"created_by"`
	Labels      []string `json:"labels,omitempty"`
}

// BeadsFormatter writes signals as Beads-compatible JSONL.
type BeadsFormatter struct{}

// Compile-time interface check.
var _ Formatter = (*BeadsFormatter)(nil)

// NewBeadsFormatter returns a new BeadsFormatter.
func NewBeadsFormatter() *BeadsFormatter {
	return &BeadsFormatter{}
}

// Name returns the format name.
func (b *BeadsFormatter) Name() string {
	return "beads"
}

// Format writes each signal as a single-line JSON object to w.
// Each line is valid JSON parseable by `bd import`.
func (b *BeadsFormatter) Format(signals []signal.RawSignal, w io.Writer) error {
	for i, sig := range signals {
		rec := signalToBead(sig)
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
func signalToBead(sig signal.RawSignal) beadRecord {
	return beadRecord{
		ID:          generateID(sig),
		Title:       sig.Title,
		Description: buildDescription(sig),
		Type:        mapKindToType(sig.Kind),
		Priority:    mapConfidenceToPriority(sig.Confidence),
		Status:      "open",
		CreatedAt:   formatTimestamp(sig.Timestamp),
		CreatedBy:   resolveAuthor(sig.Author),
		Labels:      buildLabels(sig),
	}
}

// generateID produces a deterministic ID from signal content.
// It hashes Source + Kind + FilePath + Line + Title using SHA-256,
// truncates to 8 hex characters, and prefixes with "str-".
func generateID(sig signal.RawSignal) string {
	h := sha256.New()
	// Write each field separated by null bytes to avoid collisions
	// from field concatenation (e.g., "ab"+"c" vs "a"+"bc").
	// sha256.Hash.Write never returns an error per the hash.Hash contract.
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d\x00%s", sig.Source, sig.Kind, sig.FilePath, sig.Line, sig.Title)
	sum := h.Sum(nil)
	return fmt.Sprintf("str-%x", sum[:4])
}

// mapKindToType maps a signal Kind to a bead type.
func mapKindToType(kind string) string {
	switch strings.ToLower(kind) {
	case "bug", "fixme":
		return "bug"
	case "todo":
		return "task"
	case "hack", "xxx", "optimize":
		return "chore"
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
func buildLabels(sig signal.RawSignal) []string {
	labels := make([]string, 0, len(sig.Tags)+2)
	labels = append(labels, sig.Tags...)
	labels = append(labels, "stringer-generated")
	if sig.Source != "" {
		labels = append(labels, sig.Source)
	}
	return labels
}
