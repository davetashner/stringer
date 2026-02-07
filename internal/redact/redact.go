// Package redact provides utilities to strip sensitive values from strings
// before they appear in output, logs, or error messages.
package redact

import (
	"os"
	"strings"
)

// sensitiveEnvVars lists environment variable names whose values must never
// appear in output. Add new entries here as collectors gain API integrations.
var sensitiveEnvVars = []string{
	"GITHUB_TOKEN",
	"GH_TOKEN",
	"ANTHROPIC_API_KEY",
	"OPENAI_API_KEY",
	"STRINGER_TOKEN",
}

// String replaces any occurrence of a known sensitive environment variable
// value with "[REDACTED]". Returns the original string if no secrets are found.
func String(s string) string {
	for _, envVar := range sensitiveEnvVars {
		val := os.Getenv(envVar)
		if val != "" && len(val) >= 4 {
			s = strings.ReplaceAll(s, val, "[REDACTED]")
		}
	}
	return s
}
