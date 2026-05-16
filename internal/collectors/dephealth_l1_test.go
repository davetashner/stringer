// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file adds coverage the L1 expansion (PR #310) left behind: the real
// Packagist/Hex HTTP registry clients, context-cancellation in the check
// loops (stringer-ds3), and oversized-response rejection (stringer-ds2).
// The mock clients and basic abandoned/retired detection tests live in
// dephealth_test.go.

// packagistInfo builds a packagistPackageInfo whose latest version carries
// the given abandoned value (bool or string).
func packagistInfo(name string, abandoned any) *packagistPackageInfo {
	return &packagistPackageInfo{
		Packages: map[string][]packagistVersion{
			name: {{Version: "2.0.0", Abandoned: abandoned}},
		},
	}
}

// --- Packagist: context cancellation + cap ---

func TestCheckPackagistDeps_ContextCancelled(t *testing.T) {
	client := &mockPackagistRegistryClient{
		results: map[string]*packagistPackageInfo{
			"vendor/old": packagistInfo("vendor/old", true),
		},
	}
	deps := []PackageQuery{{Ecosystem: "Packagist", Name: "vendor/old", Version: "2.0.0"}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	signals := checkPackagistDeps(ctx, client, deps, "composer.json")
	assert.Empty(t, signals, "a cancelled context should stop the loop before any check")
}

func TestCheckPackagistDeps_CapEnforced(t *testing.T) {
	results := make(map[string]*packagistPackageInfo)
	deps := make([]PackageQuery, 0, maxPackagistChecks+10)
	for i := 0; i < maxPackagistChecks+10; i++ {
		name := fmt.Sprintf("vendor/pkg%d", i)
		results[name] = packagistInfo(name, true)
		deps = append(deps, PackageQuery{Ecosystem: "Packagist", Name: name, Version: "2.0.0"})
	}
	client := &mockPackagistRegistryClient{results: results}

	signals := checkPackagistDeps(context.Background(), client, deps, "composer.json")
	assert.Len(t, signals, maxPackagistChecks, "loop should stop at the check cap")
}

// --- Packagist: abandoned-reason helper ---

func TestPackagistAbandonedReason(t *testing.T) {
	tests := []struct {
		name      string
		abandoned any
		wantEmpty bool
		wantSub   string
	}{
		{"bool true", true, false, "alternative"},
		{"bool false", false, true, ""},
		{"string replacement", "vendor/new", false, "vendor/new"},
		{"empty string", "", false, "alternative"},
		{"nil", nil, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reason := packagistAbandonedReason(packagistInfo("vendor/pkg", tc.abandoned), "vendor/pkg")
			if tc.wantEmpty {
				assert.Empty(t, reason)
				return
			}
			require.NotEmpty(t, reason)
			assert.Contains(t, reason, tc.wantSub)
		})
	}
}

func TestPackagistAbandonedReason_UnknownPackage(t *testing.T) {
	info := &packagistPackageInfo{Packages: map[string][]packagistVersion{}}
	assert.Empty(t, packagistAbandonedReason(info, "vendor/missing"))
}

// --- Packagist: real HTTP client ---

func TestRealPackagistRegistryClient_FetchPackage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"packages":{"vendor/pkg":[{"version":"1.0.0","abandoned":true}]}}`))
	}))
	defer srv.Close()

	c := &realPackagistRegistryClient{baseURL: srv.URL}
	info, err := c.FetchPackage(context.Background(), "vendor/pkg")
	require.NoError(t, err)
	require.Contains(t, info.Packages, "vendor/pkg")
	assert.Equal(t, "1.0.0", info.Packages["vendor/pkg"][0].Version)
}

func TestRealPackagistRegistryClient_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := &realPackagistRegistryClient{baseURL: srv.URL}
	_, err := c.FetchPackage(context.Background(), "vendor/missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestRealPackagistRegistryClient_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := &realPackagistRegistryClient{baseURL: srv.URL}
	_, err := c.FetchPackage(context.Background(), "vendor/pkg")
	require.Error(t, err)
}

func TestRealPackagistRegistryClient_OversizedBodyRejected(t *testing.T) {
	// Body past maxRegistryResponseBytes — decodeJSONLimited must fail rather
	// than allocating the whole thing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"packages":{"vendor/pkg":[{"version":"`))
		_, _ = w.Write([]byte(strings.Repeat("x", maxRegistryResponseBytes+1024)))
		_, _ = w.Write([]byte(`"}]}}`))
	}))
	defer srv.Close()

	c := &realPackagistRegistryClient{baseURL: srv.URL}
	_, err := c.FetchPackage(context.Background(), "vendor/pkg")
	require.Error(t, err, "an oversized response body must be rejected")
}

// --- Hex: context cancellation ---

func TestCheckHexDeps_ContextCancelled(t *testing.T) {
	client := &mockHexRegistryClient{
		results: map[string]*hexPackageInfo{
			"old_lib": {
				Name:        "old_lib",
				Retirements: map[string]hexRetirement{"1.0.0": {Reason: "security"}},
			},
		},
	}
	deps := []PackageQuery{{Ecosystem: "Hex", Name: "old_lib", Version: "1.0.0"}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	signals := checkHexDeps(ctx, client, deps, "mix.exs")
	assert.Empty(t, signals, "a cancelled context should stop the loop before any check")
}

// --- Hex: real HTTP client ---

func TestRealHexRegistryClient_FetchPackage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"poison","retirements":{"3.1.0":{"reason":"security","message":"use jason"}}}`))
	}))
	defer srv.Close()

	c := &realHexRegistryClient{baseURL: srv.URL}
	info, err := c.FetchPackage(context.Background(), "poison")
	require.NoError(t, err)
	assert.Equal(t, "poison", info.Name)
	require.Contains(t, info.Retirements, "3.1.0")
	assert.Equal(t, "security", info.Retirements["3.1.0"].Reason)
}

func TestRealHexRegistryClient_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := &realHexRegistryClient{baseURL: srv.URL}
	_, err := c.FetchPackage(context.Background(), "missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestRealHexRegistryClient_OversizedBodyRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"`))
		_, _ = w.Write([]byte(strings.Repeat("x", maxRegistryResponseBytes+1024)))
		_, _ = w.Write([]byte(`"}`))
	}))
	defer srv.Close()

	c := &realHexRegistryClient{baseURL: srv.URL}
	_, err := c.FetchPackage(context.Background(), "poison")
	require.Error(t, err, "an oversized response body must be rejected")
}
