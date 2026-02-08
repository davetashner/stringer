package report

import (
	"fmt"
	"io"
	"strings"
)

// Alignment controls how a column's content is justified.
type Alignment int

const (
	// AlignLeft pads on the right (default).
	AlignLeft Alignment = iota
	// AlignRight pads on the left.
	AlignRight
)

// Column describes a single table column.
type Column struct {
	Header string
	Align  Alignment
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

	// Render header.
	if err := t.renderRow(w, t.headerValues(), widths); err != nil {
		return err
	}

	// Render separator.
	parts := make([]string, len(t.columns))
	for i, width := range widths {
		parts[i] = strings.Repeat("-", width)
	}
	if _, err := fmt.Fprintf(w, "  %s\n", strings.Join(parts, "  ")); err != nil {
		return err
	}

	// Render data rows.
	for _, row := range t.rows {
		if err := t.renderRow(w, row, widths); err != nil {
			return err
		}
	}

	return nil
}

func (t *Table) headerValues() []string {
	headers := make([]string, len(t.columns))
	for i, col := range t.columns {
		headers[i] = col.Header
	}
	return headers
}

func (t *Table) renderRow(w io.Writer, values []string, widths []int) error {
	parts := make([]string, len(t.columns))
	for i, col := range t.columns {
		val := ""
		if i < len(values) {
			val = values[i]
		}
		if col.Align == AlignRight {
			parts[i] = fmt.Sprintf("%*s", widths[i], val)
		} else {
			parts[i] = fmt.Sprintf("%-*s", widths[i], val)
		}
	}
	_, err := fmt.Fprintf(w, "  %s\n", strings.Join(parts, "  "))
	return err
}
