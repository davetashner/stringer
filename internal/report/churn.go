package report

import (
	"fmt"
	"io"
	"sort"

	"github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/signal"
)

const churnTopN = 20

func init() {
	Register(&churnSection{})
}

// churnSection reports file churn and code stability indicators.
type churnSection struct {
	churns        []collectors.FileChurn
	revertCount   int
	staleBranches int
}

func (s *churnSection) Name() string        { return "churn" }
func (s *churnSection) Description() string { return "File churn and code stability analysis" }

func (s *churnSection) Analyze(result *signal.ScanResult) error {
	raw, ok := result.Metrics["gitlog"]
	if !ok {
		return fmt.Errorf("gitlog: %w", ErrMetricsNotAvailable)
	}
	m, ok := raw.(*collectors.GitlogMetrics)
	if !ok || m == nil {
		return fmt.Errorf("gitlog: %w", ErrMetricsNotAvailable)
	}

	s.revertCount = m.RevertCount
	s.staleBranches = m.StaleBranchCount

	// Copy and sort by change count descending.
	s.churns = make([]collectors.FileChurn, len(m.FileChurns))
	copy(s.churns, m.FileChurns)
	sort.Slice(s.churns, func(i, j int) bool {
		return s.churns[i].ChangeCount > s.churns[j].ChangeCount
	})

	// Cap at top N.
	if len(s.churns) > churnTopN {
		s.churns = s.churns[:churnTopN]
	}

	return nil
}

func (s *churnSection) Render(w io.Writer) error {
	_, _ = fmt.Fprintf(w, "%s\n", SectionTitle("Code Churn"))
	_, _ = fmt.Fprintf(w, "----------\n")

	_, _ = fmt.Fprintf(w, "  Reverts detected: %s\n", colorCount(s.revertCount))
	_, _ = fmt.Fprintf(w, "  Stale branches:   %s\n", colorCount(s.staleBranches))

	if len(s.churns) == 0 {
		if s.revertCount > 0 {
			_, _ = fmt.Fprintf(w, "  File churn requires full git history (try cloning without --depth).\n\n")
		} else {
			_, _ = fmt.Fprintf(w, "  No file churn data available.\n\n")
		}
		return nil
	}

	_, _ = fmt.Fprintf(w, "\n")

	tbl := NewTable(
		Column{Header: "File"},
		Column{Header: "Changes", Align: AlignRight},
		Column{Header: "Authors", Align: AlignRight},
		Column{Header: "Stability", Color: ColorStability},
	)

	for _, fc := range s.churns {
		tbl.AddRow(
			fc.Path,
			fmt.Sprintf("%d", fc.ChangeCount),
			fmt.Sprintf("%d", fc.AuthorCount),
			stabilityLevel(fc.ChangeCount),
		)
	}

	if err := tbl.Render(w); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "\n")
	return nil
}

func stabilityLevel(changes int) string {
	switch {
	case changes >= 20:
		return "unstable"
	case changes >= 10:
		return "moderate"
	default:
		return "stable"
	}
}
