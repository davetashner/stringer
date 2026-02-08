package config

import "github.com/davetashner/stringer/internal/signal"

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
			result.CollectorOpts[name] = co
		}
	}

	return result
}
