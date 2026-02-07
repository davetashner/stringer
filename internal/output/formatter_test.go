package output

import (
	"bytes"
	"io"
	"testing"

	"github.com/davetashner/stringer/internal/signal"
)

// Compile-time interface check.
var _ Formatter = (*stubFormatter)(nil)

type stubFormatter struct{}

func (s *stubFormatter) Name() string                                   { return "stub" }
func (s *stubFormatter) Format(_ []signal.RawSignal, _ io.Writer) error { return nil }

func TestFormatterInterface(t *testing.T) {
	var f Formatter = &stubFormatter{}
	if f.Name() != "stub" {
		t.Errorf("Name() = %q, want %q", f.Name(), "stub")
	}

	var buf bytes.Buffer
	if err := f.Format(nil, &buf); err != nil {
		t.Errorf("Format() error = %v", err)
	}
}
