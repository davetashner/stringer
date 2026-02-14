// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-github/v68/github"
	"golang.org/x/mod/modfile"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

func init() {
	collector.Register(&DepHealthCollector{})
}

// DepHealthMetrics holds structured dependency data parsed from manifests.
type DepHealthMetrics struct {
	ModulePath   string
	GoVersion    string
	Dependencies []ModuleDep
	Replaces     []ModuleReplace
	Retracts     []ModuleRetract
	Archived     []string
	Deprecated   []string
	Stale        []string
	Yanked       []string
	Ecosystems   []string // ecosystems detected (e.g., "go", "npm", "cargo")
}

// ModuleDep represents a single require directive.
type ModuleDep struct {
	Path     string
	Version  string
	Indirect bool
}

// ModuleReplace represents a single replace directive.
type ModuleReplace struct {
	OldPath    string
	OldVersion string
	NewPath    string
	NewVersion string
	IsLocal    bool
}

// ModuleRetract represents a single retract directive.
type ModuleRetract struct {
	Low       string
	High      string
	Rationale string
}

// DepHealthCollector parses dependency manifests (go.mod, package.json,
// Cargo.toml, pom.xml, *.csproj, requirements.txt, pyproject.toml) to extract
// dependency information and emits signals for deprecated, yanked, archived,
// and stale dependencies across multiple ecosystems.
type DepHealthCollector struct {
	metrics      *DepHealthMetrics
	ghAPI        dephealthGitHubAPI
	proxyClient  moduleProxyClient
	npmClient    npmRegistryClient
	cratesClient cratesRegistryClient
	mavenClient  mavenRegistryClient
	nugetClient  nugetRegistryClient
	pypiClient   pypiRegistryClient
}

// Name returns the collector name used for registration and filtering.
func (c *DepHealthCollector) Name() string { return "dephealth" }

// Collect parses dependency manifests in repoPath and returns signals for
// actionable findings (local replaces, retracted versions, archived repos,
// deprecated modules, yanked versions, stale dependencies) across Go, npm,
// Cargo, Maven, NuGet, and Python ecosystems.
func (c *DepHealthCollector) Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	metrics := &DepHealthMetrics{}
	var signals []signal.RawSignal

	// --- Go ecosystem (go.mod) ---
	goSignals, err := c.collectGoHealth(ctx, repoPath, opts, metrics)
	if err != nil {
		return nil, err
	}
	signals = append(signals, goSignals...)

	// --- npm ecosystem (package.json) ---
	npmSignals := c.collectNpmHealth(ctx, repoPath, metrics)
	signals = append(signals, npmSignals...)

	// --- Rust/Cargo ecosystem (Cargo.toml) ---
	cargoSignals := c.collectCargoHealth(ctx, repoPath, metrics)
	signals = append(signals, cargoSignals...)

	// --- Java/Maven ecosystem (pom.xml) ---
	mavenSignals := c.collectMavenHealth(ctx, repoPath, metrics)
	signals = append(signals, mavenSignals...)

	// --- C#/NuGet ecosystem (*.csproj) ---
	nugetSignals := c.collectNuGetHealth(ctx, repoPath, metrics)
	signals = append(signals, nugetSignals...)

	// --- Python/PyPI ecosystem (requirements.txt, pyproject.toml) ---
	pypiSignals := c.collectPyPIHealth(ctx, repoPath, metrics)
	signals = append(signals, pypiSignals...)

	// If no ecosystems found at all, return nil.
	if len(metrics.Ecosystems) == 0 {
		slog.Info("no dependency manifests found, skipping dephealth collector")
		return nil, nil
	}

	c.metrics = metrics
	return signals, nil
}

