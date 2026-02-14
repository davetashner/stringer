// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collector

import (
	"context"
	"sort"
	"testing"

	"github.com/davetashner/stringer/internal/signal"
)

// stubCollector is a minimal Collector implementation for testing.
type stubCollector struct {
	name string
}

func (s *stubCollector) Name() string { return s.name }
func (s *stubCollector) Collect(_ context.Context, _ string, _ signal.CollectorOpts) ([]signal.RawSignal, error) {
	return nil, nil
}

func TestRegisterAndGet(t *testing.T) {
	resetForTesting()

	c := &stubCollector{name: "test-collector"}
	Register(c)

	got := Get("test-collector")
	if got == nil {
		t.Fatal("Get returned nil for registered collector")
	}
	if got.Name() != "test-collector" {
		t.Errorf("Name() = %q, want %q", got.Name(), "test-collector")
	}
}

func TestGetUnknown(t *testing.T) {
	resetForTesting()

	got := Get("nonexistent")
	if got != nil {
		t.Errorf("Get returned %v for unregistered collector, want nil", got)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	resetForTesting()

	Register(&stubCollector{name: "dup"})

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	Register(&stubCollector{name: "dup"})
}

func TestList(t *testing.T) {
	resetForTesting()

	Register(&stubCollector{name: "alpha"})
	Register(&stubCollector{name: "beta"})

	names := List()
	sort.Strings(names)
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("List() = %v, want [alpha beta]", names)
	}
}
