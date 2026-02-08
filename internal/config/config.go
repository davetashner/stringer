// Package config handles .stringer.yaml configuration files.
package config

// Config represents the contents of a .stringer.yaml file.
type Config struct {
	OutputFormat string                     `yaml:"output_format,omitempty"`
	MaxIssues    int                        `yaml:"max_issues,omitempty"`
	NoLLM        bool                       `yaml:"no_llm,omitempty"`
	BeadsAware   *bool                      `yaml:"beads_aware,omitempty"`
	Collectors   map[string]CollectorConfig `yaml:"collectors,omitempty"`
}

// CollectorConfig holds per-collector settings in the config file.
type CollectorConfig struct {
	Enabled         *bool    `yaml:"enabled,omitempty"`
	ErrorMode       string   `yaml:"error_mode,omitempty"`
	MinConfidence   float64  `yaml:"min_confidence,omitempty"`
	IncludePatterns []string `yaml:"include_patterns,omitempty"`
	ExcludePatterns []string `yaml:"exclude_patterns,omitempty"`

	// Lottery risk collector settings.
	LotteryRiskThreshold int `yaml:"lottery_risk_threshold,omitempty"`
	DirectoryDepth       int `yaml:"directory_depth,omitempty"`
	MaxBlameFiles        int `yaml:"max_blame_files,omitempty"`

	// GitHub collector settings.
	IncludePRs            *bool `yaml:"include_prs,omitempty"`
	CommentDepth          int   `yaml:"comment_depth,omitempty"`
	MaxIssuesPerCollector int   `yaml:"max_issues_per_collector,omitempty"`
}

// FileName is the expected config file name in a repository root.
const FileName = ".stringer.yaml"
