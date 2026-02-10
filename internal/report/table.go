package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/fatih/color"
)

// Alignment controls how a column's content is justified.
type Alignment int

const (
	// AlignLeft pads on the right (default).
	AlignLeft Alignment = iota
	// AlignRight pads on the left.
	AlignRight
)

// ColorFunc maps a cell value to a colored string. If nil, no color is applied.
type ColorFunc func(value string) string

// Column describes a single table column.
type Column struct {
	Header string
	Align  Alignment
	Color  ColorFunc // optional per-cell color function
}

// Table renders aligned text tables to an io.Writer.
type Table struct {
	columns []Column
	rows    [][]string
}

// NewTable creates a table with the given column definitions.
func NewTable(columns ...Column) *Table {
	return &Table{columns: columns}
}

// AddRow appends a row. Values beyond the column count are silently ignored;
// missing values are treated as empty strings.
func (t *Table) AddRow(values ...string) {
	row := make([]string, len(t.columns))
	for i := range row {
		if i < len(values) {
			row[i] = values[i]
		}
	}
	t.rows = append(t.rows, row)
}

// Render writes the table to w with computed column widths.
func (t *Table) Render(w io.Writer) error {
	if len(t.columns) == 0 {
		return nil
	}

	// Compute max width per column.
	widths := make([]int, len(t.columns))
	for i, col := range t.columns {
		widths[i] = len(col.Header)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Render header (bold).
	if err := t.renderHeader(w, widths); err != nil {
		return err
	}

	// Render separator.
	parts := make([]string, len(t.columns))
	for i, width := range widths {
		parts[i] = strings.Repeat("-", width)
	}
	if _, err := fmt.Fprintf(w, "  %s\n", strings.Join(parts, "  ")); err != nil {
		return fmt.Errorf("render table: %w", err)
	}

	// Render data rows.
	for _, row := range t.rows {
		if err := t.renderRow(w, row, widths); err != nil {
			return err
		}
	}

	return nil
}

func (t *Table) renderHeader(w io.Writer, widths []int) error {
	bold := color.New(color.Bold)
	parts := make([]string, len(t.columns))
	for i, col := range t.columns {
		if col.Align == AlignRight {
			parts[i] = bold.Sprintf("%*s", widths[i], col.Header)
		} else {
			parts[i] = bold.Sprintf("%-*s", widths[i], col.Header)
		}
	}
	if _, err := fmt.Fprintf(w, "  %s\n", strings.Join(parts, "  ")); err != nil {
		return fmt.Errorf("render table: %w", err)
	}
	return nil
}

func (t *Table) renderRow(w io.Writer, values []string, widths []int) error {
	parts := make([]string, len(t.columns))
	for i, col := range t.columns {
		val := ""
		if i < len(values) {
			val = values[i]
		}
		// Apply color function if set, then pad.
		display := val
		if col.Color != nil {
			display = col.Color(val)
		}
		// Padding is based on raw value length, not ANSI-colored length.
		pad := widths[i] - len(val)
		if pad < 0 {
			pad = 0
		}
		if col.Align == AlignRight {
			parts[i] = strings.Repeat(" ", pad) + display
		} else {
			parts[i] = display + strings.Repeat(" ", pad)
		}
	}
	if _, err := fmt.Fprintf(w, "  %s\n", strings.Join(parts, "  ")); err != nil {
		return fmt.Errorf("render table: %w", err)
	}
	return nil
}
