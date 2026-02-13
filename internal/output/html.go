package output

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/davetashner/stringer/internal/signal"
)

func init() {
	RegisterFormatter(NewHTMLFormatter())
}

// HTMLFormatter writes signals as a self-contained HTML dashboard.
type HTMLFormatter struct {
	nowFunc func() time.Time
}

// Compile-time interface check.
var _ Formatter = (*HTMLFormatter)(nil)

// NewHTMLFormatter returns a new HTMLFormatter.
func NewHTMLFormatter() *HTMLFormatter {
	return &HTMLFormatter{}
}

// Name returns the format name.
func (h *HTMLFormatter) Name() string {
	return "html"
}

var (
	htmlTmplOnce sync.Once
	htmlTmpl     *template.Template
)

// Format writes all signals as a self-contained HTML dashboard to w.
func (h *HTMLFormatter) Format(signals []signal.RawSignal, w io.Writer) error {
	if len(signals) == 0 {
		return h.writeEmpty(w)
	}

	htmlTmplOnce.Do(func() {
		htmlTmpl = template.Must(template.New("dashboard").Funcs(template.FuncMap{
			"json": func(v any) template.JS {
				b, _ := json.Marshal(v)
				return template.JS(b) //nolint:gosec // intentional unescaped embedding
			},
		}).Parse(htmlTemplate))
	})

	now := time.Now()
	if h.nowFunc != nil {
		now = h.nowFunc()
	}

	data := buildHTMLData(signals, now)

	if err := htmlTmpl.Execute(w, data); err != nil {
		return fmt.Errorf("execute html template: %w", err)
	}
	return nil
}

// htmlData holds all template data for the HTML dashboard.
type htmlData struct {
	GeneratedAt    string
	TotalSignals   int
	Collectors     []string
	PriorityDist   [4]int
	CollectorDist  []collectorCount
	ChurnFiles     []churnEntry
	LotteryRisk    []lotteryEntry
	TodoAgeBuckets []ageBucket
	SignalRows     []signalRow
	ChartData      map[string]any
	HasWorkspaces  bool
}

type collectorCount struct {
	Name  string
	Count int
}

type churnEntry struct {
	Path  string
	Count int
}

type lotteryEntry struct {
	Directory  string
	Confidence float64
}

type ageBucket struct {
	Label string
	Count int
}

type signalRow struct {
	Title       string
	Kind        string
	Source      string
	Location    string
	Confidence  float64
	Priority    int
	Description string
	Workspace   string
}

func buildHTMLData(signals []signal.RawSignal, now time.Time) htmlData {
	groups := groupByCollector(signals)
	collectors := sortedCollectorNames(groups)
	prioDist := priorityDistribution(signals)

	data := htmlData{
		GeneratedAt:    now.UTC().Format("2006-01-02 15:04 UTC"),
		TotalSignals:   len(signals),
		Collectors:     collectors,
		PriorityDist:   prioDist,
		CollectorDist:  buildCollectorDist(groups, collectors),
		ChurnFiles:     buildChurnEntries(signals),
		LotteryRisk:    buildLotteryEntries(signals),
		TodoAgeBuckets: buildTodoAgeBuckets(signals, now),
		SignalRows:     buildSignalRows(signals),
		HasWorkspaces:  hasMultipleWorkspaces(signals),
	}

	data.ChartData = buildHTMLChartData(data)
	return data
}

func buildCollectorDist(groups map[string][]signal.RawSignal, names []string) []collectorCount {
	dist := make([]collectorCount, 0, len(names))
	for _, name := range names {
		dist = append(dist, collectorCount{Name: name, Count: len(groups[name])})
	}
	return dist
}

func buildChurnEntries(signals []signal.RawSignal) []churnEntry {
	counts := make(map[string]int)
	for _, s := range signals {
		if s.Source == "gitlog" && s.Kind == "churn" && s.FilePath != "" {
			counts[s.FilePath]++
		}
	}
	if len(counts) == 0 {
		return nil
	}
	entries := make([]churnEntry, 0, len(counts))
	for path, count := range counts {
		entries = append(entries, churnEntry{Path: path, Count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Count > entries[j].Count
	})
	if len(entries) > 20 {
		entries = entries[:20]
	}
	return entries
}

func buildLotteryEntries(signals []signal.RawSignal) []lotteryEntry {
	var entries []lotteryEntry
	for _, s := range signals {
		if s.Source == "lotteryrisk" {
			dir := filepath.Dir(s.FilePath)
			if dir == "" || dir == "." {
				dir = s.FilePath
			}
			entries = append(entries, lotteryEntry{
				Directory:  dir,
				Confidence: s.Confidence,
			})
		}
	}
	if len(entries) == 0 {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Confidence > entries[j].Confidence
	})
	if len(entries) > 20 {
		entries = entries[:20]
	}
	return entries
}

