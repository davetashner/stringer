// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/config"
)

// collectorMeta holds presentation metadata for each collector.
type collectorMeta struct {
	Description  string
	SignalKinds  []string
	ConfigFields []string // yaml tag names from CollectorConfig that are relevant
}

// knownCollectors maps collector names to their metadata.
// Common fields (enabled, error_mode, min_confidence, include_patterns,
// exclude_patterns, anonymize, include_demo_paths, timeout) apply to all
// collectors and are not listed per-collector.
var knownCollectors = map[string]collectorMeta{
	"todos": {
		Description:  "Scans for TODO, FIXME, HACK, XXX, BUG, and OPTIMIZE comments",
		SignalKinds:  []string{"todo", "fixme", "hack", "xxx", "bug", "optimize"},
		ConfigFields: []string{},
	},
	"gitlog": {
		Description:  "Detects reverts, high-churn files, and stale branches from git history",
		SignalKinds:  []string{"revert", "churn", "stale-branch"},
		ConfigFields: []string{"git_depth", "git_since"},
	},
	"patterns": {
		Description:  "Detects large files, missing tests, and low test-to-source ratios",
		SignalKinds:  []string{"large-file", "missing-tests", "low-test-ratio"},
		ConfigFields: []string{"large_file_threshold"},
	},
	"github": {
		Description:  "Imports open issues, pull requests, and actionable review comments from GitHub",
		SignalKinds:  []string{"github-issue", "github-pr", "github-review-todo"},
		ConfigFields: []string{"include_prs", "comment_depth", "max_issues_per_collector", "include_closed", "history_depth"},
	},
	"lotteryrisk": {
		Description:  "Analyzes git blame and commit history to find single-author risk areas (accuracy improves with full git history; shallow clones may underreport)",
		SignalKinds:  []string{"low-lottery-risk", "review-concentration"},
		ConfigFields: []string{"lottery_risk_threshold", "directory_depth", "max_blame_files"},
	},
	"vuln": {
		Description:  "Detects known vulnerabilities via OSV.dev across Go, npm, Maven, Cargo, NuGet, and Python",
		SignalKinds:  []string{"vulnerable-dependency"},
		ConfigFields: []string{},
	},
	"dephealth": {
		Description:  "Detects deprecated, yanked, archived, and stale dependencies",
		SignalKinds:  []string{"deprecated-dependency", "yanked-dependency", "archived-dependency", "stale-dependency"},
		ConfigFields: []string{},
	},
	"complexity": {
		Description:  "Detects complex functions using composite scoring (lines/50 + branches)",
		SignalKinds:  []string{"complex-function"},
		ConfigFields: []string{"min_function_lines", "min_complexity_score"},
	},
	"deadcode": {
		Description:  "Detects unused functions and types via regex heuristic and reference search",
		SignalKinds:  []string{"unused-function", "unused-type"},
		ConfigFields: []string{},
	},
	"duplication": {
		Description:  "Detects copy-paste code duplication using token-based clone detection",
		SignalKinds:  []string{"code-clone", "near-clone"},
		ConfigFields: []string{},
	},
	"githygiene": {
		Description:  "Detects large binaries, merge conflict markers, committed secrets, and mixed line endings",
		SignalKinds:  []string{"large-binary", "merge-conflict-marker", "committed-secret", "mixed-line-endings"},
		ConfigFields: []string{},
	},
	"docstale": {
		Description:  "Detects stale documentation, co-change drift between docs and source, and broken internal links",
		SignalKinds:  []string{"stale-doc", "doc-code-drift", "broken-doc-link"},
		ConfigFields: []string{},
	},
	"configdrift": {
		Description:  "Detects env var drift, dead config keys, and inconsistent defaults across environment files",
		SignalKinds:  []string{"env-var-drift", "dead-config-key", "inconsistent-defaults"},
		ConfigFields: []string{},
	},
	"apidrift": {
		Description:  "Detects drift between OpenAPI/Swagger specs and route handler registrations in code",
		SignalKinds:  []string{"undocumented-route", "unimplemented-route", "stale-api-version"},
		ConfigFields: []string{},
	},
	"coupling": {
		Description:  "Detects circular dependencies and high-coupling modules via import graph analysis",
		SignalKinds:  []string{"circular-dependency", "high-coupling"},
		ConfigFields: []string{},
	},
}

// Common config fields that apply to every collector.
var commonConfigFields = []string{
	"enabled", "error_mode", "min_confidence",
	"include_patterns", "exclude_patterns",
	"anonymize", "include_demo_paths", "timeout",
}

// ThresholdInfo describes a single configurable threshold for a collector.
type ThresholdInfo struct {
	Name      string `json:"name"`       // e.g. "duplication_window_size"
	Default   string `json:"default"`    // e.g. "6"
	Current   string `json:"current"`    // e.g. "8" (from config) or same as default
	ConfigKey string `json:"config_key"` // e.g. "collectors.duplication.duplication_window_size"
}

