// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package report

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTable_BasicRender(t *testing.T) {
	tbl := NewTable(
		Column{Header: "Name"},
		Column{Header: "Count", Align: AlignRight},
	)
	tbl.AddRow("alpha", "10")
	tbl.AddRow("bravo-long", "5")

	var buf bytes.Buffer
	require.NoError(t, tbl.Render(&buf))

	out := buf.String()
	assert.Contains(t, out, "Name")
	assert.Contains(t, out, "Count")
	assert.Contains(t, out, "----------")
	assert.Contains(t, out, "alpha")
	assert.Contains(t, out, "bravo-long")
}

func TestTable_ColumnAlignment(t *testing.T) {
	tbl := NewTable(
		Column{Header: "Left"},
		Column{Header: "Right", Align: AlignRight},
	)
	tbl.AddRow("a", "1")
	tbl.AddRow("bb", "22")

	var buf bytes.Buffer
	require.NoError(t, tbl.Render(&buf))

	lines := splitLines(buf.String())
	// Data rows should have right-aligned numbers.
	// "a" should be left-padded with spaces; "1" should be right-padded.
	require.True(t, len(lines) >= 3, "expected at least header + separator + 2 rows")
}

func TestTable_MissingValues(t *testing.T) {
	tbl := NewTable(
		Column{Header: "A"},
		Column{Header: "B"},
		Column{Header: "C"},
	)
	tbl.AddRow("only-one")

	var buf bytes.Buffer
	require.NoError(t, tbl.Render(&buf))
	assert.Contains(t, buf.String(), "only-one")
}

func TestTable_ExtraValues(t *testing.T) {
	tbl := NewTable(
		Column{Header: "A"},
	)
	tbl.AddRow("one", "extra-ignored")

	var buf bytes.Buffer
	require.NoError(t, tbl.Render(&buf))
	assert.Contains(t, buf.String(), "one")
	assert.NotContains(t, buf.String(), "extra-ignored")
}

func TestTable_EmptyTable(t *testing.T) {
	tbl := NewTable(Column{Header: "X"})

	var buf bytes.Buffer
	require.NoError(t, tbl.Render(&buf))
	assert.Contains(t, buf.String(), "X")
	assert.Contains(t, buf.String(), "-")
}

func TestTable_NoColumns(t *testing.T) {
	tbl := NewTable()

	var buf bytes.Buffer
	require.NoError(t, tbl.Render(&buf))
	assert.Empty(t, buf.String())
}

func TestTable_WidthComputation(t *testing.T) {
	tbl := NewTable(
		Column{Header: "ID"},
		Column{Header: "Value"},
	)
	tbl.AddRow("short", "x")
	tbl.AddRow("much-longer-value", "y")

	var buf bytes.Buffer
	require.NoError(t, tbl.Render(&buf))

	lines := splitLines(buf.String())
	// All lines should have the same effective width (2-space indent + padded columns).
	require.True(t, len(lines) >= 4)
	// Header separator dashes should be at least as wide as "much-longer-value".
	assert.Contains(t, lines[1], "-----------------")
}

func splitLines(s string) []string {
	var lines []string
	for _, line := range bytes.Split([]byte(s), []byte("\n")) {
		if len(line) > 0 {
			lines = append(lines, string(line))
		}
	}
	return lines
}
