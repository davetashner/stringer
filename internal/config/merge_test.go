package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/davetashner/stringer/internal/signal"
)

func TestMerge_CLIOverridesFile(t *testing.T) {
	fileCfg := &Config{
		OutputFormat: "markdown",
		MaxIssues:    100,
		NoLLM:        true,
	}
	cliCfg := signal.ScanConfig{
		OutputFormat: "json",
		MaxIssues:    10,
	}

	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, "json", result.OutputFormat)
	assert.Equal(t, 10, result.MaxIssues)
}

func TestMerge_FileFillsInDefaults(t *testing.T) {
	fileCfg := &Config{
		OutputFormat: "markdown",
		MaxIssues:    50,
		NoLLM:        true,
	}
	cliCfg := signal.ScanConfig{} // all zero values

	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, "markdown", result.OutputFormat)
	assert.Equal(t, 50, result.MaxIssues)
	assert.True(t, result.NoLLM)
}

func TestMerge_EmptyFileConfig(t *testing.T) {
	fileCfg := &Config{}
	cliCfg := signal.ScanConfig{
		OutputFormat: "beads",
		MaxIssues:    5,
	}

	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, "beads", result.OutputFormat)
	assert.Equal(t, 5, result.MaxIssues)
}

func TestMerge_PerCollectorOpts(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"todos": {
				MinConfidence:   0.7,
				ErrorMode:       "fail",
				IncludePatterns: []string{"*.go"},
				ExcludePatterns: []string{"vendor/**"},
			},
		},
	}
	cliCfg := signal.ScanConfig{}

	result := Merge(fileCfg, cliCfg)
	require := assert.New(t)
	require.Contains(result.CollectorOpts, "todos")
	opts := result.CollectorOpts["todos"]
	assert.InDelta(t, 0.7, opts.MinConfidence, 0.001)
	assert.Equal(t, signal.ErrorModeFail, opts.ErrorMode)
	assert.Equal(t, []string{"*.go"}, opts.IncludePatterns)
	assert.Equal(t, []string{"vendor/**"}, opts.ExcludePatterns)
}

func TestMerge_CLICollectorOptsOverrideFile(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"todos": {
				MinConfidence: 0.3,
				ErrorMode:     "skip",
			},
		},
	}
	cliCfg := signal.ScanConfig{
		CollectorOpts: map[string]signal.CollectorOpts{
			"todos": {
				MinConfidence: 0.8,
				ErrorMode:     signal.ErrorModeWarn,
			},
		},
	}

	result := Merge(fileCfg, cliCfg)
	opts := result.CollectorOpts["todos"]
	// CLI values should win.
	assert.InDelta(t, 0.8, opts.MinConfidence, 0.001)
	assert.Equal(t, signal.ErrorModeWarn, opts.ErrorMode)
}

func TestMerge_NoLLMCLITrue(t *testing.T) {
	fileCfg := &Config{NoLLM: false}
	cliCfg := signal.ScanConfig{NoLLM: true}
	result := Merge(fileCfg, cliCfg)
	assert.True(t, result.NoLLM)
}

func TestMerge_NoLLMFileTrue(t *testing.T) {
	fileCfg := &Config{NoLLM: true}
	cliCfg := signal.ScanConfig{NoLLM: false}
	result := Merge(fileCfg, cliCfg)
	assert.True(t, result.NoLLM)
}

func TestMerge_PreservesRepoPath(t *testing.T) {
	fileCfg := &Config{OutputFormat: "json"}
	cliCfg := signal.ScanConfig{RepoPath: "/my/repo"}
	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, "/my/repo", result.RepoPath)
}

func TestMerge_PreservesCollectors(t *testing.T) {
	fileCfg := &Config{}
	cliCfg := signal.ScanConfig{Collectors: []string{"todos"}}
	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, []string{"todos"}, result.Collectors)
}

