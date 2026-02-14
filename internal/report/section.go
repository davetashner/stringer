// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

// Package report provides a pluggable section registry for stringer report.
// Each section consumes metrics from collectors and renders a focused analysis.
package report

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/davetashner/stringer/internal/signal"
)

// ErrMetricsNotAvailable indicates a section's required metrics are missing,
// typically because the corresponding collector was not run.
var ErrMetricsNotAvailable = errors.New("metrics not available")

// Section is a pluggable report section that analyzes scan results and
// renders a focused report segment.
type Section interface {
	// Name returns the unique identifier for this section (e.g., "lottery-risk").
	Name() string

	// Description returns a human-readable description of what this section reports.
	Description() string

	// Analyze processes the scan result and prepares internal state for rendering.
	// Returns ErrMetricsNotAvailable (wrapped) if required metrics are missing.
	Analyze(result *signal.ScanResult) error

	// Render writes the section output to w.
	Render(w io.Writer) error
}

var (
	mu       sync.RWMutex
	registry = make(map[string]Section)
	order    []string // insertion order for deterministic listing
)

// Register adds a section to the global registry.
// It panics if a section with the same name is already registered.
func Register(s Section) {
	mu.Lock()
	defer mu.Unlock()
	name := s.Name()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("report section already registered: %s", name))
	}
	registry[name] = s
	order = append(order, name)
}

// Get returns the section with the given name, or nil if not found.
func Get(name string) Section {
	mu.RLock()
	defer mu.RUnlock()
	return registry[name]
}

// List returns the names of all registered sections in registration order.
func List() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, len(order))
	copy(out, order)
	return out
}

// resetForTesting clears the registry. Only for use in tests.
func resetForTesting() {
	mu.Lock()
	defer mu.Unlock()
	registry = make(map[string]Section)
	order = nil
}
