package report

import (
	"errors"
	"io"
	"testing"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSection is a minimal Section implementation for registry tests.
type stubSection struct {
	name string
	desc string
}

func (s *stubSection) Name() string                       { return s.name }
func (s *stubSection) Description() string                { return s.desc }
func (s *stubSection) Analyze(_ *signal.ScanResult) error { return nil }
func (s *stubSection) Render(_ io.Writer) error           { return nil }

// restoreSections resets the registry and re-registers all init-registered sections.
func restoreSections() {
	resetForTesting()
	Register(&lotteryRiskSection{})
	Register(&churnSection{})
	Register(&todoAgeSection{})
	Register(&coverageSection{})
}

func TestRegister_And_Get(t *testing.T) {
	resetForTesting()
	defer restoreSections()

	s := &stubSection{name: "test-section", desc: "A test section"}
	Register(s)

	got := Get("test-section")
	require.NotNil(t, got)
	assert.Equal(t, "test-section", got.Name())
	assert.Equal(t, "A test section", got.Description())
}

func TestRegister_DuplicatePanics(t *testing.T) {
	resetForTesting()
	defer restoreSections()

	Register(&stubSection{name: "dup"})
	assert.Panics(t, func() {
		Register(&stubSection{name: "dup"})
	})
}

func TestGet_NotFound(t *testing.T) {
	resetForTesting()
	defer restoreSections()
	assert.Nil(t, Get("nonexistent"))
}

func TestList_ReturnsRegistrationOrder(t *testing.T) {
	resetForTesting()
	defer restoreSections()

	Register(&stubSection{name: "charlie"})
	Register(&stubSection{name: "alpha"})
	Register(&stubSection{name: "bravo"})

	names := List()
	assert.Equal(t, []string{"charlie", "alpha", "bravo"}, names)
}

func TestList_ReturnsCopy(t *testing.T) {
	resetForTesting()
	defer restoreSections()

	Register(&stubSection{name: "one"})
	names := List()
	names[0] = "mutated"
	assert.Equal(t, []string{"one"}, List())
}

func TestResetForTesting(t *testing.T) {
	resetForTesting()
	defer restoreSections()

	Register(&stubSection{name: "temp"})
	require.NotNil(t, Get("temp"))

	resetForTesting()
	assert.Nil(t, Get("temp"))
	assert.Empty(t, List())
}

func TestErrMetricsNotAvailable(t *testing.T) {
	wrapped := errors.Join(ErrMetricsNotAvailable, errors.New("lotteryrisk collector not run"))
	assert.True(t, errors.Is(wrapped, ErrMetricsNotAvailable))
}
