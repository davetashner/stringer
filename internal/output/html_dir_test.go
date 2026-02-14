// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package output

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time interface checks.
var (
	_ Formatter          = (*HTMLDirFormatter)(nil)
	_ DirectoryFormatter = (*HTMLDirFormatter)(nil)
)

func TestHTMLDirFormatter_Name(t *testing.T) {
	f := NewHTMLDirFormatter()
	assert.Equal(t, "html-dir", f.Name())
}

func TestHTMLDirFormatter_Registration(t *testing.T) {
	f, err := GetFormatter("html-dir")
	require.NoError(t, err)
	assert.Equal(t, "html-dir", f.Name())
}

func TestHTMLDirFormatter_FormatReturnsError(t *testing.T) {
	f := NewHTMLDirFormatter()
	err := f.Format(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--output (-o)")
}

func TestHTMLDirFormatter_FormatDir_BasicOutput(t *testing.T) {
	dir := t.TempDir()
	f := &HTMLDirFormatter{
		nowFunc: func() time.Time { return time.Date(2026, 2, 12, 10, 0, 0, 0, time.UTC) },
	}
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Fix this", FilePath: "main.go", Line: 10, Confidence: 0.5},
	}

	err := f.FormatDir(signals, dir)
	require.NoError(t, err)

	// Verify directory structure.
	assertFileExists(t, filepath.Join(dir, "index.html"))
	assertFileExists(t, filepath.Join(dir, "assets", "dashboard.css"))
	assertFileExists(t, filepath.Join(dir, "assets", "dashboard.js"))

	// Verify index.html content.
	html := readFile(t, filepath.Join(dir, "index.html"))
	assert.Contains(t, html, "<!DOCTYPE html>")
	assert.Contains(t, html, "<title>Stringer Dashboard</title>")
	assert.Contains(t, html, "2026-02-12 10:00 UTC")
	assert.Contains(t, html, "1 signals from 1 collector(s)")
	assert.Contains(t, html, `href="assets/dashboard.css"`)
	assert.Contains(t, html, `src="assets/dashboard.js"`)
}

func TestHTMLDirFormatter_FormatDir_ExternalAssets(t *testing.T) {
	dir := t.TempDir()
	f := NewHTMLDirFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Test", FilePath: "a.go", Confidence: 0.5},
	}

	err := f.FormatDir(signals, dir)
	require.NoError(t, err)

	html := readFile(t, filepath.Join(dir, "index.html"))

	// External references present.
	assert.Contains(t, html, `<link rel="stylesheet" href="assets/dashboard.css">`)
	assert.Contains(t, html, `<script src="assets/dashboard.js">`)

	// No inline <style> block (CSS is external).
	assert.NotContains(t, html, "<style>")
}

func TestHTMLDirFormatter_FormatDir_Navigation(t *testing.T) {
	dir := t.TempDir()
	f := NewHTMLDirFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Test", FilePath: "a.go", Confidence: 0.5},
	}

	err := f.FormatDir(signals, dir)
	require.NoError(t, err)

	html := readFile(t, filepath.Join(dir, "index.html"))
	assert.Contains(t, html, "<nav>")
	assert.Contains(t, html, `href="#summary"`)
	assert.Contains(t, html, `href="#charts"`)
	assert.Contains(t, html, `href="#signals"`)
}

func TestHTMLDirFormatter_FormatDir_CSSContent(t *testing.T) {
	dir := t.TempDir()
	f := NewHTMLDirFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Test", FilePath: "a.go", Confidence: 0.5},
	}

	err := f.FormatDir(signals, dir)
	require.NoError(t, err)

	css := readFile(t, filepath.Join(dir, "assets", "dashboard.css"))
	assert.Contains(t, css, "--bg:")
	assert.Contains(t, css, "prefers-color-scheme: dark")
	assert.Contains(t, css, "scroll-behavior: smooth")
	assert.Contains(t, css, "nav {")
}

func TestHTMLDirFormatter_FormatDir_JSContent(t *testing.T) {
	dir := t.TempDir()
	f := NewHTMLDirFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Test", FilePath: "a.go", Confidence: 0.5},
	}

	err := f.FormatDir(signals, dir)
	require.NoError(t, err)

	js := readFile(t, filepath.Join(dir, "assets", "dashboard.js"))
	assert.Contains(t, js, "function svgEl")
	assert.Contains(t, js, "renderBarChart")
	assert.Contains(t, js, "renderDoughnut")
	assert.Contains(t, js, "applyFilters")
	assert.Contains(t, js, "toggleDetail")
}

