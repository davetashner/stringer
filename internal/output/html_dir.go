// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package output

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/davetashner/stringer/internal/signal"
)

func init() {
	RegisterFormatter(NewHTMLDirFormatter())
}

// HTMLDirFormatter writes signals as an HTML dashboard with external CSS and JS
// in a directory structure: index.html + assets/dashboard.{css,js}.
type HTMLDirFormatter struct {
	nowFunc func() time.Time
}

// Compile-time interface checks.
var (
	_ Formatter          = (*HTMLDirFormatter)(nil)
	_ DirectoryFormatter = (*HTMLDirFormatter)(nil)
)

// NewHTMLDirFormatter returns a new HTMLDirFormatter.
func NewHTMLDirFormatter() *HTMLDirFormatter {
	return &HTMLDirFormatter{}
}

// Name returns the format name.
func (h *HTMLDirFormatter) Name() string {
	return "html-dir"
}

// Format returns an error directing users to use --output (-o) with html-dir.
func (h *HTMLDirFormatter) Format(_ []signal.RawSignal, _ io.Writer) error {
	return fmt.Errorf("html-dir format requires --output (-o) flag to specify output directory")
}

var (
	htmlDirTmplOnce sync.Once
	htmlDirTmpl     *template.Template
)

// FormatDir writes the dashboard to dir as index.html + assets/.
func (h *HTMLDirFormatter) FormatDir(signals []signal.RawSignal, dir string) error {
	assetsDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assetsDir, 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Write CSS.
	if err := os.WriteFile(filepath.Join(assetsDir, "dashboard.css"), []byte(htmlDirCSS), 0o600); err != nil { //nolint:gosec // dashboard assets are meant to be readable
		return fmt.Errorf("write dashboard.css: %w", err)
	}

	// Write JS.
	if err := os.WriteFile(filepath.Join(assetsDir, "dashboard.js"), []byte(htmlDirJS), 0o600); err != nil { //nolint:gosec // dashboard assets are meant to be readable
		return fmt.Errorf("write dashboard.js: %w", err)
	}

	if len(signals) == 0 {
		return h.writeEmptyDir(filepath.Join(dir, "index.html"))
	}

	htmlDirTmplOnce.Do(func() {
		htmlDirTmpl = template.Must(template.New("dashboard-dir").Funcs(template.FuncMap{
			"json": func(v any) template.JS {
				b, _ := json.Marshal(v)
				return template.JS(b) //nolint:gosec // intentional unescaped embedding
			},
		}).Parse(htmlDirPageTemplate))
	})

	now := time.Now()
	if h.nowFunc != nil {
		now = h.nowFunc()
	}

	data := buildHTMLData(signals, now)

	indexPath := filepath.Join(dir, "index.html")
	f, err := os.Create(indexPath) //nolint:gosec // path is user-specified output directory
	if err != nil {
		return fmt.Errorf("create index.html: %w", err)
	}
	defer f.Close() //nolint:errcheck // best-effort close

	if err := htmlDirTmpl.Execute(f, data); err != nil {
		return fmt.Errorf("execute html-dir template: %w", err)
	}
	return nil
}

func (h *HTMLDirFormatter) writeEmptyDir(path string) error {
	const emptyHTML = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>Stringer Dashboard</title>
<link rel="stylesheet" href="assets/dashboard.css">
</head><body><p style="text-align:center;margin-top:40vh;color:var(--muted)">No signals found.</p></body></html>`
	if err := os.WriteFile(path, []byte(emptyHTML), 0o600); err != nil { //nolint:gosec // user-specified output path
		return fmt.Errorf("write empty index.html: %w", err)
	}
	return nil
}
