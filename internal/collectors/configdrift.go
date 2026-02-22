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
	collector.Register(&ConfigDriftCollector{})
}

// ConfigDriftMetrics holds structured metrics from the configuration drift scan.
type ConfigDriftMetrics struct {
	EnvTemplatesFound   int
	EnvVarsInCode       int
	EnvVarsInTemplates  int
	DriftSignals        int
	DeadKeySignals      int
	InconsistentSignals int
}

// ConfigDriftCollector detects configuration cruft and drift across ecosystems:
// env var references missing from templates, dead config keys, and inconsistent
// defaults across environment files.
type ConfigDriftCollector struct {
	metrics *ConfigDriftMetrics
}

// Name returns the collector name used for registration and filtering.
func (c *ConfigDriftCollector) Name() string { return "configdrift" }

// envTemplateNames are the filenames recognised as env variable templates.
var envTemplateNames = []string{".env.example", ".env.template", ".env.sample"}

// wellKnownEnvVars are common system/CI vars that should not be flagged as drift.
var wellKnownEnvVars = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "SHELL": true,
	"TERM": true, "LANG": true, "PWD": true, "GOPATH": true,
	"GOROOT": true, "NODE_ENV": true, "CI": true,
}

// wellKnownPrefix is checked for vars that start with "GITHUB_".
const wellKnownPrefix = "GITHUB_"

// isWellKnownVar returns true if the variable should be skipped.
func isWellKnownVar(name string) bool {
	return wellKnownEnvVars[name] || strings.HasPrefix(name, wellKnownPrefix)
}

// Source-code env-var patterns per ecosystem.
var (
	goEnvPatterns = []*regexp.Regexp{
		regexp.MustCompile(`os\.Getenv\("([^"]+)"\)`),
		regexp.MustCompile(`os\.LookupEnv\("([^"]+)"\)`),
	}
	nodeEnvPatterns = []*regexp.Regexp{
		regexp.MustCompile(`process\.env\.([A-Z_][A-Z0-9_]*)`),
		regexp.MustCompile(`process\.env\["([^"]+)"\]`),
		regexp.MustCompile(`process\.env\['([^']+)'\]`),
	}
	pythonEnvPatterns = []*regexp.Regexp{
		regexp.MustCompile(`os\.getenv\(["']([^"']+)["']\)`),
		regexp.MustCompile(`os\.environ(?:\.get)?\[["']([^"']+)["']\]`),
		regexp.MustCompile(`os\.environ\.get\(["']([^"']+)["']\)`),
	}
	rubyEnvPatterns = []*regexp.Regexp{
		regexp.MustCompile(`ENV\[["']([^"']+)["']\]`),
		regexp.MustCompile(`ENV\.fetch\(["']([^"']+)["']\)`),
	}
)

// envExtPatterns maps file extensions to the regex patterns that extract env var names.
var envExtPatterns = map[string][]*regexp.Regexp{
	".go":  goEnvPatterns,
	".js":  nodeEnvPatterns,
	".ts":  nodeEnvPatterns,
	".mjs": nodeEnvPatterns,
	".cjs": nodeEnvPatterns,
	".py":  pythonEnvPatterns,
	".rb":  rubyEnvPatterns,
}

// placeholderPattern matches values that are clearly placeholder text.
var placeholderPattern = regexp.MustCompile(`(?i)^(changeme|xxx+|your_.*|<.*>)$`)

