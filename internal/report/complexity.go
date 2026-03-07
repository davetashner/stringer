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
	hasAST    bool // true if any function was analyzed via Go AST
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

	// Copy functions.
	s.functions = make([]collectors.FunctionComplexity, len(m.Functions))
	copy(s.functions, m.Functions)

	// Detect if any AST-analyzed functions are present.
	for _, fc := range s.functions {
		if fc.ASTBased {
			s.hasAST = true
			break
		}
	}

	// Sort: AST functions by cognitive complexity descending,
	// regex functions by score descending. Mixed lists sort by
	// cognitive first (AST), then score (regex).
	sort.Slice(s.functions, func(i, j int) bool {
		fi, fj := s.functions[i], s.functions[j]
		if fi.ASTBased && fj.ASTBased {
			return fi.Cognitive > fj.Cognitive
		}
		if fi.ASTBased != fj.ASTBased {
			// AST functions sort before regex functions.
			return fi.ASTBased
		}
		return fi.Score > fj.Score
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

	// Separate AST-analyzed and regex-analyzed functions.
	var astFuncs, regexFuncs []collectors.FunctionComplexity
	for _, fc := range s.functions {
		if fc.ASTBased {
			astFuncs = append(astFuncs, fc)
		} else {
			regexFuncs = append(regexFuncs, fc)
		}
	}

	// Render AST-analyzed functions.
	if len(astFuncs) > 0 {
		_, _ = fmt.Fprintf(w, "  AST-based analysis (Go)\n\n")
		tbl := NewTable(
			Column{Header: "Function"},
			Column{Header: "Cyclomatic", Align: AlignRight, Color: ColorCyclomatic},
			Column{Header: "Cognitive", Align: AlignRight, Color: ColorCognitive},
			Column{Header: "Nesting", Align: AlignRight},
			Column{Header: "File"},
		)
		for _, fc := range astFuncs {
			tbl.AddRow(
				fc.FuncName,
				fmt.Sprintf("%d", fc.Cyclomatic),
				fmt.Sprintf("%d", fc.Cognitive),
				fmt.Sprintf("%d", fc.MaxNesting),
				fc.FilePath,
			)
		}
		if err := tbl.Render(w); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(w, "\n")
	}

	// Render regex-analyzed functions.
	if len(regexFuncs) > 0 {
		_, _ = fmt.Fprintf(w, "  Heuristic-based analysis\n\n")
		tbl := NewTable(
			Column{Header: "Function"},
			Column{Header: "File"},
			Column{Header: "Lines", Align: AlignRight},
			Column{Header: "Branches", Align: AlignRight},
			Column{Header: "Score", Align: AlignRight, Color: ColorComplexity},
		)
		for _, fc := range regexFuncs {
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
	}

	return nil
}

// ColorComplexity colors complexity score labels.
func ColorComplexity(val string) string {
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

// ColorCyclomatic colors cyclomatic complexity values.
func ColorCyclomatic(val string) string {
	var v int
	if _, err := fmt.Sscanf(val, "%d", &v); err != nil {
		return val
	}
	switch {
	case v >= 20:
		return colorRed.Sprint(val)
	case v >= 10:
		return colorYellow.Sprint(val)
	default:
		return val
	}
}

// ColorCognitive colors cognitive complexity values.
func ColorCognitive(val string) string {
	var v int
	if _, err := fmt.Sscanf(val, "%d", &v); err != nil {
		return val
	}
	switch {
	case v >= 25:
		return colorRed.Sprint(val)
	case v >= 15:
		return colorYellow.Sprint(val)
	default:
		return val
	}
}
