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

// DepHealthMetrics holds structured dependency data parsed from go.mod.
type DepHealthMetrics struct {
	ModulePath   string
	GoVersion    string
	Dependencies []ModuleDep
	Replaces     []ModuleReplace
	Retracts     []ModuleRetract
	Archived     []string
	Deprecated   []string
	Stale        []string
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

// DepHealthCollector parses go.mod to extract dependency information and
// emits signals for local replace directives, retracted versions, archived
// repos, deprecated modules, and stale dependencies.
type DepHealthCollector struct {
	metrics     *DepHealthMetrics
	ghAPI       dephealthGitHubAPI
	proxyClient moduleProxyClient
}

// Name returns the collector name used for registration and filtering.
func (c *DepHealthCollector) Name() string { return "dephealth" }

// Collect parses the go.mod file in repoPath and returns signals for
// actionable findings (local replaces, retracted versions, archived repos,
// deprecated modules, stale dependencies).
func (c *DepHealthCollector) Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error) {
	goModPath := filepath.Join(repoPath, "go.mod")

	data, err := FS.ReadFile(goModPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Info("no go.mod found, skipping dephealth collector")
			return nil, nil
		}
		return nil, fmt.Errorf("reading go.mod: %w", err)
	}

	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing go.mod: %w", err)
	}

	metrics := &DepHealthMetrics{}
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

	c.metrics = metrics
	return signals, nil
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