// collectorThresholds maps collector names to their tunable threshold definitions.
// Each entry lists the yaml field name, default value, and config key path.
var collectorThresholds = map[string][]struct {
	Field      string
	DefaultVal string
}{
	"duplication": {
		{"duplication_window_size", "6"},
		{"duplication_signal_cap", "200"},
		{"duplication_max_files", "10000"},
	},
	"deadcode": {
		{"deadcode_max_files", "10000"},
	},
	"coupling": {
		{"coupling_fan_out_threshold", "10"},
		{"coupling_max_files", "10000"},
	},
	"docstale": {
		{"doc_stale_days", "180"},
		{"doc_drift_min_commits", "10"},
	},
	"githygiene": {
		{"large_binary_threshold", "1000000"},
	},
	"patterns": {
		{"large_file_threshold", "1500"},
		{"test_ratio_threshold", "0.1"},
		{"test_ratio_min_files", "3"},
	},
	"complexity": {
		{"min_complexity_score", "6"},
		{"min_function_lines", "5"},
	},
	"lotteryrisk": {
		{"lottery_risk_threshold", "1"},
	},
}

// collectorsInfoJSON controls --json output for the info subcommand.
var collectorsInfoJSON bool

// collectorsCmd is the parent command for collector introspection.
var collectorsCmd = &cobra.Command{
	Use:   "collectors",
	Short: "List and inspect available collectors",
	Long: `Commands for listing and inspecting the collectors registered in stringer.

Collectors are the signal-extraction modules that scan repositories for
actionable work items. Each collector focuses on a specific signal source
(TODO comments, git history, vulnerabilities, etc.).`,
}

// collectorsListCmd shows all registered collectors.
var collectorsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registered collectors",
	Long: `List all registered collectors with their description and enabled status.

The enabled/disabled status reflects the current .stringer.yaml config
in the working directory. Collectors are enabled by default unless
explicitly disabled in config.`,
	Args: cobra.NoArgs,
	RunE: runCollectorsList,
}

// collectorsInfoCmd shows detailed info about a specific collector.
var collectorsInfoCmd = &cobra.Command{
	Use:   "info <name>",
	Short: "Show detailed info about a collector",
	Long: `Show detailed information about a specific collector, including its
description, signal types it produces, and available configuration options
with their current values from .stringer.yaml.`,
	Args: cobra.ExactArgs(1),
	RunE: runCollectorsInfo,
}

func init() {
	collectorsCmd.AddCommand(collectorsListCmd)
	collectorsCmd.AddCommand(collectorsInfoCmd)
	collectorsInfoCmd.Flags().BoolVar(&collectorsInfoJSON, "json", false, "output in JSON format")
}

func runCollectorsList(cmd *cobra.Command, _ []string) error {
	w := cmd.OutOrStdout()

	names := collector.List()
	sort.Strings(names)

	cfg, _ := config.Load(".") // best-effort; zero config if missing

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	bold := color.New(color.Bold)
	green := color.New(color.FgGreen)
	red := color.New(color.FgRed)

	_, _ = fmt.Fprintln(tw, bold.Sprint("NAME")+"\t"+bold.Sprint("STATUS")+"\t"+bold.Sprint("DESCRIPTION"))

	for _, name := range names {
		status := green.Sprint("enabled")
		if cc, ok := cfg.Collectors[name]; ok && cc.Enabled != nil && !*cc.Enabled {
			status = red.Sprint("disabled")
		}

		desc := name
		if meta, ok := knownCollectors[name]; ok {
			desc = meta.Description
		}

		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", name, status, desc)
	}

	return tw.Flush()
}

