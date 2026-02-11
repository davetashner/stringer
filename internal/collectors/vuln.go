package collectors

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/modfile"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

func init() {
	collector.Register(&VulnCollector{})
}

// VulnMetrics holds structured vulnerability data from the last scan.
type VulnMetrics struct {
	TotalVulns int
	Vulns      []VulnEntry
}

// VulnEntry represents a single vulnerability finding.
type VulnEntry struct {
	OSVID        string
	CVE          string
	Module       string
	Version      string
	FixedVersion string
	Summary      string
	Severity     string
}

// VulnCollector detects known vulnerabilities in Go module dependencies
// using the OSV.dev API.
type VulnCollector struct {
	metrics *VulnMetrics
	osv     osvClient
}

// Name returns the collector name used for registration and filtering.
func (c *VulnCollector) Name() string { return "vuln" }

// Collect parses go.mod in repoPath, queries OSV.dev for known vulnerabilities,
// and returns signals with severity-based confidence scoring.
func (c *VulnCollector) Collect(ctx context.Context, repoPath string, _ signal.CollectorOpts) ([]signal.RawSignal, error) {
	goModPath := filepath.Join(repoPath, "go.mod")

	data, err := FS.ReadFile(goModPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Info("no go.mod found, skipping vuln collector")
			return nil, nil
		}
		return nil, fmt.Errorf("reading go.mod: %w", err)
	}

	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing go.mod: %w", err)
	}

	if len(f.Require) == 0 {
		return nil, nil
	}

	queries := make([]PackageQuery, len(f.Require))
	for i, req := range f.Require {
		queries[i] = PackageQuery{
			Ecosystem: "Go",
			Name:      req.Mod.Path,
			Version:   req.Mod.Version,
		}
	}

	client := c.osv
	if client == nil {
		client = newOSVClient(30 * time.Second)
	}

	results, err := client.QueryBatch(ctx, queries)
	if err != nil {
		slog.Info("vuln scan unavailable, skipping", "error", err)
		return nil, nil // graceful degradation
	}

	var signals []signal.RawSignal
	metrics := &VulnMetrics{}

	for _, r := range results {
		cve := extractCVE(r.Aliases)
		titleID := cve
		if titleID == "" {
			titleID = r.ID
		}

		title := fmt.Sprintf("Vulnerable dependency: %s [%s]", r.PackageName, titleID)

		var desc string
		if r.FixedVersion != "" {
			desc = fmt.Sprintf("%s\n\nUpgrade %s from %s to %s.", r.Summary, r.PackageName, r.Version, r.FixedVersion)
		} else {
			desc = fmt.Sprintf("%s\n\nNo fix available.", r.Summary)
		}

		severity := severityFromCVSS(r.Severity)
		confidence := confidenceForSeverity(severity)

		tags := []string{"security", "vulnerable-dependency"}
		if cve != "" {
			tags = append(tags, cve)
		}
		tags = append(tags, r.ID)

		signals = append(signals, signal.RawSignal{
			Source:      "vuln",
			Kind:        "vulnerable-dependency",
			FilePath:    "go.mod",
			Title:       title,
			Description: desc,
			Confidence:  confidence,
			Tags:        tags,
		})

		metrics.Vulns = append(metrics.Vulns, VulnEntry{
			OSVID:        r.ID,
			CVE:          cve,
			Module:       r.PackageName,
			Version:      r.Version,
			FixedVersion: r.FixedVersion,
			Summary:      r.Summary,
			Severity:     severity,
		})
	}

	metrics.TotalVulns = len(signals)
	c.metrics = metrics
	return signals, nil
}

// Metrics returns structured vulnerability data from the last Collect call.
func (c *VulnCollector) Metrics() any { return c.metrics }

// extractCVE returns the first CVE-YYYY-NNNN alias from a list, or "".
func extractCVE(aliases []string) string {
	for _, a := range aliases {
		if strings.HasPrefix(a, "CVE-") {
			return a
		}
	}
	return ""
}

// severityFromCVSS parses a CVSS v3 vector string and returns a severity level
// based on the CIA (Confidentiality, Integrity, Availability) impact metrics.
func severityFromCVSS(cvss string) string {
	if cvss == "" {
		return ""
	}

	metrics := parseCVSSMetrics(cvss, "C", "I", "A")
	if metrics == nil {
		return "low"
	}

	hasHigh := false
	hasLow := false
	for _, v := range metrics {
		switch v {
		case "H":
			hasHigh = true
		case "L":
			hasLow = true
		}
	}

	switch {
	case hasHigh:
		return "high"
	case hasLow:
		return "medium"
	default:
		return "low"
	}
}

// parseCVSSMetrics extracts the values of the named metrics from a CVSS v3 vector string.
// Returns nil if none of the requested metrics are found.
func parseCVSSMetrics(cvss string, names ...string) map[string]string {
	result := make(map[string]string)
	for _, part := range strings.Split(cvss, "/") {
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			continue
		}
		for _, name := range names {
			if kv[0] == name {
				result[name] = kv[1]
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// confidenceForSeverity maps a severity level to a confidence score.
func confidenceForSeverity(severity string) float64 {
	switch severity {
	case "high":
		return 0.95
	case "medium":
		return 0.80
	case "low":
		return 0.60
	default:
		return 0.80 // no severity data → default to medium
	}
}

// Compile-time interface checks.
var _ collector.Collector = (*VulnCollector)(nil)
var _ collector.MetricsProvider = (*VulnCollector)(nil)
