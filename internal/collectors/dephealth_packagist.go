// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"fmt"
	"net/http"

	"github.com/davetashner/stringer/internal/signal"
)

// packagistBaseURL is the default Packagist API URL.
const packagistBaseURL = "https://repo.packagist.org"

// packagistRegistryClient fetches package metadata from Packagist.
type packagistRegistryClient interface {
	FetchPackage(ctx context.Context, name string) (*packagistPackageInfo, error)
}

// packagistPackageInfo represents the subset of Packagist API response we need.
type packagistPackageInfo struct {
	Packages map[string][]packagistVersion `json:"packages"`
}

// packagistVersion represents a single version entry from Packagist.
type packagistVersion struct {
	Version   string `json:"version"`
	Abandoned any    `json:"abandoned"` // false, true, or string (replacement package)
}

// realPackagistRegistryClient queries the real Packagist API.
type realPackagistRegistryClient struct {
	httpClient *http.Client
	baseURL    string
}

// FetchPackage queries Packagist for a package's metadata.
func (c *realPackagistRegistryClient) FetchPackage(ctx context.Context, name string) (*packagistPackageInfo, error) {
	base := c.baseURL
	if base == "" {
		base = packagistBaseURL
	}
	url := fmt.Sprintf("%s/p2/%s.json", base, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := doRegistryRequest(registryHTTPClient(c.httpClient), req, "packagist", name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var info packagistPackageInfo
	if err := decodeJSONLimited(resp.Body, &info); err != nil {
		return nil, fmt.Errorf("decoding packagist response for %s: %w", name, err)
	}

	return &info, nil
}

// checkPackagistDeps queries Packagist for each dependency and emits signals
// for packages that are abandoned.
func (r *registryRun) checkPackagistDeps(ctx context.Context, client packagistRegistryClient, deps []PackageQuery, filePath string) []signal.RawSignal {
	return lookupEach(ctx, r, "packagist", deps, func(ctx context.Context, dep PackageQuery) []signal.RawSignal {
		info, err := client.FetchPackage(ctx, dep.Name)
		if err != nil {
			r.lookupFailed("packagist", dep.Name, err)
			return nil
		}

		reason := packagistAbandonedReason(info, dep.Name)
		if reason == "" {
			return nil
		}
		return []signal.RawSignal{{
			Source:      "dephealth",
			Kind:        "deprecated-dependency",
			FilePath:    filePath,
			Title:       fmt.Sprintf("Abandoned Packagist package: %s", dep.Name),
			Description: fmt.Sprintf("Packagist package %s is abandoned. %s", dep.Name, reason),
			Confidence:  0.8,
			Tags:        []string{"deprecated-dependency", "dephealth", "php"},
		}}
	})
}

// packagistAbandonedReason checks if any version of the package is marked as abandoned.
// Returns a reason string, or "".
func packagistAbandonedReason(info *packagistPackageInfo, name string) string {
	versions, ok := info.Packages[name]
	if !ok || len(versions) == 0 {
		return ""
	}

	// Check the latest version (first entry in the Packagist v2 API).
	v := versions[0]
	switch a := v.Abandoned.(type) {
	case bool:
		if a {
			return "Consider migrating to an alternative."
		}
	case string:
		if a != "" {
			return fmt.Sprintf("Suggested replacement: %s.", a)
		}
		return "Consider migrating to an alternative."
	}

	return ""
}
