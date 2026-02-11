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

// --- Java manifest tests ---

func validPomXML() []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-core</artifactId>
      <version>5.3.0</version>
    </dependency>
  </dependencies>
</project>`)
}

func validBuildGradle() []byte {
	return []byte(`plugins { id 'java' }
implementation 'org.springframework:spring-core:5.3.0'
`)
}

func validBuildGradleKts() []byte {
	return []byte(`plugins { id("java") }
implementation("org.springframework:spring-core:5.3.0")
`)
}

func mavenVulnDetail() VulnDetail {
	return VulnDetail{
		ID:           "GHSA-1234-5678-abcd",
		Aliases:      []string{"CVE-2023-20861"},
		Summary:      "Spring Expression DoS",
		Ecosystem:    "Maven",
		PackageName:  "org.springframework:spring-core",
		Version:      "5.3.0",
		FixedVersion: "5.3.27",
		Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H",
	}
}

func TestVulnCollector_PomXmlOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), validPomXML(), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{mavenVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	sig := signals[0]
	assert.Equal(t, "pom.xml", sig.FilePath)
	assert.Equal(t, "vuln", sig.Source)
	assert.Contains(t, sig.Title, "org.springframework:spring-core")
	assert.Contains(t, sig.Title, "CVE-2023-20861")
	assert.Contains(t, sig.Description, "Upgrade org.springframework:spring-core from 5.3.0 to 5.3.27")
	assert.Contains(t, sig.Tags, "java")
	assert.Contains(t, sig.Tags, "security")

	metrics := c.Metrics().(*VulnMetrics)
	assert.Equal(t, "Maven", metrics.Vulns[0].Ecosystem)
	assert.Equal(t, "pom.xml", metrics.Vulns[0].FilePath)
}

func TestVulnCollector_GradleOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle"), validBuildGradle(), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{mavenVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	assert.Equal(t, "build.gradle", signals[0].FilePath)
	assert.Contains(t, signals[0].Tags, "java")
}

func TestVulnCollector_GradleKts(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle.kts"), validBuildGradleKts(), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{mavenVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	assert.Equal(t, "build.gradle.kts", signals[0].FilePath)
	assert.Contains(t, signals[0].Tags, "java")
}

func TestVulnCollector_GoAndMaven(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), validPomXML(), 0o600))

	goVuln := VulnDetail{
		ID:           "GO-2024-0001",
		Aliases:      []string{"CVE-2024-0001"},
		Summary:      "Go vuln",
		Ecosystem:    "Go",
		PackageName:  "github.com/foo/bar",
		Version:      "v1.0.0",
		FixedVersion: "v1.1.0",
		Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	}

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{goVuln, mavenVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 2)

	// Find Go and Maven signals by FilePath.
	var goSig, mavenSig signal.RawSignal
	for _, s := range signals {
		switch s.FilePath {
		case "go.mod":
			goSig = s
		case "pom.xml":
			mavenSig = s
		}
	}

	assert.Equal(t, "go.mod", goSig.FilePath)
	assert.Contains(t, goSig.Title, "github.com/foo/bar")

	assert.Equal(t, "pom.xml", mavenSig.FilePath)
	assert.Contains(t, mavenSig.Title, "org.springframework:spring-core")
}

func TestVulnCollector_JavaTags(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), validPomXML(), 0o600))

	goVuln := VulnDetail{
		ID:          "GO-2024-0001",
		Summary:     "Go vuln",
		Ecosystem:   "Go",
		PackageName: "github.com/foo/bar",
		Version:     "v1.0.0",
		Severity:    "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	}

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{goVuln, mavenVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 2)

	for _, s := range signals {
		if s.FilePath == "go.mod" {
			assert.NotContains(t, s.Tags, "java", "Go signal should not have java tag")
		} else {
			assert.Contains(t, s.Tags, "java", "Maven signal should have java tag")
		}
	}
}

func TestVulnCollector_PomParseError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<<<not valid xml>>>"), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{{
			ID:          "GO-2024-0001",
			Summary:     "Go vuln",
			Ecosystem:   "Go",
			PackageName: "github.com/foo/bar",
			Version:     "v1.0.0",
			Severity:    "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		}},
	}}

	// Malformed pom.xml should be skipped gracefully; Go signals still emitted.
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "go.mod", signals[0].FilePath)
}

func TestVulnCollector_NoManifests(t *testing.T) {
	dir := t.TempDir()
	c := &VulnCollector{osv: &mockOSVClient{}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	assert.NoError(t, err)
	assert.Nil(t, signals)
}

func TestVulnCollector_MavenNoFix(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pom.xml"), validPomXML(), 0o600))

	vuln := mavenVulnDetail()
	vuln.FixedVersion = ""

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{vuln},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	assert.Contains(t, signals[0].Description, "No fix available for org.springframework:spring-core 5.3.0")
	assert.Equal(t, "pom.xml", signals[0].FilePath)
}

// --- Rust Cargo.toml tests ---

func validCargoToml() []byte {
	return []byte(`[package]
