package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/davetashner/stringer/internal/collector"
	_ "github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/config"
	"github.com/davetashner/stringer/internal/pipeline"
	"github.com/davetashner/stringer/internal/signal"
)

// Report-specific flag values.
var (
	reportCollectors string
	reportSections   string
	reportOutput     string
	reportGitDepth   int
	reportGitSince   string
	reportAnonymize  string
)

// reportCmd is the subcommand for generating a repository health report.
var reportCmd = &cobra.Command{
	Use:   "report [path]",
	Short: "Generate a repository health report",
	Long: `Analyze a repository and generate a health report summarizing signals,
metrics, and code quality indicators from all configured collectors.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runReport,
}

func init() {
	reportCmd.Flags().StringVarP(&reportCollectors, "collectors", "c", "", "comma-separated list of collectors to run")
	reportCmd.Flags().StringVar(&reportSections, "sections", "", "comma-separated list of report sections to include")
	reportCmd.Flags().StringVarP(&reportOutput, "output", "o", "", "output file path (default: stdout)")
	reportCmd.Flags().IntVar(&reportGitDepth, "git-depth", 0, "max commits to examine (default 1000)")
	reportCmd.Flags().StringVar(&reportGitSince, "git-since", "", "only examine commits after this duration (e.g., 90d, 6m, 1y)")
	reportCmd.Flags().StringVar(&reportAnonymize, "anonymize", "auto", "anonymize author names: auto, always, or never")
}

func runReport(cmd *cobra.Command, args []string) error {
	// 1. Parse path argument.
	repoPath := "."
	if len(args) > 0 {
		repoPath = args[0]
	}

	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return fmt.Errorf("stringer: cannot resolve path %q (%v)", repoPath, err)
	}

	absPath, err = filepath.EvalSymlinks(absPath)
	if err != nil {
		return fmt.Errorf("stringer: cannot resolve path %q (%v)", repoPath, err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("stringer: path %q does not exist", repoPath)
	}
	if !info.IsDir() {
		return fmt.Errorf("stringer: %q is not a directory", repoPath)
	}

	// Walk up to find .git root for subdirectory scans.
	gitRoot := absPath
	for {
		if _, err := os.Stat(filepath.Join(gitRoot, ".git")); err == nil {
			break
		}
		parent := filepath.Dir(gitRoot)
		if parent == gitRoot {
			gitRoot = absPath
			break
		}
		gitRoot = parent
	}

	// 2. Parse collectors flag.
	var collectors []string
	if reportCollectors != "" {
		collectors = strings.Split(reportCollectors, ",")
		for i := range collectors {
			collectors[i] = strings.TrimSpace(collectors[i])
		}
	}

	// 3. Load config file.
	fileCfg, err := config.Load(absPath)
	if err != nil {
		return fmt.Errorf("stringer: failed to load %s (%v)", config.FileName, err)
	}
	if err := config.Validate(fileCfg); err != nil {
		return fmt.Errorf("stringer: %v", err)
	}

	// 4. Build scan config.
	scanCfg := signal.ScanConfig{
		RepoPath:   absPath,
		Collectors: collectors,
	}

	// 5. Merge file config.
	scanCfg = config.Merge(fileCfg, scanCfg)

	// Set GitRoot on relevant collectors for subdirectory scans.
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

	// Apply git-depth and git-since.
	if reportGitDepth > 0 || reportGitSince != "" {
		if scanCfg.CollectorOpts == nil {
			scanCfg.CollectorOpts = make(map[string]signal.CollectorOpts)
		}
		for _, name := range []string{"gitlog", "lotteryrisk"} {
			co := scanCfg.CollectorOpts[name]
			if reportGitDepth > 0 && co.GitDepth == 0 {
				co.GitDepth = reportGitDepth
			}
			if reportGitSince != "" && co.GitSince == "" {
				co.GitSince = reportGitSince
			}
			scanCfg.CollectorOpts[name] = co
		}
	}

	// Apply --anonymize to the lotteryrisk collector.
	if cmd.Flags().Changed("anonymize") {
		if scanCfg.CollectorOpts == nil {
			scanCfg.CollectorOpts = make(map[string]signal.CollectorOpts)
		}
		co := scanCfg.CollectorOpts["lotteryrisk"]
		co.Anonymize = reportAnonymize
		scanCfg.CollectorOpts["lotteryrisk"] = co
	}

	// Wire progress callback.
	progressFn := func(msg string) {
		slog.Info(msg)
	}
	if scanCfg.CollectorOpts == nil {
		scanCfg.CollectorOpts = make(map[string]signal.CollectorOpts)
	}
	for _, name := range collector.List() {
		co := scanCfg.CollectorOpts[name]
		co.ProgressFunc = progressFn
		scanCfg.CollectorOpts[name] = co
	}

	// 6. Create pipeline.
	p, err := pipeline.New(scanCfg)
	if err != nil {
		available := collector.List()
		sort.Strings(available)
		return fmt.Errorf("stringer: %v (available: %s)", err, strings.Join(available, ", "))
	}

	collectorNames := scanCfg.Collectors
	if len(collectorNames) == 0 {
		collectorNames = collector.List()
		sort.Strings(collectorNames)
	}
	slog.Info("generating report", "collectors", len(collectorNames))

	// 7. Run pipeline.
	result, err := p.Run(cmd.Context())
	if err != nil {
		return fmt.Errorf("stringer: report failed (%v)", err)
	}

	// 8. Render report.
	w := cmd.OutOrStdout()
	if reportOutput != "" {
		f, createErr := os.Create(reportOutput) //nolint:gosec // user-specified output path
		if createErr != nil {
			return fmt.Errorf("stringer: cannot create output file %q (%v)", reportOutput, createErr)
		}
		defer f.Close() //nolint:errcheck // best-effort close on output file
		w = f
	}

	if err := renderReport(result, absPath, collectorNames, w); err != nil {
		return fmt.Errorf("stringer: rendering failed (%v)", err)
	}

	slog.Info("report complete", "signals", len(result.Signals), "duration", result.Duration)
	return nil
}

// renderReport writes a terminal-friendly summary of the scan results.
func renderReport(result *signal.ScanResult, repoPath string, collectorNames []string, w interface{ Write([]byte) (int, error) }) error {
	// Header.
	_, _ = fmt.Fprintf(w, "Stringer Report\n")
	_, _ = fmt.Fprintf(w, "===============\n\n")
	_, _ = fmt.Fprintf(w, "Repository: %s\n", repoPath)
	_, _ = fmt.Fprintf(w, "Generated:  %s\n", time.Now().Format(time.RFC3339))
	_, _ = fmt.Fprintf(w, "Duration:   %s\n", result.Duration.Round(time.Millisecond))
	_, _ = fmt.Fprintf(w, "Collectors: %s\n\n", strings.Join(collectorNames, ", "))

	// Per-collector summary.
	_, _ = fmt.Fprintf(w, "Collector Results\n")
	_, _ = fmt.Fprintf(w, "-----------------\n")
	for _, cr := range result.Results {
		status := fmt.Sprintf("%d signals", len(cr.Signals))
		if cr.Err != nil {
			status = fmt.Sprintf("error: %v", cr.Err)
		}
		metricsStatus := "no"
		if cr.Metrics != nil {
			metricsStatus = "yes"
		}
		_, _ = fmt.Fprintf(w, "  %-15s %s (%s, metrics: %s)\n",
			cr.Collector, status, cr.Duration.Round(time.Millisecond), metricsStatus)
	}

	// Signal summary.
	_, _ = fmt.Fprintf(w, "\nSignal Summary\n")
	_, _ = fmt.Fprintf(w, "--------------\n")
	_, _ = fmt.Fprintf(w, "  Total signals: %d\n", len(result.Signals))

	// Count by kind.
	kindCounts := make(map[string]int)
	for _, sig := range result.Signals {
		kindCounts[sig.Kind]++
	}
	if len(kindCounts) > 0 {
		kinds := make([]string, 0, len(kindCounts))
		for k := range kindCounts {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		_, _ = fmt.Fprintf(w, "  By kind:\n")
		for _, k := range kinds {
			_, _ = fmt.Fprintf(w, "    %-20s %d\n", k, kindCounts[k])
		}
	}

	// Metrics availability.
	if len(result.Metrics) > 0 {
		_, _ = fmt.Fprintf(w, "\nMetrics Available\n")
		_, _ = fmt.Fprintf(w, "-----------------\n")
		names := make([]string, 0, len(result.Metrics))
		for name := range result.Metrics {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			_, _ = fmt.Fprintf(w, "  %s: yes\n", name)
		}
	}

	_, _ = fmt.Fprintf(w, "\n")
	return nil
}
