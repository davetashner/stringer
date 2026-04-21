// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package output

import (
	"testing"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
)

func TestSignalID_Deterministic(t *testing.T) {
	sig := signal.RawSignal{
		Source:   "todos",
		Kind:     "todo",
		FilePath: "main.go",
		Line:     42,
		Title:    "Add tests",
	}

	id1 := SignalID(sig, "str-")
	id2 := SignalID(sig, "str-")
	assert.Equal(t, id1, id2, "same signal should produce the same ID")
}

func TestSignalID_Format(t *testing.T) {
	sig := signal.RawSignal{
		Source:   "todos",
		Kind:     "todo",
		FilePath: "main.go",
		Line:     1,
		Title:    "Test",
	}

	id := SignalID(sig, "str-")
	assert.Regexp(t, `^str-[0-9a-f]{8}$`, id, "ID should be str- prefix + 8 hex chars")
}

func TestSignalID_CustomPrefix(t *testing.T) {
	sig := signal.RawSignal{
		Source:   "todos",
		Kind:     "todo",
		FilePath: "main.go",
		Line:     1,
		Title:    "Test",
	}

	id := SignalID(sig, "proj-")
	assert.Regexp(t, `^proj-[0-9a-f]{8}$`, id, "ID should use the given prefix")
}

func TestSignalID_FieldSensitivity(t *testing.T) {
	base := signal.RawSignal{
		Source:   "todos",
		Kind:     "todo",
		FilePath: "main.go",
		Line:     42,
		Title:    "Add tests",
	}

	tests := []struct {
		name   string
		mutate func(s signal.RawSignal) signal.RawSignal
	}{
		{"different_source", func(s signal.RawSignal) signal.RawSignal { s.Source = "gitlog"; return s }},
		{"different_kind", func(s signal.RawSignal) signal.RawSignal { s.Kind = "fixme"; return s }},
		{"different_filepath", func(s signal.RawSignal) signal.RawSignal { s.FilePath = "other.go"; return s }},
		{"different_line", func(s signal.RawSignal) signal.RawSignal { s.Line = 99; return s }},
		{"different_title", func(s signal.RawSignal) signal.RawSignal { s.Title = "Different title"; return s }},
	}

	baseID := SignalID(base, "str-")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutated := tt.mutate(base)
			mutatedID := SignalID(mutated, "str-")
			assert.NotEqual(t, baseID, mutatedID, "changing %s should produce a different ID", tt.name)
		})
	}
}

// TestSignalID_StabilityContract pins specific hash outputs so any change
// to the hash composition (input fields, order, separator, truncation,
// encoding) fails loudly. Signal IDs are persisted in the beads JSONL,
// baselines, and report output — changing them orphans existing records.
// Do NOT update these values without a planned migration path. See the
// stability contract in signalid.go.
func TestSignalID_StabilityContract(t *testing.T) {
	cases := []struct {
		name string
		sig  signal.RawSignal
		want string
	}{
		{
			name: "simple",
			sig:  signal.RawSignal{Source: "todos", Kind: "todo", FilePath: "main.go", Line: 42, Title: "Add tests"},
			want: "str-5b5245ca",
		},
		{
			name: "empty_fields",
			sig:  signal.RawSignal{},
			want: "str-c5c464c3",
		},
		{
			name: "unicode_title",
			sig:  signal.RawSignal{Source: "patterns", Kind: "antipattern", FilePath: "internal/foo/bar.go", Line: 123, Title: "Complex function — 复杂"},
			want: "str-e3e3ac3e",
		},
		{
			name: "zero_line",
			sig:  signal.RawSignal{Source: "todos", Kind: "todo", FilePath: "x.go", Line: 0, Title: "t"},
			want: "str-ebd1a532",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SignalID(tc.sig, "str-")
			assert.Equal(t, tc.want, got, "hash composition is part of the stability contract; see signalid.go")
		})
	}
}

func TestSignalID_MatchesBeadsFormatter(t *testing.T) {
	sig := testSignal()

	sharedID := SignalID(sig, "str-")
	beadsID := NewBeadsFormatter().generateID(sig)
	assert.Equal(t, beadsID, sharedID, "SignalID and BeadsFormatter.generateID should produce identical IDs")
}
