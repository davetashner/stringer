// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

func init() {
	collector.Register(&APIDriftCollector{})
}

// APIDriftMetrics holds structured metrics from the API drift scan.
type APIDriftMetrics struct {
	SpecFilesFound      int
	RoutesInSpec        int
	RoutesInCode        int
	UndocumentedRoutes  int
	UnimplementedRoutes int
	StaleVersionRoutes  int
}

// APIDriftCollector detects drift between OpenAPI/Swagger specs and route
// handler registrations in code.
type APIDriftCollector struct {
	metrics *APIDriftMetrics
}

// Name returns the collector name used for registration and filtering.
func (c *APIDriftCollector) Name() string { return "apidrift" }

// specFileNames are filenames recognized as API specification files.
var specFileNames = []string{
	"openapi.yaml", "openapi.yml", "openapi.json",
	"swagger.yaml", "swagger.yml", "swagger.json",
	"api-spec.yaml", "api-spec.yml", "api-spec.json",
	"api.yaml", "api.yml", "api.json",
}

// Route registration patterns per language/framework.
var (
	// Go: http.HandleFunc, .Handle, gin/echo/chi .GET/.POST/etc.
	goRoutePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?:http\.HandleFunc|\.HandleFunc|\.Handle)\(\s*"([^"]+)"`),
		regexp.MustCompile(`\.(?:GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\(\s*"([^"]+)"`),
		regexp.MustCompile(`(?:r|router|mux|e|g|app)\.(?:Get|Post|Put|Delete|Patch|Head|Options)\(\s*"([^"]+)"`),
	}

	// JS/TS: Express app.get/post/etc., router.get/post/etc.
	jsRoutePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?:app|router)\.(?:get|post|put|delete|patch|head|options|all)\(\s*["']([^"']+)["']`),
	}

	// Python: Flask @route, FastAPI @app.get, Django path()/url().
	pythonRoutePatterns = []*regexp.Regexp{
		regexp.MustCompile(`@(?:app|blueprint|bp)\.(?:route|get|post|put|delete|patch)\(\s*["']([^"']+)["']`),
		regexp.MustCompile(`(?:path|url)\(\s*["']([^"']+)["']`),
	}
)

// routeExtPatterns maps file extensions to route registration regex patterns.
var routeExtPatterns = map[string][]*regexp.Regexp{
	".go":  goRoutePatterns,
	".js":  jsRoutePatterns,
	".ts":  jsRoutePatterns,
	".mjs": jsRoutePatterns,
	".cjs": jsRoutePatterns,
	".jsx": jsRoutePatterns,
	".tsx": jsRoutePatterns,
	".py":  pythonRoutePatterns,
}

// regexMetaChars are characters that indicate a Django re_path() pattern.
const regexMetaChars = `^$?*+[(|`

// specPathYAMLPatterns match route paths in YAML spec files under the paths: section.
// Matches lines like "  /users/{id}:" or "  '/api/v1/items':"
var specPathYAMLPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\s{1,4}(/[^"'\s:]+)\s*:`), // unquoted: /users:
	regexp.MustCompile(`^\s{1,4}"(/[^"]+)"\s*:`),   // double-quoted: "/users":
	regexp.MustCompile(`^\s{1,4}'(/[^']+)'\s*:`),   // single-quoted: '/users':
}

// specPathJSONPattern matches route paths in JSON spec files.
// Matches lines like:   "/users/{id}": {
var specPathJSONPattern = regexp.MustCompile(`^\s*"(/[^"]+)"\s*:\s*\{`)

// versionPrefixPattern extracts API version prefix from a route path.
var versionPrefixPattern = regexp.MustCompile(`^(/v\d+)`)

// paramPatterns normalize different param styles to {param}.
var paramReplacements = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`:([a-zA-Z_]\w*)`), "{$1}"},      // :id → {id}
	{regexp.MustCompile(`<([a-zA-Z_]\w*)>`), "{$1}"},     // <id> → {id}
	{regexp.MustCompile(`<\w+:([a-zA-Z_]\w*)>`), "{$1}"}, // <int:id> → {id}
}

