package collectors

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
	Ecosystem    string
	FilePath     string
}

// VulnCollector detects known vulnerabilities in Go module dependencies
// using the OSV.dev API.
type VulnCollector struct {
	metrics *VulnMetrics
	osv     osvClient
}

// Name returns the collector name used for registration and filtering.
func (c *VulnCollector) Name() string { return "vuln" }

// Collect parses dependency manifests (go.mod, pom.xml, build.gradle/kts, Cargo.toml, *.csproj,
// requirements.txt, pyproject.toml, package.json) in repoPath, queries OSV.dev for known
// vulnerabilities, and returns signals with severity-based confidence scoring.
func (c *VulnCollector) Collect(ctx context.Context, repoPath string, _ signal.CollectorOpts) ([]signal.RawSignal, error) {
	// Gather queries from Go manifest (fatal on parse error).
	goQueries, err := parseGoModQueries(repoPath)
	if err != nil {
		return nil, err
	}

	// Gather queries from Java manifests (non-fatal on parse error).
	pomQueries := parsePomQueries(repoPath)
	gradleFile, gradleQueries := parseGradleQueries(repoPath)

	// Gather queries from Rust manifest (non-fatal on parse error).
	cargoQueries := parseCargoQueries(repoPath)

	// Gather queries from .NET manifests (non-fatal on parse error).
	csprojFile, csprojQueries := parseCsprojQueries(repoPath)

	// Gather queries from Python manifests (non-fatal on parse error).
	pythonFile, pythonQueries := parsePythonQueries(repoPath)

	// Gather queries from Node.js manifest (non-fatal on parse error).
	npmQueries := parseNpmQueries(repoPath)

	// Build combined query list with file/ecosystem tracking.
	// fileMap tracks which manifest a query came from; used for dedup and signal emission.
	type queryMeta struct {
		filePath  string
		ecosystem string
	}
	fileMap := make(map[string]queryMeta) // key: "ecosystem|name|version"
	var queries []PackageQuery

	for _, q := range goQueries {
		key := q.Ecosystem + "|" + q.Name + "|" + q.Version
		if _, exists := fileMap[key]; !exists {
			fileMap[key] = queryMeta{filePath: "go.mod", ecosystem: "Go"}
			queries = append(queries, q)
		}
	}
	for _, q := range pomQueries {
		key := q.Ecosystem + "|" + q.Name + "|" + q.Version
		if _, exists := fileMap[key]; !exists {
			fileMap[key] = queryMeta{filePath: "pom.xml", ecosystem: "Maven"}
			queries = append(queries, q)
		}
	}
	for _, q := range gradleQueries {
		key := q.Ecosystem + "|" + q.Name + "|" + q.Version
		if _, exists := fileMap[key]; !exists {
			fileMap[key] = queryMeta{filePath: gradleFile, ecosystem: "Maven"}
			queries = append(queries, q)
		}
	}
	for _, q := range cargoQueries {
		key := q.Ecosystem + "|" + q.Name + "|" + q.Version
		if _, exists := fileMap[key]; !exists {
			fileMap[key] = queryMeta{filePath: "Cargo.toml", ecosystem: "crates.io"}
			queries = append(queries, q)
		}
	}
	for _, q := range csprojQueries {
		key := q.Ecosystem + "|" + q.Name + "|" + q.Version
		if _, exists := fileMap[key]; !exists {
			fileMap[key] = queryMeta{filePath: csprojFile, ecosystem: "NuGet"}
			queries = append(queries, q)
		}
	}
	for _, q := range pythonQueries {
		key := q.Ecosystem + "|" + q.Name + "|" + q.Version
		if _, exists := fileMap[key]; !exists {
			fileMap[key] = queryMeta{filePath: pythonFile, ecosystem: "PyPI"}
			queries = append(queries, q)
		}
	}
	for _, q := range npmQueries {
		key := q.Ecosystem + "|" + q.Name + "|" + q.Version
		if _, exists := fileMap[key]; !exists {
			fileMap[key] = queryMeta{filePath: "package.json", ecosystem: "npm"}
			queries = append(queries, q)
		}
	}

	if len(queries) == 0 {
		return nil, nil
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
			desc = fmt.Sprintf("%s\n\nNo fix available for %s %s.", r.Summary, r.PackageName, r.Version)
		}

		severity := severityFromCVSS(r.Severity)
		confidence := confidenceForSeverity(severity)

		// Look up the manifest file for this result.
		meta := fileMap[r.Ecosystem+"|"+r.PackageName+"|"+r.Version]

		tags := []string{"security", "vulnerable-dependency"}
		if meta.ecosystem == "Maven" {
			tags = append(tags, "java")
		}
		if meta.ecosystem == "crates.io" {
			tags = append(tags, "rust")
		}
		if meta.ecosystem == "NuGet" {
			tags = append(tags, "csharp")
		}
		if meta.ecosystem == "PyPI" {
			tags = append(tags, "python")
		}
		if meta.ecosystem == "npm" {
			tags = append(tags, "nodejs")
		}
		if cve != "" {
			tags = append(tags, cve)
		}
		tags = append(tags, r.ID)

		signals = append(signals, signal.RawSignal{
			Source:      "vuln",
			Kind:        "vulnerable-dependency",
			FilePath:    meta.filePath,
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
			Ecosystem:    meta.ecosystem,
			FilePath:     meta.filePath,
		})
	}

	metrics.TotalVulns = len(signals)
	c.metrics = metrics
	return signals, nil
}

