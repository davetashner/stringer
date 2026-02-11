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

// maxCratesChecks caps the number of crates.io API lookups per scan.
const maxCratesChecks = 50

// cratesBaseURL is the default crates.io API URL.
const cratesBaseURL = "https://crates.io/api/v1"

// cratesRegistryClient fetches crate metadata from crates.io.
type cratesRegistryClient interface {
	FetchCrate(ctx context.Context, name string) (*crateInfo, error)
}

// crateInfo represents the subset of crates.io API response we need.
type crateInfo struct {
	Crate struct {
		Name       string `json:"name"`
		MaxVersion string `json:"max_version"`
	} `json:"crate"`
	Versions []crateVersion `json:"versions"`
}

// crateVersion represents a single version entry from crates.io.
type crateVersion struct {
	Num    string `json:"num"`
	Yanked bool   `json:"yanked"`
}

// realCratesRegistryClient queries the real crates.io API.
type realCratesRegistryClient struct {
	httpClient *http.Client
	baseURL    string
}

// FetchCrate queries crates.io for a crate's metadata including version info.
func (c *realCratesRegistryClient) FetchCrate(ctx context.Context, name string) (*crateInfo, error) {
	base := c.baseURL
	if base == "" {
		base = cratesBaseURL
	}
	url := fmt.Sprintf("%s/crates/%s", base, name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	// crates.io requires a User-Agent header.
	req.Header.Set("User-Agent", "stringer-dephealth (https://github.com/davetashner/stringer)")

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
		return nil, fmt.Errorf("crates.io returned %d for %s", resp.StatusCode, name)
	}

	var info crateInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding crates.io response for %s: %w", name, err)
	}

	return &info, nil
}

// checkCratesDeps queries crates.io for each dependency and emits signals
// for crates where the used version is yanked.
func checkCratesDeps(ctx context.Context, client cratesRegistryClient, deps []PackageQuery) []signal.RawSignal {
	var signals []signal.RawSignal
	checked := 0

	for _, dep := range deps {
		if checked >= maxCratesChecks {
			slog.Info("dephealth: reached crates.io check cap", "cap", maxCratesChecks)
			break
		}
		checked++

		info, err := client.FetchCrate(ctx, dep.Name)
		if err != nil {
			slog.Debug("dephealth: crates.io lookup failed", "crate", dep.Name, "error", err)
			continue
		}

		// Check if the specific version used is yanked.
		for _, v := range info.Versions {
			if v.Num == dep.Version && v.Yanked {
				signals = append(signals, signal.RawSignal{
					Source:      "dephealth",
					Kind:        "yanked-dependency",
					FilePath:    "Cargo.toml",
					Title:       fmt.Sprintf("Yanked crate: %s@%s", dep.Name, dep.Version),
					Description: fmt.Sprintf("Crate %s version %s has been yanked from crates.io. Yanked versions typically have critical bugs or security issues. Update to a non-yanked version.", dep.Name, dep.Version),
					Confidence:  0.9,
					Tags:        []string{"yanked-dependency", "dephealth", "rust"},
				})
				break
			}
		}
	}

	return signals
}
