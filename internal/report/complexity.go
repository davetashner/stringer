// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package report

import (
	"fmt"
	"io"
	"sort"

	"github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/signal"
)

const complexityTopN = 20

func init() {
	Register(&complexitySection{})
}

// complexitySection reports the most complex functions found during scanning.
type complexitySection struct {
	functions []collectors.FunctionComplexity
}

func (s *complexitySection) Name() string        { return "complexity" }
func (s *complexitySection) Description() string { return "Function complexity analysis" }

func (s *complexitySection) Analyze(result *signal.ScanResult) error {
	raw, ok := result.Metrics["complexity"]
	if !ok {
		return fmt.Errorf("complexity: %w", ErrMetricsNotAvailable)
	}
	m, ok := raw.(*collectors.ComplexityMetrics)
	if !ok || m == nil {
		return fmt.Errorf("complexity: %w", ErrMetricsNotAvailable)
	}

	// Copy and sort by score descending.
	s.functions = make([]collectors.FunctionComplexity, len(m.Functions))
	copy(s.functions, m.Functions)
	sort.Slice(s.functions, func(i, j int) bool {
		return s.functions[i].Score > s.functions[j].Score
	})

	// Cap at top N.
	if len(s.functions) > complexityTopN {
		s.functions = s.functions[:complexityTopN]
	}

	return nil
}

func (s *complexitySection) Render(w io.Writer) error {
	_, _ = fmt.Fprintf(w, "%s\n", SectionTitle("Function Complexity"))
	_, _ = fmt.Fprintf(w, "-------------------\n")

	if len(s.functions) == 0 {
		_, _ = fmt.Fprintf(w, "  No complex functions detected.\n\n")
		return nil
	}

	tbl := NewTable(
		Column{Header: "Function"},
		Column{Header: "File"},
		Column{Header: "Lines", Align: AlignRight},
		Column{Header: "Branches", Align: AlignRight},
		Column{Header: "Score", Align: AlignRight, Color: ColorComplexity},
	)

	for _, fc := range s.functions {
		tbl.AddRow(
			fc.FuncName,
			fc.FilePath,
			fmt.Sprintf("%d", fc.Lines),
			fmt.Sprintf("%d", fc.Branches),
			fmt.Sprintf("%.1f", fc.Score),
		)
	}

	if err := tbl.Render(w); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "\n")
	return nil
}

// ColorComplexity colors complexity score labels.
func ColorComplexity(val string) string {
	// Parse the score to determine color.
	var score float64
	if _, err := fmt.Sscanf(val, "%f", &score); err != nil {
		return val
	}
	switch {
	case score >= 15:
		return colorRed.Sprint(val)
	case score >= 8:
		return colorYellow.Sprint(val)
	default:
		return val
	}
}