// Collect walks the repository looking for env var drift, dead config keys,
// and inconsistent defaults across environment files.
func (c *ConfigDriftCollector) Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	excludes := mergeExcludes(opts.ExcludePatterns)
	metrics := &ConfigDriftMetrics{}

	gitRoot := opts.GitRoot
	if gitRoot == "" {
		gitRoot = repoPath
	}

	// Phase 1: Discover env template files in repo root.
	templates := discoverEnvTemplates(repoPath)
	metrics.EnvTemplatesFound = len(templates)

	// Parse template keys.
	templateKeys := make(map[string]bool)
	for _, tmpl := range templates {
		keys := parseEnvFile(repoPath, tmpl)
		for k := range keys {
			templateKeys[k] = true
		}
	}
	metrics.EnvVarsInTemplates = len(templateKeys)

	// Phase 2: Walk source files and extract env var references.
	codeVars := make(map[string]string) // var name → first file path
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
		patterns, ok := envExtPatterns[ext]
		if !ok {
			return nil
		}

		vars := extractEnvVars(filepath.Join(repoPath, relPath), patterns)
		for _, v := range vars {
			if _, exists := codeVars[v]; !exists {
				codeVars[v] = relPath
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking repo for env vars: %w", err)
	}

	metrics.EnvVarsInCode = len(codeVars)

	var signals []signal.RawSignal

	// Signal 1: env-var-drift — vars in code but not in any template.
	if len(templates) > 0 {
		for varName, filePath := range codeVars {
			if isWellKnownVar(varName) {
				continue
			}
			if templateKeys[varName] {
				continue
			}
			conf := 0.5
			if conf >= opts.MinConfidence {
				signals = append(signals, signal.RawSignal{
					Source:     "configdrift",
					Kind:       "env-var-drift",
					FilePath:   filePath,
					Title:      fmt.Sprintf("Env var %s used in code but missing from template", varName),
					Confidence: conf,
					Tags:       []string{"config", "env-drift"},
				})
				metrics.DriftSignals++
			}
		}
	}

	// Signal 2: dead-config-key — template keys not referenced in any source file.
	if len(templateKeys) > 0 {
		deadKeys := findDeadConfigKeys(ctx, repoPath, templateKeys, excludes, opts)
		for _, dk := range deadKeys {
			conf := 0.4
			if conf >= opts.MinConfidence {
				signals = append(signals, signal.RawSignal{
					Source:     "configdrift",
					Kind:       "dead-config-key",
					FilePath:   dk.templateFile,
					Title:      fmt.Sprintf("Config key %s defined in %s but never referenced in source", dk.key, dk.templateFile),
					Confidence: conf,
					Tags:       []string{"config", "dead-key"},
				})
				metrics.DeadKeySignals++
			}
		}
	}

	// Signal 3: inconsistent-defaults — same key, different non-placeholder values.
	inconsistent := findInconsistentDefaults(repoPath)
	for _, inc := range inconsistent {
		conf := 0.3
		if conf >= opts.MinConfidence {
			signals = append(signals, signal.RawSignal{
				Source:     "configdrift",
				Kind:       "inconsistent-defaults",
				FilePath:   inc.files[0],
				Title:      fmt.Sprintf("Key %s has inconsistent values across env files: %s", inc.key, strings.Join(inc.files, ", ")),
				Confidence: conf,
				Tags:       []string{"config", "inconsistent"},
			})
			metrics.InconsistentSignals++
		}
	}

	c.metrics = metrics

	// Enrich timestamps from git log.
	enrichTimestamps(ctx, gitRoot, signals)

	return signals, nil
}

// discoverEnvTemplates returns relative paths of env template files in the repo root.
func discoverEnvTemplates(repoPath string) []string {
	var found []string
	for _, name := range envTemplateNames {
		absPath := filepath.Join(repoPath, name)
		if _, err := FS.Stat(absPath); err == nil {
			found = append(found, name)
		}
	}
	return found
}

// parseEnvFile reads a dotenv-style file and returns key→value pairs.
// Lines starting with # and blank lines are skipped.
func parseEnvFile(repoPath, relPath string) map[string]string {
	absPath := filepath.Join(repoPath, relPath)
	f, err := FS.Open(absPath)
	if err != nil {
		return nil
	}
	defer f.Close() //nolint:errcheck // read-only file

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		result[key] = val
	}
	return result
}