func runCollectorsInfo(cmd *cobra.Command, args []string) error {
	name := args[0]
	w := cmd.OutOrStdout()

	c := collector.Get(name)
	if c == nil {
		registered := collector.List()
		sort.Strings(registered)
		return fmt.Errorf("unknown collector %q; registered collectors: %s",
			name, strings.Join(registered, ", "))
	}

	meta, hasMeta := knownCollectors[name]

	// Load config.
	cfg, _ := config.Load(".")

	cc := config.CollectorConfig{}
	if cfgCC, ok := cfg.Collectors[name]; ok {
		cc = cfgCC
	}

	status := "enabled"
	if cc.Enabled != nil && !*cc.Enabled {
		status = "disabled"
	}

	// Build threshold info.
	thresholds := buildThresholds(name, cc)

	if collectorsInfoJSON {
		return renderCollectorsInfoJSON(w, name, meta, hasMeta, status, cc, thresholds)
	}

	bold := color.New(color.Bold)

	// Header.
	_, _ = fmt.Fprintf(w, "%s %s\n", bold.Sprint("Collector:"), name)
	if hasMeta {
		_, _ = fmt.Fprintf(w, "%s %s\n", bold.Sprint("Description:"), meta.Description)
	}
	_, _ = fmt.Fprintf(w, "%s %s\n", bold.Sprint("Status:"), status)

	// Signal kinds.
	if hasMeta && len(meta.SignalKinds) > 0 {
		_, _ = fmt.Fprintf(w, "\n%s\n", bold.Sprint("Signal types:"))
		for _, kind := range meta.SignalKinds {
			_, _ = fmt.Fprintf(w, "  - %s\n", kind)
		}
	}

	// Config options.
	_, _ = fmt.Fprintf(w, "\n%s\n", bold.Sprint("Configuration options:"))

	allFields := commonConfigFields
	if hasMeta {
		allFields = append(allFields, meta.ConfigFields...)
	}
	printConfigFields(w, cc, allFields)

	// Thresholds.
	if len(thresholds) > 0 {
		_, _ = fmt.Fprintf(w, "\n%s\n", bold.Sprint("Thresholds:"))
		tw := tabwriter.NewWriter(w, 0, 0, 4, ' ', 0)
		_, _ = fmt.Fprintln(tw, "  Name\tDefault\tCurrent\tConfig Key")
		for _, ti := range thresholds {
			_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n",
				ti.Name, ti.Default, ti.Current, ti.ConfigKey)
		}
		_ = tw.Flush()
	}

	return nil
}

// buildThresholds constructs ThresholdInfo entries for the named collector
// using the loaded CollectorConfig to determine current values.
func buildThresholds(name string, cc config.CollectorConfig) []ThresholdInfo {
	defs, ok := collectorThresholds[name]
	if !ok {
		return nil
	}

	// Build yaml tag → field index map for CollectorConfig.
	rv := reflect.ValueOf(cc)
	rt := rv.Type()
	tagIdx := make(map[string]int)
	for i := range rt.NumField() {
		tag := rt.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		yamlName := strings.Split(tag, ",")[0]
		tagIdx[yamlName] = i
	}

	var thresholds []ThresholdInfo
	for _, def := range defs {
		current := def.DefaultVal
		if idx, found := tagIdx[def.Field]; found {
			fv := rv.Field(idx)
			if !fv.IsZero() {
				current = fmt.Sprintf("%v", fv.Interface())
			}
		}
		thresholds = append(thresholds, ThresholdInfo{
			Name:      def.Field,
			Default:   def.DefaultVal,
			Current:   current,
			ConfigKey: fmt.Sprintf("collectors.%s.%s", name, def.Field),
		})
	}
	return thresholds
}

// collectorInfoJSON is the JSON representation of collector info output.
type collectorInfoJSON struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Status      string          `json:"status"`
	SignalTypes []string        `json:"signal_types,omitempty"`
	Thresholds  []ThresholdInfo `json:"thresholds,omitempty"`
}

func renderCollectorsInfoJSON(w interface{ Write([]byte) (int, error) },
	name string, meta collectorMeta, hasMeta bool,
	status string, _ config.CollectorConfig, thresholds []ThresholdInfo,
) error {
	info := collectorInfoJSON{
		Name:   name,
		Status: status,
	}
	if hasMeta {
		info.Description = meta.Description
		info.SignalTypes = meta.SignalKinds
	}
	if len(thresholds) > 0 {
		info.Thresholds = thresholds
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(info)
}

// printConfigFields prints config field names and current values.
func printConfigFields(w interface{ Write([]byte) (int, error) }, cc config.CollectorConfig, fields []string) {
	rv := reflect.ValueOf(cc)
	rt := rv.Type()

	// Build yaml tag → field index map.
	tagIdx := make(map[string]int)
	for i := range rt.NumField() {
		tag := rt.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		yamlName := strings.Split(tag, ",")[0]
		tagIdx[yamlName] = i
	}

	for _, fieldName := range fields {
		idx, ok := tagIdx[fieldName]
		if !ok {
			continue
		}
		fv := rv.Field(idx)
		val := formatFieldValue(fv)
		_, _ = fmt.Fprintf(w, "  %-28s %s\n", fieldName+":", val)
	}
}

// formatFieldValue returns a display string for a reflected config field value.
func formatFieldValue(fv reflect.Value) string {
	if fv.Kind() == reflect.Ptr {
		if fv.IsNil() {
			return "(default)"
		}
		return fmt.Sprintf("%v", fv.Elem().Interface())
	}
	if fv.Kind() == reflect.Slice {
		if fv.IsNil() || fv.Len() == 0 {
			return "(none)"
		}
		items := make([]string, fv.Len())
		for i := range fv.Len() {
			items[i] = fmt.Sprintf("%v", fv.Index(i).Interface())
		}
		return strings.Join(items, ", ")
	}
	if fv.IsZero() {
		return "(default)"
	}
	return fmt.Sprintf("%v", fv.Interface())
}
