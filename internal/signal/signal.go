// Package signal defines the core domain types for stringer.
package signal

import "time"

// RawSignal represents a single actionable signal extracted from a repository.
type RawSignal struct {
	Source      string    // Collector name: "todos", "gitlog", etc.
	Kind        string    // Signal kind: "todo", "fixme", "revert", "churn", etc.
	FilePath    string    // Path within the repo where this was found.
	Line        int       // Line number (0 if not applicable).
	Title       string    // Short description (used as bead title).
	Description string    // Longer context (used as bead description).
	Author      string    // Git blame author or commit author.
	Timestamp   time.Time // When this signal was created.
	Confidence  float64   // 0.0-1.0, how certain we are this is real work.
	Tags        []string  // Free-form tags for clustering hints.
}

// CollectorOpts holds per-collector configuration options.
type CollectorOpts struct {
	// MinConfidence filters signals below this threshold.
	MinConfidence float64

	// IncludePatterns limits collection to files matching these globs.
	IncludePatterns []string

	// ExcludePatterns skips files matching these globs.
	ExcludePatterns []string
}

// ScanConfig holds the overall configuration for a scan operation.
type ScanConfig struct {
	// RepoPath is the path to the repository to scan.
	RepoPath string

	// Collectors lists the collector names to run. Empty means all registered.
	Collectors []string

	// OutputFormat specifies the output format (e.g., "beads", "json", "markdown").
	OutputFormat string

	// NoLLM disables the LLM clustering pass.
	NoLLM bool

	// CollectorOpts provides per-collector options keyed by collector name.
	CollectorOpts map[string]CollectorOpts

	// MaxIssues caps the number of output issues (0 = unlimited).
	MaxIssues int
}

// CollectorResult holds the output from a single collector run.
type CollectorResult struct {
	// Collector is the name of the collector that produced these signals.
	Collector string

	// Signals are the raw signals extracted.
	Signals []RawSignal

	// Duration is how long the collector took.
	Duration time.Duration

	// Err is any error encountered during collection.
	Err error
}

// ScanResult holds the aggregate output of a scan operation.
type ScanResult struct {
	// Signals is the combined list of signals from all collectors.
	Signals []RawSignal

	// Results is the per-collector breakdown.
	Results []CollectorResult

	// Duration is the total scan duration.
	Duration time.Duration
}
