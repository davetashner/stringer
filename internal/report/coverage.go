package report

import (
	"fmt"
	"io"
	"sort"

	"github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/signal"
)

func init() {
	Register(&coverageSection{})
}

// coverageSection reports test coverage gaps by directory.
type coverageSection struct {
	dirs []collectors.DirectoryTestRatio
}

func (s *coverageSection) Name() string        { return "coverage" }
func (s *coverageSection) Description() string { return "Test coverage gaps by directory" }

func (s *coverageSection) Analyze(result *signal.ScanResult) error {
	raw, ok := result.Metrics["patterns"]
	if !ok {
		return fmt.Errorf("patterns: %w", ErrMetricsNotAvailable)
	}
	m, ok := raw.(*collectors.PatternsMetrics)
	if !ok || m == nil {
		return fmt.Errorf("patterns: %w", ErrMetricsNotAvailable)
	}

	// Copy and sort by ratio ascending (worst coverage first).
	s.dirs = make([]collectors.DirectoryTestRatio, len(m.DirectoryTestRatios))
	copy(s.dirs, m.DirectoryTestRatios)
	sort.Slice(s.dirs, func(i, j int) bool {
		return s.dirs[i].Ratio < s.dirs[j].Ratio
	})

	return nil
}

func (s *coverageSection) Render(w io.Writer) error {
	_, _ = fmt.Fprintf(w, "Test Coverage Gaps\n")
	_, _ = fmt.Fprintf(w, "------------------\n")

	if len(s.dirs) == 0 {
		_, _ = fmt.Fprintf(w, "  No test coverage data available.\n\n")
		return nil
	}

	tbl := NewTable(
		Column{Header: "Directory"},
		Column{Header: "Source", Align: AlignRight},
		Column{Header: "Tests", Align: AlignRight},
		Column{Header: "Ratio", Align: AlignRight},
		Column{Header: "Assessment"},
	)

	for _, d := range s.dirs {
		tbl.AddRow(
			d.Path,
			fmt.Sprintf("%d", d.SourceFiles),
			fmt.Sprintf("%d", d.TestFiles),
			fmt.Sprintf("%.2f", d.Ratio),
			coverageAssessment(d),
		)
	}

	if err := tbl.Render(w); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "\n")
	return nil
}

func coverageAssessment(d collectors.DirectoryTestRatio) string {
	if d.TestFiles == 0 {
		return "NO TESTS"
	}
	switch {
	case d.Ratio < 0.1:
		return "CRITICAL"
	case d.Ratio < 0.3:
		return "LOW"
	case d.Ratio < 0.5:
		return "MODERATE"
	default:
		return "GOOD"
	}
}
