package main

import (
	"log/slog"
	"time"

	"github.com/davetashner/stringer/internal/collector"
	"github.com/davetashner/stringer/internal/signal"
)

// flagOverrides holds CLI flag values that need to be wired into per-collector
// options. Both runScan and runReport build one of these and pass it to
// applyFlagOverrides so the wiring logic lives in a single place.
type flagOverrides struct {
	// Git history flags (apply to gitlog + lotteryrisk).
	GitDepth int
	GitSince string

	// Anonymize controls author name anonymization (lotteryrisk).
	// AnonymizeChanged indicates the flag was explicitly set by the user.
	Anonymize        string
	AnonymizeChanged bool

	// IncludeDemoPaths disables demo-path suppression (patterns + lotteryrisk).
	IncludeDemoPaths bool

	// CollectorTimeout is the global per-collector timeout string (e.g. "60s").
	CollectorTimeout string

	// Paths restricts scanning to specific files/directories (all collectors).
	Paths []string

	// IncludeClosed includes closed/merged GitHub issues (scan-only).
	IncludeClosed bool

	// HistoryDepth filters closed items older than this duration (scan-only).
	HistoryDepth string
}

// applyFlagOverrides wires CLI flag values into the per-collector options map
// on the given ScanConfig. It initialises CollectorOpts if nil, then applies
// each flag block in order. Zero-valued fields are skipped so callers only
// need to populate the fields relevant to their command.
func applyFlagOverrides(cfg *signal.ScanConfig, flags flagOverrides) {
	ensureOpts(cfg)

	// 1. git-depth / git-since → gitlog + lotteryrisk.
	if flags.GitDepth > 0 || flags.GitSince != "" {
		for _, name := range []string{"gitlog", "lotteryrisk"} {
			co := cfg.CollectorOpts[name]
			if flags.GitDepth > 0 && co.GitDepth == 0 {
				co.GitDepth = flags.GitDepth
			}
			if flags.GitSince != "" && co.GitSince == "" {
				co.GitSince = flags.GitSince
			}
			cfg.CollectorOpts[name] = co
		}
	}

	// 2. --include-closed / --history-depth → github (scan-only; zero when called from report).
	if flags.IncludeClosed || flags.HistoryDepth != "" {
		co := cfg.CollectorOpts["github"]
		if flags.IncludeClosed {
			co.IncludeClosed = true
		}
		if flags.HistoryDepth != "" && co.HistoryDepth == "" {
			co.HistoryDepth = flags.HistoryDepth
		}
		cfg.CollectorOpts["github"] = co
	}

	// 3. --anonymize → lotteryrisk.
	if flags.AnonymizeChanged {
		co := cfg.CollectorOpts["lotteryrisk"]
		co.Anonymize = flags.Anonymize
		cfg.CollectorOpts["lotteryrisk"] = co
	}

	// 4. --include-demo-paths → patterns + lotteryrisk.
	if flags.IncludeDemoPaths {
		for _, name := range []string{"patterns", "lotteryrisk"} {
			co := cfg.CollectorOpts[name]
			co.IncludeDemoPaths = true
			cfg.CollectorOpts[name] = co
		}
	}

	// 5. Progress callback → all collectors.
	progressFn := func(msg string) {
		slog.Debug(msg)
	}
	for _, name := range collector.List() {
		co := cfg.CollectorOpts[name]
		co.ProgressFunc = progressFn
		cfg.CollectorOpts[name] = co
	}

	// 6. --collector-timeout → all collectors without a per-collector timeout.
	if flags.CollectorTimeout != "" {
		if d, err := time.ParseDuration(flags.CollectorTimeout); err == nil && d > 0 {
			for _, name := range collector.List() {
				co := cfg.CollectorOpts[name]
				if co.Timeout == 0 {
					co.Timeout = d
				}
				cfg.CollectorOpts[name] = co
			}
		}
	}

	// 7. --paths → IncludePatterns on all collectors.
	if len(flags.Paths) > 0 {
		for _, name := range collector.List() {
			co := cfg.CollectorOpts[name]
			co.IncludePatterns = append(co.IncludePatterns, flags.Paths...)
			cfg.CollectorOpts[name] = co
		}
	}
}

// ensureOpts initialises the CollectorOpts map if it is nil.
func ensureOpts(cfg *signal.ScanConfig) {
	if cfg.CollectorOpts == nil {
		cfg.CollectorOpts = make(map[string]signal.CollectorOpts)
	}
}
