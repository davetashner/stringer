// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/davetashner/stringer/internal/analysis"
	"github.com/davetashner/stringer/internal/beads"
	"github.com/davetashner/stringer/internal/collector"
	_ "github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/config"
	"github.com/davetashner/stringer/internal/llm"
	"github.com/davetashner/stringer/internal/output"
	"github.com/davetashner/stringer/internal/pipeline"
	"github.com/davetashner/stringer/internal/signal"
	"github.com/davetashner/stringer/internal/state"
)

// Scan-specific flag values.
var (
	scanCollectors        string
	scanFormat            string
	scanOutput            string
	scanDryRun            bool
	scanDelta             bool
	scanNoLLM             bool
	scanJSON              bool
	scanMaxIssues         int
	scanMinConfidence     float64
	scanKind              string
	scanStrict            bool
	scanGitDepth          int
	scanGitSince          string
	scanExclude           []string
	scanIncludeClosed     bool
	scanAnonymize         string
	scanHistoryDepth      string
	scanCollectorTimeout  string
	scanExcludeCollectors string
	scanIncludeDemoPaths  bool
	scanPaths             []string
	scanCluster           bool
	scanClusterThreshold  float64
	scanInferPriority     bool
	scanInferDeps         bool
	scanWorkspace         string
	scanNoWorkspaces      bool
)