// collectGoHealth handles go.mod parsing and Go-specific dependency checks.
func (c *DepHealthCollector) collectGoHealth(ctx context.Context, repoPath string, opts signal.CollectorOpts, metrics *DepHealthMetrics) ([]signal.RawSignal, error) {
	goModPath := filepath.Join(repoPath, "go.mod")

	data, err := FS.ReadFile(goModPath)
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

	metrics.Ecosystems = append(metrics.Ecosystems, "go")

	if f.Module != nil {
		metrics.ModulePath = f.Module.Mod.Path
	}
	if f.Go != nil {
		metrics.GoVersion = f.Go.Version
	}

	// Extract dependencies.
	for _, req := range f.Require {
		metrics.Dependencies = append(metrics.Dependencies, ModuleDep{
			Path:     req.Mod.Path,
			Version:  req.Mod.Version,
			Indirect: req.Indirect,
		})
	}

	var signals []signal.RawSignal

	// Extract replace directives.
	for _, rep := range f.Replace {
		local := isLocalPath(rep.New.Path)
		metrics.Replaces = append(metrics.Replaces, ModuleReplace{
			OldPath:    rep.Old.Path,
			OldVersion: rep.Old.Version,
			NewPath:    rep.New.Path,
			NewVersion: rep.New.Version,
			IsLocal:    local,
		})

		if local {
			line := 0
			if rep.Syntax != nil {
				line = rep.Syntax.Start.Line
			}
			signals = append(signals, signal.RawSignal{
				Source:      "dephealth",
				Kind:        "local-replace",
				FilePath:    "go.mod",
				Line:        line,
				Title:       fmt.Sprintf("Local replace: %s => %s", rep.Old.Path, rep.New.Path),
				Description: fmt.Sprintf("Replace directive points to local path %q. This makes the build non-portable — other developers and CI cannot reproduce it without the same local directory layout.", rep.New.Path),
				Confidence:  0.5,
				Tags:        []string{"local-replace", "dephealth"},
			})
		}
	}

	// Extract retract directives.
	for _, ret := range f.Retract {
		var versionStr string
		if ret.Low == ret.High {
			versionStr = ret.Low
		} else {
			versionStr = fmt.Sprintf("[%s, %s]", ret.Low, ret.High)
		}

		metrics.Retracts = append(metrics.Retracts, ModuleRetract{
			Low:       ret.Low,
			High:      ret.High,
			Rationale: ret.Rationale,
		})

		line := 0
		if ret.Syntax != nil {
			line = ret.Syntax.Start.Line
		}

		desc := fmt.Sprintf("Module retracts version %s.", versionStr)
		if ret.Rationale != "" {
			desc += fmt.Sprintf(" Reason: %s", ret.Rationale)
		}

		signals = append(signals, signal.RawSignal{
			Source:      "dephealth",
			Kind:        "retracted-version",
			FilePath:    "go.mod",
			Line:        line,
			Title:       fmt.Sprintf("Retracted version: %s", versionStr),
			Description: desc,
			Confidence:  0.3,
			Tags:        []string{"retracted-version", "dephealth"},
		})
	}

	// C6.2 + C6.4: Check GitHub repos for archived/stale status.
	ghAPI := c.ghAPI
	if ghAPI == nil {
		token := os.Getenv("GITHUB_TOKEN")
		if token != "" {
			client := github.NewClient(nil).WithAuthToken(token)
			ghAPI = &realGitHubAPI{client: client}
		} else {
			slog.Info("GITHUB_TOKEN not set, skipping dephealth GitHub checks")
		}
	}
	if ghAPI != nil {
		threshold := defaultStalenessThreshold
		if opts.StalenessThreshold != "" {
			if d, err := ParseDuration(opts.StalenessThreshold); err == nil {
				threshold = d
			} else {
				slog.Warn("invalid staleness-threshold, using default", "value", opts.StalenessThreshold, "error", err)
			}
		}
		ghSignals := checkGitHubDeps(ctx, ghAPI, metrics.Dependencies, threshold)
		for _, s := range ghSignals {
			switch s.Kind {
			case "archived-dependency":
				metrics.Archived = append(metrics.Archived, s.Title)
			case "stale-dependency":
				metrics.Stale = append(metrics.Stale, s.Title)
			}
		}
		signals = append(signals, ghSignals...)
	}

	// C6.3: Check Go module proxy for deprecated modules.
	proxyClient := c.proxyClient
	if proxyClient == nil {
		proxyClient = &realModuleProxyClient{}
	}
	deprecatedSignals := checkDeprecatedDeps(ctx, proxyClient, metrics.Dependencies)
	for _, s := range deprecatedSignals {
		metrics.Deprecated = append(metrics.Deprecated, s.Title)
	}
	signals = append(signals, deprecatedSignals...)

	return signals, nil
}

