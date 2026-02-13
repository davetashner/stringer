package output

import (
	"fmt"
	"io"
	"sort"

	"github.com/davetashner/stringer/internal/signal"
)

func init() {
	RegisterFormatter(NewMarkdownFormatter())
}

// MarkdownFormatter writes signals as a human-readable Markdown summary.
type MarkdownFormatter struct{}

// Compile-time interface check.
var _ Formatter = (*MarkdownFormatter)(nil)

// NewMarkdownFormatter returns a new MarkdownFormatter.
func NewMarkdownFormatter() *MarkdownFormatter {
	return &MarkdownFormatter{}
}

// Name returns the format name.
func (m *MarkdownFormatter) Name() string {
	return "markdown"
}

// Format writes all signals as a grouped Markdown document to w.
//
// When signals span multiple workspaces, output is grouped by workspace first,
// then by collector within each workspace. For single-workspace or non-monorepo
// signals, the output is unchanged (grouped by collector only).
func (m *MarkdownFormatter) Format(signals []signal.RawSignal, w io.Writer) error {
	if len(signals) == 0 {
		return nil
	}

	// Group signals by collector (source).
	groups := groupByCollector(signals)
	collectorNames := sortedCollectorNames(groups)

	// Compute priority distribution.
	prioDist := priorityDistribution(signals)

	// Write header.
	if err := writeHeader(w, len(signals), collectorNames); err != nil {
		return err
	}

	// Write priority table.
	if err := writePriorityTable(w, prioDist); err != nil {
		return err
	}

	// Check if signals span multiple workspaces.
	wsGroups := groupByWorkspace(signals)
	if len(wsGroups) > 1 {
		wsNames := sortedWorkspaceNames(wsGroups)
		for _, wsName := range wsNames {
			if _, err := fmt.Fprintf(w, "## %s\n\n", wsName); err != nil {
				return fmt.Errorf("write workspace heading: %w", err)
			}
			wsCollGroups := groupByCollector(wsGroups[wsName])
			for _, name := range sortedCollectorNames(wsCollGroups) {
				if err := writeCollectorSection(w, name, wsCollGroups[name]); err != nil {
					return err
				}
			}
		}
		return nil
	}

	// Single workspace or non-monorepo: group by collector only.
	for _, name := range collectorNames {
		if err := writeCollectorSection(w, name, groups[name]); err != nil {
			return err
		}
	}

	return nil
}

// groupByWorkspace groups signals by their Workspace field.
// Signals with empty Workspace are grouped under "(root)".
func groupByWorkspace(signals []signal.RawSignal) map[string][]signal.RawSignal {
	groups := make(map[string][]signal.RawSignal)
	for _, sig := range signals {
		ws := sig.Workspace
		if ws == "" {
			ws = "(root)"
		}
		groups[ws] = append(groups[ws], sig)
	}
	return groups
}

// sortedWorkspaceNames returns workspace names sorted alphabetically.
func sortedWorkspaceNames(groups map[string][]signal.RawSignal) []string {
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// groupByCollector groups signals by their Source field.
func groupByCollector(signals []signal.RawSignal) map[string][]signal.RawSignal {
	groups := make(map[string][]signal.RawSignal)
	for _, sig := range signals {
		source := sig.Source
		if source == "" {
			source = "unknown"
		}
		groups[source] = append(groups[source], sig)
	}
	return groups
}

// sortedCollectorNames returns the collector names from the map in sorted order.
func sortedCollectorNames(groups map[string][]signal.RawSignal) []string {
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// priorityDistribution counts signals per priority level.
func priorityDistribution(signals []signal.RawSignal) [4]int {
	var dist [4]int
	for _, sig := range signals {
		p := mapConfidenceToPriority(sig.Confidence)
		idx := p - 1
		if idx >= 0 && idx < len(dist) {
			dist[idx]++
		}
	}
	return dist
}

// writeHeader writes the Markdown title and summary line.
func writeHeader(w io.Writer, total int, collectorNames []string) error {
	if _, err := fmt.Fprintf(w, "# Stringer Scan Results\n\n"); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	// Build collector names list.
	collectorList := ""
	for i, name := range collectorNames {
		if i > 0 {
			collectorList += ", "
		}
		collectorList += name
	}

	if _, err := fmt.Fprintf(w, "**Total signals:** %d | **Collectors:** %s\n\n", total, collectorList); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}

	return nil
}

// writePriorityTable writes the priority distribution table.
func writePriorityTable(w io.Writer, dist [4]int) error {
	if _, err := fmt.Fprintf(w, "| Priority | Count |\n"); err != nil {
		return fmt.Errorf("write priority table: %w", err)
	}
	if _, err := fmt.Fprintf(w, "|----------|-------|\n"); err != nil {
		return fmt.Errorf("write priority table: %w", err)
	}
	for i := 0; i < 4; i++ {
		if _, err := fmt.Fprintf(w, "| P%d       | %d     |\n", i+1, dist[i]); err != nil {
			return fmt.Errorf("write priority table: %w", err)
		}
	}
	if _, err := fmt.Fprintf(w, "\n"); err != nil {
		return fmt.Errorf("write priority table: %w", err)
	}
	return nil
}

// writeCollectorSection writes a single collector's signals section.
func writeCollectorSection(w io.Writer, name string, signals []signal.RawSignal) error {
	if _, err := fmt.Fprintf(w, "## %s (%d signals)\n\n", name, len(signals)); err != nil {
		return fmt.Errorf("write collector heading: %w", err)
	}

	for _, sig := range signals {
		loc := formatLocation(sig.FilePath, sig.Line)
		if _, err := fmt.Fprintf(w, "- **%s** — `%s` (confidence: %.2f)\n", sig.Title, loc, sig.Confidence); err != nil {
			return fmt.Errorf("write signal: %w", err)
		}
	}

	if _, err := fmt.Fprintf(w, "\n"); err != nil {
		return fmt.Errorf("write section end: %w", err)
	}

	return nil
}

// formatLocation formats a file path and line number as a clickable reference.
// Returns "file:line" when line > 0, otherwise just the file path.
// Returns "unknown" if no file path is provided.
func formatLocation(filePath string, line int) string {
	if filePath == "" {
		return "unknown"
	}
	if line > 0 {
		return fmt.Sprintf("%s:%d", filePath, line)
	}
	return filePath
}
