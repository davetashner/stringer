// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSecretRegistry_Empty(t *testing.T) {
	r := newSecretRegistry()
	assert.Equal(t, 0, r.Count())
}

func TestSecretRegistry_Register(t *testing.T) {
	r := newSecretRegistry()
	r.Register(SecretPattern{
		ID:         "test-pattern",
		Name:       "Test Pattern",
		Pattern:    regexp.MustCompile(`test`),
		Confidence: 0.5,
	})
	assert.Equal(t, 1, r.Count())
}

func TestSecretRegistry_Register_PanicsOnEmptyID(t *testing.T) {
	r := newSecretRegistry()
	assert.Panics(t, func() {
		r.Register(SecretPattern{
			Pattern:    regexp.MustCompile(`test`),
			Confidence: 0.5,
		})
	})
}

func TestSecretRegistry_Register_PanicsOnNilRegex(t *testing.T) {
	r := newSecretRegistry()
	assert.Panics(t, func() {
		r.Register(SecretPattern{
			ID:         "nil-regex",
			Confidence: 0.5,
		})
	})
}

func TestSecretRegistry_Match_Basic(t *testing.T) {
	r := newSecretRegistry()
	r.Register(SecretPattern{
		ID:         "aws-key",
		Name:       "AWS Key",
		Pattern:    regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		Confidence: 0.7,
	})

	matches := r.Match(`const key = "AKIAIOSFODNN7EXAMPLE"`)
	require.Len(t, matches, 1)
	assert.Equal(t, "aws-key", matches[0].PatternID)
	assert.Equal(t, "AWS Key", matches[0].Name)
	assert.Equal(t, 0.7, matches[0].Confidence)
}

func TestSecretRegistry_Match_NoMatch(t *testing.T) {
	r := newSecretRegistry()
	r.Register(SecretPattern{
		ID:         "aws-key",
		Name:       "AWS Key",
		Pattern:    regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		Confidence: 0.7,
	})

	matches := r.Match("this is a clean line with no secrets")
	assert.Empty(t, matches)
}

func TestSecretRegistry_Match_MultiplePatterns(t *testing.T) {
	r := newSecretRegistry()
	r.Register(SecretPattern{
		ID:         "aws-key",
		Name:       "AWS Key",
		Pattern:    regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
		Confidence: 0.7,
	})
	r.Register(SecretPattern{
		ID:         "generic-secret",
		Name:       "Generic Secret",
		Pattern:    regexp.MustCompile(`(?i)api_key\s*=\s*"[^"]{8,}`),
		Confidence: 0.6,
	})

	// Line matches both patterns.
	matches := r.Match(`api_key = "AKIAIOSFODNN7EXAMPLE"`)
	assert.Len(t, matches, 2, "both patterns should match the same line")
	ids := []string{matches[0].PatternID, matches[1].PatternID}
	assert.Contains(t, ids, "aws-key")
	assert.Contains(t, ids, "generic-secret")
}

func TestSecretRegistry_KeywordPreFilter_Skips(t *testing.T) {
	callCount := 0
	// We can't easily count regex calls, but we can verify that when keywords
	// don't match, the regex is not executed by checking no match is returned
	// for a line that WOULD match the regex but lacks the keyword.
	r := newSecretRegistry()
	r.Register(SecretPattern{
		ID:         "keyword-pattern",
		Name:       "Keyword Pattern",
		Pattern:    regexp.MustCompile(`secret_value_[0-9]+`),
		Confidence: 0.8,
		Keywords:   []string{"KEYWORD_PREFIX"},
	})
	_ = callCount // suppress unused

	// This line contains text matching the regex but NOT the keyword.
	matches := r.Match("found secret_value_12345 in file")
	assert.Empty(t, matches, "should skip regex when keyword not found")
}

