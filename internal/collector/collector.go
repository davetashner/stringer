// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

// Package collector defines the Collector interface and a registry for
// managing available collectors.
package collector

import (
	"context"
	"fmt"
	"sync"

	"github.com/davetashner/stringer/internal/signal"
)

// Collector extracts raw signals from a repository.
type Collector interface {
	// Name returns the unique name of this collector (e.g., "todos", "gitlog").
	Name() string

	// Collect scans the repository at repoPath and returns discovered signals.
	Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error)
}

// MetricsProvider is an optional interface that collectors can implement to
// expose structured metrics from their analysis. The pipeline checks for this
// interface after Collect() returns and stores the result in CollectorResult.
type MetricsProvider interface {
	Metrics() any
}

var (
	mu       sync.RWMutex
	registry = make(map[string]Collector)
)

// Register adds a collector to the global registry.
// It panics if a collector with the same name is already registered.
func Register(c Collector) {
	mu.Lock()
	defer mu.Unlock()
	name := c.Name()
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("collector already registered: %s", name))
	}
	registry[name] = c
}

// Get returns the collector with the given name, or nil if not found.
func Get(name string) Collector {
	mu.RLock()
	defer mu.RUnlock()
	return registry[name]
}

// List returns the names of all registered collectors in no particular order.
func List() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

// resetForTesting clears the registry. Only for use in tests.
func resetForTesting() {
	mu.Lock()
	defer mu.Unlock()
	registry = make(map[string]Collector)
}
