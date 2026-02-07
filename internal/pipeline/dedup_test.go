package pipeline

import (
	"testing"

	"github.com/davetashner/stringer/internal/signal"
)

func TestSignalHash_Deterministic(t *testing.T) {
	s := signal.RawSignal{
		Source:   "todos",
		Kind:     "todo",
		FilePath: "main.go",
		Line:     42,
		Title:    "Fix the thing",
	}

	h1 := SignalHash(s)
	h2 := SignalHash(s)

	if h1 != h2 {
		t.Errorf("hash not deterministic: %q != %q", h1, h2)
	}

	// SHA-256 truncated to 8 hex chars (4 bytes).
	if len(h1) != 8 {
		t.Errorf("hash length = %d, want 8", len(h1))
	}
}

func TestSignalHash_DifferentInputs(t *testing.T) {
	s1 := signal.RawSignal{
		Source:   "todos",
		Kind:     "todo",
		FilePath: "main.go",
		Line:     42,
		Title:    "Fix the thing",
	}
	s2 := signal.RawSignal{
		Source:   "todos",
		Kind:     "todo",
		FilePath: "main.go",
		Line:     43,
		Title:    "Fix the thing",
	}

	if SignalHash(s1) == SignalHash(s2) {
		t.Error("different signals should produce different hashes")
	}
}

func TestSignalHash_IgnoresNonKeyFields(t *testing.T) {
	s1 := signal.RawSignal{
		Source:      "todos",
		Kind:        "todo",
		FilePath:    "main.go",
		Line:        42,
		Title:       "Fix the thing",
		Description: "Description A",
		Author:      "alice",
		Confidence:  0.9,
	}
	s2 := signal.RawSignal{
		Source:      "todos",
		Kind:        "todo",
		FilePath:    "main.go",
		Line:        42,
		Title:       "Fix the thing",
		Description: "Description B",
		Author:      "bob",
		Confidence:  0.5,
	}

	if SignalHash(s1) != SignalHash(s2) {
		t.Error("signals with same key fields but different non-key fields should have same hash")
	}
}

func TestSignalHash_NullByteSeparation(t *testing.T) {
	// Ensure that field concatenation doesn't cause collisions.
	// "ab" + "c" vs "a" + "bc" should hash differently.
	s1 := signal.RawSignal{
		Source: "ab",
		Kind:   "c",
	}
	s2 := signal.RawSignal{
		Source: "a",
		Kind:   "bc",
	}

	if SignalHash(s1) == SignalHash(s2) {
		t.Error("different field boundaries should produce different hashes")
	}
}

func TestDeduplicateSignals_NoDuplicates(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "First", Confidence: 0.8},
		{Source: "todos", Kind: "todo", FilePath: "b.go", Line: 2, Title: "Second", Confidence: 0.7},
		{Source: "gitlog", Kind: "churn", FilePath: "c.go", Line: 3, Title: "Third", Confidence: 0.6},
	}

	result := DeduplicateSignals(signals)

	if len(result) != 3 {
		t.Errorf("expected 3 signals (no duplicates), got %d", len(result))
	}
}

func TestDeduplicateSignals_WithDuplicates(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "Fix bug", Confidence: 0.8},
		{Source: "todos", Kind: "todo", FilePath: "b.go", Line: 2, Title: "Add feature", Confidence: 0.7},
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "Fix bug", Confidence: 0.6}, // duplicate of first
	}

	result := DeduplicateSignals(signals)

	if len(result) != 2 {
		t.Errorf("expected 2 signals after dedup, got %d", len(result))
	}
	if result[0].Title != "Fix bug" {
		t.Errorf("first signal Title = %q, want %q", result[0].Title, "Fix bug")
	}
	if result[1].Title != "Add feature" {
		t.Errorf("second signal Title = %q, want %q", result[1].Title, "Add feature")
	}
}

func TestDeduplicateSignals_KeepsFirst(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "Same",
			Description: "First description", Confidence: 0.5},
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "Same",
			Description: "Second description", Confidence: 0.3},
	}

	result := DeduplicateSignals(signals)

	if len(result) != 1 {
		t.Fatalf("expected 1 signal after dedup, got %d", len(result))
	}
	if result[0].Description != "First description" {
		t.Errorf("should keep first occurrence, got Description = %q", result[0].Description)
	}
}

func TestDeduplicateSignals_UpdatesConfidenceHigher(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "Same", Confidence: 0.5},
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "Same", Confidence: 0.9},
	}

	result := DeduplicateSignals(signals)

	if len(result) != 1 {
		t.Fatalf("expected 1 signal after dedup, got %d", len(result))
	}
	if result[0].Confidence != 0.9 {
		t.Errorf("Confidence should be updated to higher value 0.9, got %v", result[0].Confidence)
	}
}

func TestDeduplicateSignals_DoesNotDowngradeConfidence(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "Same", Confidence: 0.9},
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "Same", Confidence: 0.5},
	}

	result := DeduplicateSignals(signals)

	if len(result) != 1 {
		t.Fatalf("expected 1 signal after dedup, got %d", len(result))
	}
	if result[0].Confidence != 0.9 {
		t.Errorf("Confidence should remain at 0.9, got %v", result[0].Confidence)
	}
}

func TestDeduplicateSignals_EmptySlice(t *testing.T) {
	result := DeduplicateSignals(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}

	result = DeduplicateSignals([]signal.RawSignal{})
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d signals", len(result))
	}
}

func TestDeduplicateSignals_SingleSignal(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "Only one", Confidence: 0.8},
	}

	result := DeduplicateSignals(signals)

	if len(result) != 1 {
		t.Errorf("expected 1 signal, got %d", len(result))
	}
}

func TestDeduplicateSignals_AllDuplicates(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "Same", Confidence: 0.5},
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "Same", Confidence: 0.7},
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "Same", Confidence: 0.3},
	}

	result := DeduplicateSignals(signals)

	if len(result) != 1 {
		t.Fatalf("expected 1 signal after dedup, got %d", len(result))
	}
	// Should have the highest confidence from all duplicates.
	if result[0].Confidence != 0.7 {
		t.Errorf("Confidence should be 0.7 (highest), got %v", result[0].Confidence)
	}
}

func TestDeduplicateSignals_PreservesOrder(t *testing.T) {
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", FilePath: "c.go", Line: 3, Title: "Third", Confidence: 0.6},
		{Source: "todos", Kind: "todo", FilePath: "a.go", Line: 1, Title: "First", Confidence: 0.8},
		{Source: "todos", Kind: "todo", FilePath: "b.go", Line: 2, Title: "Second", Confidence: 0.7},
	}

	result := DeduplicateSignals(signals)

	if len(result) != 3 {
		t.Fatalf("expected 3 signals, got %d", len(result))
	}
	if result[0].Title != "Third" {
		t.Errorf("first result Title = %q, want %q", result[0].Title, "Third")
	}
	if result[1].Title != "First" {
		t.Errorf("second result Title = %q, want %q", result[1].Title, "First")
	}
	if result[2].Title != "Second" {
		t.Errorf("third result Title = %q, want %q", result[2].Title, "Second")
	}
}
