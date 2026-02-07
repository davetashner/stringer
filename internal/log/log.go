// Package log configures structured logging for stringer using log/slog.
package log

import (
	"log/slog"
	"os"
)

// Setup configures the default slog logger based on verbosity flags.
//
//   - quiet mode:   only WARN and ERROR messages
//   - normal mode:  INFO and above
//   - verbose mode: DEBUG and above
//
// Output is written to stderr using slog.TextHandler.
func Setup(verbose, quiet bool) {
	var level slog.Level
	switch {
	case quiet:
		level = slog.LevelWarn
	case verbose:
		level = slog.LevelDebug
	default:
		level = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
}