func TestSecretRegistry_KeywordPreFilter_Matches(t *testing.T) {
	r := newSecretRegistry()
	r.Register(SecretPattern{
		ID:         "keyword-pattern",
		Name:       "Keyword Pattern",
		Pattern:    regexp.MustCompile(`KEYWORD_PREFIX_[0-9]+`),
		Confidence: 0.8,
		Keywords:   []string{"KEYWORD_PREFIX"},
	})

	// Line contains both the keyword and matches the regex.
	matches := r.Match("found KEYWORD_PREFIX_12345 in file")
	require.Len(t, matches, 1)
	assert.Equal(t, "keyword-pattern", matches[0].PatternID)
}

func TestSecretRegistry_KeywordPreFilter_AnyKeywordSuffices(t *testing.T) {
	r := newSecretRegistry()
	r.Register(SecretPattern{
		ID:         "multi-keyword",
		Name:       "Multi Keyword",
		Pattern:    regexp.MustCompile(`(ghp_|ghs_)[A-Za-z0-9]{10,}`),
		Confidence: 0.7,
		Keywords:   []string{"ghp_", "ghs_"},
	})

	// Only second keyword present.
	matches := r.Match("token=ghs_ABCDEFGHIJ")
	require.Len(t, matches, 1)
	assert.Equal(t, "multi-keyword", matches[0].PatternID)
}

func TestSecretRegistry_NoKeywords_AlwaysRunsRegex(t *testing.T) {
	r := newSecretRegistry()
	r.Register(SecretPattern{
		ID:         "no-keywords",
		Name:       "No Keywords",
		Pattern:    regexp.MustCompile(`(?i)password\s*=\s*"[^"]{8,}`),
		Confidence: 0.6,
	})

	matches := r.Match(`password = "mysecretpassword123"`)
	require.Len(t, matches, 1)
	assert.Equal(t, "no-keywords", matches[0].PatternID)
}

func TestDefaultSecretRegistry_Initialized(t *testing.T) {
	require.NotNil(t, defaultSecretRegistry)
	assert.Equal(t, len(builtinPatterns), defaultSecretRegistry.Count(),
		"default registry should have all built-in patterns")
	assert.GreaterOrEqual(t, defaultSecretRegistry.Count(), 24,
		"should have at least 24 built-in patterns")
}

func TestDefaultSecretRegistry_AWSKeyMatch(t *testing.T) {
	matches := defaultSecretRegistry.Match(`const key = "AKIAIOSFODNN7EXAMPLE"`)
	require.Len(t, matches, 1)
	assert.Equal(t, "aws-access-key", matches[0].PatternID)
	assert.Equal(t, "AWS access key", matches[0].Name)
	assert.Equal(t, 0.7, matches[0].Confidence)
}

func TestDefaultSecretRegistry_GitHubTokenMatch(t *testing.T) {
	token := "ghp_" + "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	matches := defaultSecretRegistry.Match("TOKEN=" + token)
	require.Len(t, matches, 1)
	assert.Equal(t, "github-token", matches[0].PatternID)
	assert.Equal(t, "GitHub token", matches[0].Name)
}

func TestDefaultSecretRegistry_GenericSecretMatch(t *testing.T) {
	matches := defaultSecretRegistry.Match(`api_key = "supersecretvalue123456"`)
	require.Len(t, matches, 1)
	assert.Equal(t, "generic-secret", matches[0].PatternID)
	assert.Equal(t, "generic secret", matches[0].Name)
	assert.Equal(t, 0.6, matches[0].Confidence)
}

func TestDefaultSecretRegistry_CleanLine(t *testing.T) {
	lines := []string{
		"package main",
		"func hello() string { return \"world\" }",
		"// This is a comment",
		"var x = 42",
		"",
	}
	for _, line := range lines {
		matches := defaultSecretRegistry.Match(line)
		assert.Empty(t, matches, "clean line should not match: %q", line)
	}
}

