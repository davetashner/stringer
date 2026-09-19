// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package output

import (
	"testing"

	"github.com/davetashner/stringer/internal/baseline"
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

// TestStableSignalID_StabilityContract pins stable keys: they are committed in
// baselines (.stringer/baseline.json, DR-027), so changing the composition
// silently un-suppresses every baselined finding.
func TestStableSignalID_StabilityContract(t *testing.T) {
	merge := signal.RawSignal{Source: "complexity", Kind: "complex-function", FilePath: "internal/config/merge.go",
		Line: 14, Title: "Complex function: Merge (cyclomatic: 88, cognitive: 163, nesting: 4)"}
	assert.Equal(t, "sts-301a576c", StableSignalID(merge))
	assert.Equal(t, "sts-709e80c8", StableSignalID(signal.RawSignal{}))
}

func TestStableSignalID_IgnoresLineAndNumbers(t *testing.T) {
	base := signal.RawSignal{Source: "complexity", Kind: "complex-function", FilePath: "a.go",
		Line: 14, Title: "Complex function: Merge (cyclomatic: 88, cognitive: 163, nesting: 4)"}
	moved := base
	moved.Line = 15
	improved := base
	improved.Title = "Complex function: Merge (cyclomatic: 85, cognitive: 150, nesting: 3)"
	decimal := signal.RawSignal{Title: "Large binary file: x.png (1.2 MB)"}
	decimal2 := signal.RawSignal{Title: "Large binary file: x.png (3.75 MB)"}

	assert.Equal(t, StableSignalID(base), StableSignalID(moved), "line shift keeps the key")
	assert.Equal(t, StableSignalID(base), StableSignalID(improved), "metric change keeps the key")
	assert.Equal(t, StableSignalID(decimal), StableSignalID(decimal2), "decimals are masked")
	assert.Regexp(t, `^sts-[0-9a-f]{8}$`, StableSignalID(base))
}

func TestStableSignalID_FieldSensitivity(t *testing.T) {
	base := signal.RawSignal{Source: "complexity", Kind: "complex-function", FilePath: "a.go",
		Title: "Complex function: parseV2 (cyclomatic: 20)"}
	tests := map[string]func(s signal.RawSignal) signal.RawSignal{
		"source":   func(s signal.RawSignal) signal.RawSignal { s.Source = "deadcode"; return s },
		"kind":     func(s signal.RawSignal) signal.RawSignal { s.Kind = "complex-method"; return s },
		"filepath": func(s signal.RawSignal) signal.RawSignal { s.FilePath = "b.go"; return s },
		"symbol": func(s signal.RawSignal) signal.RawSignal {
			s.Title = "Complex function: Other (cyclomatic: 20)"
			return s
		},
		"identifier_digit": func(s signal.RawSignal) signal.RawSignal {
			s.Title = "Complex function: parseV3 (cyclomatic: 20)"
			return s
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			assert.NotEqual(t, StableSignalID(base), StableSignalID(mutate(base)))
		})
	}
}

func TestLookupSuppression(t *testing.T) {
	sig := signal.RawSignal{Source: "todos", Kind: "bug", FilePath: "a.go", Line: 3, Title: "BUG: x"}
	exact := SignalID(sig, "str-")
	stable := StableSignalID(sig)

	sup, ok := LookupSuppression(map[string]baseline.Suppression{exact: {SignalID: exact}}, sig, "str-")
	assert.True(t, ok)
	assert.Equal(t, exact, sup.SignalID)

	sup, ok = LookupSuppression(map[string]baseline.Suppression{stable: {SignalID: stable}}, sig, "str-")
	assert.True(t, ok)
	assert.Equal(t, stable, sup.SignalID)

	_, ok = LookupSuppression(map[string]baseline.Suppression{"str-00000000": {}}, sig, "str-")
	assert.False(t, ok)
	_, ok = LookupSuppression(nil, sig, "str-")
	assert.False(t, ok)
}