name = "my-app"
version = "0.1.0"

[dependencies]
serde = "1.0.197"
tokio = { version = "1.36.0", features = ["full"] }
`)
}

func crateVulnDetail() VulnDetail {
	return VulnDetail{
		ID:           "RUSTSEC-2024-0001",
		Aliases:      []string{"CVE-2024-55555"},
		Summary:      "Memory safety issue in serde",
		Ecosystem:    "crates.io",
		PackageName:  "serde",
		Version:      "1.0.197",
		FixedVersion: "1.0.200",
		Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	}
}

func TestVulnCollector_CargoOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), validCargoToml(), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{crateVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	sig := signals[0]
	assert.Equal(t, "Cargo.toml", sig.FilePath)
	assert.Equal(t, "vuln", sig.Source)
	assert.Contains(t, sig.Title, "serde")
	assert.Contains(t, sig.Title, "CVE-2024-55555")
	assert.Contains(t, sig.Description, "Upgrade serde from 1.0.197 to 1.0.200")
	assert.Contains(t, sig.Tags, "rust")
	assert.Contains(t, sig.Tags, "security")

	metrics := c.Metrics().(*VulnMetrics)
	assert.Equal(t, "crates.io", metrics.Vulns[0].Ecosystem)
	assert.Equal(t, "Cargo.toml", metrics.Vulns[0].FilePath)
}

func TestVulnCollector_GoAndCargo(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), validCargoToml(), 0o600))

	goVuln := VulnDetail{
		ID:           "GO-2024-0001",
		Aliases:      []string{"CVE-2024-0001"},
		Summary:      "Go vuln",
		Ecosystem:    "Go",
		PackageName:  "github.com/foo/bar",
		Version:      "v1.0.0",
		FixedVersion: "v1.1.0",
		Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	}

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{goVuln, crateVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 2)

	var goSig, rustSig signal.RawSignal
	for _, s := range signals {
		switch s.FilePath {
		case "go.mod":
			goSig = s
		case "Cargo.toml":
			rustSig = s
		}
	}

	assert.Equal(t, "go.mod", goSig.FilePath)
	assert.Contains(t, goSig.Title, "github.com/foo/bar")

	assert.Equal(t, "Cargo.toml", rustSig.FilePath)
	assert.Contains(t, rustSig.Title, "serde")
}

func TestVulnCollector_CargoParseError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[dependencies\nbroken"), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{{
			ID:          "GO-2024-0001",
			Summary:     "Go vuln",
			Ecosystem:   "Go",
			PackageName: "github.com/foo/bar",
			Version:     "v1.0.0",
			Severity:    "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		}},
	}}

	// Malformed Cargo.toml should be skipped gracefully; Go signals still emitted.
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "go.mod", signals[0].FilePath)
}

func TestVulnCollector_RustTags(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), validCargoToml(), 0o600))

	goVuln := VulnDetail{
		ID:          "GO-2024-0001",
		Summary:     "Go vuln",
		Ecosystem:   "Go",
		PackageName: "github.com/foo/bar",
		Version:     "v1.0.0",
		Severity:    "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	}

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{goVuln, crateVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 2)

	for _, s := range signals {
		if s.FilePath == "go.mod" {
			assert.NotContains(t, s.Tags, "rust", "Go signal should not have rust tag")
		} else {
			assert.Contains(t, s.Tags, "rust", "Rust signal should have rust tag")
		}
	}
}

func TestVulnCollector_CargoNoFix(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Cargo.toml"), validCargoToml(), 0o600))

	vuln := crateVulnDetail()
	vuln.FixedVersion = ""

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{vuln},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	assert.Contains(t, signals[0].Description, "No fix available for serde 1.0.197")
	assert.Equal(t, "Cargo.toml", signals[0].FilePath)
}

// --- C#/.NET .csproj tests ---

func validCsproj() []byte {
	return []byte(`<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.1" />
    <PackageReference Include="Serilog" Version="3.1.0" />
  </ItemGroup>
