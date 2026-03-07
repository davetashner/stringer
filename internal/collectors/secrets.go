// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/davetashner/stringer/internal/signal"
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
	patterns  []SecretPattern
	allowlist []*regexp.Regexp
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

// RegisterCustom compiles and registers a user-defined pattern.
// Unlike Register, it returns an error instead of panicking (user config).
func (r *secretRegistry) RegisterCustom(cfg signal.SecretPatternConfig) error {
	if cfg.ID == "" {
		return fmt.Errorf("custom secret pattern ID must not be empty")
	}
	if cfg.Pattern == "" {
		return fmt.Errorf("custom secret pattern %q has empty regex", cfg.ID)
	}
	re, err := regexp.Compile(cfg.Pattern)
	if err != nil {
		return fmt.Errorf("custom secret pattern %q has invalid regex: %w", cfg.ID, err)
	}
	name := cfg.Name
	if name == "" {
		name = cfg.ID
	}
	conf := cfg.Confidence
	if conf <= 0 || conf > 1 {
		conf = 0.5
	}
	r.patterns = append(r.patterns, SecretPattern{
		ID:         cfg.ID,
		Name:       name,
		Pattern:    re,
		Confidence: conf,
		Keywords:   cfg.Keywords,
	})
	return nil
}

// SetAllowlist compiles the given regex patterns and stores them for
// suppressing matches. Returns an error if any pattern is invalid.
func (r *secretRegistry) SetAllowlist(patterns []string) error {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("invalid allowlist regex %q: %w", p, err)
		}
		compiled = append(compiled, re)
	}
	r.allowlist = compiled
	return nil
}

