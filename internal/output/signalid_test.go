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

	id1 := signalID(sig, "str-")
	id2 := signalID(sig, "str-")
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

	id := signalID(sig, "str-")
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

	id := signalID(sig, "proj-")
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

	baseID := signalID(base, "str-")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutated := tt.mutate(base)
			mutatedID := signalID(mutated, "str-")
			assert.NotEqual(t, baseID, mutatedID, "changing %s should produce a different ID", tt.name)
		})
	}
}

func TestSignalID_MatchesBeadsFormatter(t *testing.T) {
	sig := testSignal()

	sharedID := signalID(sig, "str-")
	beadsID := NewBeadsFormatter().generateID(sig)
	assert.Equal(t, beadsID, sharedID, "signalID and BeadsFormatter.generateID should produce identical IDs")
}
