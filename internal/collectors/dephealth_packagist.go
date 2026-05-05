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

// maxPackagistChecks caps the number of Packagist API lookups per scan.
const maxPackagistChecks = 50

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
		return nil, fmt.Errorf("packagist returned %d for %s", resp.StatusCode, name)
	}

	var info packagistPackageInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding packagist response for %s: %w", name, err)
	}

	return &info, nil
}

// checkPackagistDeps queries Packagist for each dependency and emits signals
// for packages that are abandoned.
func checkPackagistDeps(ctx context.Context, client packagistRegistryClient, deps []PackageQuery, filePath string) []signal.RawSignal {
	var signals []signal.RawSignal
	checked := 0

	for _, dep := range deps {
		if checked >= maxPackagistChecks {
			slog.Info("dephealth: reached packagist check cap", "cap", maxPackagistChecks)
			break
		}
		checked++

		info, err := client.FetchPackage(ctx, dep.Name)
		if err != nil {
			slog.Debug("dephealth: packagist lookup failed", "package", dep.Name, "error", err)
			continue
		}

		if reason := packagistAbandonedReason(info, dep.Name); reason != "" {
			signals = append(signals, signal.RawSignal{
				Source:      "dephealth",
				Kind:        "deprecated-dependency",
				FilePath:    filePath,
				Title:       fmt.Sprintf("Abandoned Packagist package: %s", dep.Name),
				Description: fmt.Sprintf("Packagist package %s is abandoned. %s", dep.Name, reason),
				Confidence:  0.8,
				Tags:        []string{"deprecated-dependency", "dephealth", "php"},
			})
		}
	}

	return signals
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
