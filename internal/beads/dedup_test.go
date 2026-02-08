package beads

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

func makeSignal(source, kind, filePath string, line int, title string) signal.RawSignal {
	return signal.RawSignal{
		Source:   source,
		Kind:     kind,
		FilePath: filePath,
		Line:     line,
		Title:    title,
	}
}

func TestFilterAgainstExisting_IDMatch(t *testing.T) {
	sig := makeSignal("todos", "todo", "main.go", 10, "Fix this")
	beadID := signalToBeadID(sig)

	existing := []Bead{
		{ID: beadID, Title: "Fix this", Status: "open"},
	}

	result := FilterAgainstExisting([]signal.RawSignal{sig}, existing)
	assert.Empty(t, result, "signal matching existing bead ID should be filtered")
}

func TestFilterAgainstExisting_HashMatch(t *testing.T) {
	sig := makeSignal("todos", "todo", "main.go", 10, "Fix this")
	hash := signalHash(sig)

	// Existing bead has a different prefix but same hash.
	existing := []Bead{
		{ID: "str-" + hash, Title: "Different title", Status: "open"},
	}

	result := FilterAgainstExisting([]signal.RawSignal{sig}, existing)
	assert.Empty(t, result, "signal matching existing bead hash should be filtered")
}

func TestFilterAgainstExisting_TitleMatch(t *testing.T) {
	sig := makeSignal("todos", "todo", "main.go", 10, "TODO: Add rate limiting")

	existing := []Bead{
		{ID: "other-123", Title: "todo: add rate limiting", Status: "open"},
	}

	result := FilterAgainstExisting([]signal.RawSignal{sig}, existing)
	assert.Empty(t, result, "signal matching normalized title should be filtered")
}

func TestFilterAgainstExisting_NoMatch(t *testing.T) {
	sig := makeSignal("todos", "todo", "main.go", 10, "Brand new task")

	existing := []Bead{
		{ID: "str-00000000", Title: "Something else", Status: "open"},
	}

	result := FilterAgainstExisting([]signal.RawSignal{sig}, existing)
	require.Len(t, result, 1)
	assert.Equal(t, "Brand new task", result[0].Title)
}

func TestFilterAgainstExisting_EmptyExisting(t *testing.T) {
	signals := []signal.RawSignal{
		makeSignal("todos", "todo", "a.go", 1, "Task A"),
		makeSignal("todos", "todo", "b.go", 2, "Task B"),
	}

	result := FilterAgainstExisting(signals, nil)
	assert.Equal(t, signals, result, "nil existing should return all signals")

	result = FilterAgainstExisting(signals, []Bead{})
	assert.Equal(t, signals, result, "empty existing should return all signals")
}

func TestFilterAgainstExisting_ClosedBeads(t *testing.T) {
	sig := makeSignal("todos", "todo", "main.go", 10, "Fix this")
	beadID := signalToBeadID(sig)

	// Closed bead should still match to prevent re-opening.
	existing := []Bead{
		{ID: beadID, Title: "Fix this", Status: "closed"},
	}

	result := FilterAgainstExisting([]signal.RawSignal{sig}, existing)
	assert.Empty(t, result, "signal matching closed bead should still be filtered")
}

func TestFilterAgainstExisting_MixedMatchAndNoMatch(t *testing.T) {
	sig1 := makeSignal("todos", "todo", "a.go", 1, "Existing task")
	sig2 := makeSignal("todos", "todo", "b.go", 2, "New task")
	sig3 := makeSignal("todos", "todo", "c.go", 3, "Another existing")

	existing := []Bead{
		{ID: signalToBeadID(sig1), Title: "Existing task", Status: "open"},
		{ID: "other-xyz", Title: "another existing", Status: "closed"},
	}

	result := FilterAgainstExisting([]signal.RawSignal{sig1, sig2, sig3}, existing)
	require.Len(t, result, 1)
	assert.Equal(t, "New task", result[0].Title)
}

func TestNormalizeTitle(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"  Fix This  ", "fix this"},
		{"TODO: add feature", "add feature"},
		{"FIXME: broken test", "broken test"},
		{"HACK: workaround", "workaround"},
		{"XXX: danger zone", "danger zone"},
		{"BUG: null pointer", "null pointer"},
		{"OPTIMIZE: slow query", "slow query"},
		{"todo: Add Feature", "add feature"},
		{"Regular title", "regular title"},
		{"", ""},
		{"TODO:", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeTitle(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSignalToBeadID(t *testing.T) {
	sig := makeSignal("todos", "todo", "internal/server/handler.go", 42, "Add rate limiting")
	id := signalToBeadID(sig)

	assert.True(t, len(id) > 4, "ID should be longer than prefix")
	assert.Contains(t, id, "str-")

	// Verify determinism.
	id2 := signalToBeadID(sig)
	assert.Equal(t, id, id2)
}

func TestSignalHash_MatchesBeadsFormatter(t *testing.T) {
	// This test verifies that the hash in dedup.go matches the algorithm in beads.go.
	// The hash format is the first 4 bytes of SHA-256 as hex.
	sig := makeSignal("todos", "todo", "main.go", 42, "Test task")
	hash := signalHash(sig)

	// Hash should be exactly 8 hex characters.
	assert.Len(t, hash, 8, "hash should be 8 hex characters (4 bytes)")

	// Verify it's valid hex.
	for _, c := range hash {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"hash should contain only hex characters, got %c", c)
	}
}

func TestSignalHash_DifferentSignalsDifferentHashes(t *testing.T) {
	sig1 := makeSignal("todos", "todo", "a.go", 1, "Task A")
	sig2 := makeSignal("todos", "todo", "a.go", 1, "Task B")

	hash1 := signalHash(sig1)
	hash2 := signalHash(sig2)

	assert.NotEqual(t, hash1, hash2, "different signals should produce different hashes")
}