// extractEnvVars scans a file for env var references using the given patterns.
func extractEnvVars(absPath string, patterns []*regexp.Regexp) []string {
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
					seen[m[1]] = true
				}
			}
		}
	}

	vars := make([]string, 0, len(seen))
	for v := range seen {
		vars = append(vars, v)
	}
	return vars
}

// deadKey describes a config key that exists in a template but is not referenced
// in any source file.
type deadKey struct {
	key          string
	templateFile string
}

// findDeadConfigKeys checks each template key against all source files in the repo.
func findDeadConfigKeys(ctx context.Context, repoPath string, templateKeys map[string]bool, excludes []string, opts signal.CollectorOpts) []deadKey {
	// Build per-key reference found map.
	keyFound := make(map[string]bool, len(templateKeys))

	_ = FS.WalkDir(repoPath, func(path string, d os.DirEntry, walkErr error) error { //nolint:errcheck // best-effort directory scan; empty result on failure is acceptable
		if walkErr != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
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

		// Skip env files themselves.
		base := filepath.Base(relPath)
		if strings.HasPrefix(base, ".env") {
			return nil
		}

		// Read file and search for each unresolved key.
		data, err := FS.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)

		for key := range templateKeys {
			if keyFound[key] {
				continue
			}
			if strings.Contains(content, key) {
				keyFound[key] = true
			}
		}

		return nil
	})

	// Find which template file each dead key comes from.
	var result []deadKey
	for key := range templateKeys {
		if keyFound[key] {
			continue
		}
		// Find which template defines this key.
		tmplFile := ""
		for _, name := range envTemplateNames {
			keys := parseEnvFile(repoPath, name)
			if _, ok := keys[key]; ok {
				tmplFile = name
				break
			}
		}
		if tmplFile == "" {
			tmplFile = envTemplateNames[0] // fallback
		}
		result = append(result, deadKey{key: key, templateFile: tmplFile})
	}

	return result
}

// inconsistentKey describes a config key with different values across env files.
type inconsistentKey struct {
	key   string
	files []string
}

// envFileGlobPatterns are the patterns used to discover all env files.
var envFileGlobPatterns = []string{
	".env.example", ".env.template", ".env.sample",
	".env.dev", ".env.staging", ".env.prod", ".env.test",
	".env.development", ".env.production", ".env.local",
}

// findInconsistentDefaults collects all .env* files, parses them, and reports
// keys that have different non-placeholder values across files.
func findInconsistentDefaults(repoPath string) []inconsistentKey {
	// Discover env files.
	type envEntry struct {
		file  string
		key   string
		value string
	}

	// key → list of (file, value) where value is non-placeholder and non-empty.
	keyValues := make(map[string][]envEntry)

	for _, name := range envFileGlobPatterns {
		absPath := filepath.Join(repoPath, name)
		if _, err := FS.Stat(absPath); err != nil {
			continue
		}
		kv := parseEnvFile(repoPath, name)
		for k, v := range kv {
			// Skip empty or placeholder values.
			if v == "" || placeholderPattern.MatchString(v) {
				continue
			}
			keyValues[k] = append(keyValues[k], envEntry{file: name, key: k, value: v})
		}
	}

	var result []inconsistentKey
	for key, entries := range keyValues {
		if len(entries) < 2 {
			continue
		}
		// Check if any values differ.
		first := entries[0].value
		differ := false
		for _, e := range entries[1:] {
			if e.value != first {
				differ = true
				break
			}
		}
		if !differ {
			continue
		}

		files := make([]string, len(entries))
		for i, e := range entries {
			files[i] = e.file
		}
		result = append(result, inconsistentKey{key: key, files: files})
	}

	return result
}

// Metrics returns structured metrics from the config drift scan.
func (c *ConfigDriftCollector) Metrics() any { return c.metrics }

// Compile-time interface checks.
var _ collector.Collector = (*ConfigDriftCollector)(nil)
var _ collector.MetricsProvider = (*ConfigDriftCollector)(nil)
