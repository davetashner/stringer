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

// maxHexChecks caps the number of Hex.pm API lookups per scan.
const maxHexChecks = 50

// hexBaseURL is the default Hex.pm API URL.
const hexBaseURL = "https://hex.pm/api"

// hexRegistryClient fetches package metadata from Hex.pm.
type hexRegistryClient interface {
	FetchPackage(ctx context.Context, name string) (*hexPackageInfo, error)
}

// hexPackageInfo represents the subset of Hex.pm API response we need.
type hexPackageInfo struct {
	Name     string       `json:"name"`
	Releases []hexRelease `json:"releases"`
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
		return nil, fmt.Errorf("hex.pm returned %d for %s", resp.StatusCode, name)
	}

	var info hexPackageInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding hex.pm response for %s: %w", name, err)
	}

	return &info, nil
}

// checkHexDeps queries Hex.pm for each dependency and emits signals for
// packages where the used version is retired.
func checkHexDeps(ctx context.Context, client hexRegistryClient, deps []PackageQuery, filePath string) []signal.RawSignal {
	var signals []signal.RawSignal
	checked := 0

	for _, dep := range deps {
		if checked >= maxHexChecks {
			slog.Info("dephealth: reached hex.pm check cap", "cap", maxHexChecks)
			break
		}
		checked++

		info, err := client.FetchPackage(ctx, dep.Name)
		if err != nil {
			slog.Debug("dephealth: hex.pm lookup failed", "package", dep.Name, "error", err)
			continue
		}

		// Check if the specific version is retired.
		if retirement, ok := info.Retirements[dep.Version]; ok {
			desc := fmt.Sprintf("Hex package %s version %s is retired", dep.Name, dep.Version)
			if retirement.Reason != "" {
				desc += fmt.Sprintf(" (reason: %s)", retirement.Reason)
			}
			if retirement.Message != "" {
				desc += fmt.Sprintf(": %s", retirement.Message)
			}
			desc += ". Update to a non-retired version."

			signals = append(signals, signal.RawSignal{
				Source:      "dephealth",
				Kind:        "deprecated-dependency",
				FilePath:    filePath,
				Title:       fmt.Sprintf("Retired Hex package: %s@%s", dep.Name, dep.Version),
				Description: desc,
				Confidence:  0.8,
				Tags:        []string{"deprecated-dependency", "dephealth", "elixir"},
			})
		}
	}

	return signals
}
