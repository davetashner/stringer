// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

// Package redact provides utilities to strip sensitive values from strings
// before they appear in output, logs, or error messages.
package redact

import (
	"os"
	"strings"
	"sync"
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

var (
	cachedSecrets []string
	cacheOnce     sync.Once
)

func loadSecrets() {
	for _, envVar := range sensitiveEnvVars {
		val := os.Getenv(envVar)
		if val != "" && len(val) >= 4 {
			cachedSecrets = append(cachedSecrets, val)
		}
	}
}

// resetCache resets the cached secrets. Used by tests that change env vars
// between calls.
func resetCache() {
	cachedSecrets = nil
	cacheOnce = sync.Once{}
}

// ResetForTest resets the cached secrets so tests in other packages can
// verify redaction behavior after setting env vars with t.Setenv.
func ResetForTest() { resetCache() }

// String replaces any occurrence of a known sensitive environment variable
// value with "[REDACTED]". Returns the original string if no secrets are found.
// Secret values are cached on first call for performance.
func String(s string) string {
	cacheOnce.Do(loadSecrets)
	for _, secret := range cachedSecrets {
		s = strings.ReplaceAll(s, secret, "[REDACTED]")
	}
	return s
}