// normalizeRoute standardizes a route path for comparison.
func normalizeRoute(route string) string {
	route = strings.TrimRight(route, "/")
	if route == "" {
		route = "/"
	}
	for _, pr := range paramReplacements {
		route = pr.re.ReplaceAllString(route, pr.repl)
	}
	route = strings.ToLower(route)
	return route
}

// isRegexRoute returns true if the route contains regex metacharacters
// (indicates a Django re_path pattern that should be skipped).
func isRegexRoute(route string) bool {
	return strings.ContainsAny(route, regexMetaChars)
}

// Collect walks the repository to detect API contract drift between spec files
// and route handler registrations in code.
func (c *APIDriftCollector) Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	excludes := mergeExcludes(opts.ExcludePatterns)
	metrics := &APIDriftMetrics{}

	gitRoot := opts.GitRoot
	if gitRoot == "" {
		gitRoot = repoPath
	}

	// Phase 1: Discover spec files and extract route paths.
	specRoutes := make(map[string]string) // normalized route → spec file
	specFiles := discoverSpecFiles(repoPath)
	metrics.SpecFilesFound = len(specFiles)

	if len(specFiles) == 0 {
		c.metrics = metrics
		return nil, nil
	}

	for _, sf := range specFiles {
		routes := extractSpecRoutes(filepath.Join(repoPath, sf))
		for _, r := range routes {
			norm := normalizeRoute(r)
			if _, exists := specRoutes[norm]; !exists {
				specRoutes[norm] = sf
			}
		}
	}
	metrics.RoutesInSpec = len(specRoutes)

	// Phase 2: Walk source files and extract code route registrations.
	codeRoutes := make(map[string]string) // normalized route → source file
	err := FS.WalkDir(repoPath, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		relPath, relErr := filepath.Rel(repoPath, path)
		if relErr != nil {
			return nil
		}

		if d.IsDir() {
			if shouldExclude(relPath, excludes) {
				return filepath.SkipDir
			}
			return nil
		}

		if shouldExclude(relPath, excludes) {
			return nil
		}

		if len(opts.IncludePatterns) > 0 && !matchesAny(relPath, opts.IncludePatterns) {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(relPath))

		// Check for Next.js file-path routes (pages/api/ convention).
		if isNextJSAPIRoute(relPath) {
			route := nextJSFileToRoute(relPath)
			norm := normalizeRoute(route)
			if _, exists := codeRoutes[norm]; !exists {
				codeRoutes[norm] = relPath
			}
		}

		patterns, ok := routeExtPatterns[ext]
		if !ok {
			return nil
		}

		routes := extractCodeRoutes(filepath.Join(repoPath, relPath), patterns)
		for _, r := range routes {
			if !strings.HasPrefix(r, "/") {
				continue
			}
			if isRegexRoute(r) {
				continue
			}
			norm := normalizeRoute(r)
			if _, exists := codeRoutes[norm]; !exists {
				codeRoutes[norm] = relPath
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking repo for route handlers: %w", err)
	}

	metrics.RoutesInCode = len(codeRoutes)

	// Phase 3: Compare and emit signals.
	var signals []signal.RawSignal

	// Signal 1: undocumented-route — in code but not in spec.
	for route, filePath := range codeRoutes {
		if specRoutes[route] != "" {
			continue
		}
		// Check with param generalization: code route /users/{id} matches spec /users/{param}.
		if matchesSpecWithParams(route, specRoutes) {
			continue
		}
		conf := 0.6
		if conf >= opts.MinConfidence {
			signals = append(signals, signal.RawSignal{
				Source:     "apidrift",
				Kind:       "undocumented-route",
				FilePath:   filePath,
				Title:      fmt.Sprintf("Route %s registered in code but missing from API spec", route),
				Confidence: conf,
				Tags:       []string{"api", "undocumented"},
			})
			metrics.UndocumentedRoutes++
		}
	}

	// Signal 2: unimplemented-route — in spec but not in code.
	for route, specFile := range specRoutes {
		if codeRoutes[route] != "" {
			continue
		}
		if matchesCodeWithParams(route, codeRoutes) {
			continue
		}
		conf := 0.5
		if conf >= opts.MinConfidence {
			signals = append(signals, signal.RawSignal{
				Source:     "apidrift",
				Kind:       "unimplemented-route",
				FilePath:   specFile,
				Title:      fmt.Sprintf("Route %s defined in spec but no handler found in code", route),
				Confidence: conf,
				Tags:       []string{"api", "unimplemented"},
			})
			metrics.UnimplementedRoutes++
		}
	}

	// Signal 3: stale-api-version — code routes use older version prefix.
	staleSignals := detectStaleVersions(specRoutes, codeRoutes, opts.MinConfidence)
	signals = append(signals, staleSignals...)
	metrics.StaleVersionRoutes = len(staleSignals)

	c.metrics = metrics

	// Enrich timestamps from git log.
	enrichTimestamps(ctx, gitRoot, signals)

	return signals, nil
}

// discoverSpecFiles returns relative paths of API spec files found in the repo.
func discoverSpecFiles(repoPath string) []string {
	var found []string
	// Check repo root and common subdirectories.
	searchDirs := []string{"", "docs", "api", "spec"}
	for _, dir := range searchDirs {
		for _, name := range specFileNames {
			relPath := filepath.Join(dir, name)
			absPath := filepath.Join(repoPath, relPath)
			if _, err := FS.Stat(absPath); err == nil {
				found = append(found, relPath)
			}
		}
	}
	return found
}

// extractSpecRoutes reads a spec file and returns route paths found in it.
func extractSpecRoutes(absPath string) []string {
	f, err := FS.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck // read-only file

	ext := strings.ToLower(filepath.Ext(absPath))
	isJSON := ext == ".json"

	var routes []string
	inPaths := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		if isJSON {
			if m := specPathJSONPattern.FindStringSubmatch(line); m != nil {
				routes = append(routes, m[1])
			}
		} else {
			// YAML: detect paths: section.
			trimmed := strings.TrimSpace(line)
			if trimmed == "paths:" {
				inPaths = true
				continue
			}
			// A top-level key (no indent, ends with :) exits the paths section.
			if inPaths && len(line) > 0 && line[0] != ' ' && line[0] != '\t' && strings.HasSuffix(trimmed, ":") {
				inPaths = false
				continue
			}
			if inPaths {
				for _, pat := range specPathYAMLPatterns {
					if m := pat.FindStringSubmatch(line); m != nil {
						routes = append(routes, m[1])
						break
					}
				}
			}
		}
	}

	return routes
}

