// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"fmt"
	"net/http"

	"github.com/davetashner/stringer/internal/signal"
)

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

	resp, err := doRegistryRequest(registryHTTPClient(c.httpClient), req, "npm registry", name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var info npmPackageInfo
	if err := decodeJSONLimited(resp.Body, &info); err != nil {
		return nil, fmt.Errorf("decoding npm response for %s: %w", name, err)
	}

	return &info, nil
}

// checkNpmDeps queries the npm registry for each dependency and emits signals
// for packages that are deprecated.
func (r *registryRun) checkNpmDeps(ctx context.Context, client npmRegistryClient, deps []PackageQuery, filePath string) []signal.RawSignal {
	return lookupEach(ctx, r, "npm", deps, func(ctx context.Context, dep PackageQuery) []signal.RawSignal {
		info, err := client.FetchPackage(ctx, dep.Name)
		if err != nil {
			r.lookupFailed("npm", dep.Name, err)
			return nil
		}
		if info.Deprecated == "" {
			return nil
		}
		return []signal.RawSignal{{
			Source:      "dephealth",
			Kind:        "deprecated-dependency",
			FilePath:    filePath,
			Title:       fmt.Sprintf("Deprecated npm package: %s", dep.Name),
			Description: fmt.Sprintf("npm package %s is deprecated: %s", dep.Name, info.Deprecated),
			Confidence:  0.8,
			Tags:        []string{"deprecated-dependency", "dephealth", "npm"},
		}}
	})
}
