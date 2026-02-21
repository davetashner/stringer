// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package report

import (
	"fmt"
	"io"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/davetashner/stringer/internal/state"
)

func init() {
	Register(&trendsSection{})
}

// trendsSection reports signal count trends over recent scans.
type trendsSection struct {
	trends *state.TrendResult
}

func (s *trendsSection) Name() string        { return "trends" }
func (s *trendsSection) Description() string { return "Signal count trends over recent scans" }

func (s *trendsSection) Analyze(result *signal.ScanResult) error {
	raw, ok := result.Metrics["_history"]
	if !ok {
		return fmt.Errorf("trends: %w", ErrMetricsNotAvailable)
	}
	h, ok := raw.(*state.ScanHistory)
	if !ok || h == nil {
		return fmt.Errorf("trends: %w", ErrMetricsNotAvailable)
	}

	trends := state.ComputeTrends(h, state.DefaultWindowSize)
	if trends == nil {
		return fmt.Errorf("trends: insufficient data (need >= 2 scans): %w", ErrMetricsNotAvailable)
	}

	s.trends = trends
	return nil
}

func (s *trendsSection) Render(w io.Writer) error {
	_, _ = fmt.Fprintf(w, "%s\n", SectionTitle("Health Trends"))
	_, _ = fmt.Fprintf(w, "-------------\n")

	_, _ = fmt.Fprintf(w, "  Window: last %d of %d data points\n\n",
		s.trends.DataPoints, s.trends.WindowSize)

	tbl := NewTable(
		Column{Header: "Metric"},
		Column{Header: "Current", Align: AlignRight},
		Column{Header: "Previous", Align: AlignRight},
		Column{Header: "Delta", Align: AlignRight},
		Column{Header: "Direction", Color: ColorDirection},
	)

	// Total row.
	t := s.trends.TotalTrend
	tbl.AddRow("Total", itoa(t.Current), itoa(t.Previous), formatDelta(t.Delta), string(t.Direction))

	// Collector rows.
	for _, k := range SortedTrendKeys(s.trends.CollectorTrends) {
		ct := s.trends.CollectorTrends[k]
		tbl.AddRow(k, itoa(ct.Current), itoa(ct.Previous), formatDelta(ct.Delta), string(ct.Direction))
	}

	// Kind rows.
	for _, k := range SortedTrendKeys(s.trends.KindTrends) {
		kt := s.trends.KindTrends[k]
		tbl.AddRow(k, itoa(kt.Current), itoa(kt.Previous), formatDelta(kt.Delta), string(kt.Direction))
	}

	if err := tbl.Render(w); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "\n")
	return nil
}

// itoa formats an int as a string.
func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

// formatDelta formats a delta with a +/- prefix.
func formatDelta(d int) string {
	if d > 0 {
		return fmt.Sprintf("+%d", d)
	}
	return fmt.Sprintf("%d", d)
}

// SortedTrendKeys converts a map[string]TrendLine to sorted keys.
func SortedTrendKeys(m map[string]state.TrendLine) []string {
	tmp := make(map[string]int, len(m))
	for k := range m {
		tmp[k] = 0
	}
	return state.SortedKeys(tmp)
}