// extractCodeRoutes scans a source file for route registrations.
func extractCodeRoutes(absPath string, patterns []*regexp.Regexp) []string {
	f, err := FS.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck // read-only file

	seen := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		for _, pat := range patterns {
			matches := pat.FindAllStringSubmatch(line, -1)
			for _, m := range matches {
				if len(m) > 1 && m[1] != "" {
					if !seen[m[1]] {
						seen[m[1]] = true
					}
				}
			}
		}
	}

	routes := make([]string, 0, len(seen))
	for r := range seen {
		routes = append(routes, r)
	}
	return routes
}

// isNextJSAPIRoute checks if a file path matches Next.js pages/api/ convention.
func isNextJSAPIRoute(relPath string) bool {
	normalized := filepath.ToSlash(relPath)
	if !strings.HasPrefix(normalized, "pages/api/") {
		return false
	}
	ext := filepath.Ext(relPath)
	return ext == ".js" || ext == ".ts" || ext == ".jsx" || ext == ".tsx"
}

// nextJSFileToRoute converts a Next.js pages/api/ file path to a route.
// e.g., pages/api/users/[id].ts → /api/users/{id}
func nextJSFileToRoute(relPath string) string {
	normalized := filepath.ToSlash(relPath)
	// Strip "pages" prefix and extension.
	route := strings.TrimPrefix(normalized, "pages")
	route = strings.TrimSuffix(route, filepath.Ext(route))
	// Remove /index suffix.
	route = strings.TrimSuffix(route, "/index")
	// Convert [param] to {param}.
	route = strings.NewReplacer("[", "{", "]", "}").Replace(route)
	if route == "" {
		route = "/"
	}
	return route
}