// TestBuiltinPatterns_PositiveMatches tests each built-in pattern with a
// realistic positive match string.
func TestBuiltinPatterns_PositiveMatches(t *testing.T) {
	tests := []struct {
		patternID string
		input     string
	}{
		{"aws-access-key", `key = "AKIAIOSFODNN7EXAMPLE"`},
		{"aws-secret-key", `aws_secret_access_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`},
		{"github-token", "ghp_" + strings.Repeat("A", 36)},
		{"github-fine-grained", "github_pat_" + strings.Repeat("A", 82)},
		{"gitlab-personal", "glpat-" + strings.Repeat("A", 20)},
		{"gitlab-pipeline", "glptt-" + strings.Repeat("A", 20)},
		{"gitlab-runner", "glrt-" + strings.Repeat("A", 20)},
		{"slack-bot-token", "xoxb-1234567890-1234567890-" + strings.Repeat("A", 24)},  //gitleaks:allow
		{"slack-user-token", "xoxp-1234567890-1234567890-" + strings.Repeat("A", 24)}, //gitleaks:allow
		{"slack-webhook", "https://hooks.slack.com/services/T01ABCDEF/B01ABCDEF/abcdefghijklmnop"},
		{"stripe-live-key", "sk_live_" + strings.Repeat("A", 24)},
		{"stripe-restricted", "rk_live_" + strings.Repeat("A", 24)},
		{"twilio-api-key", "SK" + strings.Repeat("a", 32)},
		{"sendgrid-api-key", "SG." + strings.Repeat("A", 22) + "." + strings.Repeat("B", 43)},
		{"google-api-key", "AIza" + strings.Repeat("A", 35)},
		{"npm-token", "npm_" + strings.Repeat("A", 36)},
		{"pypi-token", "pypi-" + strings.Repeat("A", 100)},
		{"nuget-api-key", "oy2" + strings.Repeat("a", 43)},
		{"heroku-api-key", `HEROKU_API_KEY=12345678-1234-1234-1234-123456789012`}, //gitleaks:allow
		{"digitalocean-token", "dop_v1_" + strings.Repeat("a", 64)},
		{"datadog-api-key", "DATADOG_API_KEY=" + strings.Repeat("a", 32)},
		{"private-key-header", "-----BEGIN RSA PRIVATE KEY-----"},                      //gitleaks:allow
		{"private-key-header", "-----BEGIN PRIVATE KEY-----"},                          //gitleaks:allow
		{"private-key-header", "-----BEGIN EC PRIVATE KEY-----"},                       //gitleaks:allow
		{"private-key-header", "-----BEGIN DSA PRIVATE KEY-----"},                      //gitleaks:allow
		{"private-key-header", "-----BEGIN OPENSSH PRIVATE KEY-----"},                  //gitleaks:allow
		{"jwt-token", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123def456"}, //gitleaks:allow
		{"generic-secret", `password = "myverylongpassword123"`},
	}

	for _, tt := range tests {
		t.Run(tt.patternID, func(t *testing.T) {
			matches := defaultSecretRegistry.Match(tt.input)
			found := false
			for _, m := range matches {
				if m.PatternID == tt.patternID {
					found = true
					break
				}
			}
			assert.True(t, found, "pattern %q should match input %q (got matches: %v)",
				tt.patternID, tt.input, matches)
		})
	}
}

// TestBuiltinPatterns_NegativeMatches tests that common false positive strings
// do NOT trigger matches.
func TestBuiltinPatterns_NegativeMatches(t *testing.T) {
	negatives := []struct {
		name  string
		input string
	}{
		{"placeholder-dots", `sk_live_........................`},
		{"documentation-example", `# Set your key: export STRIPE_KEY=sk_test_xxxx`},
		{"test-fixture-ghp", `ghp_test1234`}, // too short
		{"env-var-ref", `os.Getenv("AWS_SECRET_ACCESS_KEY")`},
		{"import-statement", `import "github.com/stripe/stripe-go"`},
		{"comment-about-tokens", `// Generate a new token using the dashboard`},
		{"url-without-secret", `https://api.example.com/v1/users`},
		{"short-password", `password = "short"`},                   // too short
		{"empty-value", `api_key = ""`},                            // empty
		{"var-declaration", `var secretKey string`},                // no value
		{"function-name", `func getApiKey() string {`},             // no secret
		{"log-message", `log.Info("checking password")`},           // no value
		{"config-key-name", `yaml:"api_key,omitempty"`},            // struct tag
		{"short-sk", `SK` + strings.Repeat("g", 5)},                // too short for Twilio
		{"test-mode-stripe", `sk_test_` + strings.Repeat("A", 24)}, // test key, not live
		{"constant-name", `const AWSSecretKeyName = "aws_secret_access_key"`},
		{"heroku-cli-help", `heroku login --interactive`},
		{"jwt-header-only", `eyJhbGci`}, // incomplete JWT
		{"npm-scope", `@npm_org/package`},
		{"normal-code", `for i := 0; i < len(items); i++ {`},
		{"markdown-heading", `## Setting up API Keys`},
	}

	for _, tt := range negatives {
		t.Run(tt.name, func(t *testing.T) {
			matches := defaultSecretRegistry.Match(tt.input)
			assert.Empty(t, matches, "should NOT match %q but got: %v", tt.input, matches)
		})
	}
}

// TestBuiltinPatterns_UniqueIDs ensures all pattern IDs are unique.
func TestBuiltinPatterns_UniqueIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, p := range builtinPatterns {
		assert.False(t, seen[p.ID], "duplicate pattern ID: %s", p.ID)
		seen[p.ID] = true
	}
}

