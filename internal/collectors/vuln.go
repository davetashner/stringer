package collectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
}

// VulnCollector detects known vulnerabilities in Go module dependencies
// using the govulncheck engine.
type VulnCollector struct {
	metrics *VulnMetrics
	scanner vulnScanner
}

// vulnScanner abstracts vulnerability scanning for testability.
type vulnScanner interface {
	Scan(ctx context.Context, repoPath string) ([]vulnResult, error)
}

// vulnResult represents a single deduplicated vulnerability finding.
type vulnResult struct {
	OSVID        string
	Aliases      []string
	Summary      string
	Module       string
	Version      string
	FixedVersion string
}

// Name returns the collector name used for registration and filtering.
func (c *VulnCollector) Name() string { return "vuln" }

// Collect runs govulncheck on the repository at repoPath and returns signals
// for each known vulnerability in the module's dependencies.
func (c *VulnCollector) Collect(ctx context.Context, repoPath string, _ signal.CollectorOpts) ([]signal.RawSignal, error) {
	goModPath := filepath.Join(repoPath, "go.mod")

	_, err := FS.ReadFile(goModPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Info("no go.mod found, skipping vuln collector")
			return nil, nil
		}
		return nil, fmt.Errorf("checking go.mod: %w", err)
	}

	s := c.scanner
	if s == nil {
		s = &realVulnScanner{}
	}

	results, err := s.Scan(ctx, repoPath)
	if err != nil {
		slog.Warn("vuln scan failed, skipping", "error", err)
		return nil, nil // C7.3: graceful degradation
	}

	var signals []signal.RawSignal
	metrics := &VulnMetrics{}

	for _, r := range results {
		cve := extractCVE(r.Aliases)
		titleID := cve
		if titleID == "" {
			titleID = r.OSVID
		}

		title := fmt.Sprintf("Vulnerable dependency: %s [%s]", r.Module, titleID)

		var desc string
		if r.FixedVersion != "" {
			desc = fmt.Sprintf("%s\n\nUpgrade %s from %s to %s.", r.Summary, r.Module, r.Version, r.FixedVersion)
		} else {
			desc = fmt.Sprintf("%s\n\nNo fix available.", r.Summary)
		}

		confidence := 0.9
		if r.FixedVersion == "" {
			confidence = 0.85
		}

		tags := []string{"security", "vulnerable-dependency"}
		if cve != "" {
			tags = append(tags, cve)
		}
		tags = append(tags, r.OSVID)

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
			OSVID:        r.OSVID,
			CVE:          cve,
			Module:       r.Module,
			Version:      r.Version,
			FixedVersion: r.FixedVersion,
			Summary:      r.Summary,
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

// --- Real scanner implementation ---

type realVulnScanner struct{}

func (s *realVulnScanner) Scan(ctx context.Context, repoPath string) ([]vulnResult, error) {
	govulncheck, err := exec.LookPath("govulncheck")
	if err != nil {
		return nil, fmt.Errorf("govulncheck not found: %w", err)
	}

	cmd := exec.CommandContext(ctx, govulncheck, "-json", "-scan", "module", "-C", repoPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// govulncheck exits non-zero when vulnerabilities are found.
		// If we have output, parse it; otherwise surface the error.
		if stdout.Len() == 0 {
			return nil, fmt.Errorf("govulncheck: %w: %s", err, stderr.String())
		}
	}

	return parseVulnOutput(stdout.Bytes())
}

// JSON message types from govulncheck -json output.
type vulnMessage struct {
	OSV     *vulnOSV     `json:"osv,omitempty"`
	Finding *vulnFinding `json:"finding,omitempty"`
}

type vulnOSV struct {
	ID      string   `json:"id"`
	Aliases []string `json:"aliases"`
	Summary string   `json:"summary"`
}

type vulnFinding struct {
	OSV          string      `json:"osv"`
	FixedVersion string      `json:"fixed_version"`
	Trace        []vulnFrame `json:"trace"`
}

type vulnFrame struct {
	Module  string `json:"module"`
	Version string `json:"version"`
}

// parseVulnOutput parses govulncheck -json output into deduplicated results.
func parseVulnOutput(data []byte) ([]vulnResult, error) {
	osvs := make(map[string]*vulnOSV)
	findings := make(map[string]*vulnFinding)

	dec := json.NewDecoder(bytes.NewReader(data))
	for dec.More() {
		var msg vulnMessage
		if err := dec.Decode(&msg); err != nil {
			continue // skip config/progress messages
		}
		if msg.OSV != nil {
			osvs[msg.OSV.ID] = msg.OSV
		}
		if msg.Finding != nil {
			// Keep the first finding per OSV ID (dedup multiple findings).
			if _, exists := findings[msg.Finding.OSV]; !exists {
				findings[msg.Finding.OSV] = msg.Finding
			}
		}
	}

	var results []vulnResult
	for osvID, finding := range findings {
		osv := osvs[osvID]
		if osv == nil {
			continue
		}

		var module, version string
		if len(finding.Trace) > 0 {
			module = finding.Trace[0].Module
			version = finding.Trace[0].Version
		}

		results = append(results, vulnResult{
			OSVID:        osv.ID,
			Aliases:      osv.Aliases,
			Summary:      osv.Summary,
			Module:       module,
			Version:      version,
			FixedVersion: finding.FixedVersion,
		})
	}

	return results, nil
}

// Compile-time interface checks.
var _ collector.Collector = (*VulnCollector)(nil)
var _ collector.MetricsProvider = (*VulnCollector)(nil)