</Project>`)
}

func nugetVulnDetail() VulnDetail {
	return VulnDetail{
		ID:           "GHSA-5crp-9r3c-p9vr",
		Aliases:      []string{"CVE-2024-12345"},
		Summary:      "Deserialization vulnerability in Newtonsoft.Json",
		Ecosystem:    "NuGet",
		PackageName:  "Newtonsoft.Json",
		Version:      "13.0.1",
		FixedVersion: "13.0.4",
		Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	}
}

func TestVulnCollector_CsprojOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "MyApp.csproj"), validCsproj(), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{nugetVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	sig := signals[0]
	assert.Equal(t, "MyApp.csproj", sig.FilePath)
	assert.Equal(t, "vuln", sig.Source)
	assert.Contains(t, sig.Title, "Newtonsoft.Json")
	assert.Contains(t, sig.Title, "CVE-2024-12345")
	assert.Contains(t, sig.Description, "Upgrade Newtonsoft.Json from 13.0.1 to 13.0.4")
	assert.Contains(t, sig.Tags, "csharp")
	assert.Contains(t, sig.Tags, "security")

	metrics := c.Metrics().(*VulnMetrics)
	assert.Equal(t, "NuGet", metrics.Vulns[0].Ecosystem)
	assert.Equal(t, "MyApp.csproj", metrics.Vulns[0].FilePath)
}

func TestVulnCollector_GoAndCsproj(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "MyApp.csproj"), validCsproj(), 0o600))

	goVuln := VulnDetail{
		ID:           "GO-2024-0001",
		Aliases:      []string{"CVE-2024-0001"},
		Summary:      "Go vuln",
		Ecosystem:    "Go",
		PackageName:  "github.com/foo/bar",
		Version:      "v1.0.0",
		FixedVersion: "v1.1.0",
		Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	}

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{goVuln, nugetVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 2)

	var goSig, csharpSig signal.RawSignal
	for _, s := range signals {
		switch s.FilePath {
		case "go.mod":
			goSig = s
		case "MyApp.csproj":
			csharpSig = s
		}
	}

	assert.Equal(t, "go.mod", goSig.FilePath)
	assert.Contains(t, goSig.Title, "github.com/foo/bar")

	assert.Equal(t, "MyApp.csproj", csharpSig.FilePath)
	assert.Contains(t, csharpSig.Title, "Newtonsoft.Json")
}

func TestVulnCollector_CsprojParseError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Bad.csproj"), []byte("<<<not valid xml>>>"), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{{
			ID:          "GO-2024-0001",
			Summary:     "Go vuln",
			Ecosystem:   "Go",
			PackageName: "github.com/foo/bar",
			Version:     "v1.0.0",
			Severity:    "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		}},
	}}

	// Malformed .csproj should be skipped gracefully; Go signals still emitted.
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "go.mod", signals[0].FilePath)
}

func TestVulnCollector_CsharpTags(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "MyApp.csproj"), validCsproj(), 0o600))

	goVuln := VulnDetail{
		ID:          "GO-2024-0001",
		Summary:     "Go vuln",
		Ecosystem:   "Go",
		PackageName: "github.com/foo/bar",
		Version:     "v1.0.0",
		Severity:    "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	}

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{goVuln, nugetVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 2)

	for _, s := range signals {
		if s.FilePath == "go.mod" {
			assert.NotContains(t, s.Tags, "csharp", "Go signal should not have csharp tag")
		} else {
			assert.Contains(t, s.Tags, "csharp", "NuGet signal should have csharp tag")
		}
	}
}

func TestVulnCollector_CsprojNoFix(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "MyApp.csproj"), validCsproj(), 0o600))

	vuln := nugetVulnDetail()
	vuln.FixedVersion = ""

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{vuln},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	assert.Contains(t, signals[0].Description, "No fix available for Newtonsoft.Json 13.0.1")
	assert.Equal(t, "MyApp.csproj", signals[0].FilePath)
}

// --- Python requirements.txt / pyproject.toml tests ---

func validRequirementsTxt() []byte {
	return []byte(`requests==2.31.0
django~=4.2.0
`)
}

func validPyprojectToml() []byte {
	return []byte(`[project]
