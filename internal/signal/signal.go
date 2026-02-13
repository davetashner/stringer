// Package signal defines the core domain types for stringer.
package signal

import "time"

// ErrorMode controls how the pipeline handles errors from a collector.
type ErrorMode string

const (
	// ErrorModeWarn logs the error and continues (default).
	ErrorModeWarn ErrorMode = "warn"

	// ErrorModeSkip silently ignores errors.
	ErrorModeSkip ErrorMode = "skip"

	// ErrorModeFail aborts the entire scan on first error.
	ErrorModeFail ErrorMode = "fail"
)

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
	ClosedAt    time.Time // When this signal was closed/resolved (zero if open).
	Priority    *int      // LLM-inferred priority (1-4). Nil = use confidence mapping.
	Blocks      []string  // Bead IDs this signal blocks (downstream depends on this).
	DependsOn   []string  // Bead IDs this signal depends on (upstream blockers).
	Workspace   string    `json:"workspace,omitempty"` // Monorepo workspace name (empty for non-monorepo).
}

// CollectorOpts holds per-collector configuration options.
type CollectorOpts struct {
	// MinConfidence filters signals below this threshold.
	MinConfidence float64

	// IncludePatterns limits collection to files matching these globs.
	IncludePatterns []string

	// ExcludePatterns skips files matching these globs.
	ExcludePatterns []string

	// ErrorMode controls how errors from this collector are handled.
	// Default (zero value or empty string) is treated as ErrorModeWarn.
	ErrorMode ErrorMode

	// LargeFileThreshold overrides the default large-file line count.
	// If zero, the default (1000 lines) is used.
	LargeFileThreshold int

	// GitRoot is the path to the .git directory root, which may differ
	// from RepoPath when scanning a subdirectory.
	GitRoot string

	// GitDepth caps the number of commits walked. 0 uses default (1000).
	GitDepth int

	// GitSince limits commit walking to commits after this duration (e.g., "90d", "6m", "1y").
	GitSince string

	// ProgressFunc is called periodically with status messages during long operations.
	ProgressFunc func(msg string)

	// IncludeClosed includes closed/merged issues and PRs in the GitHub collector.
	IncludeClosed bool

	// HistoryDepth filters out closed items older than this duration (e.g., "6m", "90d").
	HistoryDepth string

	// Anonymize controls author name anonymization: "auto", "always", or "never".
	Anonymize string

	// IncludeDemoPaths disables the default suppression of noise-prone signals
	// (missing-tests, low-test-ratio, low-lottery-risk) in demo/example/tutorial paths.
	IncludeDemoPaths bool

	// MaxIssues caps the number of issues/PRs fetched by the GitHub collector.
	// 0 uses the collector default.
	MaxIssues int

	// Timeout is the per-collector timeout. 0 means no timeout.
	Timeout time.Duration

	// StalenessThreshold overrides the default staleness threshold for
	// dependency health checks (e.g., "2y", "18m"). If empty, the default
	// (2 years) is used.
	StalenessThreshold string
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

	// ExcludePatterns holds global exclude globs applied to all collectors.
	ExcludePatterns []string

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

	// Metrics holds optional structured data from collectors that implement
	// the MetricsProvider interface. Nil if the collector does not provide metrics.
	Metrics any
}

// ScanResult holds the aggregate output of a scan operation.
type ScanResult struct {
	// Signals is the combined list of signals from all collectors.
	Signals []RawSignal

	// Results is the per-collector breakdown.
	Results []CollectorResult

	// Duration is the total scan duration.
	Duration time.Duration

	// Metrics maps collector names to their structured metrics. Only populated
	// for collectors that implement the MetricsProvider interface.
	Metrics map[string]any
}