// isAllowlisted returns (allowed, matchingPattern) for a given line.
func (r *secretRegistry) isAllowlisted(line string) (bool, string) {
	for _, re := range r.allowlist {
		if re.MatchString(line) {
			return true, re.String()
		}
	}
	return false, ""
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
			// Check allowlist before adding.
			if allowed, _ := r.isAllowlisted(line); allowed {
				continue
			}
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

// builtinPatterns holds all built-in secret detection patterns.
var builtinPatterns = []SecretPattern{
	{
		ID:         "aws-access-key",
		Name:       "AWS access key",
		Pattern:    regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		Confidence: 0.7,
		Keywords:   []string{"AKIA"},
	},
	{
		ID:         "aws-secret-key",
		Name:       "AWS secret access key",
		Pattern:    regexp.MustCompile(`(?i)aws_?secret_?access_?key\s*[:=]\s*["']?[A-Za-z0-9/+=]{40}`),
		Confidence: 0.7,
		Keywords:   []string{"aws_secret", "AWS_SECRET"},
	},
	{
		ID:         "github-token",
		Name:       "GitHub token",
		Pattern:    regexp.MustCompile(`gh[ps]_[A-Za-z0-9_]{36,}`),
		Confidence: 0.7,
		Keywords:   []string{"ghp_", "ghs_"},
	},
	{
		ID:         "github-fine-grained",
		Name:       "GitHub fine-grained PAT",
		Pattern:    regexp.MustCompile(`github_pat_[A-Za-z0-9_]{82,}`),
		Confidence: 0.7,
		Keywords:   []string{"github_pat_"},
	},
	{
		ID:         "gitlab-personal",
		Name:       "GitLab personal access token",
		Pattern:    regexp.MustCompile(`glpat-[A-Za-z0-9_-]{20,}`),
		Confidence: 0.7,
		Keywords:   []string{"glpat-"},
	},
	{
		ID:         "gitlab-pipeline",
		Name:       "GitLab pipeline trigger token",
		Pattern:    regexp.MustCompile(`glptt-[A-Za-z0-9_-]{20,}`),
		Confidence: 0.7,
		Keywords:   []string{"glptt-"},
	},
	{
		ID:         "gitlab-runner",
		Name:       "GitLab runner registration token",
		Pattern:    regexp.MustCompile(`glrt-[A-Za-z0-9_-]{20,}`),
		Confidence: 0.7,
		Keywords:   []string{"glrt-"},
	},
	{
		ID:         "slack-bot-token",
		Name:       "Slack bot token",
		Pattern:    regexp.MustCompile(`xoxb-[0-9]{10,}-[0-9]{10,}-[A-Za-z0-9]{24,}`),
		Confidence: 0.7,
		Keywords:   []string{"xoxb-"},
	},
	{
		ID:         "slack-user-token",
		Name:       "Slack user token",
		Pattern:    regexp.MustCompile(`xoxp-[0-9]{10,}-[0-9]{10,}-[A-Za-z0-9]{24,}`),
		Confidence: 0.7,
		Keywords:   []string{"xoxp-"},
	},
	{
		ID:         "slack-webhook",
		Name:       "Slack webhook URL",
		Pattern:    regexp.MustCompile(`\bhttps://hooks\.slack\.com/services/T[A-Z0-9]+/B[A-Z0-9]+/[A-Za-z0-9]+\b`),
		Confidence: 0.7,
		Keywords:   []string{"hooks.slack.com"},
	},
	{
		ID:         "stripe-live-key",
		Name:       "Stripe live secret key",
		Pattern:    regexp.MustCompile(`sk_live_[A-Za-z0-9]{24,}`),
		Confidence: 0.8,
		Keywords:   []string{"sk_live_"},
	},
	{
		ID:         "stripe-restricted",
		Name:       "Stripe restricted key",
		Pattern:    regexp.MustCompile(`rk_live_[A-Za-z0-9]{24,}`),
		Confidence: 0.7,
		Keywords:   []string{"rk_live_"},
	},
	{
		ID:         "twilio-api-key",
		Name:       "Twilio API key",
		Pattern:    regexp.MustCompile(`SK[0-9a-fA-F]{32}`),
		Confidence: 0.6,
		Keywords:   []string{"SK"},
	},
	{
		ID:         "sendgrid-api-key",
		Name:       "SendGrid API key",
		Pattern:    regexp.MustCompile(`SG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}`),
		Confidence: 0.8,
		Keywords:   []string{"SG."},
	},
	{
		ID:         "google-api-key",
		Name:       "Google API key",
		Pattern:    regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`),
		Confidence: 0.6,
		Keywords:   []string{"AIza"},
	},
	{
		ID:         "npm-token",
		Name:       "npm access token",
		Pattern:    regexp.MustCompile(`npm_[A-Za-z0-9]{36,}`),
		Confidence: 0.7,
		Keywords:   []string{"npm_"},
	},
	{
		ID:         "pypi-token",
		Name:       "PyPI API token",
		Pattern:    regexp.MustCompile(`pypi-[A-Za-z0-9_-]{100,}`),
		Confidence: 0.7,
		Keywords:   []string{"pypi-"},
	},
	{
		ID:         "nuget-api-key",
		Name:       "NuGet API key",
		Pattern:    regexp.MustCompile(`oy2[a-z0-9]{43}`),
		Confidence: 0.6,
		Keywords:   []string{"oy2"},
	},
	{
		ID:         "heroku-api-key",
		Name:       "Heroku API key",
		Pattern:    regexp.MustCompile(`(?i)heroku.*[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`),
		Confidence: 0.6,
		Keywords:   []string{"heroku", "HEROKU", "Heroku"},
	},
	{
		ID:         "digitalocean-token",
		Name:       "DigitalOcean access token",
		Pattern:    regexp.MustCompile(`dop_v1_[a-f0-9]{64}`),
		Confidence: 0.7,
		Keywords:   []string{"dop_v1_"},
	},
	{
		ID:         "datadog-api-key",
		Name:       "Datadog API key",
		Pattern:    regexp.MustCompile(`(?i)datadog.*[a-f0-9]{32}`),
		Confidence: 0.5,
		Keywords:   []string{"datadog", "DATADOG", "Datadog"},
	},
	{
		ID:         "private-key-header",
		Name:       "private key file",
		Pattern:    regexp.MustCompile(`-----BEGIN (RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`),
		Confidence: 0.8,
		Keywords:   []string{"PRIVATE KEY"},
	},
	{
		ID:         "jwt-token",
		Name:       "JWT token",
		Pattern:    regexp.MustCompile(`eyJ[A-Za-z0-9_-]*\.eyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]+`),
		Confidence: 0.5,
		Keywords:   []string{"eyJ"},
	},
	{
		ID:         "generic-secret",
		Name:       "generic secret",
		Pattern:    regexp.MustCompile(`(?i)(api[_-]?key|secret[_-]?key|password)\s*[:=]\s*["'][^"']{8,}`),
		Confidence: 0.6,
	},
}

// defaultSecretRegistry is the package-level registry initialized with the
// built-in secret patterns. It replaces the former secretPatterns slice.
var defaultSecretRegistry *secretRegistry

func init() {
	defaultSecretRegistry = newSecretRegistry()
	for _, p := range builtinPatterns {
		defaultSecretRegistry.Register(p)
	}
}

// secretAssignmentPattern matches lines with secret-like variable names
// used for entropy-based detection.
var secretAssignmentPattern = regexp.MustCompile(
	`(?i)(secret|password|token|api[_-]?key|auth[_-]?token|credential|private[_-]?key)`,
)

// stringLiteralPattern extracts quoted string literals from a line.
var stringLiteralPattern = regexp.MustCompile(`["']([^"']{16,})["']`)

// shannonEntropy computes the Shannon entropy of a string in bits per character.
func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	freq := make(map[rune]int)
	for _, c := range s {
		freq[c]++
	}
	length := float64(len([]rune(s)))
	entropy := 0.0
	for _, count := range freq {
		p := float64(count) / length
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}
	return entropy
}
