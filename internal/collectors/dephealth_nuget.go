package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/davetashner/stringer/internal/signal"
)

// maxNuGetChecks caps the number of NuGet API lookups per scan.
const maxNuGetChecks = 50

// nugetRegistrationBaseURL is the default NuGet registration API URL.
const nugetRegistrationBaseURL = "https://api.nuget.org/v3/registration5-semver1"

// nugetRegistryClient fetches package metadata from NuGet.
type nugetRegistryClient interface {
	FetchRegistration(ctx context.Context, id string) (*nugetRegistrationInfo, error)
}

// nugetRegistrationInfo represents the subset of NuGet registration response we need.
type nugetRegistrationInfo struct {
	Items []nugetRegistrationPage `json:"items"`
}

// nugetRegistrationPage represents a page of NuGet catalog items.
type nugetRegistrationPage struct {
	Items []nugetRegistrationLeaf `json:"items"`
}

// nugetRegistrationLeaf represents a single NuGet version entry.
type nugetRegistrationLeaf struct {
	CatalogEntry nugetCatalogEntry `json:"catalogEntry"`
}

// nugetCatalogEntry represents the catalog entry for a NuGet package version.
type nugetCatalogEntry struct {
	ID          string            `json:"id"`
	Version     string            `json:"version"`
	Deprecation *nugetDeprecation `json:"deprecation,omitempty"`
	Listed      bool              `json:"listed"`
}

// nugetDeprecation represents deprecation information for a NuGet package.
type nugetDeprecation struct {
	Reasons []string `json:"reasons"`
	Message string   `json:"message"`
}

// realNuGetRegistryClient queries the real NuGet registration API.
type realNuGetRegistryClient struct {
	httpClient *http.Client
	baseURL    string
}

// FetchRegistration queries the NuGet registration API for a package's metadata.
func (c *realNuGetRegistryClient) FetchRegistration(ctx context.Context, id string) (*nugetRegistrationInfo, error) {
	base := c.baseURL
	if base == "" {
		base = nugetRegistrationBaseURL
	}
	// NuGet IDs are case-insensitive; the API expects lowercase.
	url := fmt.Sprintf("%s/%s/index.json", base, strings.ToLower(id))

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
		return nil, fmt.Errorf("nuget returned %d for %s", resp.StatusCode, id)
	}

	var info nugetRegistrationInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding nuget response for %s: %w", id, err)
	}

	return &info, nil
}

// checkNuGetDeps queries the NuGet registration API for each dependency and
// emits signals for deprecated packages.
func checkNuGetDeps(ctx context.Context, client nugetRegistryClient, deps []PackageQuery, filePath string) []signal.RawSignal {
	var signals []signal.RawSignal
	checked := 0

	for _, dep := range deps {
		if checked >= maxNuGetChecks {
			slog.Info("dephealth: reached NuGet check cap", "cap", maxNuGetChecks)
			break
		}
		checked++

		info, err := client.FetchRegistration(ctx, dep.Name)
		if err != nil {
			slog.Debug("dephealth: nuget lookup failed", "package", dep.Name, "error", err)
			continue
		}

		// Check if the latest version of the package is deprecated.
		if isNuGetDeprecated(info, dep.Version) {
			signals = append(signals, signal.RawSignal{
				Source:      "dephealth",
				Kind:        "deprecated-dependency",
				FilePath:    filePath,
				Title:       fmt.Sprintf("Deprecated NuGet package: %s", dep.Name),
				Description: fmt.Sprintf("NuGet package %s version %s is deprecated. Consider migrating to an alternative.", dep.Name, dep.Version),
				Confidence:  0.8,
				Tags:        []string{"deprecated-dependency", "dephealth", "nuget"},
			})
		}
	}

	return signals
}

// isNuGetDeprecated checks if a specific version of a NuGet package is deprecated.
// It walks the registration pages looking for the matching version's catalog entry.
func isNuGetDeprecated(info *nugetRegistrationInfo, version string) bool {
	for _, page := range info.Items {
		for _, leaf := range page.Items {
			if strings.EqualFold(leaf.CatalogEntry.Version, version) {
				return leaf.CatalogEntry.Deprecation != nil
			}
		}
	}
	return false
}
