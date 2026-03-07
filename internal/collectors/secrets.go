// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"regexp"
	"strings"
)

// SecretPattern defines a single secret detection pattern.
type SecretPattern struct {
	ID         string         // unique identifier e.g. "aws-access-key"
	Name       string         // human-readable name
	Pattern    *regexp.Regexp // compiled regex
	Confidence float64        // 0.0-1.0
	Keywords   []string       // optional pre-filter keywords for performance
}

// SecretMatch holds a match result from the registry.
type SecretMatch struct {
	PatternID  string
	Name       string
	Confidence float64
	Line       int
}

// secretRegistry holds registered patterns and provides matching.
type secretRegistry struct {
	patterns []SecretPattern
}

// newSecretRegistry creates an empty registry.
func newSecretRegistry() *secretRegistry {
	return &secretRegistry{}
}

// Register adds a pattern to the registry.
// It panics if the pattern's regex is nil or ID is empty (fail-fast).
func (r *secretRegistry) Register(p SecretPattern) {
	if p.ID == "" {
		panic("secret pattern ID must not be empty")
	}
	if p.Pattern == nil {
		panic("secret pattern regex must not be nil")
	}
	r.patterns = append(r.patterns, p)
}

// Match returns all pattern matches for a given line of text.
func (r *secretRegistry) Match(line string) []SecretMatch {
	var matches []SecretMatch
	for _, p := range r.patterns {
		// Keyword pre-filter: if Keywords are set, at least one must appear
		// in the line before we run the (expensive) regex.
		if len(p.Keywords) > 0 {
			found := false
			for _, kw := range p.Keywords {
				if strings.Contains(line, kw) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if p.Pattern.MatchString(line) {
			matches = append(matches, SecretMatch{
				PatternID:  p.ID,
				Name:       p.Name,
				Confidence: p.Confidence,
			})
		}
	}
	return matches
}

// Count returns the number of registered patterns.
func (r *secretRegistry) Count() int {
	return len(r.patterns)
}

// defaultSecretRegistry is the package-level registry initialized with the
// built-in secret patterns. It replaces the former secretPatterns slice.
var defaultSecretRegistry *secretRegistry

func init() {
	defaultSecretRegistry = newSecretRegistry()

	defaultSecretRegistry.Register(SecretPattern{
		ID:         "aws-access-key",
		Name:       "AWS access key",
		Pattern:    regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		Confidence: 0.7,
		Keywords:   []string{"AKIA"},
	})
	defaultSecretRegistry.Register(SecretPattern{
		ID:         "github-token",
		Name:       "GitHub token",
		Pattern:    regexp.MustCompile(`gh[ps]_[A-Za-z0-9_]{36,}`),
		Confidence: 0.7,
		Keywords:   []string{"ghp_", "ghs_"},
	})
	defaultSecretRegistry.Register(SecretPattern{
		ID:         "generic-secret",
		Name:       "generic secret",
		Pattern:    regexp.MustCompile(`(?i)(api[_-]?key|secret[_-]?key|password)\s*[:=]\s*["'][^"']{8,}`),
		Confidence: 0.6,
	})
}
