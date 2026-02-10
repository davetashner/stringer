package mcpserver

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/davetashner/stringer/internal/beads"
	"github.com/davetashner/stringer/internal/collector"
	_ "github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/config"
	strcontext "github.com/davetashner/stringer/internal/context"
	"github.com/davetashner/stringer/internal/docs"
	"github.com/davetashner/stringer/internal/output"
	"github.com/davetashner/stringer/internal/pipeline"
	"github.com/davetashner/stringer/internal/report"
	"github.com/davetashner/stringer/internal/signal"
	"github.com/davetashner/stringer/internal/state"
)

// ScanInput is the input schema for the stringer scan MCP tool.
type ScanInput struct {
	Path          string  `json:"path" jsonschema:"Repository path to scan (defaults to current directory)"`
	Collectors    string  `json:"collectors,omitempty" jsonschema:"Comma-separated list of collectors to run (default: all)"`
	Format        string  `json:"format,omitempty" jsonschema:"Output format: json, beads, markdown, tasks (default: json)"`
	MaxIssues     int     `json:"max_issues,omitempty" jsonschema:"Cap output count (0 = unlimited)"`
	MinConfidence float64 `json:"min_confidence,omitempty" jsonschema:"Filter signals below this confidence threshold (0.0-1.0)"`
	Kind          string  `json:"kind,omitempty" jsonschema:"Filter signals by kind (comma-separated)"`
	GitDepth      int     `json:"git_depth,omitempty" jsonschema:"Max commits to examine (default 1000)"`
	GitSince      string  `json:"git_since,omitempty" jsonschema:"Only examine commits after this duration (e.g. 90d, 6m, 1y)"`
}

// ReportInput is the input schema for the stringer report MCP tool.
type ReportInput struct {
	Path       string `json:"path" jsonschema:"Repository path to analyze (defaults to current directory)"`
	Collectors string `json:"collectors,omitempty" jsonschema:"Comma-separated list of collectors to run (default: all)"`
	Sections   string `json:"sections,omitempty" jsonschema:"Comma-separated list of report sections to include"`
	GitDepth   int    `json:"git_depth,omitempty" jsonschema:"Max commits to examine (default 1000)"`
	GitSince   string `json:"git_since,omitempty" jsonschema:"Only examine commits after this duration (e.g. 90d, 6m, 1y)"`
}

// ContextInput is the input schema for the stringer context MCP tool.
type ContextInput struct {
	Path   string `json:"path" jsonschema:"Repository path to analyze (defaults to current directory)"`
	Weeks  int    `json:"weeks,omitempty" jsonschema:"Weeks of git history to include (default: 4)"`
	Format string `json:"format,omitempty" jsonschema:"Output format: json or markdown (default: json)"`
}

// DocsInput is the input schema for the stringer docs MCP tool.
type DocsInput struct {
	Path   string `json:"path" jsonschema:"Repository path to analyze (defaults to current directory)"`
	Update bool   `json:"update,omitempty" jsonschema:"Update existing AGENTS.md preserving manual sections"`
}

// boolPtr returns a pointer to a bool.
func boolPtr(b bool) *bool { return &b }

// registerTools adds all stringer tools to the MCP server.
func registerTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "scan",
		Description: "Scan a repository for actionable work items (TODOs, FIXMEs, git patterns, code smells). Returns structured signals.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(false),
		},
	}, handleScan)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "report",
		Description: "Generate a repository health report with metrics on lottery risk, churn hotspots, TODO age, coverage gaps, and recommendations.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(false),
		},
	}, handleReport)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "context",
		Description: "Generate a CONTEXT.md summary for agent onboarding: tech stack, architecture, recent activity, contributors, and technical debt.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(false),
		},
	}, handleContext)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "docs",
		Description: "Generate or update an AGENTS.md scaffold documenting the project's architecture, tech stack, and build commands.",
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:    false,
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(false),
		},
	}, handleDocs)
}

