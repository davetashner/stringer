package collectors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/davetashner/stringer/internal/testable"
)

// mockVulnScanner implements vulnScanner for testing.
type mockVulnScanner struct {
	results []vulnResult
	err     error
}

func (m *mockVulnScanner) Scan(_ context.Context, _ string) ([]vulnResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}

func TestVulnCollector_Name(t *testing.T) {
	c := &VulnCollector{}
	assert.Equal(t, "vuln", c.Name())
}

func TestVulnCollector_NoGoMod(t *testing.T) {
	dir := t.TempDir()
	c := &VulnCollector{scanner: &mockVulnScanner{}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	assert.NoError(t, err)
	assert.Nil(t, signals)
}

func TestVulnCollector_SingleVuln(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o600))

	c := &VulnCollector{scanner: &mockVulnScanner{
		results: []vulnResult{{
			OSVID:        "GO-2024-2687",
			Aliases:      []string{"CVE-2024-24790"},
			Summary:      "Mishandling of DNS in net/netip",
			Module:       "stdlib",
			Version:      "v1.22.3",
			FixedVersion: "v1.22.4",
		}},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	sig := signals[0]
	assert.Equal(t, "vuln", sig.Source)
	assert.Equal(t, "vulnerable-dependency", sig.Kind)
	assert.Equal(t, "go.mod", sig.FilePath)
	assert.Contains(t, sig.Title, "CVE-2024-24790")
	assert.Contains(t, sig.Title, "stdlib")
	assert.Contains(t, sig.Description, "Upgrade stdlib from v1.22.3 to v1.22.4")
	assert.Equal(t, 0.9, sig.Confidence)
	assert.Contains(t, sig.Tags, "security")
	assert.Contains(t, sig.Tags, "vulnerable-dependency")
	assert.Contains(t, sig.Tags, "CVE-2024-24790")
	assert.Contains(t, sig.Tags, "GO-2024-2687")
}

func TestVulnCollector_NoFixAvailable(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o600))

	c := &VulnCollector{scanner: &mockVulnScanner{
		results: []vulnResult{{
			OSVID:        "GO-2024-9999",
			Aliases:      []string{"CVE-2024-99999"},
			Summary:      "Unpatched vulnerability",
			Module:       "github.com/foo/bar",
			Version:      "v1.0.0",
			FixedVersion: "",
		}},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	assert.Equal(t, 0.85, signals[0].Confidence)
	assert.Contains(t, signals[0].Description, "No fix available")
}

func TestVulnCollector_MultipleVulns(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o600))

	c := &VulnCollector{scanner: &mockVulnScanner{
		results: []vulnResult{
			{
				OSVID:        "GO-2024-0001",
				Aliases:      []string{"CVE-2024-0001"},
				Summary:      "Vuln one",
				Module:       "github.com/a/b",
				Version:      "v1.0.0",
				FixedVersion: "v1.1.0",
			},
			{
				OSVID:        "GO-2024-0002",
				Aliases:      []string{"CVE-2024-0002"},
				Summary:      "Vuln two",
				Module:       "github.com/c/d",
				Version:      "v2.0.0",
				FixedVersion: "v2.1.0",
			},
			{
				OSVID:        "GO-2024-0003",
				Aliases:      []string{"CVE-2024-0003"},
				Summary:      "Vuln three",
				Module:       "github.com/e/f",
				Version:      "v3.0.0",
				FixedVersion: "",
			},
		},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 3)

	// Verify metrics.
	m := c.Metrics()
	require.NotNil(t, m)
	metrics, ok := m.(*VulnMetrics)
	require.True(t, ok)
	assert.Equal(t, 3, metrics.TotalVulns)
	assert.Len(t, metrics.Vulns, 3)
}

func TestVulnCollector_NoCVEAlias(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o600))

	c := &VulnCollector{scanner: &mockVulnScanner{
		results: []vulnResult{{
			OSVID:        "GO-2024-5555",
			Aliases:      []string{"GHSA-xxxx-yyyy-zzzz"}, // no CVE alias
			Summary:      "Some vulnerability",
			Module:       "github.com/foo/bar",
			Version:      "v1.0.0",
			FixedVersion: "v1.1.0",
		}},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	// Title should use the OSV ID as fallback.
	assert.Contains(t, signals[0].Title, "GO-2024-5555")
	assert.NotContains(t, signals[0].Title, "CVE-")
	// Tags should not contain a CVE tag but should have the OSV ID.
	assert.Contains(t, signals[0].Tags, "GO-2024-5555")
	for _, tag := range signals[0].Tags {
		assert.False(t, tag == "GHSA-xxxx-yyyy-zzzz", "GHSA alias should not be in tags")
	}
}

func TestVulnCollector_ScanError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o600))

	c := &VulnCollector{scanner: &mockVulnScanner{
		err: fmt.Errorf("network unavailable"),
	}}

	// C7.3: graceful degradation — error → nil signals, nil error.
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	assert.NoError(t, err)
	assert.Nil(t, signals)
}

