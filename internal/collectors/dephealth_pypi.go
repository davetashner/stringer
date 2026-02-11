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

// maxPyPIChecks caps the number of PyPI API lookups per scan.
const maxPyPIChecks = 50

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
		return nil, fmt.Errorf("pypi returned %d for %s", resp.StatusCode, name)
	}

	var info pypiPackageInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding pypi response for %s: %w", name, err)
	}

	return &info, nil
}

// checkPyPIDeps queries PyPI for each dependency and emits signals for
// packages that are inactive or deprecated based on classifiers.
func checkPyPIDeps(ctx context.Context, client pypiRegistryClient, deps []PackageQuery, filePath string) []signal.RawSignal {
	var signals []signal.RawSignal
	checked := 0

	for _, dep := range deps {
		if checked >= maxPyPIChecks {
			slog.Info("dephealth: reached PyPI check cap", "cap", maxPyPIChecks)
			break
		}
		checked++

		info, err := client.FetchPackage(ctx, dep.Name)
		if err != nil {
			slog.Debug("dephealth: pypi lookup failed", "package", dep.Name, "error", err)
			continue
		}

		// Check for inactive/deprecated classifiers.
		if reason := pypiDeprecationReason(info); reason != "" {
			signals = append(signals, signal.RawSignal{
				Source:      "dephealth",
				Kind:        "deprecated-dependency",
				FilePath:    filePath,
				Title:       fmt.Sprintf("Deprecated PyPI package: %s", dep.Name),
				Description: fmt.Sprintf("PyPI package %s is marked as %s. Consider migrating to an alternative.", dep.Name, reason),
				Confidence:  0.7,
				Tags:        []string{"deprecated-dependency", "dephealth", "python"},
			})
		}
	}

	return signals
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