// collectNpmHealth parses package.json and checks the npm registry for deprecated packages.
func (c *DepHealthCollector) collectNpmHealth(ctx context.Context, repoPath string, metrics *DepHealthMetrics) []signal.RawSignal {
	data, err := FS.ReadFile(filepath.Join(repoPath, "package.json"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("dephealth: reading package.json", "error", err)
		}
		return nil
	}

	deps, err := parseNpmDeps(data)
	if err != nil {
		slog.Warn("dephealth: parsing package.json", "error", err)
		return nil
	}
	if len(deps) == 0 {
		return nil
	}

	metrics.Ecosystems = append(metrics.Ecosystems, "npm")

	client := c.npmClient
	if client == nil {
		client = &realNpmRegistryClient{}
	}

	npmSignals := checkNpmDeps(ctx, client, deps, "package.json")
	for _, s := range npmSignals {
		metrics.Deprecated = append(metrics.Deprecated, s.Title)
	}
	return npmSignals
}

// collectCargoHealth parses Cargo.toml and checks crates.io for yanked crates.
func (c *DepHealthCollector) collectCargoHealth(ctx context.Context, repoPath string, metrics *DepHealthMetrics) []signal.RawSignal {
	data, err := FS.ReadFile(filepath.Join(repoPath, "Cargo.toml"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("dephealth: reading Cargo.toml", "error", err)
		}
		return nil
	}

	deps, err := parseCargoDeps(data)
	if err != nil {
		slog.Warn("dephealth: parsing Cargo.toml", "error", err)
		return nil
	}
	if len(deps) == 0 {
		return nil
	}

	metrics.Ecosystems = append(metrics.Ecosystems, "cargo")

	client := c.cratesClient
	if client == nil {
		client = &realCratesRegistryClient{}
	}

	cargoSignals := checkCratesDeps(ctx, client, deps)
	for _, s := range cargoSignals {
		metrics.Yanked = append(metrics.Yanked, s.Title)
	}
	return cargoSignals
}

// collectMavenHealth parses pom.xml and checks Maven Central for stale artifacts.
func (c *DepHealthCollector) collectMavenHealth(ctx context.Context, repoPath string, metrics *DepHealthMetrics) []signal.RawSignal {
	data, err := FS.ReadFile(filepath.Join(repoPath, "pom.xml"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("dephealth: reading pom.xml", "error", err)
		}
		return nil
	}

	deps, err := parseMavenDeps(data)
	if err != nil {
		slog.Warn("dephealth: parsing pom.xml", "error", err)
		return nil
	}
	if len(deps) == 0 {
		return nil
	}

	metrics.Ecosystems = append(metrics.Ecosystems, "maven")

	client := c.mavenClient
	if client == nil {
		client = &realMavenRegistryClient{}
	}

	mavenSignals := checkMavenDeps(ctx, client, deps, "pom.xml")
	for _, s := range mavenSignals {
		metrics.Stale = append(metrics.Stale, s.Title)
	}
	return mavenSignals
}

// collectNuGetHealth parses .csproj files and checks NuGet for deprecated packages.
func (c *DepHealthCollector) collectNuGetHealth(ctx context.Context, repoPath string, metrics *DepHealthMetrics) []signal.RawSignal {
	filePath, deps := parseCsprojQueries(repoPath)
	if len(deps) == 0 {
		return nil
	}

	metrics.Ecosystems = append(metrics.Ecosystems, "nuget")

	client := c.nugetClient
	if client == nil {
		client = &realNuGetRegistryClient{}
	}

	nugetSignals := checkNuGetDeps(ctx, client, deps, filePath)
	for _, s := range nugetSignals {
		metrics.Deprecated = append(metrics.Deprecated, s.Title)
	}
	return nugetSignals
}

// collectPyPIHealth parses Python manifests and checks PyPI for deprecated packages.
func (c *DepHealthCollector) collectPyPIHealth(ctx context.Context, repoPath string, metrics *DepHealthMetrics) []signal.RawSignal {
	filePath, deps := parsePythonQueries(repoPath)
	if len(deps) == 0 {
		return nil
	}

	metrics.Ecosystems = append(metrics.Ecosystems, "python")

	client := c.pypiClient
	if client == nil {
		client = &realPyPIRegistryClient{}
	}

	pypiSignals := checkPyPIDeps(ctx, client, deps, filePath)
	for _, s := range pypiSignals {
		metrics.Deprecated = append(metrics.Deprecated, s.Title)
	}
	return pypiSignals
}

// Metrics returns structured dependency data from the last Collect call.
func (c *DepHealthCollector) Metrics() any { return c.metrics }

// isLocalPath returns true if the path is a local filesystem reference.
func isLocalPath(p string) bool {
	return strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/")
}

// Compile-time interface checks.
var _ collector.Collector = (*DepHealthCollector)(nil)
var _ collector.MetricsProvider = (*DepHealthCollector)(nil)
