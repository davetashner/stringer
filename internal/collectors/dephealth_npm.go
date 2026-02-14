// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/davetashner/stringer/internal/signal"
)

// maxNpmChecks caps the number of npm registry lookups per scan.
const maxNpmChecks = 50

// npmRegistryBaseURL is the default npm registry URL.
const npmRegistryBaseURL = "https://registry.npmjs.org"

// npmRegistryClient fetches package metadata from the npm registry.
type npmRegistryClient interface {
	FetchPackage(ctx context.Context, name string) (*npmPackageInfo, error)
}

// npmPackageInfo represents the subset of npm registry response we need.
type npmPackageInfo struct {
	Name       string `json:"name"`
	Deprecated string `json:"deprecated"`
}

// realNpmRegistryClient queries the real npm registry.
type realNpmRegistryClient struct {
	httpClient *http.Client
	baseURL    string
}

// FetchPackage queries the npm registry for a package's abbreviated metadata.
func (c *realNpmRegistryClient) FetchPackage(ctx context.Context, name string) (*npmPackageInfo, error) {
	base := c.baseURL
	if base == "" {
		base = npmRegistryBaseURL
	}
	url := fmt.Sprintf("%s/%s", base, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	// Request abbreviated metadata to reduce response size.
	req.Header.Set("Accept", "application/vnd.npm.install-v1+json")

	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("npm registry returned %d for %s", resp.StatusCode, name)
	}

	var info npmPackageInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding npm response for %s: %w", name, err)
	}

	return &info, nil
}

// checkNpmDeps queries the npm registry for each dependency and emits signals
// for packages that are deprecated.
func checkNpmDeps(ctx context.Context, client npmRegistryClient, deps []PackageQuery, filePath string) []signal.RawSignal {
	var signals []signal.RawSignal
	checked := 0

	for _, dep := range deps {
		if checked >= maxNpmChecks {
			slog.Info("dephealth: reached npm registry check cap", "cap", maxNpmChecks)
			break
		}
		checked++

		info, err := client.FetchPackage(ctx, dep.Name)
		if err != nil {
			slog.Debug("dephealth: npm lookup failed", "package", dep.Name, "error", err)
			continue
		}

		if info.Deprecated != "" {
			signals = append(signals, signal.RawSignal{
				Source:      "dephealth",
				Kind:        "deprecated-dependency",
				FilePath:    filePath,
				Title:       fmt.Sprintf("Deprecated npm package: %s", dep.Name),
				Description: fmt.Sprintf("npm package %s is deprecated: %s", dep.Name, info.Deprecated),
				Confidence:  0.8,
				Tags:        []string{"deprecated-dependency", "dephealth", "npm"},
			})
		}
	}

	return signals
}
