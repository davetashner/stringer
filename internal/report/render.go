package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/davetashner/stringer/internal/signal"
)

// ReportJSON is the top-level JSON structure for --format json output.
type ReportJSON struct {
	Repository string                `json:"repository"`
	Generated  string                `json:"generated"`
	Duration   string                `json:"duration"`
	Collectors []CollectorResultJSON `json:"collectors"`
	Signals    SignalSummaryJSON     `json:"signals"`
	Sections   []SectionJSON         `json:"sections,omitempty"`
}

// CollectorResultJSON is the JSON representation of a single collector result.
type CollectorResultJSON struct {
	Name       string `json:"name"`
	Signals    int    `json:"signals"`
	Duration   string `json:"duration"`
	Error      string `json:"error,omitempty"`
	HasMetrics bool   `json:"has_metrics"`
}

// SignalSummaryJSON is the JSON representation of the signal summary.
type SignalSummaryJSON struct {
	Total  int            `json:"total"`
	ByKind map[string]int `json:"by_kind"`
}

// SectionJSON is the JSON representation of a single report section.
type SectionJSON struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`            // "ok", "skipped"
	Content     string `json:"content,omitempty"` // rendered text
}

// RenderJSON writes the report as machine-readable JSON.
func RenderJSON(result *signal.ScanResult, repoPath string, collectorNames []string, sections []string, w interface{ Write([]byte) (int, error) }) error {
	out := ReportJSON{
		Repository: repoPath,
		Generated:  time.Now().Format(time.RFC3339),
		Duration:   result.Duration.Round(time.Millisecond).String(),
	}

	// Collector results.
	for _, cr := range result.Results {
		cj := CollectorResultJSON{
			Name:       cr.Collector,
			Signals:    len(cr.Signals),
			Duration:   cr.Duration.Round(time.Millisecond).String(),
			HasMetrics: cr.Metrics != nil,
		}
		if cr.Err != nil {
			cj.Error = cr.Err.Error()
		}
		out.Collectors = append(out.Collectors, cj)
	}

	// Signal summary.
	kindCounts := make(map[string]int)
	for _, sig := range result.Signals {
		kindCounts[sig.Kind]++
	}
	out.Signals = SignalSummaryJSON{
		Total:  len(result.Signals),
		ByKind: kindCounts,
	}

	// Report sections.
	sectionNames := ResolveSections(sections)
	for _, name := range sectionNames {
		sec := Get(name)
		if sec == nil {
			continue
		}

		sj := SectionJSON{
			Name:        sec.Name(),
			Description: sec.Description(),
		}

		if err := sec.Analyze(result); err != nil {
			if errors.Is(err, ErrMetricsNotAvailable) {
				sj.Status = "skipped"
				out.Sections = append(out.Sections, sj)
				continue
			}
			return fmt.Errorf("section %s: %w", name, err)
		}

		sj.Status = "ok"
		var buf bytes.Buffer
		if err := sec.Render(&buf); err != nil {
			return fmt.Errorf("section %s render: %w", name, err)
		}
		sj.Content = buf.String()
		out.Sections = append(out.Sections, sj)
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON marshal: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// ResolveSections determines which sections to run without printing warnings.
// If filter is empty, all registered sections are used.
func ResolveSections(filter []string) []string {
	if len(filter) == 0 {
		return List()
	}

	available := make(map[string]bool)
	for _, name := range List() {
		available[name] = true
	}

	var names []string
	for _, name := range filter {
		if available[name] {
			names = append(names, name)
		}
	}
	return names
}
