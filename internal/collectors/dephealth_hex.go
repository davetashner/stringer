// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"fmt"
	"net/http"

	"github.com/davetashner/stringer/internal/signal"
)

// hexBaseURL is the default Hex.pm API URL.
const hexBaseURL = "https://hex.pm/api"

// hexRegistryClient fetches package metadata from Hex.pm.
type hexRegistryClient interface {
	FetchPackage(ctx context.Context, name string) (*hexPackageInfo, error)
}

// hexPackageInfo represents the subset of Hex.pm API response we need.
type hexPackageInfo struct {
	Name        string                   `json:"name"`
	Releases    []hexRelease             `json:"releases"`
	Retirements map[string]hexRetirement `json:"retirements"`
}

// hexRelease represents a single release from Hex.pm.
type hexRelease struct {
	Version string `json:"version"`
}

// hexRetirement represents a retirement entry for a version.
type hexRetirement struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// realHexRegistryClient queries the real Hex.pm API.
type realHexRegistryClient struct {
	httpClient *http.Client
	baseURL    string
}

// FetchPackage queries Hex.pm for a package's metadata including retirements.
func (c *realHexRegistryClient) FetchPackage(ctx context.Context, name string) (*hexPackageInfo, error) {
	base := c.baseURL
	if base == "" {
		base = hexBaseURL
	}
	url := fmt.Sprintf("%s/packages/%s", base, name)

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
		return nil, fmt.Errorf("hex.pm returned %d for %s", resp.StatusCode, name)
	}

	var info hexPackageInfo
	if err := decodeJSONLimited(resp.Body, &info); err != nil {
		return nil, fmt.Errorf("decoding hex.pm response for %s: %w", name, err)
	}

	return &info, nil
}

// checkHexDeps queries Hex.pm for each dependency and emits signals for
// packages where the used version is retired.
func (r *registryRun) checkHexDeps(ctx context.Context, client hexRegistryClient, deps []PackageQuery, filePath string) []signal.RawSignal {
	return lookupEach(ctx, r, "hex", deps, func(ctx context.Context, dep PackageQuery) []signal.RawSignal {
		info, err := client.FetchPackage(ctx, dep.Name)
		if err != nil {
			r.lookupFailed("hex", dep.Name, err)
			return nil
		}

		// Check if the specific version is retired.
		retirement, ok := info.Retirements[dep.Version]
		if !ok {
			return nil
		}
		desc := fmt.Sprintf("Hex package %s version %s is retired", dep.Name, dep.Version)
		if retirement.Reason != "" {
			desc += fmt.Sprintf(" (reason: %s)", retirement.Reason)
		}
		if retirement.Message != "" {
			desc += fmt.Sprintf(": %s", retirement.Message)
		}
		desc += ". Update to a non-retired version."

		s := signal.RawSignal{
			Source:      "dephealth",
			Kind:        "deprecated-dependency",
			FilePath:    filePath,
			Title:       fmt.Sprintf("Retired Hex package: %s@%s", dep.Name, dep.Version),
			Description: desc,
			Confidence:  0.8,
			Tags:        []string{"deprecated-dependency", "dephealth", "elixir"},
		}
		// "~> x.y" resolves to the newest compatible release; only the
		// declared minimum is known to be retired.
		if dep.IsRange {
			s.Title = fmt.Sprintf("Retired Hex package floor: %s", declaredSpec(dep.Name, dep.Constraint))
			s.Description = fmt.Sprintf("The declared minimum %s of Hex package %s is retired; the installed version is not known without mix.lock. Raise the floor to a non-retired version.", dep.Version, dep.Name)
			s.Confidence = applyRangeDiscount(s.Confidence)
			s.Tags = append(s.Tags, "version-floor")
		}
		return []signal.RawSignal{s}
	})
}
