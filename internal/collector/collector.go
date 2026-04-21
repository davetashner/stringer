// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

// Package collector defines the Collector interface and a registry for
// managing available collectors.
package collector

import (
	"context"
	"errors"
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

// ErrAlreadyRegistered is returned by TryRegister when a collector with the
// same Name() is already in the registry. Wrapped with the offending name.
var ErrAlreadyRegistered = errors.New("collector already registered")

// TryRegister adds a collector to the global registry and returns an error
// if a collector with the same Name() is already registered. Prefer this
// over Register when the caller can handle the error (e.g. dynamic loaders,
// tests).
func TryRegister(c Collector) error {
	mu.Lock()
	defer mu.Unlock()
	name := c.Name()
	if _, exists := registry[name]; exists {
		return fmt.Errorf("%w: %s", ErrAlreadyRegistered, name)
	}
	registry[name] = c
	return nil
}

// Register adds a collector to the global registry. It panics with a
// descriptive message if registration fails — typically because a collector
// with the same Name() was already registered (usually a duplicate blank
// import). Intended for use from package init() where returning an error
// isn't an option; runtime callers should use TryRegister.
func Register(c Collector) {
	if err := TryRegister(c); err != nil {
		panic(err.Error())
	}
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