func TestVulnCollector_ZeroVulns(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o600))

	c := &VulnCollector{scanner: &mockVulnScanner{
		results: []vulnResult{},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals)

	m := c.Metrics()
	require.NotNil(t, m)
	metrics := m.(*VulnMetrics)
	assert.Equal(t, 0, metrics.TotalVulns)
	assert.Empty(t, metrics.Vulns)
}

func TestVulnCollector_Metrics(t *testing.T) {
	c := &VulnCollector{}

	// Before Collect, Metrics returns nil.
	assert.Nil(t, c.Metrics())

	// After Collect with a valid go.mod, Metrics is populated.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o600))

	c.scanner = &mockVulnScanner{
		results: []vulnResult{{
			OSVID:        "GO-2024-1234",
			Aliases:      []string{"CVE-2024-1234"},
			Summary:      "Test vuln",
			Module:       "github.com/x/y",
			Version:      "v1.0.0",
			FixedVersion: "v1.1.0",
		}},
	}

	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	m := c.Metrics()
	require.NotNil(t, m)
	metrics, ok := m.(*VulnMetrics)
	require.True(t, ok)
	assert.Equal(t, 1, metrics.TotalVulns)
	require.Len(t, metrics.Vulns, 1)
	assert.Equal(t, "GO-2024-1234", metrics.Vulns[0].OSVID)
	assert.Equal(t, "CVE-2024-1234", metrics.Vulns[0].CVE)
	assert.Equal(t, "github.com/x/y", metrics.Vulns[0].Module)
	assert.Equal(t, "v1.0.0", metrics.Vulns[0].Version)
	assert.Equal(t, "v1.1.0", metrics.Vulns[0].FixedVersion)
}

func TestVulnCollector_ReadFileError(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		ReadFileFn: func(name string) ([]byte, error) {
			return nil, os.ErrPermission
		},
	}

	c := &VulnCollector{}
	signals, err := c.Collect(context.Background(), "/fake", signal.CollectorOpts{})
	assert.Error(t, err)
	assert.Nil(t, signals)
	assert.Contains(t, err.Error(), "checking go.mod")
}

func TestParseVulnOutput(t *testing.T) {
	// Simulated govulncheck -json output with two OSV entries and
	// three findings (two for the same OSV → should dedup).
	input := `{"config":{"protocol_version":"v1.0.0"}}
{"progress":{"message":"Scanning modules..."}}
{"osv":{"id":"GO-2024-0001","aliases":["CVE-2024-0001"],"summary":"Vuln A"}}
{"osv":{"id":"GO-2024-0002","aliases":["CVE-2024-0002"],"summary":"Vuln B"}}
{"finding":{"osv":"GO-2024-0001","fixed_version":"v1.1.0","trace":[{"module":"github.com/a/b","version":"v1.0.0"}]}}
{"finding":{"osv":"GO-2024-0001","fixed_version":"v1.1.0","trace":[{"module":"github.com/a/b","version":"v1.0.0"}]}}
{"finding":{"osv":"GO-2024-0002","fixed_version":"","trace":[{"module":"github.com/c/d","version":"v2.0.0"}]}}
`
	results, err := parseVulnOutput([]byte(input))
	require.NoError(t, err)
	assert.Len(t, results, 2, "should dedup multiple findings for same OSV")

	// Build a map for order-independent assertion.
	byID := make(map[string]vulnResult)
	for _, r := range results {
		byID[r.OSVID] = r
	}

	r1 := byID["GO-2024-0001"]
	assert.Equal(t, "github.com/a/b", r1.Module)
	assert.Equal(t, "v1.0.0", r1.Version)
	assert.Equal(t, "v1.1.0", r1.FixedVersion)
	assert.Equal(t, "Vuln A", r1.Summary)
	assert.Equal(t, []string{"CVE-2024-0001"}, r1.Aliases)

	r2 := byID["GO-2024-0002"]
	assert.Equal(t, "github.com/c/d", r2.Module)
	assert.Equal(t, "", r2.FixedVersion)
}

func TestParseVulnOutput_Empty(t *testing.T) {
	results, err := parseVulnOutput([]byte(""))
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestParseVulnOutput_FindingWithoutOSV(t *testing.T) {
	// A finding referencing an OSV ID that was never sent should be skipped.
	input := `{"finding":{"osv":"GO-2024-MISSING","fixed_version":"v1.0.0","trace":[{"module":"github.com/x/y","version":"v0.1.0"}]}}
`
	results, err := parseVulnOutput([]byte(input))
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestExtractCVE(t *testing.T) {
	tests := []struct {
		name    string
		aliases []string
		want    string
	}{
		{"single CVE", []string{"CVE-2024-24790"}, "CVE-2024-24790"},
		{"CVE among others", []string{"GHSA-xxxx-yyyy-zzzz", "CVE-2024-1234"}, "CVE-2024-1234"},
		{"multiple CVEs returns first", []string{"CVE-2024-0001", "CVE-2024-0002"}, "CVE-2024-0001"},
		{"no CVE", []string{"GHSA-xxxx-yyyy-zzzz"}, ""},
		{"empty aliases", []string{}, ""},
		{"nil aliases", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractCVE(tt.aliases))
		})
	}
}
