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

// mockOSVClient implements osvClient for testing.
type mockOSVClient struct {
	results []VulnDetail
	err     error
}

func (m *mockOSVClient) QueryBatch(_ context.Context, _ []PackageQuery) ([]VulnDetail, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}

// validGoMod returns a go.mod with require directives for testing.
func validGoMod() []byte {
	return []byte(`module example.com/test

go 1.22

require (
	github.com/foo/bar v1.0.0
	github.com/baz/qux v0.2.0
)
`)
}

func TestVulnCollector_Name(t *testing.T) {
	c := &VulnCollector{}
	assert.Equal(t, "vuln", c.Name())
}

func TestVulnCollector_NoGoMod(t *testing.T) {
	dir := t.TempDir()
	c := &VulnCollector{osv: &mockOSVClient{}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	assert.NoError(t, err)
	assert.Nil(t, signals)
}

func TestVulnCollector_SingleVuln(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{{
			ID:           "GO-2024-2687",
			Aliases:      []string{"CVE-2024-24790"},
			Summary:      "Mishandling of DNS in net/netip",
			Ecosystem:    "Go",
			PackageName:  "github.com/foo/bar",
			Version:      "v1.0.0",
			FixedVersion: "v1.0.1",
			Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
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
	assert.Contains(t, sig.Title, "github.com/foo/bar")
	assert.Contains(t, sig.Description, "Upgrade github.com/foo/bar from v1.0.0 to v1.0.1")
	assert.Equal(t, 0.95, sig.Confidence) // high severity
	assert.Contains(t, sig.Tags, "security")
	assert.Contains(t, sig.Tags, "vulnerable-dependency")
	assert.Contains(t, sig.Tags, "CVE-2024-24790")
	assert.Contains(t, sig.Tags, "GO-2024-2687")
}

func TestVulnCollector_NoFixAvailable(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{{
			ID:           "GO-2024-9999",
			Aliases:      []string{"CVE-2024-99999"},
			Summary:      "Unpatched vulnerability",
			Ecosystem:    "Go",
			PackageName:  "github.com/foo/bar",
			Version:      "v1.0.0",
			FixedVersion: "",
			Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N",
		}},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	assert.Equal(t, 0.80, signals[0].Confidence) // medium severity (C:L)
	assert.Contains(t, signals[0].Description, "No fix available")
}

func TestVulnCollector_MultipleVulns(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{
			{
				ID:           "GO-2024-0001",
				Aliases:      []string{"CVE-2024-0001"},
				Summary:      "Vuln one",
				Ecosystem:    "Go",
				PackageName:  "github.com/a/b",
				Version:      "v1.0.0",
				FixedVersion: "v1.1.0",
				Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
			},
			{
				ID:           "GO-2024-0002",
				Aliases:      []string{"CVE-2024-0002"},
				Summary:      "Vuln two",
				Ecosystem:    "Go",
				PackageName:  "github.com/c/d",
				Version:      "v2.0.0",
				FixedVersion: "v2.1.0",
				Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N",
			},
			{
				ID:           "GO-2024-0003",
				Aliases:      []string{"CVE-2024-0003"},
				Summary:      "Vuln three",
				Ecosystem:    "Go",
				PackageName:  "github.com/e/f",
				Version:      "v3.0.0",
				FixedVersion: "",
				Severity:     "",
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{{
			ID:           "GO-2024-5555",
			Aliases:      []string{"GHSA-xxxx-yyyy-zzzz"},
			Summary:      "Some vulnerability",
			Ecosystem:    "Go",
			PackageName:  "github.com/foo/bar",
			Version:      "v1.0.0",
			FixedVersion: "v1.1.0",
			Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N",
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

func TestVulnCollector_QueryBatchError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		err: fmt.Errorf("network unavailable"),
	}}

	// Graceful degradation — error → nil signals, nil error.
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	assert.NoError(t, err)
	assert.Nil(t, signals)
}

func TestVulnCollector_ZeroVulns(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{},
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))

	c.osv = &mockOSVClient{
		results: []VulnDetail{{
			ID:           "GO-2024-1234",
			Aliases:      []string{"CVE-2024-1234"},
			Summary:      "Test vuln",
			Ecosystem:    "Go",
			PackageName:  "github.com/x/y",
			Version:      "v1.0.0",
			FixedVersion: "v1.1.0",
			Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
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
	assert.Equal(t, "high", metrics.Vulns[0].Severity)
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
	assert.Contains(t, err.Error(), "reading go.mod")
}

func TestVulnCollector_GoModParseError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("this is not valid go.mod content{{{"), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{}}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	assert.Error(t, err)
	assert.Nil(t, signals)
	assert.Contains(t, err.Error(), "parsing go.mod")
}

func TestVulnCollector_NoRequirements(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.22\n"), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{{ID: "should-not-reach"}},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	assert.NoError(t, err)
	assert.Nil(t, signals)
}

func TestVulnCollector_SeverityBasedConfidence(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))

	tests := []struct {
		name     string
		severity string
		wantConf float64
		wantSev  string
	}{
		{
			name:     "high severity → 0.95",
			severity: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
			wantConf: 0.95,
			wantSev:  "high",
		},
		{
			name:     "medium severity → 0.80",
			severity: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N",
			wantConf: 0.80,
			wantSev:  "medium",
		},
		{
			name:     "low severity → 0.60",
			severity: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N",
			wantConf: 0.60,
			wantSev:  "low",
		},
		{
			name:     "no severity data → 0.80 default",
			severity: "",
			wantConf: 0.80,
			wantSev:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &VulnCollector{osv: &mockOSVClient{
				results: []VulnDetail{{
					ID:          "GO-2024-0001",
					Aliases:     []string{"CVE-2024-0001"},
					Summary:     "Test vuln",
					Ecosystem:   "Go",
					PackageName: "github.com/foo/bar",
					Version:     "v1.0.0",
					Severity:    tt.severity,
				}},
			}}

			signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
			require.NoError(t, err)
			require.Len(t, signals, 1)
			assert.InDelta(t, tt.wantConf, signals[0].Confidence, 0.001)

			metrics := c.Metrics().(*VulnMetrics)
			assert.Equal(t, tt.wantSev, metrics.Vulns[0].Severity)
		})
	}
}

func TestSeverityFromCVSS(t *testing.T) {
	tests := []struct {
		name string
		cvss string
		want string
	}{
		{"high - all H", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", "high"},
		{"high - one H", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:N/A:N", "high"},
		{"medium - one L", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:N/A:N", "medium"},
		{"medium - mixed L", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:L/A:N", "medium"},
		{"low - all N", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N", "low"},
		{"empty string", "", ""},
		{"malformed - no CIA metrics", "not-a-cvss-string", "low"},
		{"partial vector", "CVSS:3.1/AV:N", "low"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, severityFromCVSS(tt.cvss))
		})
	}
}

func TestConfidenceForSeverity(t *testing.T) {
	tests := []struct {
		severity string
		want     float64
	}{
		{"high", 0.95},
		{"medium", 0.80},
		{"low", 0.60},
		{"", 0.80},
		{"unknown", 0.80},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			assert.InDelta(t, tt.want, confidenceForSeverity(tt.severity), 0.001)
		})
	}
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
