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

// maxMavenChecks caps the number of Maven Central API lookups per scan.
const maxMavenChecks = 50

// mavenSearchBaseURL is the default Maven Central search URL.
const mavenSearchBaseURL = "https://search.maven.org/solrsearch"

// mavenRegistryClient fetches package metadata from Maven Central.
type mavenRegistryClient interface {
	FetchArtifact(ctx context.Context, groupID, artifactID string) (*mavenArtifactInfo, error)
}

// mavenArtifactInfo represents the subset of Maven Central search response we need.
type mavenArtifactInfo struct {
	Response struct {
		NumFound int             `json:"numFound"`
		Docs     []mavenArtifact `json:"docs"`
	} `json:"response"`
}

// mavenArtifact represents a single artifact from Maven Central search results.
type mavenArtifact struct {
	GroupID    string `json:"g"`
	ArtifactID string `json:"a"`
	Version    string `json:"latestVersion"`
	Timestamp  int64  `json:"timestamp"` // millis since epoch
}

// realMavenRegistryClient queries the real Maven Central search API.
type realMavenRegistryClient struct {
	httpClient *http.Client
	baseURL    string
}

// FetchArtifact queries Maven Central for an artifact's metadata.
func (c *realMavenRegistryClient) FetchArtifact(ctx context.Context, groupID, artifactID string) (*mavenArtifactInfo, error) {
	base := c.baseURL
	if base == "" {
		base = mavenSearchBaseURL
	}
	url := fmt.Sprintf("%s/select?q=g:%%22%s%%22+AND+a:%%22%s%%22&rows=1&wt=json", base, groupID, artifactID)

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
		return nil, fmt.Errorf("maven central returned %d for %s:%s", resp.StatusCode, groupID, artifactID)
	}

	var info mavenArtifactInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding maven response for %s:%s: %w", groupID, artifactID, err)
	}

	return &info, nil
}

// mavenStalenessThreshold is 4 years — artifacts with no new release beyond
// this are flagged as potentially unmaintained.
const mavenStalenessThreshold = 4 * 365 * 24 * time.Hour

// checkMavenDeps queries Maven Central for each dependency and emits signals
// for artifacts that have not been updated in a long time (potentially abandoned).
func checkMavenDeps(ctx context.Context, client mavenRegistryClient, deps []PackageQuery, filePath string) []signal.RawSignal {
	var signals []signal.RawSignal
	checked := 0

	for _, dep := range deps {
		if checked >= maxMavenChecks {
			slog.Info("dephealth: reached Maven Central check cap", "cap", maxMavenChecks)
			break
		}
		checked++

		// Split groupId:artifactId.
		parts := strings.SplitN(dep.Name, ":", 2)
		if len(parts) != 2 {
			continue
		}
		groupID, artifactID := parts[0], parts[1]

		info, err := client.FetchArtifact(ctx, groupID, artifactID)
		if err != nil {
			slog.Debug("dephealth: maven lookup failed", "artifact", dep.Name, "error", err)
			continue
		}

		if info.Response.NumFound == 0 {
			continue
		}

		doc := info.Response.Docs[0]
		if doc.Timestamp > 0 {
			lastUpdated := time.UnixMilli(doc.Timestamp)
			if time.Since(lastUpdated) > mavenStalenessThreshold {
				signals = append(signals, signal.RawSignal{
					Source:      "dephealth",
					Kind:        "stale-dependency",
					FilePath:    filePath,
					Title:       fmt.Sprintf("Stale Maven artifact: %s", dep.Name),
					Description: fmt.Sprintf("Maven artifact %s was last updated on %s (>%d years ago). The project may be unmaintained.", dep.Name, lastUpdated.Format("2006-01-02"), int(mavenStalenessThreshold.Hours()/24/365)),
					Confidence:  0.5,
					Tags:        []string{"stale-dependency", "dephealth", "maven"},
				})
			}
		}
	}

	return signals
}