// TestBuiltinPatterns_ValidFields ensures all patterns have required fields.
func TestBuiltinPatterns_ValidFields(t *testing.T) {
	for _, p := range builtinPatterns {
		t.Run(p.ID, func(t *testing.T) {
			assert.NotEmpty(t, p.ID)
			assert.NotEmpty(t, p.Name)
			assert.NotNil(t, p.Pattern)
			assert.Greater(t, p.Confidence, 0.0)
			assert.LessOrEqual(t, p.Confidence, 1.0)
		})
	}
}

// --- Custom pattern tests (SA3.3) ---

func TestRegisterCustom_ValidPattern(t *testing.T) {
	r := newSecretRegistry()
	err := r.RegisterCustom(signal.SecretPatternConfig{
		ID:         "custom-test",
		Name:       "Custom Test",
		Pattern:    `CUSTOM_[A-Z]{10}`,
		Confidence: 0.6,
		Keywords:   []string{"CUSTOM_"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, r.Count())

	matches := r.Match("found CUSTOM_ABCDEFGHIJ in file")
	require.Len(t, matches, 1)
	assert.Equal(t, "custom-test", matches[0].PatternID)
}

func TestRegisterCustom_InvalidRegex(t *testing.T) {
	r := newSecretRegistry()
	err := r.RegisterCustom(signal.SecretPatternConfig{
		ID:      "bad-regex",
		Name:    "Bad Regex",
		Pattern: `[invalid`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad-regex")
	assert.Contains(t, err.Error(), "invalid regex")
}

func TestRegisterCustom_EmptyID(t *testing.T) {
	r := newSecretRegistry()
	err := r.RegisterCustom(signal.SecretPatternConfig{
		Pattern: `test`,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestRegisterCustom_EmptyPattern(t *testing.T) {
	r := newSecretRegistry()
	err := r.RegisterCustom(signal.SecretPatternConfig{
		ID: "no-pattern",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty regex")
}

func TestRegisterCustom_DefaultConfidence(t *testing.T) {
	r := newSecretRegistry()
	err := r.RegisterCustom(signal.SecretPatternConfig{
		ID:      "default-conf",
		Pattern: `test`,
	})
	require.NoError(t, err)

	matches := r.Match("test")
	require.Len(t, matches, 1)
	assert.Equal(t, 0.5, matches[0].Confidence, "should default to 0.5")
}

func TestRegisterCustom_DefaultName(t *testing.T) {
	r := newSecretRegistry()
	err := r.RegisterCustom(signal.SecretPatternConfig{
		ID:      "my-custom-id",
		Pattern: `test`,
	})
	require.NoError(t, err)

	matches := r.Match("test")
	require.Len(t, matches, 1)
	assert.Equal(t, "my-custom-id", matches[0].Name, "should default name to ID")
}

// --- Allowlist tests (SA3.4) ---

func TestAllowlist_SuppressesMatch(t *testing.T) {
	r := newSecretRegistry()
	r.Register(SecretPattern{
		ID:         "test-secret",
		Name:       "Test Secret",
		Pattern:    regexp.MustCompile(`SECRET_[A-Z]{10}`),
		Confidence: 0.7,
	})
	err := r.SetAllowlist([]string{`test_fixture`})
	require.NoError(t, err)

	// Line contains a match AND the allowlist pattern.
	matches := r.Match("test_fixture: SECRET_ABCDEFGHIJ")
	assert.Empty(t, matches, "allowlist should suppress match")

	// Line without allowlist pattern should still match.
	matches = r.Match("production: SECRET_ABCDEFGHIJ")
	require.Len(t, matches, 1)
}

func TestAllowlist_InvalidRegex(t *testing.T) {
	r := newSecretRegistry()
	err := r.SetAllowlist([]string{`[invalid`})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid allowlist regex")
}

func TestAllowlist_MultiplePatterns(t *testing.T) {
	r := newSecretRegistry()
	r.Register(SecretPattern{
		ID:         "test-secret",
		Name:       "Test Secret",
		Pattern:    regexp.MustCompile(`SECRET_[A-Z]{10}`),
		Confidence: 0.7,
	})
	err := r.SetAllowlist([]string{`test_data`, `example\.com`})
	require.NoError(t, err)

	// First allowlist pattern.
	matches := r.Match("test_data SECRET_ABCDEFGHIJ")
	assert.Empty(t, matches)

	// Second allowlist pattern.
	matches = r.Match("example.com SECRET_ABCDEFGHIJ")
	assert.Empty(t, matches)

	// Neither.
	matches = r.Match("production SECRET_ABCDEFGHIJ")
	require.Len(t, matches, 1)
}

func TestAllowlist_EmptyDoesNothing(t *testing.T) {
	r := newSecretRegistry()
	r.Register(SecretPattern{
		ID:         "test-secret",
		Name:       "Test Secret",
		Pattern:    regexp.MustCompile(`SECRET_[A-Z]{10}`),
		Confidence: 0.7,
	})
	err := r.SetAllowlist(nil)
	require.NoError(t, err)

	matches := r.Match("SECRET_ABCDEFGHIJ")
	require.Len(t, matches, 1)
}

// --- Entropy tests (SA3.5) ---

func TestShannonEntropy_EmptyString(t *testing.T) {
	assert.Equal(t, 0.0, shannonEntropy(""))
}

func TestShannonEntropy_SingleChar(t *testing.T) {
	assert.Equal(t, 0.0, shannonEntropy("aaaa"))
}

func TestShannonEntropy_TwoCharsEqual(t *testing.T) {
	// "abababab" => 2 chars, equal frequency => 1.0 bit
	e := shannonEntropy("abababab")
	assert.InDelta(t, 1.0, e, 0.001)
}

func TestShannonEntropy_HighEntropy(t *testing.T) {
	// A random-looking string should have high entropy.
	e := shannonEntropy("aB3$xY7!mN9@pQ2&")
	assert.Greater(t, e, 3.5, "random-looking string should have high entropy")
}

func TestShannonEntropy_LowEntropy(t *testing.T) {
	// Repeated pattern has low entropy.
	e := shannonEntropy("aaaaaaaabbbbbbbb")
	assert.Less(t, e, 1.5)
}

func TestShannonEntropy_KnownValue(t *testing.T) {
	// "abcd" => 4 chars, each appearing once => log2(4) = 2.0 bits
	e := shannonEntropy("abcd")
	assert.InDelta(t, 2.0, e, 0.001)
}

func TestShannonEntropy_MaxEntropy(t *testing.T) {
	// All unique characters => max entropy = log2(n)
	s := "abcdefghijklmnop" // 16 unique chars
	e := shannonEntropy(s)
	assert.InDelta(t, math.Log2(16), e, 0.001)
}

func TestSecretAssignmentPattern(t *testing.T) {
	positives := []string{
		`SECRET_KEY = "value"`,
		`password = "test"`,
		`api_key: "test"`,
		`auth_token = "test"`,
		`credential = "test"`,
		`private_key = "test"`,
		`TOKEN = "test"`,
		`ApiKey = "test"`,
	}
	for _, line := range positives {
		assert.True(t, secretAssignmentPattern.MatchString(line),
			"should match: %q", line)
	}

	negatives := []string{
		`var x = 42`,
		`func hello() {}`,
		`import "fmt"`,
	}
	for _, line := range negatives {
		assert.False(t, secretAssignmentPattern.MatchString(line),
			"should NOT match: %q", line)
	}
}

func TestStringLiteralPattern(t *testing.T) {
	// Must extract strings of length >= 16.
	line := `token = "abcdefghijklmnop1234"` //gitleaks:allow
	matches := stringLiteralPattern.FindAllStringSubmatch(line, -1)
	require.Len(t, matches, 1)
	assert.Equal(t, "abcdefghijklmnop1234", matches[0][1])

	// Short strings should not match.
	line = `token = "short"`
	matches = stringLiteralPattern.FindAllStringSubmatch(line, -1)
	assert.Empty(t, matches)
}

// ---------------------------------------------------------------------------
// Context-aware suppression (stringer-nxx.7). All values below are
// deliberately fake fixture strings, never real credential formats.
// ---------------------------------------------------------------------------

func TestNewSecretScanContext_DocFiles(t *testing.T) {
	docs := []string{
		"README.md", "docs/config.rst", "notes.txt", "guide.adoc", "index.html",
		"docs/tutorial/deploy.rst", "doc/settings.py", "documentation/x.py",
		"examples/app/config.py", "samples/settings.yaml", "docs_site/page.md",
		"docs_src/security/tutorial.py", "docs-site/src/config.js",
	}
	for _, p := range docs {
		assert.True(t, newSecretScanContext(p).docFile, "expected doc file: %s", p)
	}
	notDocs := []string{"src/app/config.py", "config.yaml", "docstore/config.go", "settings.py"}
	for _, p := range notDocs {
		assert.False(t, newSecretScanContext(p).docFile, "expected non-doc file: %s", p)
	}
}

func TestNewSecretScanContext_TemplateFiles(t *testing.T) {
	tpls := []string{
		"settings.py-tpl", "config.tpl", "nginx.conf.tmpl", "env.j2",
		".env.example", "config.yaml.sample", "phpunit.xml.dist", "app.config.template",
	}
	for _, p := range tpls {
		assert.True(t, newSecretScanContext(p).templateFile, "expected template: %s", p)
	}
	assert.False(t, newSecretScanContext("config.yaml").templateFile)
	assert.False(t, newSecretScanContext("template.go").templateFile)
}

func TestNewSecretScanContext_TestFiles(t *testing.T) {
	tests := []string{
		"tests/conftest.py", "conftest.py", "tests/settings.py", "test/helpers.rb",
		"src/__tests__/setup.js", "spec/support/env.rb", "pkg/testdata/config.yaml",
		"fixtures/users.json", "app/config_test.go", "test_settings.py",
	}
	for _, p := range tests {
		assert.True(t, newSecretScanContext(p).testFile, "expected test file: %s", p)
	}
	assert.False(t, newSecretScanContext("src/app/config.py").testFile)
	assert.False(t, newSecretScanContext("contest.py").testFile)
}

func TestSecretCommentMarkers(t *testing.T) {
	assert.Equal(t, []string{"#"}, secretCommentMarkers("app.py", ".py"))
	assert.Equal(t, []string{"#", ";"}, secretCommentMarkers("app.ini", ".ini"))
	assert.Equal(t, []string{"//", "/*", "*"}, secretCommentMarkers("main.go", ".go"))
	assert.Equal(t, []string{"//", "#", "/*", "*"}, secretCommentMarkers("index.php", ".php"))
	assert.Equal(t, []string{"--"}, secretCommentMarkers("schema.sql", ".sql"))
	assert.Equal(t, []string{"<!--"}, secretCommentMarkers("pom.xml", ".xml"))
	assert.Equal(t, []string{"%"}, secretCommentMarkers("paper.tex", ".tex"))
	assert.Equal(t, []string{"#"}, secretCommentMarkers("dockerfile", ""))
	assert.Equal(t, []string{"#"}, secretCommentMarkers("makefile", ""))
	assert.Equal(t, []string{"#", "//"}, secretCommentMarkers("unknownfile", ".zzz"))
}

func TestSecretScanContext_SkipGeneric_DocAndTemplate(t *testing.T) {
	line := `SECRET_KEY = 'development key'`
	assert.True(t, newSecretScanContext("docs/config.rst").skipGeneric(line))
	assert.True(t, newSecretScanContext("settings.py-tpl").skipGeneric(line))
	assert.False(t, newSecretScanContext("src/settings.py").skipGeneric(line))
}

func TestSecretScanContext_SkipGeneric_Comments(t *testing.T) {
	py := newSecretScanContext("src/settings.py")
	assert.True(t, py.skipGeneric(`# SECRET_KEY = 'development key'`))
	assert.True(t, py.skipGeneric(`    # password = "example fixture"`))
	assert.False(t, py.skipGeneric(`SECRET_KEY = 'development key'`))

	goFile := newSecretScanContext("cmd/main.go")
	assert.True(t, goFile.skipGeneric(`// password = "example fixture"`))
	assert.True(t, goFile.skipGeneric(`/* password = "example fixture" */`))
	assert.True(t, goFile.skipGeneric(` * password = "example fixture"`))
	assert.False(t, goFile.skipGeneric(`# password = "example fixture"`), "hash is not a Go comment")
	assert.False(t, goFile.skipGeneric(`password := "example fixture"`))

	sql := newSecretScanContext("db/schema.sql")
	assert.True(t, sql.skipGeneric(`-- password = 'example fixture'`))
	assert.False(t, sql.skipGeneric(`SET password = 'example fixture'`))
}

func TestSecretScanContext_SkipGeneric_PythonDocstring(t *testing.T) {
	sc := newSecretScanContext("src/flask/config.py")
	lines := []struct {
		text string
		skip bool
	}{
		{`class Config:`, false},
		{`    """Configuration example.`, true}, // opens docstring
		{``, true},
		{`    SECRET_KEY = 'development key'`, true},  // inside docstring
		{`    """`, true},                             // closes docstring (still inside at line start)
		{`    SECRET_KEY = 'development key'`, false}, // real code again
		{`    """single line docstring"""`, false},    // opens and closes
		{`    x = '''`, false},                        // opens mid-line: code precedes the delimiter
		{`    password = "example fixture"`, true},
		{`    '''`, true},
		{`    password = "example fixture"`, false},
	}
	for i, l := range lines {
		assert.Equal(t, l.skip, sc.skipGeneric(l.text), "line %d: %q", i+1, l.text)
	}
}

func TestSecretScanContext_DocstringNotTrackedForNonPython(t *testing.T) {
	sc := newSecretScanContext("main.go")
	assert.False(t, sc.skipGeneric(`s := """`))
	assert.False(t, sc.skipGeneric(`password = "example fixture"`))
}

func TestIsPlaceholderSecretValue(t *testing.T) {
	placeholders := []string{
		"{{ secret_key }}", "${SECRET_KEY}", "<your-api-key>", "<YOUR_KEY>",
		`ALTER USER %(user)s IDENTIFIED BY "%(password)s"`, "user=%s pass=%s",
		"changeme-please", "example-not-a-real-key", "placeholder-value",
		"xxxxxxxxxxxx", "abc...", "ChangeMe",
	}
	for _, v := range placeholders {
		assert.True(t, isPlaceholderSecretValue(v), "expected placeholder: %q", v)
	}
	notPlaceholders := []string{"development key", "test key", "supersecretvalue123456"}
	for _, v := range notPlaceholders {
		assert.False(t, isPlaceholderSecretValue(v), "expected non-placeholder: %q", v)
	}
}

func TestIsFakeSecretValue(t *testing.T) {
	fakes := []string{
		"short", "1234567", "test key", "development key", "dummy", "fake-secret",
		"secret", "password", "changeme", "key", "test_password", "dev.key",
		"Test Key", "        ",
	}
	for _, v := range fakes {
		assert.True(t, isFakeSecretValue(v), "expected fake: %q", v)
	}
	real := []string{"supersecretvalue123456", "test key 9f8e7d", "keyboard cat", "development-2026-rotate"}
	for _, v := range real {
		assert.False(t, isFakeSecretValue(v), "expected real-looking: %q", v)
	}
}

func TestAdjustGenericSecret(t *testing.T) {
	prod := newSecretScanContext("src/settings.py")
	testCtx := newSecretScanContext("tests/conftest.py")

	// Placeholder values are dropped.
	_, _, keep := prod.adjustGenericSecret(`SECRET_KEY = "{{ secret_key }}"`, 0.6)
	assert.False(t, keep)
	_, _, keep = prod.adjustGenericSecret(`password = "<your-password-here>"`, 0.6)
	assert.False(t, keep)

	// Fake values drop to 0.2.
	conf, tags, keep := prod.adjustGenericSecret(`SECRET_KEY = "development key"`, 0.6)
	assert.True(t, keep)
	assert.Equal(t, secretFakeConfidence, conf)
	assert.Equal(t, []string{"likely-placeholder"}, tags)

	// Real-looking values in production code keep their confidence.
	conf, tags, keep = prod.adjustGenericSecret(`api_key = "supersecretvalue123456"`, 0.6)
	assert.True(t, keep)
	assert.Equal(t, 0.6, conf)
	assert.Empty(t, tags)

	// Test files are capped at 0.3.
	conf, tags, keep = testCtx.adjustGenericSecret(`api_key = "supersecretvalue123456"`, 0.6)
	assert.True(t, keep)
	assert.Equal(t, secretTestFileMaxConfidence, conf)
	assert.Equal(t, []string{"test-file"}, tags)

	// Fake value in a test file: min of both.
	conf, tags, keep = testCtx.adjustGenericSecret(`SECRET_KEY = "test key"`, 0.6)
	assert.True(t, keep)
	assert.Equal(t, secretFakeConfidence, conf)
	assert.Equal(t, []string{"likely-placeholder", "test-file"}, tags)

	// Lines where the value cannot be extracted keep the original confidence.
	conf, _, keep = prod.adjustGenericSecret(`something unrelated`, 0.6)
	assert.True(t, keep)
	assert.Equal(t, 0.6, conf)
}

func TestSecretRegistry_Match_GenericFlag(t *testing.T) {
	matches := defaultSecretRegistry.Match(`api_key = "supersecretvalue123456"`)
	require.Len(t, matches, 1)
	assert.True(t, matches[0].Generic)

	matches = defaultSecretRegistry.Match("const awsKey = \"AKIAIOSFODNN7EXAMPLE\"")
	require.Len(t, matches, 1)
	assert.False(t, matches[0].Generic)

	// A JWT-shaped example token is context-dependent as well.
	matches = defaultSecretRegistry.Match("token = eyJabc.eyJdef.notarealsignature")
	require.Len(t, matches, 1)
	assert.Equal(t, "jwt-token", matches[0].PatternID)
	assert.True(t, matches[0].Generic)
}
