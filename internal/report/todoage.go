package report

import (
	"fmt"
	"io"
	"time"

	"github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/signal"
)

func init() {
	Register(&todoAgeSection{})
}

// ageBucket defines a histogram bucket for TODO age distribution.
type ageBucket struct {
	Label string
	Max   time.Duration // upper bound (exclusive); 0 means unlimited
	Count int
}

// staleTODO represents a TODO older than 1 year.
type staleTODO struct {
	File  string
	Line  int
	Title string
	Age   time.Duration
}

// todoAgeSection reports the age distribution of TODO comments.
type todoAgeSection struct {
	total   int
	buckets []ageBucket
	stale   []staleTODO
	now     func() time.Time // injectable for testing
}

func (s *todoAgeSection) Name() string { return "todo-age" }
func (s *todoAgeSection) Description() string {
	return "TODO/FIXME age distribution and stale item detection"
}

func (s *todoAgeSection) Analyze(result *signal.ScanResult) error {
	raw, ok := result.Metrics["todos"]
	if !ok {
		return fmt.Errorf("todos: %w", ErrMetricsNotAvailable)
	}
	m, ok := raw.(*collectors.TodoMetrics)
	if !ok || m == nil {
		return fmt.Errorf("todos: %w", ErrMetricsNotAvailable)
	}

	now := time.Now()
	if s.now != nil {
		now = s.now()
	}

	s.total = m.Total
	s.buckets = []ageBucket{
		{Label: "< 1 week", Max: 7 * 24 * time.Hour},
		{Label: "1-4 weeks", Max: 28 * 24 * time.Hour},
		{Label: "1-3 months", Max: 90 * 24 * time.Hour},
		{Label: "3-12 months", Max: 365 * 24 * time.Hour},
		{Label: "> 1 year", Max: 0},
	}
	s.stale = nil

	oneYear := 365 * 24 * time.Hour

	// Classify signals from the todos collector by their timestamp.
	for _, sig := range result.Signals {
		if sig.Source != "todos" {
			continue
		}
		if sig.Timestamp.IsZero() {
			continue
		}

		age := now.Sub(sig.Timestamp)
		if age < 0 {
			age = 0
		}

		placed := false
		for i := range s.buckets {
			if s.buckets[i].Max > 0 && age < s.buckets[i].Max {
				s.buckets[i].Count++
				placed = true
				break
			}
		}
		if !placed {
			// Falls into the last bucket (> 1 year).
			s.buckets[len(s.buckets)-1].Count++
		}

		if age >= oneYear {
			s.stale = append(s.stale, staleTODO{
				File:  sig.FilePath,
				Line:  sig.Line,
				Title: sig.Title,
				Age:   age,
			})
		}
	}

	return nil
}

func (s *todoAgeSection) Render(w io.Writer) error {
	_, _ = fmt.Fprintf(w, "%s\n", SectionTitle("TODO Age Distribution"))
	_, _ = fmt.Fprintf(w, "---------------------\n")
	_, _ = fmt.Fprintf(w, "  Total TODOs: %d\n\n", s.total)

	// Histogram.
	maxCount := 0
	for _, b := range s.buckets {
		if b.Count > maxCount {
			maxCount = b.Count
		}
	}

	barWidth := 30
	bucketTotal := 0
	for i, b := range s.buckets {
		bucketTotal += b.Count
		bar := ""
		if maxCount > 0 {
			n := (b.Count * barWidth) / maxCount
			for j := 0; j < n; j++ {
				bar += "#"
			}
		}
		// Color the last bucket (> 1 year) red if non-empty.
		if i == len(s.buckets)-1 && b.Count > 0 {
			_, _ = fmt.Fprintf(w, "  %-12s %3d  %s\n", colorRed.Sprint(b.Label), b.Count, colorRed.Sprint(bar))
		} else {
			_, _ = fmt.Fprintf(w, "  %-12s %3d  %s\n", b.Label, b.Count, bar)
		}
	}

	// Show unknown-age count when some TODOs couldn't be bucketed.
	if unknown := s.total - bucketTotal; unknown > 0 {
		_, _ = fmt.Fprintf(w, "  %-12s %3d\n", "Unknown age", unknown)
	}

	// Stale TODOs.
	if len(s.stale) > 0 {
		_, _ = fmt.Fprintf(w, "\n  Stale TODOs (> 1 year):\n")
		for _, st := range s.stale {
			_, _ = fmt.Fprintf(w, "    %s:%d  %s (%s old)\n",
				st.File, st.Line, st.Title, formatAge(st.Age))
		}
	}

	_, _ = fmt.Fprintf(w, "\n")
	return nil
}

// formatAge returns a human-readable age string.
func formatAge(d time.Duration) string {
	days := int(d.Hours() / 24)
	switch {
	case days >= 365:
		years := days / 365
		if years == 1 {
			return "1 year"
		}
		return fmt.Sprintf("%d years", years)
	case days >= 30:
		months := days / 30
		if months == 1 {
			return "1 month"
		}
		return fmt.Sprintf("%d months", months)
	case days >= 7:
		weeks := days / 7
		if weeks == 1 {
			return "1 week"
		}
		return fmt.Sprintf("%d weeks", weeks)
	default:
		if days <= 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	}
}