// scanCmd is the subcommand for scanning a repository.
var scanCmd = &cobra.Command{
	Use:   "scan [path]",
	Short: "Scan a repository for actionable work items",
	Long: `Scan a repository and output machine-readable issues (Beads JSONL, JSON,
or Markdown). Use 'stringer scan . | bd import' to add issues to your tracker.

For a human-readable health dashboard, use 'stringer report' instead.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runScan,
}

func init() {
	scanCmd.Flags().StringVarP(&scanCollectors, "collectors", "c", "", "comma-separated list of collectors to run")
	scanCmd.Flags().StringVarP(&scanFormat, "format", "f", "beads", "output format (beads, html, html-dir, json, markdown, sarif, tasks)")
	scanCmd.Flags().StringVarP(&scanOutput, "output", "o", "", "output file path (default: stdout)")
	scanCmd.Flags().BoolVar(&scanDryRun, "dry-run", false, "show signal count without producing output")
	scanCmd.Flags().BoolVar(&scanDelta, "delta", false, "only output new signals since last scan")
	scanCmd.Flags().BoolVar(&scanNoLLM, "no-llm", false, "skip LLM clustering pass (noop for MVP)")
	scanCmd.Flags().BoolVar(&scanJSON, "json", false, "machine-readable output for --dry-run")
	scanCmd.Flags().IntVar(&scanMaxIssues, "max-issues", 0, "cap output count (0 = unlimited)")
	scanCmd.Flags().Float64Var(&scanMinConfidence, "min-confidence", 0, "filter signals below this confidence threshold (0.0-1.0)")
	scanCmd.Flags().StringVar(&scanKind, "kind", "", "filter signals by kind (comma-separated, e.g., todo,churn,revert)")
	scanCmd.Flags().BoolVar(&scanStrict, "strict", false, "exit non-zero on any collector failure")
	scanCmd.Flags().IntVar(&scanGitDepth, "git-depth", 0, "max commits to examine (default 1000)")
	scanCmd.Flags().StringVar(&scanGitSince, "git-since", "", "only examine commits after this duration (e.g., 90d, 6m, 1y)")
	scanCmd.Flags().StringSliceVarP(&scanExclude, "exclude", "e", nil, "glob patterns to exclude from scanning (e.g. \"tests/**,docs/**\")")
	scanCmd.Flags().BoolVar(&scanIncludeClosed, "include-closed", false, "include closed/merged issues and PRs from GitHub")
	scanCmd.Flags().StringVar(&scanHistoryDepth, "history-depth", "", "filter closed items older than this duration (e.g., 90d, 6m, 1y)")
	scanCmd.Flags().StringVar(&scanAnonymize, "anonymize", "auto", "anonymize author names: auto, always, or never")
	scanCmd.Flags().StringVar(&scanCollectorTimeout, "collector-timeout", "", "per-collector timeout (e.g. 60s, 2m); 0 or empty = no timeout")
	scanCmd.Flags().StringVarP(&scanExcludeCollectors, "exclude-collectors", "x", "", "comma-separated list of collectors to skip")
	scanCmd.Flags().BoolVar(&scanIncludeDemoPaths, "include-demo-paths", false, "include demo/example/tutorial paths in noise-prone signals")
	scanCmd.Flags().StringSliceVar(&scanPaths, "paths", nil, "restrict scanning to specific files or directories (comma-separated)")
	scanCmd.Flags().BoolVar(&scanCluster, "cluster", false, "enable LLM-based signal clustering")
	scanCmd.Flags().Float64Var(&scanClusterThreshold, "cluster-threshold", 0.7, "similarity threshold for signal pre-filtering (0.0-1.0)")
	scanCmd.Flags().BoolVar(&scanInferPriority, "infer-priority", false, "use LLM to assign P1-P4 priorities to signals")
	scanCmd.Flags().BoolVar(&scanInferDeps, "infer-deps", false, "use LLM to detect dependencies between signals")
	scanCmd.Flags().StringVar(&scanWorkspace, "workspace", "", "scan only named workspace(s) (comma-separated)")
	scanCmd.Flags().BoolVar(&scanNoWorkspaces, "no-workspaces", false, "disable monorepo auto-detection, scan root as single directory")
}

// scanContext holds shared state across the scan lifecycle, reducing parameter
// passing between stages.
type scanContext struct {
	cmd            *cobra.Command
	absPath        string
	gitRoot        string
	workspaces     []workspaceEntry
	scanCfg        signal.ScanConfig
	fileCfg        *config.Config
	result         *signal.ScanResult
	collectorNames []string
	allSignals     []signal.RawSignal // pre-filter signals for delta state
}

func runScan(cmd *cobra.Command, args []string) error {
	// 1. Resolve scan path and find git root.
	repoPath := "."
	if len(args) > 0 {
		repoPath = args[0]
	}
	absPath, gitRoot, err := resolveScanPath(repoPath)
	if err != nil {
		return err
	}

	if scanMinConfidence < 0 || scanMinConfidence > 1.0 {
		return exitError(ExitInvalidArgs,
			"stringer: --min-confidence must be between 0.0 and 1.0 (got %.2f)", scanMinConfidence)
	}

	sc := &scanContext{
		cmd:        cmd,
		absPath:    absPath,
		gitRoot:    gitRoot,
		workspaces: resolveWorkspaces(absPath, scanNoWorkspaces, scanWorkspace),
		result:     &signal.ScanResult{Metrics: make(map[string]any)},
	}

	// 2. Load root config for output format and filters.
	sc.scanCfg, sc.fileCfg, err = loadScanConfig(cmd, absPath, gitRoot)
	if err != nil {
		return err
	}

	// 3. Run pipeline per workspace and aggregate results.
	if err := sc.runPipeline(); err != nil {
		return err
	}

	// 4. Filter results (delta, beads dedup, confidence, kind).
	sc.allSignals = sc.result.Signals
	if err := sc.filterResults(); err != nil {
		return err
	}

	// 5. LLM-based analysis (priority inference, dependency detection).
	if err := sc.runLLMAnalysis(); err != nil {
		return err
	}

	// 6. Determine exit code based on collector results.
	exitCode := computeExitCode(sc.result, scanStrict)

	// 7. Handle dry-run.
	if scanDryRun {
		return printDryRun(cmd, sc.result, exitCode)
	}

	// 8. Write formatted output.
	if err := writeScanOutput(cmd, sc.result, sc.scanCfg); err != nil {
		return err
	}

	// 9. Save delta state from ALL signals (pre-filter), not just new ones.
	if scanDelta {
		if err := saveDeltaState(absPath, sc.collectorNames, sc.allSignals, sc.workspaces); err != nil {
			return exitError(ExitTotalFailure, "stringer: failed to save delta state (%v)", err)
		}
	}

	// 10. Save scan history (best-effort).
	if err := saveHistory(absPath, sc.result, sc.workspaces); err != nil {
		slog.Warn("failed to save scan history", "error", err)
	}

	if exitCode != ExitOK {
		return exitError(exitCode, "")
	}
	return nil
}

// runPipeline runs the scan pipeline for each workspace and aggregates results.
func (sc *scanContext) runPipeline() error {
	for _, ws := range sc.workspaces {
		wsPath := ws.Path
		if ws.Name != "" {
			slog.Info("scanning workspace", "name", ws.Name, "path", ws.Rel)
		}

		wsCfg, _, err := loadScanConfig(sc.cmd, wsPath, sc.gitRoot)
		if err != nil {
			return err
		}

		p, err := pipeline.New(wsCfg)
		if err != nil {
			available := collector.List()
			sort.Strings(available)
			return exitError(ExitInvalidArgs, "stringer: %v (available: %s)", err, strings.Join(available, ", "))
		}

		cn := wsCfg.Collectors
		if len(cn) == 0 {
			cn = collector.List()
			sort.Strings(cn)
		}
		if sc.collectorNames == nil {
			sc.collectorNames = cn
		}
		slog.Info("scanning", "collectors", len(cn))

		wsResult, err := p.Run(sc.cmd.Context())
		if err != nil {
			return exitError(ExitTotalFailure, "stringer: scan failed (%v)", err)
		}

		// Stamp workspace on signals and adjust file paths.
		stampWorkspace(ws, wsResult.Signals)
		for i := range wsResult.Results {
			stampWorkspace(ws, wsResult.Results[i].Signals)
		}

		// Aggregate into combined result.
		sc.result.Signals = append(sc.result.Signals, wsResult.Signals...)
		sc.result.Results = append(sc.result.Results, wsResult.Results...)
		sc.result.Duration += wsResult.Duration
		for k, v := range wsResult.Metrics {
			sc.result.Metrics[k] = v
		}
	}

	for _, cr := range sc.result.Results {
		if cr.Err != nil {
			slog.Error("collector failed", "name", cr.Collector, "error", cr.Err, "duration", cr.Duration)
		} else {
			slog.Info("collector complete", "name", cr.Collector, "signals", len(cr.Signals), "duration", cr.Duration)
		}
	}

	// Warn when an explicitly requested collector produced no signals and no error.
	if scanCollectors != "" {
		resultByName := make(map[string]bool)
		for _, cr := range sc.result.Results {
			if cr.Err == nil && len(cr.Signals) > 0 {
				resultByName[cr.Collector] = true
			}
		}
		for _, name := range strings.Split(scanCollectors, ",") {
			name = strings.TrimSpace(name)
			if name != "" && !resultByName[name] {
				slog.Warn("requested collector produced no signals", "name", name)
			}
		}
	}

	return nil
}

// runLLMAnalysis runs optional LLM-based priority inference and dependency
// detection on the scan results.
func (sc *scanContext) runLLMAnalysis() error {
	if !scanInferPriority && !scanInferDeps {
		return nil
	}

	provider, provErr := llm.NewAnthropicProvider()
	if provErr != nil {
		return exitError(ExitInvalidArgs, "stringer: LLM features require ANTHROPIC_API_KEY (%v)", provErr)
	}

	if scanInferPriority {
		var overrides []analysis.PriorityOverride
		if sc.fileCfg != nil {
			for _, o := range sc.fileCfg.PriorityOverrides {
				overrides = append(overrides, analysis.PriorityOverride{
					Pattern:  o.Pattern,
					Priority: o.Priority,
				})
			}
		}
		var inferErr error
		sc.result.Signals, inferErr = analysis.InferPriorities(sc.cmd.Context(), sc.result.Signals, provider, overrides)
		if inferErr != nil {
			slog.Warn("priority inference error", "error", inferErr)
		}
	}

	if scanInferDeps {
		idPrefix := "str-"
		deps, depErr := analysis.InferDependencies(sc.cmd.Context(), sc.result.Signals, provider, idPrefix)
		if depErr != nil {
			slog.Warn("dependency inference error", "error", depErr)
		} else if len(deps) > 0 {
			analysis.ApplyDepsToSignals(sc.result.Signals, deps, idPrefix)
			slog.Info("dependencies inferred", "count", len(deps))
		}
	}

	return nil
}

// resolveScanPath resolves the given path argument into an absolute path and
// finds the nearest git root by walking up the directory tree. For non-git
// directories, gitRoot equals absPath.
func resolveScanPath(repoPath string) (absPath, gitRoot string, err error) {
	absPath, err = cmdFS.Abs(repoPath)
	if err != nil {
		return "", "", exitError(ExitInvalidArgs, "stringer: cannot resolve path %q (%v)", repoPath, err)
	}

	absPath, err = cmdFS.EvalSymlinks(absPath)
	if err != nil {
		return "", "", exitError(ExitInvalidArgs, "stringer: cannot resolve path %q (%v)", repoPath, err)
	}

	info, err := cmdFS.Stat(absPath)
	if err != nil {
		return "", "", exitError(ExitInvalidArgs, "stringer: path %q does not exist (check the path and try again)", repoPath)
	}
	if !info.IsDir() {
		return "", "", exitError(ExitInvalidArgs, "stringer: %q is not a directory (provide a repository root)", repoPath)
	}

	// Walk up to find .git root for subdirectory scans.
	gitRoot = absPath
	for {
		if _, statErr := cmdFS.Stat(filepath.Join(gitRoot, ".git")); statErr == nil {
			break
		}
		parent := filepath.Dir(gitRoot)
		if parent == gitRoot {
			// No .git found — use absPath as-is (non-git repos are fine).
			gitRoot = absPath
			break
		}
		gitRoot = parent
	}

	return absPath, gitRoot, nil
}

// loadScanConfig builds the merged ScanConfig from CLI flags and file config.
// It parses the collectors flag, loads the config file, merges them, sets the
// git root for subdirectory scans, applies defaults, validates the output
// format, and wires CLI flag overrides into per-collector options.
func loadScanConfig(cmd *cobra.Command, absPath, gitRoot string) (signal.ScanConfig, *config.Config, error) {
	// Parse collectors flag.
	var collectors []string
	if scanCollectors != "" {
		collectors = strings.Split(scanCollectors, ",")
		for i := range collectors {
			collectors[i] = strings.TrimSpace(collectors[i])
		}
	}
	collectors = applyCollectorExclusions(collectors, scanExcludeCollectors)

	// Load config file.
	fileCfg, err := config.Load(absPath)
	if err != nil {
		return signal.ScanConfig{}, nil, exitError(ExitInvalidArgs, "stringer: failed to load %s (%v)", config.FileName, err)
	}
	if err := config.Validate(fileCfg); err != nil {
		return signal.ScanConfig{}, nil, exitError(ExitInvalidArgs, "stringer: %v", err)
	}

	// Build CLI scan config (only set OutputFormat if explicitly passed).
	cliFormat := ""
	if cmd.Flags().Changed("format") {
		cliFormat = scanFormat
	}
	scanCfg := signal.ScanConfig{
		RepoPath:        absPath,
		Collectors:      collectors,
		OutputFormat:    cliFormat,
		NoLLM:           scanNoLLM,
		ExcludePatterns: scanExclude,
		MaxIssues:       scanMaxIssues,
	}

	// Merge file config into CLI config.
	scanCfg = config.Merge(fileCfg, scanCfg)

	// Set GitRoot on all collector opts so collectors can open the git repo
	// even when scanning a subdirectory.
	if gitRoot != absPath {
		if scanCfg.CollectorOpts == nil {
			scanCfg.CollectorOpts = make(map[string]signal.CollectorOpts)
		}
		for _, name := range []string{"todos", "gitlog", "lotteryrisk"} {
			co := scanCfg.CollectorOpts[name]
			co.GitRoot = gitRoot
			scanCfg.CollectorOpts[name] = co
		}
	}

	// Apply default format if neither CLI nor file config specified one.
	if scanCfg.OutputFormat == "" {
		scanCfg.OutputFormat = "beads"
	}

	// Filter disabled collectors from file config.
	if len(fileCfg.Collectors) > 0 && len(scanCfg.Collectors) == 0 {
		for name, cc := range fileCfg.Collectors {
			if cc.Enabled != nil && !*cc.Enabled {
				if scanCfg.Collectors == nil {
					all := collector.List()
					for _, c := range all {
						if fc, ok := fileCfg.Collectors[c]; ok && fc.Enabled != nil && !*fc.Enabled {
							continue
						}
						scanCfg.Collectors = append(scanCfg.Collectors, c)
					}
					_ = name // used in loop above
					break
				}
			}
		}
	}

	// Validate format after merge.
	if _, err := output.GetFormatter(scanCfg.OutputFormat); err != nil {
		return signal.ScanConfig{}, nil, exitError(ExitInvalidArgs, "stringer: %v", err)
	}

	// Apply CLI flag overrides to per-collector options.
	applyFlagOverrides(&scanCfg, flagOverrides{
		GitDepth:         scanGitDepth,
		GitSince:         scanGitSince,
		Anonymize:        scanAnonymize,
		AnonymizeChanged: cmd.Flags().Changed("anonymize"),
		IncludeDemoPaths: scanIncludeDemoPaths,
		CollectorTimeout: scanCollectorTimeout,
		Paths:            scanPaths,
		IncludeClosed:    scanIncludeClosed,
		HistoryDepth:     scanHistoryDepth,
	})

	return scanCfg, fileCfg, nil
}

// filterResults applies post-pipeline filters to the scan result: delta
// filtering, beads-aware dedup, confidence threshold, and kind filter. It
// mutates sc.result.Signals in place.
func (sc *scanContext) filterResults() error {
	// Delta filtering: load previous state, filter to new signals.
	if scanDelta {
		prevState, err := state.Load(sc.absPath)
		if err != nil {
			return exitError(ExitTotalFailure, "stringer: failed to load delta state (%v)", err)
		}
		if prevState != nil && !state.CollectorsMatch(prevState, sc.collectorNames) {
			slog.Warn("collector mismatch from previous scan, treating all signals as new")
			prevState = nil
		}
		newSignals := state.FilterNew(sc.allSignals, prevState)
		slog.Info("delta filter", "total", len(sc.allSignals), "new", len(newSignals))
		sc.result.Signals = newSignals

		// Compute and display diff summary to stderr.
		if prevState != nil {
			currentState := state.Build(sc.absPath, sc.collectorNames, sc.allSignals)
			diff := state.ComputeDiff(prevState, currentState)
			if err := state.FormatDiff(diff, sc.absPath, sc.cmd.ErrOrStderr()); err != nil {
				slog.Warn("failed to write diff summary", "error", err)
			}

			// Emit resolved TODOs as pre-closed signals.
			resolvedTodos := state.BuildResolvedTodoSignals(sc.absPath, diff.Removed)
			if len(resolvedTodos) > 0 {
				slog.Info("resolved TODOs detected", "count", len(resolvedTodos))
				sc.result.Signals = append(sc.result.Signals, resolvedTodos...)
			}
		}
	}

	// Beads-aware dedup: filter signals already tracked as beads.
	beadsAwareEnabled := sc.fileCfg.BeadsAware == nil || *sc.fileCfg.BeadsAware
	if beadsAwareEnabled {
		existingBeads, beadsErr := beads.LoadBeads(sc.absPath)
		if beadsErr != nil {
			slog.Warn("failed to load existing beads", "error", beadsErr)
		}

		// Also check workspace-level beads directories.
		for _, ws := range sc.workspaces {
			if ws.Name == "" {
				continue
			}
			wsBeads, err := beads.LoadBeads(ws.Path)
			if err != nil {
				slog.Warn("failed to load workspace beads", "workspace", ws.Name, "error", err)
				continue
			}
			existingBeads = append(existingBeads, wsBeads...)
		}

		if existingBeads != nil {
			before := len(sc.result.Signals)
			sc.result.Signals = beads.FilterAgainstExisting(sc.result.Signals, existingBeads)
			slog.Info("beads dedup", "before", before, "after", len(sc.result.Signals),
				"filtered", before-len(sc.result.Signals))

			// Adopt beads conventions for output formatting.
			if sc.scanCfg.OutputFormat == "beads" {
				if conventions := beads.DetectConventions(existingBeads); conventions != nil {
					if f, _ := output.GetFormatter("beads"); f != nil {
						if bf, ok := f.(*output.BeadsFormatter); ok {
							bf.SetConventions(conventions)
						}
					}
				}
			}
		}
	}

	// Post-pipeline confidence filter.
	if scanMinConfidence > 0 {
		var filtered []signal.RawSignal
		for _, sig := range sc.result.Signals {
			if sig.Confidence >= scanMinConfidence {
				filtered = append(filtered, sig)
			}
		}
		slog.Info("confidence filter", "before", len(sc.result.Signals), "after", len(filtered), "min", scanMinConfidence)
		sc.result.Signals = filtered
	}

	// Post-pipeline kind filter.
	if scanKind != "" {
		kinds := make(map[string]bool)
		for _, k := range strings.Split(scanKind, ",") {
			kinds[strings.TrimSpace(strings.ToLower(k))] = true
		}
		var filtered []signal.RawSignal
		for _, sig := range sc.result.Signals {
			if kinds[sig.Kind] {
				filtered = append(filtered, sig)
			}
		}
		slog.Info("kind filter", "before", len(sc.result.Signals), "after", len(filtered), "kinds", scanKind)
		sc.result.Signals = filtered
	}

	return nil
}

// writeScanOutput selects the formatter and writes the scan result to the
// configured output destination (file or stdout).
func writeScanOutput(cmd *cobra.Command, result *signal.ScanResult, scanCfg signal.ScanConfig) error {
	formatter, _ := output.GetFormatter(scanCfg.OutputFormat) // already validated in loadScanConfig

	// Directory formatters write to a directory instead of a stream.
	if df, ok := formatter.(output.DirectoryFormatter); ok {
		if scanOutput == "" {
			return exitError(ExitInvalidArgs, "stringer: %s format requires --output (-o) flag to specify output directory", scanCfg.OutputFormat)
		}
		if err := df.FormatDir(result.Signals, scanOutput); err != nil {
			return exitError(ExitTotalFailure, "stringer: formatting failed (%v)", err)
		}
		slog.Info("scan complete", "issues", len(result.Signals), "duration", result.Duration)
		return nil
	}

	w := cmd.OutOrStdout()
	if scanOutput != "" {
		f, err := cmdFS.Create(scanOutput)
		if err != nil {
			return exitError(ExitInvalidArgs, "stringer: cannot create output file %q (%v)", scanOutput, err)
		}
		defer f.Close() //nolint:errcheck // best-effort close on output file
		w = f
	}

	if err := formatter.Format(result.Signals, w); err != nil {
		return exitError(ExitTotalFailure, "stringer: formatting failed (%v)", err)
	}

	slog.Info("scan complete", "issues", len(result.Signals), "duration", result.Duration)
	return nil
}

// computeExitCode returns the appropriate exit code based on collector results.
// When strict is true, partial failures return ExitPartialFailure instead of ExitOK.
func computeExitCode(result *signal.ScanResult, strict bool) int {
	if len(result.Results) == 0 {
		return ExitOK
	}

	failCount := 0
	for _, cr := range result.Results {
		if cr.Err != nil {
			failCount++
		}
	}

	switch {
	case failCount == 0:
		return ExitOK
	case failCount == len(result.Results):
		return ExitTotalFailure
	case strict:
		return ExitPartialFailure
	default:
		// Non-strict: partial failures are OK, exit 0.
		return ExitOK
	}
}

// printDryRun prints a summary of the scan results without producing formatted output.
func printDryRun(cmd *cobra.Command, result *signal.ScanResult, exitCode int) error {
	if scanJSON {
		type collectorSummary struct {
			Name     string `json:"name"`
			Signals  int    `json:"signals"`
			Duration string `json:"duration"`
			Error    string `json:"error,omitempty"`
		}
		type dryRunOutput struct {
			TotalSignals int                `json:"total_signals"`
			Collectors   []collectorSummary `json:"collectors"`
			Duration     string             `json:"duration"`
			ExitCode     int                `json:"exit_code"`
		}

		out := dryRunOutput{
			TotalSignals: len(result.Signals),
			Duration:     result.Duration.String(),
			ExitCode:     exitCode,
		}
		for _, cr := range result.Results {
			cs := collectorSummary{
				Name:     cr.Collector,
				Signals:  len(cr.Signals),
				Duration: cr.Duration.String(),
			}
			if cr.Err != nil {
				cs.Error = cr.Err.Error()
			}
			out.Collectors = append(out.Collectors, cs)
		}

		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return exitError(ExitTotalFailure, "stringer: JSON marshal failed (%v)", err)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	} else {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "stringer: dry run — %d signal(s) found\n", len(result.Signals))
		for _, cr := range result.Results {
			status := fmt.Sprintf("%d signals", len(cr.Signals))
			if cr.Err != nil {
				status = fmt.Sprintf("error: %v", cr.Err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s (%s)\n", cr.Collector, status, cr.Duration.Round(1_000_000))
		}
	}

	if exitCode != ExitOK {
		return exitError(exitCode, "")
	}
	return nil
}

// applyCollectorExclusions removes excluded collectors from the include list.
// If include is empty, it starts from the full registry (collector.List()).
func applyCollectorExclusions(include []string, exclude string) []string {
	if exclude == "" {
		return include
	}
	skip := make(map[string]bool)
	for _, name := range strings.Split(exclude, ",") {
		name = strings.TrimSpace(name)
		if name != "" {
			skip[name] = true
		}
	}
	if len(include) == 0 {
		include = collector.List()
	}
	var result []string
	for _, name := range include {
		if !skip[name] {
			result = append(result, name)
		}
	}
	return result
}

// exitCodeError carries a non-zero exit code through cobra's error handling.
type exitCodeError struct {
	code int
	msg  string
}

func (e *exitCodeError) Error() string { return e.msg }

// ExitCode returns the exit code for this error.
func (e *exitCodeError) ExitCode() int { return e.code }

// exitError creates an exitCodeError. If msg is empty, the error message is
// set to a generic description of the exit code.
func exitError(code int, format string, args ...any) *exitCodeError {
	msg := fmt.Sprintf(format, args...)
	if msg == "" {
		switch code {
		case ExitPartialFailure:
			msg = "stringer: some collectors failed"
		case ExitTotalFailure:
			msg = "stringer: all collectors failed"
		default:
			msg = "stringer: error"
		}
	}
	return &exitCodeError{code: code, msg: msg}
}