func handleScan(ctx context.Context, _ *mcp.CallToolRequest, input ScanInput) (*mcp.CallToolResult, any, error) {
	pathInfo, err := ResolvePath(input.Path)
	if err != nil {
		return nil, nil, err
	}

	// Parse collectors.
	var collectors []string
	if input.Collectors != "" {
		collectors = splitAndTrim(input.Collectors)
	}

	// Determine format (default to json for MCP consumers).
	format := "json"
	if input.Format != "" {
		format = input.Format
	}

	// Validate format.
	if _, err := output.GetFormatter(format); err != nil {
		return nil, nil, fmt.Errorf("unsupported format %q", format)
	}

	// Validate confidence bounds.
	if input.MinConfidence < 0 || input.MinConfidence > 1 {
		return nil, nil, fmt.Errorf("min_confidence must be between 0.0 and 1.0, got %g", input.MinConfidence)
	}

	// Load and merge config.
	fileCfg, err := config.Load(pathInfo.AbsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	scanCfg := signal.ScanConfig{
		RepoPath:     pathInfo.AbsPath,
		Collectors:   collectors,
		OutputFormat: format,
		MaxIssues:    input.MaxIssues,
	}
	scanCfg = config.Merge(fileCfg, scanCfg)

	// Set GitRoot for subdirectory scans.
	if pathInfo.GitRoot != pathInfo.AbsPath {
		if scanCfg.CollectorOpts == nil {
			scanCfg.CollectorOpts = make(map[string]signal.CollectorOpts)
		}
		for _, name := range []string{"todos", "gitlog", "lotteryrisk"} {
			co := scanCfg.CollectorOpts[name]
			co.GitRoot = pathInfo.GitRoot
			scanCfg.CollectorOpts[name] = co
		}
	}

	// Apply git-depth and git-since.
	if input.GitDepth > 0 || input.GitSince != "" {
		if scanCfg.CollectorOpts == nil {
			scanCfg.CollectorOpts = make(map[string]signal.CollectorOpts)
		}
		for _, name := range []string{"gitlog", "lotteryrisk"} {
			co := scanCfg.CollectorOpts[name]
			if input.GitDepth > 0 && co.GitDepth == 0 {
				co.GitDepth = input.GitDepth
			}
			if input.GitSince != "" && co.GitSince == "" {
				co.GitSince = input.GitSince
			}
			scanCfg.CollectorOpts[name] = co
		}
	}

	// Create and run pipeline.
	p, err := pipeline.New(scanCfg)
	if err != nil {
		available := collector.List()
		sort.Strings(available)
		return nil, nil, fmt.Errorf("%v (available: %s)", err, strings.Join(available, ", "))
	}

	result, err := p.Run(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("scan failed: %w", err)
	}

	// Apply confidence filter.
	if input.MinConfidence > 0 {
		var filtered []signal.RawSignal
		for _, sig := range result.Signals {
			if sig.Confidence >= input.MinConfidence {
				filtered = append(filtered, sig)
			}
		}
		result.Signals = filtered
	}

	// Apply kind filter.
	if input.Kind != "" {
		kinds := make(map[string]bool)
		for _, k := range splitAndTrim(input.Kind) {
			kinds[strings.ToLower(k)] = true
		}
		var filtered []signal.RawSignal
		for _, sig := range result.Signals {
			if kinds[sig.Kind] {
				filtered = append(filtered, sig)
			}
		}
		result.Signals = filtered
	}

	// Beads-aware dedup (read-only).
	beadsAwareEnabled := fileCfg.BeadsAware == nil || *fileCfg.BeadsAware
	if beadsAwareEnabled {
		existingBeads, beadsErr := beads.LoadBeads(pathInfo.AbsPath)
		if beadsErr != nil {
			slog.Warn("failed to load existing beads", "error", beadsErr)
		} else if existingBeads != nil {
			result.Signals = beads.FilterAgainstExisting(result.Signals, existingBeads)
		}
	}

	// Format output.
	formatter, _ := output.GetFormatter(format)
	var buf bytes.Buffer
	if err := formatter.Format(result.Signals, &buf); err != nil {
		return nil, nil, fmt.Errorf("formatting failed: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: buf.String()},
		},
	}, nil, nil
}

func handleReport(ctx context.Context, _ *mcp.CallToolRequest, input ReportInput) (*mcp.CallToolResult, any, error) {
	pathInfo, err := ResolvePath(input.Path)
	if err != nil {
		return nil, nil, err
	}

	// Parse collectors.
	var collectors []string
	if input.Collectors != "" {
		collectors = splitAndTrim(input.Collectors)
	}

	// Load and merge config.
	fileCfg, err := config.Load(pathInfo.AbsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	scanCfg := signal.ScanConfig{
		RepoPath:   pathInfo.AbsPath,
		Collectors: collectors,
	}
	scanCfg = config.Merge(fileCfg, scanCfg)

	// Set GitRoot for subdirectory scans.
	if pathInfo.GitRoot != pathInfo.AbsPath {
		if scanCfg.CollectorOpts == nil {
			scanCfg.CollectorOpts = make(map[string]signal.CollectorOpts)
		}
		for _, name := range []string{"todos", "gitlog", "lotteryrisk"} {
			co := scanCfg.CollectorOpts[name]
			co.GitRoot = pathInfo.GitRoot
			scanCfg.CollectorOpts[name] = co
		}
	}

	// Apply git-depth and git-since.
	if input.GitDepth > 0 || input.GitSince != "" {
		if scanCfg.CollectorOpts == nil {
			scanCfg.CollectorOpts = make(map[string]signal.CollectorOpts)
		}
		for _, name := range []string{"gitlog", "lotteryrisk"} {
			co := scanCfg.CollectorOpts[name]
			if input.GitDepth > 0 && co.GitDepth == 0 {
				co.GitDepth = input.GitDepth
			}
			if input.GitSince != "" && co.GitSince == "" {
				co.GitSince = input.GitSince
			}
			scanCfg.CollectorOpts[name] = co
		}
	}

	// Create and run pipeline.
	p, err := pipeline.New(scanCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("pipeline: %w", err)
	}

	collectorNames := scanCfg.Collectors
	if len(collectorNames) == 0 {
		collectorNames = collector.List()
		sort.Strings(collectorNames)
	}

	result, err := p.Run(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("report failed: %w", err)
	}

	// Parse sections.
	var sections []string
	if input.Sections != "" {
		sections = splitAndTrim(input.Sections)
	}

	// Render JSON report.
	var buf bytes.Buffer
	if err := report.RenderJSON(result, pathInfo.AbsPath, collectorNames, sections, &buf); err != nil {
		return nil, nil, fmt.Errorf("rendering failed: %w", err)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: buf.String()},
		},
	}, nil, nil
}

