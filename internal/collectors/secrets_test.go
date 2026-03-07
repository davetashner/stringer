// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"regexp"
	"testing"

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
	assert.Equal(t, 3, defaultSecretRegistry.Count(), "default registry should have 3 patterns")
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
