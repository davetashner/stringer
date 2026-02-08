package config

import (
	"fmt"
	"strings"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/output"
	"github.com/davetashner/stringer/internal/signal"
)

// Validate checks all fields in the config and returns all errors at once.
func Validate(cfg *Config) error {
	var errs []string

	if cfg.OutputFormat != "" {
		if _, err := output.GetFormatter(cfg.OutputFormat); err != nil {
			errs = append(errs, fmt.Sprintf("output_format: %v", err))
		}
	}

	if cfg.MaxIssues < 0 {
		errs = append(errs, fmt.Sprintf("max_issues: must be non-negative, got %d", cfg.MaxIssues))
	}

	for name, cc := range cfg.Collectors {
		if collector.Get(name) == nil {
			errs = append(errs, fmt.Sprintf("collectors.%s: unknown collector", name))
		}

		if cc.ErrorMode != "" {
			switch signal.ErrorMode(cc.ErrorMode) {
			case signal.ErrorModeWarn, signal.ErrorModeSkip, signal.ErrorModeFail:
				// valid
			default:
				errs = append(errs, fmt.Sprintf("collectors.%s.error_mode: invalid value %q (must be warn, skip, or fail)", name, cc.ErrorMode))
			}
		}

		if cc.MinConfidence < 0 || cc.MinConfidence > 1 {
			errs = append(errs, fmt.Sprintf("collectors.%s.min_confidence: must be between 0.0 and 1.0, got %g", name, cc.MinConfidence))
		}

		if cc.BusFactorThreshold < 0 {
			errs = append(errs, fmt.Sprintf("collectors.%s.bus_factor_threshold: must be non-negative, got %d", name, cc.BusFactorThreshold))
		}

		if cc.DirectoryDepth != 0 && (cc.DirectoryDepth < 1 || cc.DirectoryDepth > 10) {
			errs = append(errs, fmt.Sprintf("collectors.%s.directory_depth: must be between 1 and 10, got %d", name, cc.DirectoryDepth))
		}

		if cc.MaxBlameFiles != 0 && (cc.MaxBlameFiles < 1 || cc.MaxBlameFiles > 1000) {
			errs = append(errs, fmt.Sprintf("collectors.%s.max_blame_files: must be between 1 and 1000, got %d", name, cc.MaxBlameFiles))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation failed:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}