func handleContext(_ context.Context, _ *mcp.CallToolRequest, input ContextInput) (*mcp.CallToolResult, any, error) {
	pathInfo, err := ResolvePath(input.Path)
	if err != nil {
		return nil, nil, err
	}

	// Analyze architecture.
	analysis, err := docs.Analyze(pathInfo.AbsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("analysis failed: %w", err)
	}

	// Analyze git history.
	weeks := input.Weeks
	if weeks <= 0 {
		weeks = 4
	}
	history, err := strcontext.AnalyzeHistory(pathInfo.AbsPath, weeks)
	if err != nil {
		slog.Warn("git history analysis failed, continuing without it", "error", err)
		history = nil
	}

	// Load scan state (optional).
	scanState, err := state.Load(pathInfo.AbsPath)
	if err != nil {
		slog.Warn("failed to load scan state, continuing without it", "error", err)
		scanState = nil
	}

	var buf bytes.Buffer
	format := input.Format
	if format == "" {
		format = "json"
	}

	switch format {
	case "json":
		if err := strcontext.RenderJSON(analysis, history, scanState, &buf); err != nil {
			return nil, nil, fmt.Errorf("generation failed: %w", err)
		}
	case "markdown":
		if err := strcontext.Generate(analysis, history, scanState, &buf); err != nil {
			return nil, nil, fmt.Errorf("generation failed: %w", err)
		}
	default:
		return nil, nil, fmt.Errorf("unsupported format %q (supported: json, markdown)", format)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: buf.String()},
		},
	}, nil, nil
}

func handleDocs(_ context.Context, _ *mcp.CallToolRequest, input DocsInput) (*mcp.CallToolResult, any, error) {
	pathInfo, err := ResolvePath(input.Path)
	if err != nil {
		return nil, nil, err
	}

	analysis, err := docs.Analyze(pathInfo.AbsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("analysis failed: %w", err)
	}

	var buf bytes.Buffer
	if input.Update {
		if err := docs.Update(pathInfo.AbsPath+"/AGENTS.md", analysis, &buf); err != nil {
			return nil, nil, fmt.Errorf("update failed: %w", err)
		}
	} else {
		if err := docs.Generate(analysis, &buf); err != nil {
			return nil, nil, fmt.Errorf("generation failed: %w", err)
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: buf.String()},
		},
	}, nil, nil
}

// splitAndTrim splits a comma-separated string and trims whitespace from each element.
func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
