// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package config

import (
	"time"

	"github.com/davetashner/stringer/internal/signal"
)

// Merge combines file-based config with CLI-provided ScanConfig.
// CLI values take precedence; zero-value CLI fields fall through to file config.
func Merge(fileCfg *Config, cliCfg signal.ScanConfig) signal.ScanConfig {
	result := cliCfg

	// OutputFormat: CLI wins if set.
	if result.OutputFormat == "" && fileCfg.OutputFormat != "" {
		result.OutputFormat = fileCfg.OutputFormat
	}

	// MaxIssues: CLI wins if non-zero.
	if result.MaxIssues == 0 && fileCfg.MaxIssues > 0 {
		result.MaxIssues = fileCfg.MaxIssues
	}

	// NoLLM: CLI wins if true, otherwise file config.
	if !result.NoLLM && fileCfg.NoLLM {
		result.NoLLM = true
	}

	// Per-collector opts: merge file config into CLI config.
	if len(fileCfg.Collectors) > 0 {
		if result.CollectorOpts == nil {
			result.CollectorOpts = make(map[string]signal.CollectorOpts)
		}
		for name, fc := range fileCfg.Collectors {
			co := result.CollectorOpts[name]
			if co.MinConfidence == 0 && fc.MinConfidence > 0 {
				co.MinConfidence = fc.MinConfidence
			}
			if len(co.IncludePatterns) == 0 && len(fc.IncludePatterns) > 0 {
				co.IncludePatterns = fc.IncludePatterns
			}
			if len(co.ExcludePatterns) == 0 && len(fc.ExcludePatterns) > 0 {
				co.ExcludePatterns = fc.ExcludePatterns
			}
			if co.ErrorMode == "" && fc.ErrorMode != "" {
				co.ErrorMode = signal.ErrorMode(fc.ErrorMode)
			}
			if co.LargeFileThreshold == 0 && fc.LargeFileThreshold > 0 {
				co.LargeFileThreshold = fc.LargeFileThreshold
			}
			if co.GitDepth == 0 && fc.GitDepth > 0 {
				co.GitDepth = fc.GitDepth
			}
			if co.GitSince == "" && fc.GitSince != "" {
				co.GitSince = fc.GitSince
			}
			if !co.IncludeClosed && fc.IncludeClosed != nil && *fc.IncludeClosed {
				co.IncludeClosed = true
			}
			if co.Anonymize == "" && fc.Anonymize != "" {
				co.Anonymize = fc.Anonymize
			}
			if !co.IncludeDemoPaths && fc.IncludeDemoPaths != nil && *fc.IncludeDemoPaths {
				co.IncludeDemoPaths = true
			}
			if co.HistoryDepth == "" && fc.HistoryDepth != "" {
				co.HistoryDepth = fc.HistoryDepth
			}
			if co.MaxIssues == 0 && fc.MaxIssuesPerCollector > 0 {
				co.MaxIssues = fc.MaxIssuesPerCollector
			}
			if co.Timeout == 0 && fc.Timeout != "" {
				if d, err := time.ParseDuration(fc.Timeout); err == nil {
					co.Timeout = d
				}
			}
			if co.MinFunctionLines == 0 && fc.MinFunctionLines > 0 {
				co.MinFunctionLines = fc.MinFunctionLines
			}
			if co.MinComplexityScore == 0 && fc.MinComplexityScore > 0 {
				co.MinComplexityScore = fc.MinComplexityScore
			}
			if co.DuplicationWindowSize == 0 && fc.DuplicationWindowSize > 0 {
				co.DuplicationWindowSize = fc.DuplicationWindowSize
			}
			if co.DuplicationSignalCap == 0 && fc.DuplicationSignalCap > 0 {
				co.DuplicationSignalCap = fc.DuplicationSignalCap
			}
			if co.DuplicationMaxFiles == 0 && fc.DuplicationMaxFiles > 0 {
				co.DuplicationMaxFiles = fc.DuplicationMaxFiles
			}
			if co.DeadcodeMaxFiles == 0 && fc.DeadcodeMaxFiles > 0 {
				co.DeadcodeMaxFiles = fc.DeadcodeMaxFiles
			}
			if co.CouplingFanOutThreshold == 0 && fc.CouplingFanOutThreshold > 0 {
				co.CouplingFanOutThreshold = fc.CouplingFanOutThreshold
			}
			if co.CouplingMaxFiles == 0 && fc.CouplingMaxFiles > 0 {
				co.CouplingMaxFiles = fc.CouplingMaxFiles
			}
			if co.DocStaleDays == 0 && fc.DocStaleDays > 0 {
				co.DocStaleDays = fc.DocStaleDays
			}
			if co.DocDriftMinCommits == 0 && fc.DocDriftMinCommits > 0 {
				co.DocDriftMinCommits = fc.DocDriftMinCommits
			}
			if co.LargeBinaryThreshold == 0 && fc.LargeBinaryThreshold > 0 {
				co.LargeBinaryThreshold = fc.LargeBinaryThreshold
			}
			if co.TestRatioThreshold == 0 && fc.TestRatioThreshold > 0 {
				co.TestRatioThreshold = fc.TestRatioThreshold
			}
			if co.TestRatioMinFiles == 0 && fc.TestRatioMinFiles > 0 {
				co.TestRatioMinFiles = fc.TestRatioMinFiles
			}
			result.CollectorOpts[name] = co
		}
	}

	return result
}
