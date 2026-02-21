// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package report

import (
	"fmt"
	"io"

	"github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/signal"
)

func init() {
	Register(&gitHygieneSection{})
}

// gitHygieneSection reports git hygiene issues found during scanning.
type gitHygieneSection struct {
	metrics *collectors.GitHygieneMetrics
}

func (s *gitHygieneSection) Name() string        { return "git-hygiene" }
func (s *gitHygieneSection) Description() string { return "Git repository hygiene analysis" }

func (s *gitHygieneSection) Analyze(result *signal.ScanResult) error {
	raw, ok := result.Metrics["githygiene"]
	if !ok {
		return fmt.Errorf("git-hygiene: %w", ErrMetricsNotAvailable)
	}
	m, ok := raw.(*collectors.GitHygieneMetrics)
	if !ok || m == nil {
		return fmt.Errorf("git-hygiene: %w", ErrMetricsNotAvailable)
	}
	s.metrics = m
	return nil
}

func (s *gitHygieneSection) Render(w io.Writer) error {
	_, _ = fmt.Fprintf(w, "%s\n", SectionTitle("Git Hygiene"))
	_, _ = fmt.Fprintf(w, "-------------------\n")

	if s.metrics == nil {
		_, _ = fmt.Fprintf(w, "  No git hygiene data available.\n\n")
		return nil
	}

	total := s.metrics.LargeBinaries + s.metrics.MergeConflictMarkers +
		s.metrics.CommittedSecrets + s.metrics.MixedLineEndings

	if total == 0 {
		_, _ = fmt.Fprintf(w, "  No git hygiene issues detected (%d files scanned).\n\n",
			s.metrics.FilesScanned)
		return nil
	}

	tbl := NewTable(
		Column{Header: "Issue Type"},
		Column{Header: "Count", Align: AlignRight, Color: colorHygieneCount},
	)

	tbl.AddRow("Large binaries", fmt.Sprintf("%d", s.metrics.LargeBinaries))
	tbl.AddRow("Merge conflict markers", fmt.Sprintf("%d", s.metrics.MergeConflictMarkers))
	tbl.AddRow("Committed secrets", fmt.Sprintf("%d", s.metrics.CommittedSecrets))
	tbl.AddRow("Mixed line endings", fmt.Sprintf("%d", s.metrics.MixedLineEndings))

	if err := tbl.Render(w); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(w, "\n  %d files scanned, %d issues found.\n\n",
		s.metrics.FilesScanned, total)
	return nil
}

// colorHygieneCount colors issue counts: 0 is green, >0 is yellow/red.
func colorHygieneCount(val string) string {
	if val == "0" {
		return colorGreen.Sprint(val)
	}
	return colorYellow.Sprint(val)
}
