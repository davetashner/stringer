package config

import (
	"testing"

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