func TestMerge_FileCollectorOptsNewCollector(t *testing.T) {
	// File config adds opts for a collector not specified in CLI opts.
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"gitlog": {
				MinConfidence: 0.4,
			},
		},
	}
	cliCfg := signal.ScanConfig{
		CollectorOpts: map[string]signal.CollectorOpts{
			"todos": {MinConfidence: 0.5},
		},
	}

	result := Merge(fileCfg, cliCfg)
	assert.InDelta(t, 0.5, result.CollectorOpts["todos"].MinConfidence, 0.001)
	assert.InDelta(t, 0.4, result.CollectorOpts["gitlog"].MinConfidence, 0.001)
}

func TestMerge_LargeFileThresholdFromFile(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"patterns": {
				LargeFileThreshold: 500,
			},
		},
	}
	cliCfg := signal.ScanConfig{}

	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, 500, result.CollectorOpts["patterns"].LargeFileThreshold)
}

func TestMerge_LargeFileThresholdCLIOverride(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"patterns": {
				LargeFileThreshold: 500,
			},
		},
	}
	cliCfg := signal.ScanConfig{
		CollectorOpts: map[string]signal.CollectorOpts{
			"patterns": {LargeFileThreshold: 750},
		},
	}

	result := Merge(fileCfg, cliCfg)
	// CLI value should win.
	assert.Equal(t, 750, result.CollectorOpts["patterns"].LargeFileThreshold)
}

func TestMerge_GitDepthFromFile(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"gitlog": {
				GitDepth: 500,
			},
		},
	}
	cliCfg := signal.ScanConfig{}

	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, 500, result.CollectorOpts["gitlog"].GitDepth)
}

func TestMerge_GitDepthCLIOverridesFile(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"gitlog": {
				GitDepth: 500,
			},
		},
	}
	cliCfg := signal.ScanConfig{
		CollectorOpts: map[string]signal.CollectorOpts{
			"gitlog": {GitDepth: 200},
		},
	}

	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, 200, result.CollectorOpts["gitlog"].GitDepth)
}

func TestMerge_GitSinceFromFile(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"gitlog": {
				GitSince: "90d",
			},
		},
	}
	cliCfg := signal.ScanConfig{}

	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, "90d", result.CollectorOpts["gitlog"].GitSince)
}

func TestMerge_GitSinceCLIOverridesFile(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"gitlog": {
				GitSince: "90d",
			},
		},
	}
	cliCfg := signal.ScanConfig{
		CollectorOpts: map[string]signal.CollectorOpts{
			"gitlog": {GitSince: "30d"},
		},
	}

	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, "30d", result.CollectorOpts["gitlog"].GitSince)
}

func TestMerge_HistoryDepthFromFile(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"github": {HistoryDepth: "6m"},
		},
	}
	cliCfg := signal.ScanConfig{}

	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, "6m", result.CollectorOpts["github"].HistoryDepth)
}

func TestMerge_HistoryDepthCLIOverridesFile(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"github": {HistoryDepth: "6m"},
		},
	}
	cliCfg := signal.ScanConfig{
		CollectorOpts: map[string]signal.CollectorOpts{
			"github": {HistoryDepth: "90d"},
		},
	}

	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, "90d", result.CollectorOpts["github"].HistoryDepth)
}

func TestMerge_GitDepthAndSinceBothFromFile(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"lotteryrisk": {
				GitDepth: 300,
				GitSince: "6m",
			},
		},
	}
	cliCfg := signal.ScanConfig{}

	result := Merge(fileCfg, cliCfg)
	opts := result.CollectorOpts["lotteryrisk"]
	assert.Equal(t, 300, opts.GitDepth)
	assert.Equal(t, "6m", opts.GitSince)
}

func TestMerge_TimeoutFromFile(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"todos": {Timeout: "60s"},
		},
	}
	cliCfg := signal.ScanConfig{}

	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, 60*time.Second, result.CollectorOpts["todos"].Timeout)
}

