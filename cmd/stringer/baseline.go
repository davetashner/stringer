// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/davetashner/stringer/internal/baseline"
	"github.com/davetashner/stringer/internal/collector"
	_ "github.com/davetashner/stringer/internal/collectors"
	"github.com/davetashner/stringer/internal/config"
	"github.com/davetashner/stringer/internal/output"
	"github.com/davetashner/stringer/internal/pipeline"
	"github.com/davetashner/stringer/internal/signal"
)

// Signal ID format: str-[0-9a-f]{8}.
var signalIDPattern = regexp.MustCompile(`^str-[0-9a-f]{8}$`)

// Baseline command flags.
var (
	baselineCreateReason   string
	baselineCollectors     string
	baselineForce          bool
	baselineSuppressReason string
	baselineComment        string
	baselineExpires        string
	baselineJSON           bool
	baselineListReason     string
	baselineExpired        bool
)

// baselineCmd is the parent command for baseline subcommands.
var baselineCmd = &cobra.Command{
	Use:   "baseline",
	Short: "Manage signal suppressions for repeat scans",
	Long: `Manage signal suppressions so that acknowledged, won't-fix, or
false-positive signals are filtered from future scan output.

Suppressions are stored in .stringer/baseline.json and are intended to
be version-controlled.`,
}

// baselineCreateCmd runs a scan and writes all signal IDs to the baseline.
var baselineCreateCmd = &cobra.Command{
	Use:   "create [path]",
	Short: "Create a baseline from current scan results",
	Long: `Run a full scan of the repository and write all signal IDs to
.stringer/baseline.json. This establishes a baseline so that future scans
only surface new signals.

If a baseline already exists, use --force to overwrite it.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBaselineCreate,
}

// baselineSuppressCmd adds or updates a single suppression.
var baselineSuppressCmd = &cobra.Command{
	Use:   "suppress <signal-id>",
	Short: "Suppress a specific signal",
	Long: `Add a suppression for the given signal ID. If the signal is already
suppressed, its reason, comment, and expiry are updated.

Signal IDs follow the format str-XXXXXXXX (8 hex digits).`,
	Args: cobra.ExactArgs(1),
	RunE: runBaselineSuppress,
}

// baselineListCmd shows current suppressions.
var baselineListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all suppressions",
	Long: `Display all current suppressions in a table format.

Use --json for machine-readable output, --reason to filter by reason,
and --expired to show only expired suppressions.`,
	Args: cobra.NoArgs,
	RunE: runBaselineList,
}

// baselineRemoveCmd removes suppressions.
var baselineRemoveCmd = &cobra.Command{
	Use:   "remove <signal-id>",
	Short: "Remove a suppression",
	Long: `Remove a single suppression by signal ID, or use --expired to remove
all expired suppressions at once.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBaselineRemove,
}

// baselineStatusCmd shows baseline summary statistics.
var baselineStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show baseline summary statistics",
	Long: `Display an overview of the current baseline: total suppressions,
breakdown by reason, expired count, and oldest/newest suppression dates.`,
	Args: cobra.NoArgs,
	RunE: runBaselineStatus,
}

func init() {
	// create flags
	baselineCreateCmd.Flags().StringVar(&baselineCreateReason, "reason", "acknowledged",
		"suppression reason (acknowledged, won't-fix, false-positive)")
	baselineCreateCmd.Flags().StringVarP(&baselineCollectors, "collectors", "c", "",
		"comma-separated list of collectors to run")
	baselineCreateCmd.Flags().BoolVar(&baselineForce, "force", false,
		"overwrite existing baseline")

	// suppress flags
	baselineSuppressCmd.Flags().StringVar(&baselineSuppressReason, "reason", "acknowledged",
		"suppression reason (acknowledged, won't-fix, false-positive)")
	baselineSuppressCmd.Flags().StringVar(&baselineComment, "comment", "",
		"free-text comment")
	baselineSuppressCmd.Flags().StringVar(&baselineExpires, "expires", "",
		"expiration duration (e.g. 90d, 6m, 1y)")

	// list flags
	baselineListCmd.Flags().BoolVar(&baselineJSON, "json", false,
		"machine-readable JSON output")
	baselineListCmd.Flags().StringVar(&baselineListReason, "reason", "",
		"filter by reason")
	baselineListCmd.Flags().BoolVar(&baselineExpired, "expired", false,
		"show only expired suppressions")

	// remove flags
	baselineRemoveCmd.Flags().BoolVar(&baselineExpired, "expired", false,
		"remove all expired suppressions")

	// status flags
	baselineStatusCmd.Flags().BoolVar(&baselineJSON, "json", false,
		"structured JSON output")

	baselineCmd.AddCommand(baselineCreateCmd)
	baselineCmd.AddCommand(baselineSuppressCmd)
	baselineCmd.AddCommand(baselineListCmd)
	baselineCmd.AddCommand(baselineRemoveCmd)
	baselineCmd.AddCommand(baselineStatusCmd)

	rootCmd.AddCommand(baselineCmd)
}