// parseGoModQueries reads go.mod and returns PackageQuery entries for OSV lookup.
// Returns nil, nil if no go.mod exists. Parse errors are fatal (returned as error).
func parseGoModQueries(repoPath string) ([]PackageQuery, error) {
	data, err := FS.ReadFile(filepath.Join(repoPath, "go.mod"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
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
	return queries, nil
}

// parsePomQueries reads pom.xml and returns PackageQuery entries for OSV lookup.
// Returns nil if no pom.xml exists or on parse error (non-fatal, logged as warning).
func parsePomQueries(repoPath string) []PackageQuery {
	data, err := FS.ReadFile(filepath.Join(repoPath, "pom.xml"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("vuln: reading pom.xml", "error", err)
		}
		return nil
	}

	queries, err := parseMavenDeps(data)
	if err != nil {
		slog.Warn("vuln: parsing pom.xml", "error", err)
		return nil
	}
	return queries
}

// parseGradleQueries reads build.gradle or build.gradle.kts and returns the
// chosen filename and PackageQuery entries for OSV lookup.
// Returns "", nil if no Gradle build file exists or on parse error (non-fatal).
func parseGradleQueries(repoPath string) (string, []PackageQuery) {
	for _, name := range []string{"build.gradle", "build.gradle.kts"} {
		data, err := FS.ReadFile(filepath.Join(repoPath, name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			slog.Warn("vuln: reading "+name, "error", err)
			return "", nil
		}

		queries, err := parseGradleDeps(data)
		if err != nil {
			slog.Warn("vuln: parsing "+name, "error", err)
			return "", nil
		}
		return name, queries
	}
	return "", nil
}

// parseCargoQueries reads Cargo.toml and returns PackageQuery entries for OSV lookup.
// Returns nil if no Cargo.toml exists or on parse error (non-fatal, logged as warning).
func parseCargoQueries(repoPath string) []PackageQuery {
	data, err := FS.ReadFile(filepath.Join(repoPath, "Cargo.toml"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("vuln: reading Cargo.toml", "error", err)
		}
		return nil
	}

	queries, err := parseCargoDeps(data)
	if err != nil {
		slog.Warn("vuln: parsing Cargo.toml", "error", err)
		return nil
	}
	return queries
}

// findCsprojFiles walks repoPath up to depth 2 and returns relative paths
// to *.csproj files. This covers root-level and one-subdirectory layouts
// (e.g., src/MyApp/MyApp.csproj) without expensive deep tree walks.
func findCsprojFiles(repoPath string) []string {
	var files []string
	_ = FS.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip inaccessible paths
		}

		rel, relErr := filepath.Rel(repoPath, path)
		if relErr != nil {
			return nil
		}

		depth := strings.Count(rel, string(filepath.Separator))
		if d.IsDir() && depth >= 2 {
			return fs.SkipDir
		}

		if !d.IsDir() && strings.HasSuffix(d.Name(), ".csproj") {
			files = append(files, rel)
		}

		return nil
	})
	return files
}

// parseCsprojQueries discovers .csproj files in repoPath, parses each for
// NuGet package references, and returns an aggregate display filename and
// combined queries (deduplicated by package name).
func parseCsprojQueries(repoPath string) (string, []PackageQuery) {
	csprojFiles := findCsprojFiles(repoPath)
	if len(csprojFiles) == 0 {
		return "", nil
	}

	seen := make(map[string]bool)
	var queries []PackageQuery

	for _, f := range csprojFiles {
		data, err := FS.ReadFile(filepath.Join(repoPath, f))
		if err != nil {
			slog.Warn("vuln: reading "+f, "error", err)
			continue
		}

		parsed, err := parseCsprojDeps(data)
		if err != nil {
			slog.Warn("vuln: parsing "+f, "error", err)
			continue
		}

		for _, q := range parsed {
			if !seen[q.Name] {
				seen[q.Name] = true
				queries = append(queries, q)
			}
		}
	}

	if len(queries) == 0 {
		return "", nil
	}

	filePath := csprojFiles[0]
	if len(csprojFiles) > 1 {
		filePath = "*.csproj"
	}

	return filePath, queries
}

// parsePythonQueries reads requirements.txt and/or pyproject.toml and returns the
// chosen filename and PackageQuery entries for OSV lookup.
// Returns "", nil if no Python manifest exists or on parse error (non-fatal).
// If both files exist, requirements.txt takes precedence (it's the lockfile equivalent).
func parsePythonQueries(repoPath string) (string, []PackageQuery) {
	// Try requirements.txt first (more common, often pinned).
	data, err := FS.ReadFile(filepath.Join(repoPath, "requirements.txt"))
	if err == nil {
		queries, parseErr := parsePythonRequirements(data)
		if parseErr != nil {
			slog.Warn("vuln: parsing requirements.txt", "error", parseErr)
			return "", nil
		}
		if len(queries) > 0 {
			return "requirements.txt", queries
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		slog.Warn("vuln: reading requirements.txt", "error", err)
	}

	// Fall back to pyproject.toml.
	data, err = FS.ReadFile(filepath.Join(repoPath, "pyproject.toml"))
	if err == nil {
		queries, parseErr := parsePyprojectDeps(data)
		if parseErr != nil {
			slog.Warn("vuln: parsing pyproject.toml", "error", parseErr)
			return "", nil
		}
		if len(queries) > 0 {
			return "pyproject.toml", queries
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		slog.Warn("vuln: reading pyproject.toml", "error", err)
	}

	return "", nil
}

// parseNpmQueries reads package.json and returns PackageQuery entries for OSV lookup.
// Returns nil if no package.json exists or on parse error (non-fatal, logged as warning).
func parseNpmQueries(repoPath string) []PackageQuery {
	data, err := FS.ReadFile(filepath.Join(repoPath, "package.json"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("vuln: reading package.json", "error", err)
		}
		return nil
	}

	queries, err := parseNpmDeps(data)
	if err != nil {
		slog.Warn("vuln: parsing package.json", "error", err)
		return nil
	}
	return queries
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
