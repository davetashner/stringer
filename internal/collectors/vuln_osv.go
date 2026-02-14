// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"
)

const (
	osvDefaultBaseURL   = "https://api.osv.dev/v1"
	osvBatchLimit       = 1000
	osvMaxRetries       = 3
	osvRetryBaseDelay   = 500 * time.Millisecond
	osvMaxResponseBytes = 10 * 1024 * 1024 // 10 MiB
)

// PackageQuery represents a single dependency to check for vulnerabilities.
type PackageQuery struct {
	Ecosystem string
	Name      string
	Version   string
}

// VulnDetail holds processed vulnerability information from OSV.dev.
type VulnDetail struct {
	ID           string
	Aliases      []string
	Summary      string
	Ecosystem    string
	PackageName  string
	Version      string
	FixedVersion string
	Severity     string // CVSS v3 score string, or ""
}

// osvClient abstracts OSV.dev API access for testability.
type osvClient interface {
	QueryBatch(ctx context.Context, queries []PackageQuery) ([]VulnDetail, error)
}

// Compile-time check that realOSVClient implements osvClient.
var _ osvClient = (*realOSVClient)(nil)

// realOSVClient implements osvClient using the OSV.dev REST API.
type realOSVClient struct {
	httpClient *http.Client
	baseURL    string
}

func newOSVClient(timeout time.Duration) *realOSVClient {
	return &realOSVClient{
		httpClient: &http.Client{Timeout: timeout},
		baseURL:    osvDefaultBaseURL,
	}
}

// QueryBatch queries OSV.dev for vulnerabilities affecting the given packages.
// It splits large query sets into batches of osvBatchLimit, deduplicates vuln IDs,
// fetches full details, and maps results back to the originating packages.
func (c *realOSVClient) QueryBatch(ctx context.Context, queries []PackageQuery) ([]VulnDetail, error) {
	if len(queries) == 0 {
		return nil, nil
	}

	// Map query index → original PackageQuery for result mapping.
	type vulnHit struct {
		vulnID string
		query  PackageQuery
	}
	var hits []vulnHit

	// Process in batches.
	for start := 0; start < len(queries); start += osvBatchLimit {
		end := start + osvBatchLimit
		if end > len(queries) {
			end = len(queries)
		}
		batch := queries[start:end]

		items := make([]osvQueryItem, len(batch))
		for i, q := range batch {
			items[i] = osvQueryItem{
				Package: osvPackage{Name: q.Name, Ecosystem: q.Ecosystem},
				Version: q.Version,
			}
		}

		results, err := c.postBatchQuery(ctx, items)
		if err != nil {
			return nil, fmt.Errorf("osv batch query: %w", err)
		}

		for i, r := range results {
			for _, v := range r.Vulns {
				hits = append(hits, vulnHit{vulnID: v.ID, query: batch[i]})
			}
		}
	}

	if len(hits) == 0 {
		return nil, nil
	}

	// Collect unique vuln IDs and fetch full details.
	seen := make(map[string]bool)
	var uniqueIDs []string
	for _, h := range hits {
		if !seen[h.vulnID] {
			seen[h.vulnID] = true
			uniqueIDs = append(uniqueIDs, h.vulnID)
		}
	}

	vulnCache := make(map[string]*osvVulnerability, len(uniqueIDs))
	for _, id := range uniqueIDs {
		vuln, err := c.fetchVuln(ctx, id)
		if err != nil {
			slog.Warn("osv: failed to fetch vuln details, skipping", "id", id, "error", err)
			continue
		}
		vulnCache[id] = vuln
	}

	// Build results mapping each (vuln, package) pair to a VulnDetail.
	var details []VulnDetail
	dedupKey := make(map[string]bool)
	for _, h := range hits {
		vuln := vulnCache[h.vulnID]
		if vuln == nil {
			continue
		}

		key := h.vulnID + "|" + h.query.Ecosystem + "|" + h.query.Name
		if dedupKey[key] {
			continue
		}
		dedupKey[key] = true

		details = append(details, VulnDetail{
			ID:           vuln.ID,
			Aliases:      vuln.Aliases,
			Summary:      vuln.Summary,
			Ecosystem:    h.query.Ecosystem,
			PackageName:  h.query.Name,
			Version:      h.query.Version,
			FixedVersion: extractOSVFixVersion(vuln, h.query.Ecosystem, h.query.Name),
			Severity:     extractOSVSeverity(vuln),
		})
	}

	return details, nil
}