// resetBaselineFlags resets baseline command flags for testing.
func resetBaselineFlags() {
	baselineCreateReason = "acknowledged"
	baselineSuppressReason = "acknowledged"
	baselineListReason = ""
	baselineCollectors = ""
	baselineForce = false
	baselineComment = ""
	baselineExpires = ""
	baselineJSON = false
	baselineExpired = false

	for _, cmd := range []*cobra.Command{
		baselineCreateCmd, baselineSuppressCmd, baselineListCmd,
		baselineRemoveCmd, baselineStatusCmd,
	} {
		cmd.Flags().VisitAll(func(f *pflag.Flag) {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
	}
}

func runBaselineCreate(cmd *cobra.Command, args []string) error {
	repoPath := "."
	if len(args) > 0 {
		repoPath = args[0]
	}

	absPath, gitRoot, err := resolveScanPath(repoPath)
	if err != nil {
		return err
	}

	reason := baseline.Reason(baselineCreateReason)
	if err := baseline.ValidateReason(reason); err != nil {
		return exitError(ExitInvalidArgs, "stringer: %v", err)
	}

	// Check for existing baseline.
	existing, err := baseline.Load(absPath)
	if err != nil {
		return exitError(ExitTotalFailure, "stringer: failed to load baseline (%v)", err)
	}
	if existing != nil && !baselineForce {
		return exitError(ExitInvalidArgs,
			"stringer: baseline already exists — use --force to overwrite")
	}

	// Build scan config.
	var collectors []string
	if baselineCollectors != "" {
		collectors = strings.Split(baselineCollectors, ",")
		for i := range collectors {
			collectors[i] = strings.TrimSpace(collectors[i])
		}
	}

	fileCfg, err := config.Load(absPath)
	if err != nil {
		return exitError(ExitInvalidArgs, "stringer: failed to load config (%v)", err)
	}

	scanCfg := signal.ScanConfig{
		RepoPath:   absPath,
		Collectors: collectors,
	}
	scanCfg = config.Merge(fileCfg, scanCfg)

	// Set GitRoot for subdirectory scans.
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

	p, err := pipeline.New(scanCfg)
	if err != nil {
		available := collector.List()
		sort.Strings(available)
		return exitError(ExitInvalidArgs, "stringer: %v (available: %s)", err, strings.Join(available, ", "))
	}

	result, err := p.Run(cmd.Context())
	if err != nil {
		return exitError(ExitTotalFailure, "stringer: scan failed (%v)", err)
	}

	// Build baseline state from scan signals.
	now := time.Now()
	user := gitUserName()
	state := &baseline.BaselineState{
		Version: "1",
	}

	collectorSet := make(map[string]bool)
	for _, sig := range result.Signals {
		id := output.SignalID(sig, "str-")
		baseline.AddOrUpdate(state, baseline.Suppression{
			SignalID:     id,
			Reason:       reason,
			SuppressedBy: user,
			SuppressedAt: now,
		})
		collectorSet[sig.Source] = true
	}

	if err := baseline.Save(absPath, state); err != nil {
		return exitError(ExitTotalFailure, "stringer: failed to save baseline (%v)", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Baselined %d signals from %d collectors\n",
		len(state.Suppressions), len(collectorSet))
	return nil
}

func runBaselineSuppress(cmd *cobra.Command, args []string) error {
	signalID := args[0]

	if !signalIDPattern.MatchString(signalID) {
		return exitError(ExitInvalidArgs,
			"stringer: invalid signal ID %q — must match str-[0-9a-f]{8}", signalID)
	}

	reason := baseline.Reason(baselineSuppressReason)
	if err := baseline.ValidateReason(reason); err != nil {
		return exitError(ExitInvalidArgs, "stringer: %v", err)
	}

	var expiresAt *time.Time
	if baselineExpires != "" {
		dur, err := parseDuration(baselineExpires)
		if err != nil {
			return exitError(ExitInvalidArgs, "stringer: invalid --expires value: %v", err)
		}
		t := time.Now().Add(dur)
		expiresAt = &t
	}

	absPath, err := cmdFS.Abs(".")
	if err != nil {
		return exitError(ExitInvalidArgs, "stringer: cannot resolve path (%v)", err)
	}

	state, err := baseline.Load(absPath)
	if err != nil {
		return exitError(ExitTotalFailure, "stringer: failed to load baseline (%v)", err)
	}
	if state == nil {
		state = &baseline.BaselineState{Version: "1"}
	}

	baseline.AddOrUpdate(state, baseline.Suppression{
		SignalID:     signalID,
		Reason:       reason,
		Comment:      baselineComment,
		SuppressedBy: gitUserName(),
		SuppressedAt: time.Now(),
		ExpiresAt:    expiresAt,
	})

	if err := baseline.Save(absPath, state); err != nil {
		return exitError(ExitTotalFailure, "stringer: failed to save baseline (%v)", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Suppressed %s (reason: %s)\n", signalID, reason)
	return nil
}

func runBaselineList(cmd *cobra.Command, _ []string) error {
	absPath, err := cmdFS.Abs(".")
	if err != nil {
		return exitError(ExitInvalidArgs, "stringer: cannot resolve path (%v)", err)
	}

	state, err := baseline.Load(absPath)
	if err != nil {
		return exitError(ExitTotalFailure, "stringer: failed to load baseline (%v)", err)
	}

	if state == nil || len(state.Suppressions) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No suppressions")
		return nil
	}

	// Apply filters.
	filtered := filterSuppressions(state.Suppressions)

	if len(filtered) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No suppressions")
		return nil
	}

	if baselineJSON {
		data, err := json.MarshalIndent(filtered, "", "  ")
		if err != nil {
			return exitError(ExitTotalFailure, "stringer: JSON marshal failed (%v)", err)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	// Table output.
	w := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(w, "%-14s %-16s %-40s %-16s %s\n",
		"ID", "Reason", "Comment", "SuppressedBy", "Age")
	_, _ = fmt.Fprintf(w, "%-14s %-16s %-40s %-16s %s\n",
		"--", "------", "-------", "------------", "---")

	for _, s := range filtered {
		comment := s.Comment
		if len(comment) > 40 {
			comment = comment[:37] + "..."
		}
		age := formatAge(s.SuppressedAt)
		_, _ = fmt.Fprintf(w, "%-14s %-16s %-40s %-16s %s\n",
			s.SignalID, string(s.Reason), comment, s.SuppressedBy, age)
	}

	return nil
}

func runBaselineRemove(cmd *cobra.Command, args []string) error {
	absPath, err := cmdFS.Abs(".")
	if err != nil {
		return exitError(ExitInvalidArgs, "stringer: cannot resolve path (%v)", err)
	}

	state, err := baseline.Load(absPath)
	if err != nil {
		return exitError(ExitTotalFailure, "stringer: failed to load baseline (%v)", err)
	}

	if state == nil {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No suppressions")
		return nil
	}

	if baselineExpired {
		// Remove all expired suppressions.
		count := 0
		var kept []baseline.Suppression
		for _, s := range state.Suppressions {
			if baseline.IsExpired(s) {
				count++
			} else {
				kept = append(kept, s)
			}
		}
		state.Suppressions = kept

		if err := baseline.Save(absPath, state); err != nil {
			return exitError(ExitTotalFailure, "stringer: failed to save baseline (%v)", err)
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed %d expired suppressions\n", count)
		return nil
	}

	if len(args) == 0 {
		return exitError(ExitInvalidArgs,
			"stringer: provide a signal ID or use --expired")
	}

	signalID := args[0]
	if !signalIDPattern.MatchString(signalID) {
		return exitError(ExitInvalidArgs,
			"stringer: invalid signal ID %q — must match str-[0-9a-f]{8}", signalID)
	}

	if !baseline.Remove(state, signalID) {
		return exitError(ExitInvalidArgs,
			"stringer: signal %s not found in baseline", signalID)
	}

	if err := baseline.Save(absPath, state); err != nil {
		return exitError(ExitTotalFailure, "stringer: failed to save baseline (%v)", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", signalID)
	return nil
}

func runBaselineStatus(cmd *cobra.Command, _ []string) error {
	absPath, err := cmdFS.Abs(".")
	if err != nil {
		return exitError(ExitInvalidArgs, "stringer: cannot resolve path (%v)", err)
	}

	state, err := baseline.Load(absPath)
	if err != nil {
		return exitError(ExitTotalFailure, "stringer: failed to load baseline (%v)", err)
	}

	if state == nil || len(state.Suppressions) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(),
			"No baseline — run `stringer baseline create` to establish one")
		return nil
	}

	total := len(state.Suppressions)
	byReason := make(map[baseline.Reason]int)
	expiredCount := 0
	var oldest, newest time.Time

	for _, s := range state.Suppressions {
		byReason[s.Reason]++
		if baseline.IsExpired(s) {
			expiredCount++
		}
		if oldest.IsZero() || s.SuppressedAt.Before(oldest) {
			oldest = s.SuppressedAt
		}
		if newest.IsZero() || s.SuppressedAt.After(newest) {
			newest = s.SuppressedAt
		}
	}

	if baselineJSON {
		type statusJSON struct {
			Total    int            `json:"total"`
			ByReason map[string]int `json:"by_reason"`
			Expired  int            `json:"expired"`
			Oldest   string         `json:"oldest"`
			Newest   string         `json:"newest"`
		}
		reasonMap := make(map[string]int, len(byReason))
		for r, c := range byReason {
			reasonMap[string(r)] = c
		}
		out := statusJSON{
			Total:    total,
			ByReason: reasonMap,
			Expired:  expiredCount,
			Oldest:   oldest.Format(time.RFC3339),
			Newest:   newest.Format(time.RFC3339),
		}
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return exitError(ExitTotalFailure, "stringer: JSON marshal failed (%v)", err)
		}
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}

	w := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(w, "Total suppressions: %d\n", total)

	// Sort reasons for stable output.
	reasons := make([]string, 0, len(byReason))
	for r := range byReason {
		reasons = append(reasons, string(r))
	}
	sort.Strings(reasons)
	for _, r := range reasons {
		_, _ = fmt.Fprintf(w, "  %-16s %d\n", r+":", byReason[baseline.Reason(r)])
	}

	_, _ = fmt.Fprintf(w, "Expired: %d\n", expiredCount)
	_, _ = fmt.Fprintf(w, "Oldest: %s\n", oldest.Format("2006-01-02"))
	_, _ = fmt.Fprintf(w, "Newest: %s\n", newest.Format("2006-01-02"))

	if total > 0 && float64(expiredCount)/float64(total) > 0.20 {
		_, _ = fmt.Fprintf(w, "\nWarning: >20%% of suppressions are expired — run `stringer baseline remove --expired`\n")
	}

	return nil
}

// filterSuppressions applies the --reason and --expired filters.
func filterSuppressions(suppressions []baseline.Suppression) []baseline.Suppression {
	var result []baseline.Suppression
	for _, s := range suppressions {
		if baselineListReason != "" && string(s.Reason) != baselineListReason {
			continue
		}
		if baselineExpired && !baseline.IsExpired(s) {
			continue
		}
		result = append(result, s)
	}
	return result
}

// formatAge returns a human-readable age string from a timestamp.
func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// gitUserName returns the git user.name or "unknown".
func gitUserName() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return "unknown"
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return "unknown"
	}
	return name
}

// parseDuration parses duration strings like "90d", "6m", "1y".
func parseDuration(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}
	numStr := s[:len(s)-1]
	unit := s[len(s)-1]

	var n int
	if _, err := fmt.Sscanf(numStr, "%d", &n); err != nil {
		return 0, fmt.Errorf("invalid duration number: %q", s)
	}

	switch unit {
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	case 'm':
		return time.Duration(n) * 30 * 24 * time.Hour, nil
	case 'y':
		return time.Duration(n) * 365 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid duration unit %q in %q (use d/w/m/y)", string(unit), s)
	}
}