name = "myapp"
version = "1.0.0"
dependencies = [
    "requests>=2.31.0",
    "flask==3.0.0",
]
`)
}

func pypiVulnDetail() VulnDetail {
	return VulnDetail{
		ID:           "PYSEC-2024-0001",
		Aliases:      []string{"CVE-2024-88888"},
		Summary:      "SSRF vulnerability in requests",
		Ecosystem:    "PyPI",
		PackageName:  "requests",
		Version:      "2.31.0",
		FixedVersion: "2.32.0",
		Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	}
}

func TestVulnCollector_RequirementsTxtOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "requirements.txt"), validRequirementsTxt(), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{pypiVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	sig := signals[0]
	assert.Equal(t, "requirements.txt", sig.FilePath)
	assert.Equal(t, "vuln", sig.Source)
	assert.Contains(t, sig.Title, "requests")
	assert.Contains(t, sig.Title, "CVE-2024-88888")
	assert.Contains(t, sig.Description, "Upgrade requests from 2.31.0 to 2.32.0")
	assert.Contains(t, sig.Tags, "python")
	assert.Contains(t, sig.Tags, "security")

	metrics := c.Metrics().(*VulnMetrics)
	assert.Equal(t, "PyPI", metrics.Vulns[0].Ecosystem)
	assert.Equal(t, "requirements.txt", metrics.Vulns[0].FilePath)
}

func TestVulnCollector_PyprojectOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pyproject.toml"), validPyprojectToml(), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{pypiVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	sig := signals[0]
	assert.Equal(t, "pyproject.toml", sig.FilePath)
	assert.Contains(t, sig.Tags, "python")

	metrics := c.Metrics().(*VulnMetrics)
	assert.Equal(t, "PyPI", metrics.Vulns[0].Ecosystem)
	assert.Equal(t, "pyproject.toml", metrics.Vulns[0].FilePath)
}

func TestVulnCollector_RequirementsTxtPrecedence(t *testing.T) {
	// When both files exist, requirements.txt takes precedence.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "requirements.txt"), validRequirementsTxt(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pyproject.toml"), validPyprojectToml(), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{pypiVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	assert.Equal(t, "requirements.txt", signals[0].FilePath)
}

func TestVulnCollector_GoAndPython(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "requirements.txt"), validRequirementsTxt(), 0o600))

	goVuln := VulnDetail{
		ID:           "GO-2024-0001",
		Aliases:      []string{"CVE-2024-0001"},
		Summary:      "Go vuln",
		Ecosystem:    "Go",
		PackageName:  "github.com/foo/bar",
		Version:      "v1.0.0",
		FixedVersion: "v1.1.0",
		Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	}

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{goVuln, pypiVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 2)

	var goSig, pythonSig signal.RawSignal
	for _, s := range signals {
		switch s.FilePath {
		case "go.mod":
			goSig = s
		case "requirements.txt":
			pythonSig = s
		}
	}

	assert.Equal(t, "go.mod", goSig.FilePath)
	assert.Contains(t, goSig.Title, "github.com/foo/bar")

	assert.Equal(t, "requirements.txt", pythonSig.FilePath)
	assert.Contains(t, pythonSig.Title, "requests")
}

func TestVulnCollector_PythonTags(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "requirements.txt"), validRequirementsTxt(), 0o600))

	goVuln := VulnDetail{
		ID:          "GO-2024-0001",
		Summary:     "Go vuln",
		Ecosystem:   "Go",
		PackageName: "github.com/foo/bar",
		Version:     "v1.0.0",
		Severity:    "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	}

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{goVuln, pypiVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 2)

	for _, s := range signals {
		if s.FilePath == "go.mod" {
			assert.NotContains(t, s.Tags, "python", "Go signal should not have python tag")
		} else {
			assert.Contains(t, s.Tags, "python", "Python signal should have python tag")
		}
	}
}

func TestVulnCollector_PythonNoFix(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "requirements.txt"), validRequirementsTxt(), 0o600))

	vuln := pypiVulnDetail()
	vuln.FixedVersion = ""

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{vuln},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	assert.Contains(t, signals[0].Description, "No fix available for requests 2.31.0")
	assert.Equal(t, "requirements.txt", signals[0].FilePath)
}

func TestVulnCollector_PythonParseError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))
	// requirements.txt can't really have parse errors (line-based), so test pyproject.toml fallback.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project\nbroken"), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{{
			ID:          "GO-2024-0001",
			Summary:     "Go vuln",
			Ecosystem:   "Go",
			PackageName: "github.com/foo/bar",
			Version:     "v1.0.0",
			Severity:    "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		}},
	}}

	// Malformed pyproject.toml should be skipped gracefully; Go signals still emitted.
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "go.mod", signals[0].FilePath)
}

// --- Node.js package.json tests ---

func validPackageJSON() []byte {
	return []byte(`{
		"name": "myapp",
		"version": "1.0.0",
		"dependencies": {
			"express": "^4.18.2",
			"lodash": "4.17.21"
		}
	}`)
}

func npmVulnDetail() VulnDetail {
	return VulnDetail{
		ID:           "GHSA-29mw-wpgm-hmr9",
		Aliases:      []string{"CVE-2024-29041"},
		Summary:      "Open redirect in express",
		Ecosystem:    "npm",
		PackageName:  "express",
		Version:      "4.18.2",
		FixedVersion: "4.19.2",
		Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:R/S:C/C:L/I:L/A:N",
	}
}

func TestVulnCollector_PackageJSONOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), validPackageJSON(), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{npmVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	sig := signals[0]
	assert.Equal(t, "package.json", sig.FilePath)
	assert.Equal(t, "vuln", sig.Source)
	assert.Contains(t, sig.Title, "express")
	assert.Contains(t, sig.Title, "CVE-2024-29041")
	assert.Contains(t, sig.Description, "Upgrade express from 4.18.2 to 4.19.2")
	assert.Contains(t, sig.Tags, "nodejs")
	assert.Contains(t, sig.Tags, "security")

	metrics := c.Metrics().(*VulnMetrics)
	assert.Equal(t, "npm", metrics.Vulns[0].Ecosystem)
	assert.Equal(t, "package.json", metrics.Vulns[0].FilePath)
}

func TestVulnCollector_GoAndNpm(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), validPackageJSON(), 0o600))

	goVuln := VulnDetail{
		ID:           "GO-2024-0001",
		Aliases:      []string{"CVE-2024-0001"},
		Summary:      "Go vuln",
		Ecosystem:    "Go",
		PackageName:  "github.com/foo/bar",
		Version:      "v1.0.0",
		FixedVersion: "v1.1.0",
		Severity:     "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	}

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{goVuln, npmVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 2)

	var goSig, npmSig signal.RawSignal
	for _, s := range signals {
		switch s.FilePath {
		case "go.mod":
			goSig = s
		case "package.json":
			npmSig = s
		}
	}

	assert.Equal(t, "go.mod", goSig.FilePath)
	assert.Contains(t, goSig.Title, "github.com/foo/bar")

	assert.Equal(t, "package.json", npmSig.FilePath)
	assert.Contains(t, npmSig.Title, "express")
}

func TestVulnCollector_NodejsTags(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), validPackageJSON(), 0o600))

	goVuln := VulnDetail{
		ID:          "GO-2024-0001",
		Summary:     "Go vuln",
		Ecosystem:   "Go",
		PackageName: "github.com/foo/bar",
		Version:     "v1.0.0",
		Severity:    "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
	}

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{goVuln, npmVulnDetail()},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 2)

	for _, s := range signals {
		if s.FilePath == "go.mod" {
			assert.NotContains(t, s.Tags, "nodejs", "Go signal should not have nodejs tag")
		} else {
			assert.Contains(t, s.Tags, "nodejs", "npm signal should have nodejs tag")
		}
	}
}

func TestVulnCollector_NpmNoFix(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), validPackageJSON(), 0o600))

	vuln := npmVulnDetail()
	vuln.FixedVersion = ""

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{vuln},
	}}

	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)

	assert.Contains(t, signals[0].Description, "No fix available for express 4.18.2")
	assert.Equal(t, "package.json", signals[0].FilePath)
}

func TestVulnCollector_NpmParseError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), validGoMod(), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte("{broken json"), 0o600))

	c := &VulnCollector{osv: &mockOSVClient{
		results: []VulnDetail{{
			ID:          "GO-2024-0001",
			Summary:     "Go vuln",
			Ecosystem:   "Go",
			PackageName: "github.com/foo/bar",
			Version:     "v1.0.0",
			Severity:    "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H",
		}},
	}}

	// Malformed package.json should be skipped gracefully; Go signals still emitted.
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 1)
	assert.Equal(t, "go.mod", signals[0].FilePath)
}