// matchesSpecWithParams checks if a code route matches any spec route when
// param names are generalized (e.g., /users/{id} matches /users/{userId}).
func matchesSpecWithParams(codeRoute string, specRoutes map[string]string) bool {
	generalized := generalizeParams(codeRoute)
	for specRoute := range specRoutes {
		if generalizeParams(specRoute) == generalized {
			return true
		}
	}
	return false
}

// matchesCodeWithParams checks if a spec route matches any code route when
// param names are generalized.
func matchesCodeWithParams(specRoute string, codeRoutes map[string]string) bool {
	generalized := generalizeParams(specRoute)
	for codeRoute := range codeRoutes {
		if generalizeParams(codeRoute) == generalized {
			return true
		}
	}
	return false
}

// generalizeParamPattern replaces all {name} params with {param} for comparison.
var generalizeParamPattern = regexp.MustCompile(`\{[^}]+\}`)

// generalizeParams replaces all param placeholders with a generic {param}.
func generalizeParams(route string) string {
	return generalizeParamPattern.ReplaceAllString(route, "{param}")
}

// detectStaleVersions finds code routes using an older API version prefix
// than what the spec declares.
func detectStaleVersions(specRoutes map[string]string, codeRoutes map[string]string, minConf float64) []signal.RawSignal {
	// Find the highest version prefix in spec routes.
	specMaxVersion := 0
	for route := range specRoutes {
		if v := extractVersionNum(route); v > specMaxVersion {
			specMaxVersion = v
		}
	}

	if specMaxVersion == 0 {
		return nil
	}

	var signals []signal.RawSignal
	seenVersions := make(map[int]bool)
	for route, filePath := range codeRoutes {
		v := extractVersionNum(route)
		if v == 0 || v >= specMaxVersion {
			continue
		}
		if seenVersions[v] {
			continue
		}
		seenVersions[v] = true

		conf := 0.7
		if conf >= minConf {
			signals = append(signals, signal.RawSignal{
				Source:     "apidrift",
				Kind:       "stale-api-version",
				FilePath:   filePath,
				Title:      fmt.Sprintf("Code uses /v%d/ routes but spec declares /v%d/", v, specMaxVersion),
				Confidence: conf,
				Tags:       []string{"api", "stale-version"},
			})
		}
	}

	return signals
}

// extractVersionNum extracts the version number from a route's /vN/ prefix.
func extractVersionNum(route string) int {
	m := versionPrefixPattern.FindStringSubmatch(route)
	if m == nil {
		return 0
	}
	// Parse the digit(s) after /v.
	numStr := strings.TrimPrefix(m[1], "/v")
	var n int
	for _, ch := range numStr {
		if ch >= '0' && ch <= '9' {
			n = n*10 + int(ch-'0')
		}
	}
	return n
}

// Metrics returns structured metrics from the API drift scan.
func (c *APIDriftCollector) Metrics() any { return c.metrics }

// Compile-time interface checks.
var _ collector.Collector = (*APIDriftCollector)(nil)
var _ collector.MetricsProvider = (*APIDriftCollector)(nil)