// postBatchQuery POSTs to /v1/querybatch and returns the results.
func (c *realOSVClient) postBatchQuery(ctx context.Context, items []osvQueryItem) ([]osvBatchResult, error) {
	body, err := json.Marshal(osvBatchRequest{Queries: items})
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	url := c.baseURL + "/querybatch"
	resp, err := c.doWithRetry(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var result osvBatchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, osvMaxResponseBytes)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding batch response: %w", err)
	}

	return result.Results, nil
}

// fetchVuln GETs /v1/vulns/{id} and returns the full vulnerability record.
func (c *realOSVClient) fetchVuln(ctx context.Context, id string) (*osvVulnerability, error) {
	url := c.baseURL + "/vulns/" + id
	resp, err := c.doWithRetry(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var vuln osvVulnerability
	if err := json.NewDecoder(io.LimitReader(resp.Body, osvMaxResponseBytes)).Decode(&vuln); err != nil {
		return nil, fmt.Errorf("decoding vuln %s: %w", id, err)
	}

	return &vuln, nil
}

// doWithRetry executes an HTTP request with exponential backoff retry on
// transient failures (5xx, 429).
func (c *realOSVClient) doWithRetry(ctx context.Context, method, url string, body []byte) (*http.Response, error) {
	var lastErr error

	for attempt := range osvMaxRetries {
		if attempt > 0 {
			delay := time.Duration(math.Pow(2, float64(attempt-1))) * osvRetryBaseDelay
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request to %s: %w", url, err)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}

		// Read and discard body to allow connection reuse.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = fmt.Errorf("osv api %s returned %d", url, resp.StatusCode)
			slog.Debug("osv: retryable error", "url", url, "status", resp.StatusCode, "attempt", attempt+1)
			continue
		}

		return nil, fmt.Errorf("osv api %s returned %d", url, resp.StatusCode)
	}

	return nil, fmt.Errorf("osv: max retries exceeded: %w", lastErr)
}

// extractOSVFixVersion finds the fix version for a specific package from the vuln's
// affected data.
func extractOSVFixVersion(vuln *osvVulnerability, ecosystem, pkgName string) string {
	for _, aff := range vuln.Affected {
		if !strings.EqualFold(aff.Package.Ecosystem, ecosystem) || aff.Package.Name != pkgName {
			continue
		}
		for _, r := range aff.Ranges {
			for _, e := range r.Events {
				if e.Fixed != "" {
					return e.Fixed
				}
			}
		}
	}
	return ""
}

// extractOSVSeverity returns the CVSS v3 score string if available.
func extractOSVSeverity(vuln *osvVulnerability) string {
	for _, s := range vuln.Severity {
		if s.Type == "CVSS_V3" {
			return s.Score
		}
	}
	if len(vuln.Severity) > 0 {
		return vuln.Severity[0].Score
	}
	return ""
}

// --- OSV API request/response types ---

type osvBatchRequest struct {
	Queries []osvQueryItem `json:"queries"`
}

type osvQueryItem struct {
	Package osvPackage `json:"package"`
	Version string     `json:"version"`
}

type osvPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type osvBatchResponse struct {
	Results []osvBatchResult `json:"results"`
}

type osvBatchResult struct {
	Vulns []osvBatchVuln `json:"vulns"`
}

type osvBatchVuln struct {
	ID       string `json:"id"`
	Modified string `json:"modified"`
}

type osvVulnerability struct {
	ID       string        `json:"id"`
	Aliases  []string      `json:"aliases"`
	Summary  string        `json:"summary"`
	Severity []osvSeverity `json:"severity"`
	Affected []osvAffected `json:"affected"`
}

type osvSeverity struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

type osvAffected struct {
	Package osvPackage `json:"package"`
	Ranges  []osvRange `json:"ranges"`
}

type osvRange struct {
	Type   string     `json:"type"`
	Events []osvEvent `json:"events"`
}

type osvEvent struct {
	Introduced string `json:"introduced,omitempty"`
	Fixed      string `json:"fixed,omitempty"`
}
