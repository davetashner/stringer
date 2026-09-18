// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"fmt"
	"math"
	"path/filepath"
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
	// Generic marks low-precision, context-dependent detectors (e.g. the
	// generic "password = '...'" pattern). Generic matches are suppressed in
	// documentation, templates, comments and docstrings, and their values
	// are checked for placeholders; high-precision detectors (private-key
	// blocks, provider-specific key formats) run everywhere because a real
	// key in a README is still a leak (stringer-nxx.7).
	Generic bool
}

// SecretMatch holds a match result from the registry.
type SecretMatch struct {
	PatternID  string
	Name       string
	Confidence float64
	Line       int
	Generic    bool
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
				Generic:    p.Generic,
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
		Pattern:    regexp.MustCompile(`https://hooks\.slack\.com/services/T[A-Z0-9]+/B[A-Z0-9]+/[A-Za-z0-9]+`),
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
		// JWTs in docs/tests are almost always the well-known example token
		// (fastapi ships it once per translation), so treat as generic.
		Generic: true,
	},
	{
		ID:         "generic-secret",
		Name:       "generic secret",
		Pattern:    regexp.MustCompile(`(?i)(api[_-]?key|secret[_-]?key|password)\s*[:=]\s*["'][^"']{8,}`),
		Confidence: 0.6,
		Generic:    true,
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

// ---------------------------------------------------------------------------
// Context-aware suppression for generic (low-precision) secret detectors.
//
// The September 2026 benchmark showed committed-secret firing almost
// exclusively on documentation examples (flask docs/config.rst, fastapi
// oauth2-jwt.md per translation), docstrings, project templates and test
// fixtures with obviously fake values. The helpers below classify the file
// and line once per scan so the generic detector and the entropy detector
// stay quiet there, while provider-specific patterns still run everywhere.
// ---------------------------------------------------------------------------

// secretDocExtensions are file extensions treated as documentation for the
// generic detector. HTML is included because framework HTML templates
// (e.g. Django admin templates) carry example form values, not secrets.
var secretDocExtensions = map[string]bool{
	".md": true, ".markdown": true, ".mdx": true, ".rst": true, ".txt": true,
	".adoc": true, ".asciidoc": true, ".html": true, ".htm": true,
}

// secretDocDirs are directory names whose contents are documentation or
// examples for the purposes of the generic detector.
var secretDocDirs = map[string]bool{
	"docs": true, "doc": true, "documentation": true,
	"examples": true, "example": true, "samples": true, "sample": true,
}

// secretTemplateSuffixes mark files that are templates or example configs
// whose values are placeholders to be filled in by the user.
var secretTemplateSuffixes = []string{
	".tpl", "-tpl", ".tmpl", ".j2", ".jinja", ".jinja2", ".example", ".sample", ".dist", ".template",
}

// secretTestDirs are directory names that hold tests or fixtures.
var secretTestDirs = map[string]bool{
	"test": true, "tests": true, "__tests__": true, "spec": true, "specs": true,
	"testdata": true, "fixtures": true, "fixture": true, "testing": true,
}

// secretPlaceholderMarkers are substrings that mark a value as a template
// placeholder or format-string interpolation rather than a real credential.
var secretPlaceholderMarkers = []string{
	"{{", "${", "%(", "%s", "<your", "changeme", "example", "placeholder", "xxx", "...",
}

// secretFakeWords are tokens that, when a value consists of nothing else,
// identify an obviously fake fixture value ("test key", "development key").
var secretFakeWords = map[string]bool{
	"test": true, "testing": true, "dev": true, "development": true, "dummy": true,
	"fake": true, "secret": true, "password": true, "passwd": true, "changeme": true,
	"key": true, "value": true, "string": true, "sample": true, "insert": true,
	"your": true, "here": true, "foo": true, "bar": true, "baz": true,
}

// secretFakeConfidence is the confidence assigned to generic matches whose
// value is an obvious fixture placeholder.
const secretFakeConfidence = 0.2

// secretTestFileMaxConfidence caps generic matches found in test files:
// they may stay as low-priority hygiene but should never outrank real leaks.
const secretTestFileMaxConfidence = 0.3

// genericSecretValuePattern extracts the quoted value of a generic
// key/secret/password assignment so it can be checked for placeholders.
var genericSecretValuePattern = regexp.MustCompile(
	`(?i)(?:api[_-]?key|secret[_-]?key|password|secret|token)\s*[:=]\s*["']([^"']*)`,
)

// secretScanContext carries per-file state used to decide whether a
// generic secret match on a given line is worth reporting.
type secretScanContext struct {
	docFile        bool
	templateFile   bool
	testFile       bool
	python         bool
	commentMarkers []string
	inDocstring    bool
	docstringDelim string
}

// newSecretScanContext classifies relPath once so per-line checks are cheap.
func newSecretScanContext(relPath string) *secretScanContext {
	slash := filepath.ToSlash(relPath)
	base := strings.ToLower(filepath.Base(slash))
	ext := strings.ToLower(filepath.Ext(base))
	sc := &secretScanContext{
		docFile:        secretDocExtensions[ext],
		python:         ext == ".py" || ext == ".pyi",
		commentMarkers: secretCommentMarkers(base, ext),
	}
	for _, s := range secretTemplateSuffixes {
		if strings.HasSuffix(base, s) {
			sc.templateFile = true
			break
		}
	}
	if isTestFile(relPath) || base == "conftest.py" {
		sc.testFile = true
	}
	for _, part := range strings.Split(filepath.Dir(slash), "/") {
		lp := strings.ToLower(part)
		// docs_src, docs-site and similar are documentation source trees.
		if secretDocDirs[lp] || strings.HasPrefix(lp, "docs_") || strings.HasPrefix(lp, "docs-") {
			sc.docFile = true
		}
		if secretTestDirs[lp] {
			sc.testFile = true
		}
	}
	return sc
}

// secretCommentMarkers returns the line-comment prefixes for a file. Unknown
// file types get both "#" and "//" since those cover nearly every config
// and script format.
func secretCommentMarkers(base, ext string) []string {
	switch ext {
	case ".py", ".pyi", ".rb", ".sh", ".bash", ".zsh", ".fish", ".yaml", ".yml", ".toml",
		".cfg", ".conf", ".pl", ".pm", ".r", ".ps1", ".ex", ".exs", ".env", ".properties", ".mk":
		return []string{"#"}
	case ".ini":
		return []string{"#", ";"}
	case ".go", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".java", ".kt", ".kts", ".scala",
		".swift", ".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".rs", ".dart", ".groovy", ".m", ".mm":
		return []string{"//", "/*", "*"}
	case ".php":
		return []string{"//", "#", "/*", "*"}
	case ".sql", ".lua", ".hs":
		return []string{"--"}
	case ".xml", ".vue", ".svelte":
		return []string{"<!--"}
	case ".tex", ".erl":
		return []string{"%"}
	}
	switch base {
	case "dockerfile", "makefile", "vagrantfile", "rakefile", "gemfile":
		return []string{"#"}
	}
	return []string{"#", "//"}
}

// skipGeneric reports whether generic detectors should ignore this line.
// It must be called for every line in file order because it tracks Python
// triple-quoted string state.
func (sc *secretScanContext) skipGeneric(line string) bool {
	if sc.docFile || sc.templateFile {
		return true
	}
	trimmed := strings.TrimSpace(line)
	if sc.python {
		wasInDocstring := sc.inDocstring
		sc.trackDocstring(trimmed)
		if wasInDocstring {
			return true
		}
		// A docstring that opens on this line and has no closing delimiter
		// (the first line of a multi-line docstring) is also skipped.
		if sc.inDocstring && strings.HasPrefix(trimmed, sc.docstringDelim) {
			return true
		}
	}
	for _, m := range sc.commentMarkers {
		if strings.HasPrefix(trimmed, m) {
			return true
		}
	}
	return false
}

// trackDocstring updates triple-quote state for a Python line. It is
// deliberately approximate (no escape handling) — cheap and good enough to
// keep example assignments inside module and class docstrings quiet.
func (sc *secretScanContext) trackDocstring(trimmed string) {
	if sc.inDocstring {
		if strings.Count(trimmed, sc.docstringDelim)%2 == 1 {
			sc.inDocstring = false
			sc.docstringDelim = ""
		}
		return
	}
	dq := strings.Index(trimmed, `"""`)
	sq := strings.Index(trimmed, `'''`)
	delim := ""
	switch {
	case dq >= 0 && (sq < 0 || dq < sq):
		delim = `"""`
	case sq >= 0:
		delim = `'''`
	default:
		return
	}
	if strings.Count(trimmed, delim)%2 == 1 {
		sc.inDocstring = true
		sc.docstringDelim = delim
	}
}

// isPlaceholderSecretValue reports whether a value is a template
// placeholder ("{{ secret }}", "${SECRET}", "<your-key>", "changeme").
func isPlaceholderSecretValue(value string) bool {
	lower := strings.ToLower(value)
	for _, m := range secretPlaceholderMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// isFakeSecretValue reports whether a value is an obvious fixture value:
// shorter than 8 characters, or made only of words like "test", "dev",
// "development", "dummy", "fake", "secret", "password", "changeme", "key".
func isFakeSecretValue(value string) bool {
	v := strings.TrimSpace(value)
	if len(v) < 8 {
		return true
	}
	tokens := strings.FieldsFunc(strings.ToLower(v), func(r rune) bool {
		return r == ' ' || r == '_' || r == '-' || r == '.' || r == ':'
	})
	if len(tokens) == 0 {
		return true
	}
	for _, t := range tokens {
		if !secretFakeWords[t] {
			return false
		}
	}
	return true
}

// adjustGenericSecret applies value and location heuristics to a generic
// match found on a non-skipped line. It returns the adjusted confidence,
// extra tags describing the adjustment, and false when the match should be
// dropped entirely (placeholder values).
func (sc *secretScanContext) adjustGenericSecret(line string, conf float64) (float64, []string, bool) {
	var tags []string
	value := ""
	if m := genericSecretValuePattern.FindStringSubmatch(line); len(m) == 2 {
		value = m[1]
	}
	if value != "" {
		if isPlaceholderSecretValue(value) {
			return 0, nil, false
		}
		if isFakeSecretValue(value) {
			conf = math.Min(conf, secretFakeConfidence)
			tags = append(tags, "likely-placeholder")
		}
	}
	if sc.testFile {
		conf = math.Min(conf, secretTestFileMaxConfidence)
		tags = append(tags, "test-file")
	}
	return conf, tags, true
}
