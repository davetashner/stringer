// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package report

import (
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/signal"
)

const hotspotsTopN = 15

// minHotspotComplexity is the minimum complexity score for hotspot inclusion.
const minHotspotComplexity = 6.0

// minHotspotChurn is the minimum change count for hotspot inclusion.
const minHotspotChurn = 5

func init() {
	Register(&hotspotsSection{})
}

// hotspot represents a file at the intersection of high complexity and high churn.
type hotspot struct {
	FilePath     string
	FuncName     string
	Complexity   float64
	ChangeCount  int
	HotspotScore float64 // complexity_score * log2(change_count + 1)
}

// hotspotsSection cross-references complexity and churn data to find toxic hotspots.
type hotspotsSection struct {
	hotspots []hotspot
}

func (s *hotspotsSection) Name() string        { return "hotspots" }
func (s *hotspotsSection) Description() string { return "Toxic hotspots (complexity x churn)" }

func (s *hotspotsSection) Analyze(result *signal.ScanResult) error {
	// Read complexity metrics.
	rawC, okC := result.Metrics["complexity"]
	if !okC {
		return fmt.Errorf("complexity: %w", ErrMetricsNotAvailable)
	}
	cm, okCM := rawC.(*collectors.ComplexityMetrics)
	if !okCM || cm == nil {
		return fmt.Errorf("complexity: %w", ErrMetricsNotAvailable)
	}

	// Read gitlog metrics.
	rawG, okG := result.Metrics["gitlog"]
	if !okG {
		return fmt.Errorf("gitlog: %w", ErrMetricsNotAvailable)
	}
	gm, okGM := rawG.(*collectors.GitlogMetrics)
	if !okGM || gm == nil {
		return fmt.Errorf("gitlog: %w", ErrMetricsNotAvailable)
	}

	// Build churn lookup by file path.
	churnMap := make(map[string]int, len(gm.FileChurns))
	for _, fc := range gm.FileChurns {
		churnMap[fc.Path] = fc.ChangeCount
	}

	// Cross-reference: for each complex function, check if its file has high churn.
	var spots []hotspot
	for _, fc := range cm.Functions {
		if fc.Score < minHotspotComplexity {
			continue
		}
		changes, ok := churnMap[fc.FilePath]
		if !ok || changes < minHotspotChurn {
			continue
		}
		spots = append(spots, hotspot{
			FilePath:     fc.FilePath,
			FuncName:     fc.FuncName,
			Complexity:   fc.Score,
			ChangeCount:  changes,
			HotspotScore: fc.Score * math.Log2(float64(changes+1)),
		})
	}

	// Sort by hotspot score descending.
	sort.Slice(spots, func(i, j int) bool {
		return spots[i].HotspotScore > spots[j].HotspotScore
	})

	if len(spots) > hotspotsTopN {
		spots = spots[:hotspotsTopN]
	}

	s.hotspots = spots
	return nil
}

func (s *hotspotsSection) Render(w io.Writer) error {
	_, _ = fmt.Fprintf(w, "%s\n", SectionTitle("Toxic Hotspots"))
	_, _ = fmt.Fprintf(w, "--------------\n")

	if len(s.hotspots) == 0 {
		_, _ = fmt.Fprintf(w, "  No toxic hotspots detected (complex functions in high-churn files).\n\n")
		return nil
	}

	_, _ = fmt.Fprintf(w, "  Complex functions in frequently-changed files — highest-value refactoring targets.\n\n")

	tbl := NewTable(
		Column{Header: "Function"},
		Column{Header: "File"},
		Column{Header: "Complexity", Align: AlignRight},
		Column{Header: "Changes", Align: AlignRight},
		Column{Header: "Hotspot", Align: AlignRight, Color: colorHotspot},
	)

	for _, hs := range s.hotspots {
		tbl.AddRow(
			hs.FuncName,
			hs.FilePath,
			fmt.Sprintf("%.1f", hs.Complexity),
			fmt.Sprintf("%d", hs.ChangeCount),
			fmt.Sprintf("%.1f", hs.HotspotScore),
		)
	}

	if err := tbl.Render(w); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "\n")
	return nil
}

// colorHotspot colors hotspot scores.
func colorHotspot(val string) string {
	var score float64
	if _, err := fmt.Sscanf(val, "%f", &score); err != nil {
		return val
	}
	switch {
	case score >= 50:
		return colorRed.Sprint(val)
	case score >= 25:
		return colorYellow.Sprint(val)
	default:
		return val
	}
}
