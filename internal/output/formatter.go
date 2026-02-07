// Package output defines the OutputFormatter interface for writing scan results
// in various formats.
package output

import (
	"io"

	"github.com/davetashner/stringer/internal/signal"
)

// Formatter writes a slice of raw signals to the given writer in a specific format.
type Formatter interface {
	// Name returns the format name (e.g., "beads", "json", "markdown").
	Name() string

	// Format writes the signals to w.
	Format(signals []signal.RawSignal, w io.Writer) error
}
