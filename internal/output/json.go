package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/davetashner/stringer/internal/signal"
)

func init() {
	RegisterFormatter(NewJSONFormatter())
}

// JSONEnvelope wraps signals with metadata for the JSON output format.
type JSONEnvelope struct {
	Signals  []signal.RawSignal `json:"signals"`
	Metadata JSONMetadata       `json:"metadata"`
}

// JSONMetadata contains information about the scan that produced these signals.
type JSONMetadata struct {
	TotalCount  int      `json:"total_count"`
	Collectors  []string `json:"collectors"`
	GeneratedAt string   `json:"generated_at"`
}

// JSONFormatter writes signals as a JSON object with metadata envelope.
type JSONFormatter struct {
	// Compact controls whether output is compact (single line) or pretty-printed.
	// When false (default), output is indented with two spaces.
	Compact bool

	// nowFunc is used for testing to override the current time.
	nowFunc func() time.Time
}

// Compile-time interface check.
var _ Formatter = (*JSONFormatter)(nil)

// NewJSONFormatter returns a new JSONFormatter with default settings.
func NewJSONFormatter() *JSONFormatter {
	return &JSONFormatter{}
}

// Name returns the format name.
func (f *JSONFormatter) Name() string {
	return "json"
}

// Format writes all signals as a JSON document with a metadata envelope to w.
// If Compact is false and w is a TTY (an *os.File connected to a terminal),
// or Compact is explicitly false, output is pretty-printed. If Compact is true,
// output is a single line.
func (f *JSONFormatter) Format(signals []signal.RawSignal, w io.Writer) error {
	if signals == nil {
		signals = []signal.RawSignal{}
	}

	// Collect unique collector names from the signals.
	collectors := extractCollectors(signals)

	now := time.Now()
	if f.nowFunc != nil {
		now = f.nowFunc()
	}

	envelope := JSONEnvelope{
		Signals: signals,
		Metadata: JSONMetadata{
			TotalCount:  len(signals),
			Collectors:  collectors,
			GeneratedAt: now.UTC().Format("2006-01-02T15:04:05Z"),
		},
	}

	compact := f.shouldCompact(w)

	var data []byte
	var err error
	if compact {
		data, err = json.Marshal(envelope)
	} else {
		data, err = json.MarshalIndent(envelope, "", "  ")
	}
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write json trailing newline: %w", err)
	}

	return nil
}

// shouldCompact determines whether to use compact mode.
// If Compact is explicitly set, use that value.
// Otherwise, auto-detect: pretty-print for TTYs, compact for pipes.
func (f *JSONFormatter) shouldCompact(w io.Writer) bool {
	// If Compact is explicitly set to true, always compact.
	if f.Compact {
		return true
	}

	// Auto-detect: if the writer is an *os.File, check if it's a terminal.
	if file, ok := w.(*os.File); ok {
		fi, err := file.Stat()
		if err != nil {
			return false // default to pretty on error
		}
		// If the file is a character device (terminal), pretty-print.
		// Otherwise (pipe, regular file), use compact.
		if fi.Mode()&os.ModeCharDevice != 0 {
			return false // TTY -> pretty
		}
		return true // pipe/file -> compact
	}

	// For non-file writers (e.g., bytes.Buffer in tests), default to pretty.
	return false
}

// extractCollectors returns a sorted, deduplicated list of collector names
// from the given signals.
func extractCollectors(signals []signal.RawSignal) []string {
	seen := make(map[string]bool)
	var names []string
	for _, s := range signals {
		if s.Source != "" && !seen[s.Source] {
			seen[s.Source] = true
			names = append(names, s.Source)
		}
	}
	// Sort for deterministic output.
	if len(names) > 1 {
		slices.Sort(names)
	}
	return names
}
