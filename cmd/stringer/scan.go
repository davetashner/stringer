package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/davetashner/stringer/internal/beads"
	"github.com/davetashner/stringer/internal/collector"
	_ "github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/config"
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
)

// scanCmd is the subcommand for scanning a repository.
var scanCmd = &cobra.Command{
	Use:   "scan [path]",
	Short: "Scan a repository for actionable work items",
	Long: `Scan a repository for actionable work items such as TODOs, FIXMEs,
git history patterns, and other signals. Outputs Beads-formatted JSONL
suitable for import with 'bd import'.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runScan,
}

func init() {
	scanCmd.Flags().StringVarP(&scanCollectors, "collectors", "c", "", "comma-separated list of collectors to run")
	scanCmd.Flags().StringVarP(&scanFormat, "format", "f", "beads", "output format (beads, json, markdown, tasks)")
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
}

func runScan(cmd *cobra.Command, args []string) error {
	// 1. Parse path argument.
	repoPath := "."
	if len(args) > 0 {
		repoPath = args[0]
	}

	absPath, err := cmdFS.Abs(repoPath)
	if err != nil {
		return exitError(ExitInvalidArgs, "stringer: cannot resolve path %q (%v)", repoPath, err)
	}

	// Resolve symlinks to prevent path traversal outside the intended tree.
	absPath, err = cmdFS.EvalSymlinks(absPath)
	if err != nil {
		return exitError(ExitInvalidArgs, "stringer: cannot resolve path %q (%v)", repoPath, err)
	}

	info, err := cmdFS.Stat(absPath)
	if err != nil {
		return exitError(ExitInvalidArgs, "stringer: path %q does not exist (check the path and try again)", repoPath)
	}
	if !info.IsDir() {
		return exitError(ExitInvalidArgs, "stringer: %q is not a directory (provide a repository root)", repoPath)
	}

	// Walk up to find .git root for subdirectory scans.
	gitRoot := absPath
	for {
		if _, err := cmdFS.Stat(filepath.Join(gitRoot, ".git")); err == nil {
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

	// 2. Parse collectors flag.
	var collectors []string
	if scanCollectors != "" {
		collectors = strings.Split(scanCollectors, ",")
		for i := range collectors {
			collectors[i] = strings.TrimSpace(collectors[i])
		}
	}

	// 2b. Apply --exclude-collectors.
	collectors = applyCollectorExclusions(collectors, scanExcludeCollectors)

	// 3. Load config file.
	fileCfg, err := config.Load(absPath)
	if err != nil {
		return exitError(ExitInvalidArgs, "stringer: failed to load %s (%v)", config.FileName, err)
	}
	if err := config.Validate(fileCfg); err != nil {
		return exitError(ExitInvalidArgs, "stringer: %v", err)
	}

	// 4. Build CLI scan config (only set OutputFormat if explicitly passed).
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

	// 5. Merge file config into CLI config.
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
		// No CLI collector filter — apply enabled/disabled from file config.
		for name, cc := range fileCfg.Collectors {
			if cc.Enabled != nil && !*cc.Enabled {
				// Ensure this collector is excluded.
				if scanCfg.Collectors == nil {
					// Build explicit list from all registered minus disabled.
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
		return exitError(ExitInvalidArgs, "stringer: %v", err)
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

	// 6. Create pipeline.
	p, err := pipeline.New(scanCfg)
	if err != nil {
		// Provide helpful error for unknown collectors.
		available := collector.List()
		sort.Strings(available)
		return exitError(ExitInvalidArgs, "stringer: %v (available: %s)", err, strings.Join(available, ", "))
	}

	// 7. Progress: announce scan.
	collectorNames := scanCfg.Collectors
	if len(collectorNames) == 0 {
		collectorNames = collector.List()
		sort.Strings(collectorNames)
	}
	slog.Info("scanning", "collectors", len(collectorNames))

	// 8. Run pipeline.
	result, err := p.Run(cmd.Context())
	if err != nil {
		return exitError(ExitTotalFailure, "stringer: scan failed (%v)", err)
	}

	// 9. Report per-collector progress.
	for _, cr := range result.Results {
		if cr.Err != nil {
			slog.Error("collector failed", "name", cr.Collector, "error", cr.Err, "duration", cr.Duration)
		} else {
			slog.Info("collector complete", "name", cr.Collector, "signals", len(cr.Signals), "duration", cr.Duration)
		}
	}

	// 10. Delta filtering: load previous state, filter to new signals.
	allSignals := result.Signals // keep full list for state saving
	if scanDelta {
		prevState, err := state.Load(absPath)
		if err != nil {
			return exitError(ExitTotalFailure, "stringer: failed to load delta state (%v)", err)
		}
		if prevState != nil && !state.CollectorsMatch(prevState, collectorNames) {
			slog.Warn("collector mismatch from previous scan, treating all signals as new")
			prevState = nil
		}
		newSignals := state.FilterNew(allSignals, prevState)
		slog.Info("delta filter", "total", len(allSignals), "new", len(newSignals))
		result.Signals = newSignals

		// 10.5a. Compute and display diff summary to stderr.
		if prevState != nil {
			currentState := state.Build(absPath, collectorNames, allSignals)
			diff := state.ComputeDiff(prevState, currentState)
			if err := state.FormatDiff(diff, absPath, cmd.ErrOrStderr()); err != nil {
				slog.Warn("failed to write diff summary", "error", err)
			}

			// Emit resolved TODOs as pre-closed signals.
			resolvedTodos := state.BuildResolvedTodoSignals(absPath, diff.Removed)
			if len(resolvedTodos) > 0 {
				slog.Info("resolved TODOs detected", "count", len(resolvedTodos))
				result.Signals = append(result.Signals, resolvedTodos...)
			}
		}
	}

	// 10.5. Beads-aware dedup: filter signals already tracked as beads.
	beadsAwareEnabled := fileCfg.BeadsAware == nil || *fileCfg.BeadsAware
	if beadsAwareEnabled {
		existingBeads, beadsErr := beads.LoadBeads(absPath)
		if beadsErr != nil {
			slog.Warn("failed to load existing beads", "error", beadsErr)
		} else if existingBeads != nil {
			before := len(result.Signals)
			result.Signals = beads.FilterAgainstExisting(result.Signals, existingBeads)
			slog.Info("beads dedup", "before", before, "after", len(result.Signals),
				"filtered", before-len(result.Signals))

			// Adopt beads conventions for output formatting.
			if scanCfg.OutputFormat == "beads" {
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

	// 10.6. Post-pipeline confidence filter.
	if scanMinConfidence > 0 {
		var filtered []signal.RawSignal
		for _, sig := range result.Signals {
			if sig.Confidence >= scanMinConfidence {
				filtered = append(filtered, sig)
			}
		}
		slog.Info("confidence filter", "before", len(result.Signals), "after", len(filtered), "min", scanMinConfidence)
		result.Signals = filtered
	}

	// 10.7. Post-pipeline kind filter.
	if scanKind != "" {
		kinds := make(map[string]bool)
		for _, k := range strings.Split(scanKind, ",") {
			kinds[strings.TrimSpace(strings.ToLower(k))] = true
		}
		var filtered []signal.RawSignal
		for _, sig := range result.Signals {
			if kinds[sig.Kind] {
				filtered = append(filtered, sig)
			}
		}
		slog.Info("kind filter", "before", len(result.Signals), "after", len(filtered), "kinds", scanKind)
		result.Signals = filtered
	}

	// 11. Determine exit code based on collector results.
	exitCode := computeExitCode(result, scanStrict)

	// 12. Handle dry-run.
	if scanDryRun {
		return printDryRun(cmd, result, exitCode)
	}

	// 13. Format and write output.
	formatter, _ := output.GetFormatter(scanCfg.OutputFormat) // already validated above

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

	// 14. Save delta state from ALL signals (pre-filter), not just new ones.
	if scanDelta {
		newState := state.Build(absPath, collectorNames, allSignals)
		if err := state.Save(absPath, newState); err != nil {
			return exitError(ExitTotalFailure, "stringer: failed to save delta state (%v)", err)
		}
		slog.Info("delta state saved", "hashes", newState.SignalCount)
	}

	if exitCode != ExitOK {
		return exitError(exitCode, "")
	}
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
