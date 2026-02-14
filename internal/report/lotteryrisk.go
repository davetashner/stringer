// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package report

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/signal"
)

func init() {
	Register(&lotteryRiskSection{})
}

// lotteryRiskSection reports directory ownership concentration risk.
type lotteryRiskSection struct {
	dirs []collectors.DirectoryOwnership
}

func (s *lotteryRiskSection) Name() string { return "lottery-risk" }
func (s *lotteryRiskSection) Description() string {
	return "Directory ownership concentration (lottery risk)"
}

func (s *lotteryRiskSection) Analyze(result *signal.ScanResult) error {
	raw, ok := result.Metrics["lotteryrisk"]
	if !ok {
		return fmt.Errorf("lotteryrisk: %w", ErrMetricsNotAvailable)
	}
	m, ok := raw.(*collectors.LotteryRiskMetrics)
	if !ok || m == nil {
		return fmt.Errorf("lotteryrisk: %w", ErrMetricsNotAvailable)
	}

	// Copy and sort by risk ascending (worst first: 1 is CRITICAL).
	s.dirs = make([]collectors.DirectoryOwnership, len(m.Directories))
	copy(s.dirs, m.Directories)
	sort.Slice(s.dirs, func(i, j int) bool {
		return s.dirs[i].LotteryRisk < s.dirs[j].LotteryRisk
	})

	return nil
}

func (s *lotteryRiskSection) Render(w io.Writer) error {
	_, _ = fmt.Fprintf(w, "%s\n", SectionTitle("Lottery Risk"))
	_, _ = fmt.Fprintf(w, "------------\n")

	if len(s.dirs) == 0 {
		_, _ = fmt.Fprintf(w, "  No directory ownership data available.\n\n")
		return nil
	}

	tbl := NewTable(
		Column{Header: "Directory"},
		Column{Header: "Risk", Align: AlignRight},
		Column{Header: "Top Contributors"},
		Column{Header: "Level", Color: ColorRiskLevel},
	)

	for _, d := range s.dirs {
		tbl.AddRow(
			d.Path,
			fmt.Sprintf("%d", d.LotteryRisk),
			topContributors(d.Authors, 3),
			riskLevel(d.LotteryRisk),
		)
	}

	if err := tbl.Render(w); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "\n")
	return nil
}

func riskLevel(risk int) string {
	switch {
	case risk <= 1:
		return "CRITICAL"
	case risk == 2:
		return "WARNING"
	default:
		return "ok"
	}
}

func topContributors(authors []collectors.AuthorShare, n int) string {
	if len(authors) == 0 {
		return "-"
	}
	limit := n
	if len(authors) < limit {
		limit = len(authors)
	}
	names := make([]string, limit)
	for i := 0; i < limit; i++ {
		names[i] = fmt.Sprintf("%s (%.0f%%)", authors[i].Name, authors[i].Ownership*100)
	}
	return strings.Join(names, ", ")
}
