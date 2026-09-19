// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/davetashner/stringer/internal/signal"
)

// pypiBaseURL is the default PyPI JSON API URL.
const pypiBaseURL = "https://pypi.org/pypi"

// pypiRegistryClient fetches package metadata from PyPI.
type pypiRegistryClient interface {
	FetchPackage(ctx context.Context, name string) (*pypiPackageInfo, error)
}

// pypiPackageInfo represents the subset of PyPI JSON API response we need.
type pypiPackageInfo struct {
	Info struct {
		Name         string   `json:"name"`
		Classifiers  []string `json:"classifiers"`
		Yanked       bool     `json:"yanked"`
		YankedReason string   `json:"yanked_reason"`
	} `json:"info"`
}

// realPyPIRegistryClient queries the real PyPI JSON API.
type realPyPIRegistryClient struct {
	httpClient *http.Client
	baseURL    string
}

// FetchPackage queries PyPI for a package's metadata.
func (c *realPyPIRegistryClient) FetchPackage(ctx context.Context, name string) (*pypiPackageInfo, error) {
	base := c.baseURL
	if base == "" {
		base = pypiBaseURL
	}
	url := fmt.Sprintf("%s/%s/json", base, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := registryHTTPClient(c.httpClient).Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pypi returned %d for %s", resp.StatusCode, name)
	}

	var info pypiPackageInfo
	if err := decodeJSONLimited(resp.Body, &info); err != nil {
		return nil, fmt.Errorf("decoding pypi response for %s: %w", name, err)
	}

	return &info, nil
}

// checkPyPIDeps queries PyPI for each dependency and emits signals for
// packages that are inactive or deprecated based on classifiers.
func (r *registryRun) checkPyPIDeps(ctx context.Context, client pypiRegistryClient, deps []PackageQuery, filePath string) []signal.RawSignal {
	return lookupEach(ctx, r, "python", deps, func(ctx context.Context, dep PackageQuery) []signal.RawSignal {
		info, err := client.FetchPackage(ctx, dep.Name)
		if err != nil {
			r.lookupFailed("python", dep.Name, err)
			return nil
		}

		// Check for inactive/deprecated classifiers.
		reason := pypiDeprecationReason(info)
		if reason == "" {
			return nil
		}
		return []signal.RawSignal{{
			Source:      "dephealth",
			Kind:        "deprecated-dependency",
			FilePath:    filePath,
			Title:       fmt.Sprintf("Deprecated PyPI package: %s", dep.Name),
			Description: fmt.Sprintf("PyPI package %s is marked as %s. Consider migrating to an alternative.", dep.Name, reason),
			Confidence:  0.7,
			Tags:        []string{"deprecated-dependency", "dephealth", "python"},
		}}
	})
}

// pypiDeprecationReason checks classifiers for development status indicating
// the package is inactive or deprecated. Returns the reason string, or "".
func pypiDeprecationReason(info *pypiPackageInfo) string {
	for _, c := range info.Info.Classifiers {
		lower := strings.ToLower(c)
		if strings.Contains(lower, "development status :: 7 - inactive") {
			return "inactive (Development Status :: 7 - Inactive)"
		}
		if strings.Contains(lower, "development status :: 1 - planning") {
			// Not deprecated, but could be a signal. Skip for now.
			continue
		}
	}
	return ""
}
