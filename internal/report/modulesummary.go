// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package report

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/davetashner/stringer/internal/signal"
)

func init() {
	Register(&moduleSummarySection{})
}

// moduleSummary holds per-module health stats.
type moduleSummary struct {
	Module         string
	Total          int
	P1, P2, P3, P4 int
	HealthScore    int
	TopKinds       []string // up to 3 most frequent kinds
}

// moduleSummarySection groups signals by directory/package and produces per-module health scores.
type moduleSummarySection struct {
	modules []moduleSummary
	total   int
	depth   int // path segment depth for grouping (default 2)
}

func (s *moduleSummarySection) Name() string { return "module-summary" }
func (s *moduleSummarySection) Description() string {
	return "Signal grouping by module/package with per-module health scores"
}

// extractModule returns the module prefix from a file path using the first N segments.
func extractModule(filePath string, depth int) string {
	if filePath == "" {
		return "(root)"
	}
	// Normalize separators.
	clean := filepath.ToSlash(filePath)
	dir := filepath.ToSlash(filepath.Dir(clean))
	if dir == "." || dir == "" {
		return "(root)"
	}

	parts := strings.Split(dir, "/")
	if len(parts) > depth {
		parts = parts[:depth]
	}
	return strings.Join(parts, "/")
}

// mapConfidenceToPriorityLocal mirrors the confidence→priority mapping from output/beads.go.
func mapConfidenceToPriorityLocal(confidence float64) int {
	switch {
	case confidence >= 0.8:
		return 1
	case confidence >= 0.6:
		return 2
	case confidence >= 0.4:
		return 3
	default:
		return 4
	}
}

func (s *moduleSummarySection) Analyze(result *signal.ScanResult) error {
	depth := s.depth
	if depth <= 0 {
		depth = 2
	}

	if len(result.Signals) == 0 {
		s.modules = nil
		s.total = 0
		return nil
	}

	type moduleStats struct {
		total          int
		p1, p2, p3, p4 int
		kindCounts     map[string]int
	}

	groups := make(map[string]*moduleStats)

	for _, sig := range result.Signals {
		mod := extractModule(sig.FilePath, depth)
		stats, ok := groups[mod]
		if !ok {
			stats = &moduleStats{kindCounts: make(map[string]int)}
			groups[mod] = stats
		}
		stats.total++

		priority := mapConfidenceToPriorityLocal(sig.Confidence)
		if sig.Priority != nil {
			priority = *sig.Priority
		}
		switch priority {
		case 1:
			stats.p1++
		case 2:
			stats.p2++
		case 3:
			stats.p3++
		default:
			stats.p4++
		}

		if sig.Kind != "" {
			stats.kindCounts[sig.Kind]++
		}
	}

	modules := make([]moduleSummary, 0, len(groups))
	for mod, stats := range groups {
		health := stats.p1*4 + stats.p2*3 + stats.p3*2 + stats.p4*1
		modules = append(modules, moduleSummary{
			Module:      mod,
			Total:       stats.total,
			P1:          stats.p1,
			P2:          stats.p2,
			P3:          stats.p3,
			P4:          stats.p4,
			HealthScore: health,
			TopKinds:    topKinds(stats.kindCounts, 3),
		})
	}

	// Sort by health score descending (worst first), then by module name for stability.
	sort.Slice(modules, func(i, j int) bool {
		if modules[i].HealthScore != modules[j].HealthScore {
			return modules[i].HealthScore > modules[j].HealthScore
		}
		return modules[i].Module < modules[j].Module
	})

	// Cap at top 20.
	if len(modules) > 20 {
		modules = modules[:20]
	}

	s.modules = modules
	s.total = len(result.Signals)
	return nil
}

// topKinds returns the N most frequent kinds from a count map.
func topKinds(counts map[string]int, n int) []string {
	if len(counts) == 0 {
		return nil
	}

	type kc struct {
		kind  string
		count int
	}
	sorted := make([]kc, 0, len(counts))
	for k, c := range counts {
		sorted = append(sorted, kc{k, c})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].count != sorted[j].count {
			return sorted[i].count > sorted[j].count
		}
		return sorted[i].kind < sorted[j].kind
	})

	result := make([]string, 0, n)
	for i := 0; i < n && i < len(sorted); i++ {
		result = append(result, sorted[i].kind)
	}
	return result
}

func (s *moduleSummarySection) Render(w io.Writer) error {
	_, _ = fmt.Fprintf(w, "%s\n", SectionTitle("Module Health Summary"))
	_, _ = fmt.Fprintf(w, "----------------------\n")

	if len(s.modules) == 0 {
		_, _ = fmt.Fprintf(w, "  No signals to group.\n\n")
		return nil
	}

	_, _ = fmt.Fprintf(w, "  %d modules, %d total signals\n\n", len(s.modules), s.total)

	tbl := NewTable(
		Column{Header: "Module"},
		Column{Header: "Signals", Align: AlignRight},
		Column{Header: "P1", Align: AlignRight},
		Column{Header: "P2", Align: AlignRight},
		Column{Header: "P3", Align: AlignRight},
		Column{Header: "P4", Align: AlignRight},
		Column{Header: "Health", Align: AlignRight, Color: func(val string) string {
			// Parse health score for coloring.
			var h int
			if _, err := fmt.Sscanf(val, "%d", &h); err == nil {
				switch {
				case h >= 20:
					return colorRed.Sprint(val)
				case h >= 10:
					return colorYellow.Sprint(val)
				}
			}
			return val
		}},
		Column{Header: "Top Kinds"},
	)

	for _, m := range s.modules {
		tbl.AddRow(
			m.Module,
			fmt.Sprintf("%d", m.Total),
			fmt.Sprintf("%d", m.P1),
			fmt.Sprintf("%d", m.P2),
			fmt.Sprintf("%d", m.P3),
			fmt.Sprintf("%d", m.P4),
			fmt.Sprintf("%d", m.HealthScore),
			strings.Join(m.TopKinds, ", "),
		)
	}

	if err := tbl.Render(w); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(w, "\n")
	return nil
}