func TestMerge_TimeoutCLIOverridesFile(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"todos": {Timeout: "60s"},
		},
	}
	cliCfg := signal.ScanConfig{
		CollectorOpts: map[string]signal.CollectorOpts{
			"todos": {Timeout: 30 * time.Second},
		},
	}

	result := Merge(fileCfg, cliCfg)
	// CLI value should win.
	assert.Equal(t, 30*time.Second, result.CollectorOpts["todos"].Timeout)
}

func TestMerge_IncludeDemoPathsFromFile(t *testing.T) {
	boolTrue := true
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"patterns": {IncludeDemoPaths: &boolTrue},
		},
	}
	cliCfg := signal.ScanConfig{}

	result := Merge(fileCfg, cliCfg)
	assert.True(t, result.CollectorOpts["patterns"].IncludeDemoPaths)
}

func TestMerge_IncludeDemoPathsCLIOverridesFile(t *testing.T) {
	boolTrue := true
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"patterns": {IncludeDemoPaths: &boolTrue},
		},
	}
	cliCfg := signal.ScanConfig{
		CollectorOpts: map[string]signal.CollectorOpts{
			"patterns": {IncludeDemoPaths: true},
		},
	}

	result := Merge(fileCfg, cliCfg)
	// CLI already true, should remain true.
	assert.True(t, result.CollectorOpts["patterns"].IncludeDemoPaths)
}

func TestMerge_IncludeDemoPathsDefaultFalse(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"patterns": {},
		},
	}
	cliCfg := signal.ScanConfig{}

	result := Merge(fileCfg, cliCfg)
	assert.False(t, result.CollectorOpts["patterns"].IncludeDemoPaths)
}

func TestMerge_IncludeClosedFromFile(t *testing.T) {
	boolTrue := true
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"github": {IncludeClosed: &boolTrue},
		},
	}
	cliCfg := signal.ScanConfig{}

	result := Merge(fileCfg, cliCfg)
	assert.True(t, result.CollectorOpts["github"].IncludeClosed)
}

func TestMerge_IncludeClosedFalseNotOverridden(t *testing.T) {
	boolFalse := false
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"github": {IncludeClosed: &boolFalse},
		},
	}
	cliCfg := signal.ScanConfig{}

	result := Merge(fileCfg, cliCfg)
	assert.False(t, result.CollectorOpts["github"].IncludeClosed)
}

func TestMerge_AnonymizeFromFile(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"github": {Anonymize: "always"},
		},
	}
	cliCfg := signal.ScanConfig{}

	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, "always", result.CollectorOpts["github"].Anonymize)
}

func TestMerge_AnonymizeCLIOverridesFile(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"github": {Anonymize: "always"},
		},
	}
	cliCfg := signal.ScanConfig{
		CollectorOpts: map[string]signal.CollectorOpts{
			"github": {Anonymize: "never"},
		},
	}

	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, "never", result.CollectorOpts["github"].Anonymize)
}

func TestMerge_MaxIssuesPerCollectorFromFile(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"github": {MaxIssuesPerCollector: 50},
		},
	}
	cliCfg := signal.ScanConfig{}

	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, 50, result.CollectorOpts["github"].MaxIssues)
}

func TestMerge_MaxIssuesPerCollectorCLIOverridesFile(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"github": {MaxIssuesPerCollector: 50},
		},
	}
	cliCfg := signal.ScanConfig{
		CollectorOpts: map[string]signal.CollectorOpts{
			"github": {MaxIssues: 10},
		},
	}

	result := Merge(fileCfg, cliCfg)
	// CLI value should win.
	assert.Equal(t, 10, result.CollectorOpts["github"].MaxIssues)
}

func TestMerge_TimeoutInvalidDurationIgnored(t *testing.T) {
	fileCfg := &Config{
		Collectors: map[string]CollectorConfig{
			"todos": {Timeout: "not-a-duration"},
		},
	}
	cliCfg := signal.ScanConfig{}

	result := Merge(fileCfg, cliCfg)
	assert.Equal(t, time.Duration(0), result.CollectorOpts["todos"].Timeout)
}
