// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package pipeline

import (
	"testing"
	"time"

	"github.com/davetashner/stringer/internal/baseline"
	"github.com/davetashner/stringer/internal/output"
	"github.com/davetashner/stringer/internal/signal"
)

func makeTestSignal(source, kind, file string, line int, title string) signal.RawSignal {
	return signal.RawSignal{
		Source:     source,
		Kind:       kind,
		FilePath:   file,
		Line:       line,
		Title:      title,
		Confidence: 0.5,
	}
}

func TestFilterSuppressed_NilBaseline(t *testing.T) {
	signals := []signal.RawSignal{
		makeTestSignal("todos", "todo", "main.go", 10, "fix this"),
	}
	result, count := FilterSuppressed(signals, nil, "str-")
	if count != 0 {
		t.Errorf("suppressed count = %d, want 0", count)
	}
	if len(result) != 1 {
		t.Errorf("result length = %d, want 1", len(result))
	}
}

func TestFilterSuppressed_EmptyBaseline(t *testing.T) {
	signals := []signal.RawSignal{
		makeTestSignal("todos", "todo", "main.go", 10, "fix this"),
	}
	state := &baseline.BaselineState{
		Version:      "1",
		Suppressions: []baseline.Suppression{},
	}
	result, count := FilterSuppressed(signals, state, "str-")
	if count != 0 {
		t.Errorf("suppressed count = %d, want 0", count)
	}
	if len(result) != 1 {
		t.Errorf("result length = %d, want 1", len(result))
	}
}

func TestFilterSuppressed_SomeMatched(t *testing.T) {
	signals := []signal.RawSignal{
		makeTestSignal("todos", "todo", "main.go", 10, "fix this"),
		makeTestSignal("todos", "todo", "main.go", 20, "handle error"),
		makeTestSignal("todos", "todo", "util.go", 5, "refactor"),
		makeTestSignal("gitlog", "churn", "main.go", 0, "high churn"),
		makeTestSignal("patterns", "pattern", "config.go", 1, "singleton"),
	}

	// Suppress the first 3 signals by computing their IDs.
	prefix := "str-"
	id0 := output.SignalID(signals[0], prefix)
	id1 := output.SignalID(signals[1], prefix)
	id2 := output.SignalID(signals[2], prefix)

	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: id0, Reason: baseline.ReasonAcknowledged, SuppressedAt: time.Now()},
			{SignalID: id1, Reason: baseline.ReasonWontFix, SuppressedAt: time.Now()},
			{SignalID: id2, Reason: baseline.ReasonFalsePositive, SuppressedAt: time.Now()},
		},
	}

	result, count := FilterSuppressed(signals, state, prefix)
	if count != 3 {
		t.Errorf("suppressed count = %d, want 3", count)
	}
	if len(result) != 2 {
		t.Errorf("result length = %d, want 2", len(result))
	}

	// Verify the remaining signals are the unsuppressed ones.
	if result[0].Title != "high churn" {
		t.Errorf("result[0].Title = %q, want %q", result[0].Title, "high churn")
	}
	if result[1].Title != "singleton" {
		t.Errorf("result[1].Title = %q, want %q", result[1].Title, "singleton")
	}
}

func TestFilterSuppressed_ExpiredNotFiltered(t *testing.T) {
	sig := makeTestSignal("todos", "todo", "main.go", 10, "expired suppression")
	prefix := "str-"
	id := output.SignalID(sig, prefix)

	past := time.Now().Add(-24 * time.Hour)
	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: id, Reason: baseline.ReasonAcknowledged, SuppressedAt: time.Now().Add(-48 * time.Hour), ExpiresAt: &past},
		},
	}

	result, count := FilterSuppressed([]signal.RawSignal{sig}, state, prefix)
	if count != 0 {
		t.Errorf("suppressed count = %d, want 0 (expired suppression should not filter)", count)
	}
	if len(result) != 1 {
		t.Errorf("result length = %d, want 1", len(result))
	}
}

func TestFilterSuppressed_NotExpiredStillFiltered(t *testing.T) {
	sig := makeTestSignal("todos", "todo", "main.go", 10, "future expiry")
	prefix := "str-"
	id := output.SignalID(sig, prefix)

	future := time.Now().Add(24 * time.Hour)
	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: id, Reason: baseline.ReasonWontFix, SuppressedAt: time.Now(), ExpiresAt: &future},
		},
	}

	result, count := FilterSuppressed([]signal.RawSignal{sig}, state, prefix)
	if count != 1 {
		t.Errorf("suppressed count = %d, want 1", count)
	}
	if len(result) != 0 {
		t.Errorf("result length = %d, want 0", len(result))
	}
}

func TestFilterSuppressed_RoundTrip(t *testing.T) {
	// Verify that signal ID computation is consistent: create a signal,
	// compute its ID, store in baseline, and confirm it gets filtered.
	sig := signal.RawSignal{
		Source:     "todos",
		Kind:       "todo",
		FilePath:   "pkg/handler.go",
		Line:       42,
		Title:      "TODO: implement retry logic",
		Confidence: 0.7,
	}
	prefix := "str-"
	id := output.SignalID(sig, prefix)

	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: id, Reason: baseline.ReasonAcknowledged, SuppressedAt: time.Now()},
		},
	}

	result, count := FilterSuppressed([]signal.RawSignal{sig}, state, prefix)
	if count != 1 {
		t.Errorf("round-trip: suppressed count = %d, want 1", count)
	}
	if len(result) != 0 {
		t.Errorf("round-trip: result length = %d, want 0", len(result))
	}
}

func TestFilterSuppressed_EmptySignals(t *testing.T) {
	state := &baseline.BaselineState{
		Version: "1",
		Suppressions: []baseline.Suppression{
			{SignalID: "str-abc123", Reason: baseline.ReasonAcknowledged, SuppressedAt: time.Now()},
		},
	}
	result, count := FilterSuppressed(nil, state, "str-")
	if count != 0 {
		t.Errorf("suppressed count = %d, want 0", count)
	}
	if len(result) != 0 {
		t.Errorf("result length = %d, want 0", len(result))
	}
}
