package report

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/signal"
)

func init() {
	Register(&recommendationsSection{})
}

// Severity levels for recommendations.
const (
	SeverityHigh   = "high"
	SeverityMedium = "medium"
	SeverityLow    = "low"
)

// Recommendation is a single actionable suggestion derived from metrics.
type Recommendation struct {
	Severity string
	Message  string
}

// recommendationsSection generates actionable recommendations from all metrics.
type recommendationsSection struct {
	recs []Recommendation
	now  func() time.Time // injectable for testing
}

func (s *recommendationsSection) Name() string { return "recommendations" }
func (s *recommendationsSection) Description() string {
	return "Actionable recommendations based on all collected metrics"
}

func (s *recommendationsSection) Analyze(result *signal.ScanResult) error {
	s.recs = nil

	s.analyzeLotteryRisk(result)
	s.analyzeChurn(result)
	s.analyzeTodos(result)
	s.analyzeCoverage(result)

	// Sort: high > medium > low.
	sort.SliceStable(s.recs, func(i, j int) bool {
		return severityOrder(s.recs[i].Severity) < severityOrder(s.recs[j].Severity)
	})

	return nil
}

func severityOrder(s string) int {
	switch s {
	case SeverityHigh:
		return 0
	case SeverityMedium:
		return 1
	case SeverityLow:
		return 2
	default:
		return 3
	}
}

func (s *recommendationsSection) analyzeLotteryRisk(result *signal.ScanResult) {
	raw, ok := result.Metrics["lotteryrisk"]
	if !ok {
		return
	}
	m, ok := raw.(*collectors.LotteryRiskMetrics)
	if !ok || m == nil {
		return
	}

	for _, d := range m.Directories {
		if d.LotteryRisk <= 1 {
			s.recs = append(s.recs, Recommendation{
				Severity: SeverityHigh,
				Message:  fmt.Sprintf("Directory %q has a lottery risk of %d (single contributor). Consider pairing or cross-training.", d.Path, d.LotteryRisk),
			})
		}
	}
}

func (s *recommendationsSection) analyzeChurn(result *signal.ScanResult) {
	raw, ok := result.Metrics["gitlog"]
	if !ok {
		return
	}
	m, ok := raw.(*collectors.GitlogMetrics)
	if !ok || m == nil {
		return
	}

	if m.RevertCount > 0 {
		s.recs = append(s.recs, Recommendation{
			Severity: SeverityMedium,
			Message:  fmt.Sprintf("%d revert(s) detected. Review testing and code review processes.", m.RevertCount),
		})
	}

	for _, fc := range m.FileChurns {
		if fc.ChangeCount >= 20 {
			s.recs = append(s.recs, Recommendation{
				Severity: SeverityMedium,
				Message:  fmt.Sprintf("File %q changed %d times recently. Consider refactoring to reduce churn.", fc.Path, fc.ChangeCount),
			})
		}
	}
}

func (s *recommendationsSection) analyzeTodos(result *signal.ScanResult) {
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}

	// Count stale TODOs (> 1 year) from signals.
	staleCount := 0
	oneYear := 365 * 24 * time.Hour
	for _, sig := range result.Signals {
		if sig.Source != "todos" || sig.Timestamp.IsZero() {
			continue
		}
		if now.Sub(sig.Timestamp) >= oneYear {
			staleCount++
		}
	}

	if staleCount > 0 {
		s.recs = append(s.recs, Recommendation{
			Severity: SeverityLow,
			Message:  fmt.Sprintf("%d TODO(s) are over 1 year old. Review and resolve or remove.", staleCount),
		})
	}
}

func (s *recommendationsSection) analyzeCoverage(result *signal.ScanResult) {
	raw, ok := result.Metrics["patterns"]
	if !ok {
		return
	}
	m, ok := raw.(*collectors.PatternsMetrics)
	if !ok || m == nil {
		return
	}

	noTests := 0
	for _, d := range m.DirectoryTestRatios {
		if d.TestFiles == 0 {
			noTests++
		} else if d.Ratio < 0.1 {
			s.recs = append(s.recs, Recommendation{
				Severity: SeverityMedium,
				Message:  fmt.Sprintf("Directory %q has very low test coverage (ratio: %.2f). Consider adding tests.", d.Path, d.Ratio),
			})
		}
	}

	if noTests > 0 {
		s.recs = append(s.recs, Recommendation{
			Severity: SeverityHigh,
			Message:  fmt.Sprintf("%d directory(ies) have no test files. Prioritize test coverage for critical paths.", noTests),
		})
	}
}

func (s *recommendationsSection) Render(w io.Writer) error {
	_, _ = fmt.Fprintf(w, "%s\n", SectionTitle("Recommendations"))
	_, _ = fmt.Fprintf(w, "---------------\n")

	if len(s.recs) == 0 {
		_, _ = fmt.Fprintf(w, "  No actionable recommendations.\n\n")
		return nil
	}

	for _, r := range s.recs {
		label := colorSeverity(r.Severity)
		_, _ = fmt.Fprintf(w, "  [%s] %s\n", label, r.Message)
	}

	_, _ = fmt.Fprintf(w, "\n")
	return nil
}

func colorSeverity(severity string) string {
	switch severity {
	case SeverityHigh:
		return colorRed.Sprint("HIGH")
	case SeverityMedium:
		return colorYellow.Sprint("MEDIUM")
	case SeverityLow:
		return colorGreen.Sprint("LOW")
	default:
		return severity
	}
}