func TestHTMLDirFormatter_FormatDir_EmptySignals(t *testing.T) {
	dir := t.TempDir()
	f := NewHTMLDirFormatter()

	t.Run("nil", func(t *testing.T) {
		d := filepath.Join(dir, "nil")
		err := f.FormatDir(nil, d)
		require.NoError(t, err)

		html := readFile(t, filepath.Join(d, "index.html"))
		assert.Contains(t, html, "No signals found")
		assert.Contains(t, html, `href="assets/dashboard.css"`)
		assertFileExists(t, filepath.Join(d, "assets", "dashboard.css"))
		assertFileExists(t, filepath.Join(d, "assets", "dashboard.js"))
	})

	t.Run("empty_slice", func(t *testing.T) {
		d := filepath.Join(dir, "empty")
		err := f.FormatDir([]signal.RawSignal{}, d)
		require.NoError(t, err)

		html := readFile(t, filepath.Join(d, "index.html"))
		assert.Contains(t, html, "No signals found")
	})
}

func TestHTMLDirFormatter_FormatDir_XSSSafety(t *testing.T) {
	dir := t.TempDir()
	f := NewHTMLDirFormatter()
	signals := []signal.RawSignal{
		{
			Source:      "todos",
			Kind:        "todo",
			Title:       `<script>alert('xss')</script>`,
			FilePath:    `<img src=x onerror=alert(1)>`,
			Confidence:  0.5,
			Description: `<b onmouseover="alert('xss')">hover</b>`,
		},
	}

	err := f.FormatDir(signals, dir)
	require.NoError(t, err)

	html := readFile(t, filepath.Join(dir, "index.html"))
	assert.NotContains(t, html, "<script>alert")
	assert.NotContains(t, html, `<img src=x`)
	assert.Contains(t, html, "&lt;script&gt;")
}

func TestHTMLDirFormatter_FormatDir_ChartData(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 2, 12, 10, 0, 0, 0, time.UTC)
	f := &HTMLDirFormatter{
		nowFunc: func() time.Time { return now },
	}
	signals := []signal.RawSignal{
		{Source: "gitlog", Kind: "churn", Title: "Churn: config.go", FilePath: "config.go", Confidence: 0.7},
		{Source: "lotteryrisk", Kind: "lottery-risk", Title: "Risk in pkg/", FilePath: "pkg/handler.go", Confidence: 0.8},
		{Source: "todos", Kind: "todo", Title: "Old todo", FilePath: "main.go", Line: 5, Confidence: 0.5,
			Timestamp: now.Add(-400 * 24 * time.Hour)},
	}

	err := f.FormatDir(signals, dir)
	require.NoError(t, err)

	html := readFile(t, filepath.Join(dir, "index.html"))
	// Chart data should be inline as a script variable.
	assert.Contains(t, html, "var chartData =")
	assert.Contains(t, html, "churnLabels")
	assert.Contains(t, html, "lotteryLabels")
	assert.Contains(t, html, "todoAgeLabels")
}

func TestHTMLDirFormatter_FormatDir_SignalTable(t *testing.T) {
	dir := t.TempDir()
	f := NewHTMLDirFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Add tests", FilePath: "handler.go", Line: 42, Confidence: 0.75},
		{Source: "gitlog", Kind: "churn", Title: "High churn", FilePath: "config.go", Confidence: 0.85},
	}

	err := f.FormatDir(signals, dir)
	require.NoError(t, err)

	html := readFile(t, filepath.Join(dir, "index.html"))
	assert.Contains(t, html, "Add tests")
	assert.Contains(t, html, "handler.go:42")
	assert.Contains(t, html, "0.75")
	assert.Contains(t, html, "High churn")
}

func TestHTMLDirFormatter_FormatDir_CreatesSubdirectories(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "deep", "nested", "output")

	f := NewHTMLDirFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "Test", FilePath: "a.go", Confidence: 0.5},
	}

	err := f.FormatDir(signals, nested)
	require.NoError(t, err)

	assertFileExists(t, filepath.Join(nested, "index.html"))
	assertFileExists(t, filepath.Join(nested, "assets", "dashboard.css"))
	assertFileExists(t, filepath.Join(nested, "assets", "dashboard.js"))
}

func TestHTMLDirFormatter_FormatDir_Workspaces(t *testing.T) {
	dir := t.TempDir()
	f := NewHTMLDirFormatter()
	signals := []signal.RawSignal{
		{Source: "todos", Kind: "todo", Title: "A", FilePath: "a.go", Confidence: 0.5, Workspace: "frontend"},
		{Source: "todos", Kind: "todo", Title: "B", FilePath: "b.go", Confidence: 0.5, Workspace: "backend"},
	}

	err := f.FormatDir(signals, dir)
	require.NoError(t, err)

	html := readFile(t, filepath.Join(dir, "index.html"))
	assert.Contains(t, html, "Workspace")
	assert.Contains(t, html, "frontend")
	assert.Contains(t, html, "backend")
}

// --- Helpers ---

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.NoError(t, err, "expected file to exist: %s", path)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test helper reads test output files
	require.NoError(t, err)
	return string(data)
}
