package collectors

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestOSVServer returns an httptest.Server that serves canned OSV API responses.
// batchResp maps to /v1/querybatch, vulns maps vuln ID → full vuln for /v1/vulns/{id}.
func newTestOSVServer(t *testing.T, batchResp *osvBatchResponse, vulns map[string]*osvVulnerability) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/querybatch" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			if batchResp == nil {
				_ = json.NewEncoder(w).Encode(osvBatchResponse{})
				return
			}
			_ = json.NewEncoder(w).Encode(batchResp)

		case len(r.URL.Path) > len("/vulns/") && r.URL.Path[:7] == "/vulns/":
			id := r.URL.Path[7:]
			vuln, ok := vulns[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(vuln)

		default:
			http.NotFound(w, r)
		}
	}))
}

func newTestOSVClient(baseURL string) *realOSVClient {
	return &realOSVClient{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    baseURL,
	}
}

func TestOSVClient_QueryBatch_SingleVuln(t *testing.T) {
	batchResp := &osvBatchResponse{
		Results: []osvBatchResult{
			{Vulns: []osvBatchVuln{{ID: "GHSA-1234-5678-abcd"}}},
		},
	}
	vulns := map[string]*osvVulnerability{
		"GHSA-1234-5678-abcd": {
			ID:      "GHSA-1234-5678-abcd",
			Aliases: []string{"CVE-2024-12345"},
			Summary: "SQL injection in example-lib",
			Severity: []osvSeverity{
				{Type: "CVSS_V3", Score: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"},
			},
			Affected: []osvAffected{{
				Package: osvPackage{Name: "com.example:example-lib", Ecosystem: "Maven"},
				Ranges: []osvRange{{
					Type:   "ECOSYSTEM",
					Events: []osvEvent{{Introduced: "0"}, {Fixed: "2.0.1"}},
				}},
			}},
		},
	}

	srv := newTestOSVServer(t, batchResp, vulns)
	defer srv.Close()

	client := newTestOSVClient(srv.URL)
	results, err := client.QueryBatch(context.Background(), []PackageQuery{
		{Ecosystem: "Maven", Name: "com.example:example-lib", Version: "1.5.0"},
	})

	require.NoError(t, err)
	require.Len(t, results, 1)

	d := results[0]
	assert.Equal(t, "GHSA-1234-5678-abcd", d.ID)
	assert.Equal(t, []string{"CVE-2024-12345"}, d.Aliases)
	assert.Equal(t, "SQL injection in example-lib", d.Summary)
	assert.Equal(t, "Maven", d.Ecosystem)
	assert.Equal(t, "com.example:example-lib", d.PackageName)
	assert.Equal(t, "1.5.0", d.Version)
	assert.Equal(t, "2.0.1", d.FixedVersion)
	assert.Equal(t, "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", d.Severity)
}

func TestOSVClient_QueryBatch_MultipleVulns(t *testing.T) {
	batchResp := &osvBatchResponse{
		Results: []osvBatchResult{
			{Vulns: []osvBatchVuln{{ID: "GO-2024-0001"}, {ID: "GO-2024-0002"}}},
			{Vulns: []osvBatchVuln{{ID: "GO-2024-0003"}}},
		},
	}
	vulns := map[string]*osvVulnerability{
		"GO-2024-0001": {
			ID: "GO-2024-0001", Summary: "Vuln A",
			Aliases:  []string{"CVE-2024-0001"},
			Affected: []osvAffected{{Package: osvPackage{Name: "github.com/a/b", Ecosystem: "Go"}, Ranges: []osvRange{{Events: []osvEvent{{Fixed: "v1.1.0"}}}}}},
		},
		"GO-2024-0002": {
			ID: "GO-2024-0002", Summary: "Vuln B",
			Affected: []osvAffected{{Package: osvPackage{Name: "github.com/a/b", Ecosystem: "Go"}, Ranges: []osvRange{{Events: []osvEvent{{Fixed: "v1.2.0"}}}}}},
		},
		"GO-2024-0003": {
			ID: "GO-2024-0003", Summary: "Vuln C",
			Aliases:  []string{"CVE-2024-0003"},
			Affected: []osvAffected{{Package: osvPackage{Name: "github.com/c/d", Ecosystem: "Go"}, Ranges: []osvRange{{Events: []osvEvent{{Fixed: "v2.1.0"}}}}}},
		},
	}

	srv := newTestOSVServer(t, batchResp, vulns)
	defer srv.Close()

	client := newTestOSVClient(srv.URL)
	results, err := client.QueryBatch(context.Background(), []PackageQuery{
		{Ecosystem: "Go", Name: "github.com/a/b", Version: "v1.0.0"},
		{Ecosystem: "Go", Name: "github.com/c/d", Version: "v2.0.0"},
	})

	require.NoError(t, err)
	require.Len(t, results, 3)

	byID := make(map[string]VulnDetail)
	for _, r := range results {
		byID[r.ID] = r
	}

	assert.Equal(t, "v1.1.0", byID["GO-2024-0001"].FixedVersion)
	assert.Equal(t, "v1.2.0", byID["GO-2024-0002"].FixedVersion)
	assert.Equal(t, "v2.1.0", byID["GO-2024-0003"].FixedVersion)
	assert.Equal(t, "github.com/a/b", byID["GO-2024-0001"].PackageName)
	assert.Equal(t, "github.com/c/d", byID["GO-2024-0003"].PackageName)
}

func TestOSVClient_QueryBatch_NoVulns(t *testing.T) {
	batchResp := &osvBatchResponse{
		Results: []osvBatchResult{
			{Vulns: nil},
			{Vulns: []osvBatchVuln{}},
		},
	}

	srv := newTestOSVServer(t, batchResp, nil)
	defer srv.Close()

	client := newTestOSVClient(srv.URL)
	results, err := client.QueryBatch(context.Background(), []PackageQuery{
		{Ecosystem: "Go", Name: "github.com/safe/pkg", Version: "v1.0.0"},
		{Ecosystem: "Go", Name: "github.com/also/safe", Version: "v2.0.0"},
	})

	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestOSVClient_QueryBatch_EmptyInput(t *testing.T) {
	client := newTestOSVClient("http://unused")
	results, err := client.QueryBatch(context.Background(), nil)
	require.NoError(t, err)
	assert.Nil(t, results)

	results, err = client.QueryBatch(context.Background(), []PackageQuery{})
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestOSVClient_QueryBatch_VulnFetchFailure(t *testing.T) {
	batchResp := &osvBatchResponse{
		Results: []osvBatchResult{
			{Vulns: []osvBatchVuln{{ID: "MISSING-VULN"}, {ID: "PRESENT-VULN"}}},
		},
	}
	// Only PRESENT-VULN has details; MISSING-VULN will 404.
	vulns := map[string]*osvVulnerability{
		"PRESENT-VULN": {
			ID: "PRESENT-VULN", Summary: "Present",
			Affected: []osvAffected{{Package: osvPackage{Name: "pkg", Ecosystem: "Go"}}},
		},
	}

	srv := newTestOSVServer(t, batchResp, vulns)
	defer srv.Close()

	client := newTestOSVClient(srv.URL)
	results, err := client.QueryBatch(context.Background(), []PackageQuery{
		{Ecosystem: "Go", Name: "pkg", Version: "v1.0.0"},
	})

	// Should succeed with partial results — MISSING-VULN skipped with warning.
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "PRESENT-VULN", results[0].ID)
}

func TestOSVClient_QueryBatch_RetryOn5xx(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if r.URL.Path == "/querybatch" {
			if n == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(osvBatchResponse{
				Results: []osvBatchResult{{Vulns: []osvBatchVuln{{ID: "V1"}}}},
			})
			return
		}
		if r.URL.Path == "/vulns/V1" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(osvVulnerability{
				ID: "V1", Summary: "Retry worked",
				Affected: []osvAffected{{Package: osvPackage{Name: "pkg", Ecosystem: "Go"}}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := newTestOSVClient(srv.URL)
	results, err := client.QueryBatch(context.Background(), []PackageQuery{
		{Ecosystem: "Go", Name: "pkg", Version: "v1.0.0"},
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "V1", results[0].ID)
	assert.GreaterOrEqual(t, int(attempts.Load()), 2, "should have retried")
}

func TestOSVClient_QueryBatch_NetworkError(t *testing.T) {
	// Use a server that immediately closes.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	defer srv.Close()

	client := &realOSVClient{
		httpClient: &http.Client{Timeout: 1 * time.Second},
		baseURL:    srv.URL,
	}

	_, err := client.QueryBatch(context.Background(), []PackageQuery{
		{Ecosystem: "Go", Name: "pkg", Version: "v1.0.0"},
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max retries exceeded")
}

func TestOSVClient_QueryBatch_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	client := newTestOSVClient("http://127.0.0.1:1") // won't connect
	_, err := client.QueryBatch(ctx, []PackageQuery{
		{Ecosystem: "Go", Name: "pkg", Version: "v1.0.0"},
	})

	assert.Error(t, err)
}

func TestOSVClient_QueryBatch_DedupSameVulnAcrossPackages(t *testing.T) {
	// Same vuln affects two queried packages.
	batchResp := &osvBatchResponse{
		Results: []osvBatchResult{
			{Vulns: []osvBatchVuln{{ID: "SHARED-VULN"}}},
			{Vulns: []osvBatchVuln{{ID: "SHARED-VULN"}}},
		},
	}
	vulns := map[string]*osvVulnerability{
		"SHARED-VULN": {
			ID: "SHARED-VULN", Summary: "Affects both",
			Aliases: []string{"CVE-2024-9999"},
			Affected: []osvAffected{
				{Package: osvPackage{Name: "pkg-a", Ecosystem: "Go"}, Ranges: []osvRange{{Events: []osvEvent{{Fixed: "v1.1.0"}}}}},
				{Package: osvPackage{Name: "pkg-b", Ecosystem: "Go"}, Ranges: []osvRange{{Events: []osvEvent{{Fixed: "v2.1.0"}}}}},
			},
		},
	}

	srv := newTestOSVServer(t, batchResp, vulns)
	defer srv.Close()

	client := newTestOSVClient(srv.URL)
	results, err := client.QueryBatch(context.Background(), []PackageQuery{
		{Ecosystem: "Go", Name: "pkg-a", Version: "v1.0.0"},
		{Ecosystem: "Go", Name: "pkg-b", Version: "v2.0.0"},
	})

	require.NoError(t, err)
	// Two results: one per package, even though it's the same vuln.
	require.Len(t, results, 2)

	byPkg := make(map[string]VulnDetail)
	for _, r := range results {
		byPkg[r.PackageName] = r
	}
	assert.Equal(t, "v1.1.0", byPkg["pkg-a"].FixedVersion)
	assert.Equal(t, "v2.1.0", byPkg["pkg-b"].FixedVersion)
}

func TestOSVClient_QueryBatch_NoFixVersion(t *testing.T) {
	batchResp := &osvBatchResponse{
		Results: []osvBatchResult{
			{Vulns: []osvBatchVuln{{ID: "NO-FIX"}}},
		},
	}
	vulns := map[string]*osvVulnerability{
		"NO-FIX": {
			ID: "NO-FIX", Summary: "No fix yet",
			Affected: []osvAffected{{
				Package: osvPackage{Name: "pkg", Ecosystem: "Go"},
				Ranges:  []osvRange{{Events: []osvEvent{{Introduced: "0"}}}},
			}},
		},
	}

	srv := newTestOSVServer(t, batchResp, vulns)
	defer srv.Close()

	client := newTestOSVClient(srv.URL)
	results, err := client.QueryBatch(context.Background(), []PackageQuery{
		{Ecosystem: "Go", Name: "pkg", Version: "v1.0.0"},
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "", results[0].FixedVersion)
}

func TestOSVClient_QueryBatch_NoSeverity(t *testing.T) {
	batchResp := &osvBatchResponse{
		Results: []osvBatchResult{
			{Vulns: []osvBatchVuln{{ID: "NO-SEV"}}},
		},
	}
	vulns := map[string]*osvVulnerability{
		"NO-SEV": {
			ID: "NO-SEV", Summary: "No severity info",
			Affected: []osvAffected{{Package: osvPackage{Name: "pkg", Ecosystem: "Go"}}},
		},
	}

	srv := newTestOSVServer(t, batchResp, vulns)
	defer srv.Close()

	client := newTestOSVClient(srv.URL)
	results, err := client.QueryBatch(context.Background(), []PackageQuery{
		{Ecosystem: "Go", Name: "pkg", Version: "v1.0.0"},
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "", results[0].Severity)
}

func TestOSVClient_QueryBatch_NonCVSS3Severity(t *testing.T) {
	batchResp := &osvBatchResponse{
		Results: []osvBatchResult{
			{Vulns: []osvBatchVuln{{ID: "V1"}}},
		},
	}
	vulns := map[string]*osvVulnerability{
		"V1": {
			ID: "V1", Summary: "Old severity",
			Severity: []osvSeverity{{Type: "CVSS_V2", Score: "AV:N/AC:L/Au:N/C:C/I:C/A:C"}},
			Affected: []osvAffected{{Package: osvPackage{Name: "pkg", Ecosystem: "Go"}}},
		},
	}

	srv := newTestOSVServer(t, batchResp, vulns)
	defer srv.Close()

	client := newTestOSVClient(srv.URL)
	results, err := client.QueryBatch(context.Background(), []PackageQuery{
		{Ecosystem: "Go", Name: "pkg", Version: "v1.0.0"},
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	// Falls back to first severity entry.
	assert.Equal(t, "AV:N/AC:L/Au:N/C:C/I:C/A:C", results[0].Severity)
}

func TestExtractOSVFixVersion(t *testing.T) {
	vuln := &osvVulnerability{
		Affected: []osvAffected{
			{
				Package: osvPackage{Name: "com.example:lib", Ecosystem: "Maven"},
				Ranges: []osvRange{{
					Type:   "ECOSYSTEM",
					Events: []osvEvent{{Introduced: "1.0.0"}, {Fixed: "1.2.3"}},
				}},
			},
			{
				Package: osvPackage{Name: "other-pkg", Ecosystem: "PyPI"},
				Ranges: []osvRange{{
					Type:   "ECOSYSTEM",
					Events: []osvEvent{{Fixed: "3.0.0"}},
				}},
			},
		},
	}

	assert.Equal(t, "1.2.3", extractOSVFixVersion(vuln, "Maven", "com.example:lib"))
	assert.Equal(t, "3.0.0", extractOSVFixVersion(vuln, "PyPI", "other-pkg"))
	assert.Equal(t, "", extractOSVFixVersion(vuln, "Go", "not-affected"))
}

func TestExtractOSVFixVersion_CaseInsensitiveEcosystem(t *testing.T) {
	vuln := &osvVulnerability{
		Affected: []osvAffected{{
			Package: osvPackage{Name: "pkg", Ecosystem: "Go"},
			Ranges:  []osvRange{{Events: []osvEvent{{Fixed: "v1.0.1"}}}},
		}},
	}

	// Ecosystem matching should be case-insensitive.
	assert.Equal(t, "v1.0.1", extractOSVFixVersion(vuln, "go", "pkg"))
	assert.Equal(t, "v1.0.1", extractOSVFixVersion(vuln, "GO", "pkg"))
}

func TestExtractOSVSeverity(t *testing.T) {
	tests := []struct {
		name     string
		severity []osvSeverity
		want     string
	}{
		{"CVSS v3", []osvSeverity{{Type: "CVSS_V3", Score: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}}, "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"},
		{"CVSS v3 preferred over v2", []osvSeverity{{Type: "CVSS_V2", Score: "v2-score"}, {Type: "CVSS_V3", Score: "v3-score"}}, "v3-score"},
		{"fallback to first", []osvSeverity{{Type: "CVSS_V2", Score: "v2-score"}}, "v2-score"},
		{"empty", nil, ""},
		{"empty slice", []osvSeverity{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vuln := &osvVulnerability{Severity: tt.severity}
			assert.Equal(t, tt.want, extractOSVSeverity(vuln))
		})
	}
}

func TestOSVClient_QueryBatch_4xxNonRetryable(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	client := newTestOSVClient(srv.URL)
	_, err := client.QueryBatch(context.Background(), []PackageQuery{
		{Ecosystem: "Go", Name: "pkg", Version: "v1.0.0"},
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "400")
	// 4xx should not trigger retries.
	assert.Equal(t, int32(1), attempts.Load())
}

func TestNewOSVClient(t *testing.T) {
	client := newOSVClient(10 * time.Second)
	assert.Equal(t, osvDefaultBaseURL, client.baseURL)
	assert.Equal(t, 10*time.Second, client.httpClient.Timeout)
}