func buildTodoAgeBuckets(signals []signal.RawSignal, now time.Time) []ageBucket {
	buckets := [5]int{} // <1w, 1-4w, 1-3m, 3-12m, >1y
	found := false
	for _, s := range signals {
		if s.Source != "todos" || s.Timestamp.IsZero() {
			continue
		}
		found = true
		age := now.Sub(s.Timestamp)
		var idx int
		switch {
		case age < 7*24*time.Hour:
			idx = 0
		case age < 28*24*time.Hour:
			idx = 1
		case age < 90*24*time.Hour:
			idx = 2
		case age < 365*24*time.Hour:
			idx = 3
		default:
			idx = 4
		}
		buckets[idx]++
	}
	if !found {
		return nil
	}
	labels := []string{"<1 week", "1-4 weeks", "1-3 months", "3-12 months", ">1 year"}
	result := make([]ageBucket, len(labels))
	for i, l := range labels {
		result[i] = ageBucket{Label: l, Count: buckets[i]}
	}
	return result
}

func buildSignalRows(signals []signal.RawSignal) []signalRow {
	rows := make([]signalRow, len(signals))
	for i, s := range signals {
		p := mapConfidenceToPriority(s.Confidence)
		if s.Priority != nil {
			p = *s.Priority
		}
		rows[i] = signalRow{
			Title:       s.Title,
			Kind:        s.Kind,
			Source:      s.Source,
			Location:    formatLocation(s.FilePath, s.Line),
			Confidence:  s.Confidence,
			Priority:    p,
			Description: s.Description,
			Workspace:   s.Workspace,
		}
	}
	return rows
}

// hasMultipleWorkspaces returns true if signals come from more than one workspace.
func hasMultipleWorkspaces(signals []signal.RawSignal) bool {
	seen := ""
	for _, s := range signals {
		ws := s.Workspace
		if seen == "" {
			seen = ws
			continue
		}
		if ws != seen {
			return true
		}
	}
	return false
}

func buildHTMLChartData(data htmlData) map[string]any {
	cd := map[string]any{
		"priority": data.PriorityDist,
	}

	sourceLabels := make([]string, len(data.CollectorDist))
	sourceValues := make([]int, len(data.CollectorDist))
	for i, c := range data.CollectorDist {
		sourceLabels[i] = c.Name
		sourceValues[i] = c.Count
	}
	cd["sourceLabels"] = sourceLabels
	cd["sourceValues"] = sourceValues

	if len(data.ChurnFiles) > 0 {
		cl := make([]string, len(data.ChurnFiles))
		cv := make([]int, len(data.ChurnFiles))
		for i, e := range data.ChurnFiles {
			cl[i] = e.Path
			cv[i] = e.Count
		}
		cd["churnLabels"] = cl
		cd["churnValues"] = cv
	}

	if len(data.LotteryRisk) > 0 {
		ll := make([]string, len(data.LotteryRisk))
		lv := make([]int, len(data.LotteryRisk))
		for i, e := range data.LotteryRisk {
			ll[i] = e.Directory
			lv[i] = int(e.Confidence * 100)
		}
		cd["lotteryLabels"] = ll
		cd["lotteryValues"] = lv
	}

	if len(data.TodoAgeBuckets) > 0 {
		tl := make([]string, len(data.TodoAgeBuckets))
		tv := make([]int, len(data.TodoAgeBuckets))
		for i, b := range data.TodoAgeBuckets {
			tl[i] = b.Label
			tv[i] = b.Count
		}
		cd["todoAgeLabels"] = tl
		cd["todoAgeValues"] = tv
	}

	return cd
}

func (h *HTMLFormatter) writeEmpty(w io.Writer) error {
	const emptyHTML = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>Stringer Dashboard</title>
<style>body{font-family:sans-serif;display:flex;justify-content:center;align-items:center;height:100vh;color:#6c757d;}</style>
</head><body><p>No signals found.</p></body></html>`
	if _, err := io.WriteString(w, emptyHTML); err != nil {
		return fmt.Errorf("write empty html: %w", err)
	}
	return nil
}
