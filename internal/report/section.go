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

// ErrAlreadyRegistered is returned by TryRegister when a section with the
// same Name() is already in the registry. Wrapped with the offending name.
var ErrAlreadyRegistered = errors.New("report section already registered")

// TryRegister adds a section to the global registry and returns an error if
// a section with the same Name() is already registered. Prefer this over
// Register when the caller can handle the error.
func TryRegister(s Section) error {
	mu.Lock()
	defer mu.Unlock()
	name := s.Name()
	if _, exists := registry[name]; exists {
		return fmt.Errorf("%w: %s", ErrAlreadyRegistered, name)
	}
	registry[name] = s
	order = append(order, name)
	return nil
}

// Register adds a section to the global registry. It panics with a
// descriptive message if registration fails — typically because a section
// with the same Name() was already registered (usually a duplicate blank
// import). Intended for use from package init() where returning an error
// isn't an option; runtime callers should use TryRegister.
func Register(s Section) {
	if err := TryRegister(s); err != nil {
		panic(err.Error())
	}
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
