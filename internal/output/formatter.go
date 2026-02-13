// Package output defines the OutputFormatter interface for writing scan results
// in various formats.
package output

import (
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/davetashner/stringer/internal/signal"
)

// Formatter writes a slice of raw signals to the given writer in a specific format.
type Formatter interface {
	// Name returns the format name (e.g., "beads", "json", "markdown").
	Name() string

	// Format writes the signals to w.
	Format(signals []signal.RawSignal, w io.Writer) error
}

// DirectoryFormatter extends Formatter for formats that produce a directory
// of files (e.g., index.html + assets/) instead of a single stream.
type DirectoryFormatter interface {
	Formatter
	FormatDir(signals []signal.RawSignal, dir string) error
}

var (
	fmtMu       sync.RWMutex
	fmtRegistry = make(map[string]Formatter)
)

// RegisterFormatter adds a formatter to the global registry.
func RegisterFormatter(f Formatter) {
	fmtMu.Lock()
	defer fmtMu.Unlock()
	fmtRegistry[f.Name()] = f
}

// GetFormatter returns the formatter with the given name, or an error if not found.
func GetFormatter(name string) (Formatter, error) {
	fmtMu.RLock()
	defer fmtMu.RUnlock()
	f, ok := fmtRegistry[name]
	if !ok {
		return nil, fmt.Errorf("unknown format: %q (available: %s)", name, formatNames())
	}
	return f, nil
}

// resetFmtForTesting clears the formatter registry. Only for use in tests.
func resetFmtForTesting() {
	fmtMu.Lock()
	defer fmtMu.Unlock()
	fmtRegistry = make(map[string]Formatter)
}

// formatNames returns a comma-separated sorted list of registered format names.
func formatNames() string {
	names := make([]string, 0, len(fmtRegistry))
	for name := range fmtRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	result := ""
	for i, n := range names {
		if i > 0 {
			result += ", "
		}
		result += n
	}
	return result
}
