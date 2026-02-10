package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/mod/module"

	"github.com/davetashner/stringer/internal/signal"
)

// maxProxyChecks caps the number of module proxy lookups.
const maxProxyChecks = 50

// proxyBaseURL is the default Go module proxy URL.
const proxyBaseURL = "https://proxy.golang.org"

// moduleProxyClient fetches module metadata from the Go module proxy.
type moduleProxyClient interface {
	FetchLatest(ctx context.Context, modulePath string) (*moduleInfo, error)
}

// moduleInfo represents the JSON response from the Go module proxy /@latest endpoint.
type moduleInfo struct {
	Version    string    `json:"Version"`
	Time       time.Time `json:"Time"`
	Deprecated string    `json:"Deprecated"`
}

// realModuleProxyClient queries the real Go module proxy.
type realModuleProxyClient struct {
	httpClient *http.Client
	baseURL    string
}

// FetchLatest queries proxy.golang.org/{module}/@latest and returns the parsed response.
func (c *realModuleProxyClient) FetchLatest(ctx context.Context, modulePath string) (*moduleInfo, error) {
	escaped, err := module.EscapePath(modulePath)
	if err != nil {
		return nil, fmt.Errorf("escaping module path %q: %w", modulePath, err)
	}

	base := c.baseURL
	if base == "" {
		base = proxyBaseURL
	}
	url := fmt.Sprintf("%s/%s/@latest", base, escaped)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	client := c.httpClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy returned %d for %s", resp.StatusCode, modulePath)
	}

	var info moduleInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding proxy response for %s: %w", modulePath, err)
	}

	return &info, nil
}

// checkDeprecatedDeps queries the Go module proxy for each dependency and
// emits signals for modules that declare a Deprecated field.
func checkDeprecatedDeps(ctx context.Context, client moduleProxyClient, deps []ModuleDep) []signal.RawSignal {
	var signals []signal.RawSignal
	checked := 0

	for _, dep := range deps {
		if checked >= maxProxyChecks {
			slog.Info("dephealth: reached module proxy check cap", "cap", maxProxyChecks)
			break
		}
		checked++

		info, err := client.FetchLatest(ctx, dep.Path)
		if err != nil {
			slog.Debug("dephealth: proxy lookup failed", "module", dep.Path, "error", err)
			continue
		}

		if info.Deprecated != "" {
			signals = append(signals, signal.RawSignal{
				Source:      "dephealth",
				Kind:        "deprecated-dependency",
				FilePath:    "go.mod",
				Title:       fmt.Sprintf("Deprecated dependency: %s", dep.Path),
				Description: fmt.Sprintf("Module %s is deprecated: %s", dep.Path, info.Deprecated),
				Confidence:  0.8,
				Tags:        []string{"deprecated-dependency", "dephealth"},
			})
		}
	}

	return signals
}
