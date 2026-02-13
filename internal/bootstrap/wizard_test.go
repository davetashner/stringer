package bootstrap

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockHTTPClient returns canned responses for token validation tests.
type mockHTTPClient struct {
	StatusCode int
	Err        error
}

func (m *mockHTTPClient) Do(_ *http.Request) (*http.Response, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return &http.Response{
		StatusCode: m.StatusCode,
		Body:       io.NopCloser(strings.NewReader("")),
	}, nil
}

func TestRunWizard_AllDefaults(t *testing.T) {
	// All empty lines → accept every default.
	input := strings.Repeat("\n", 20)
	var out bytes.Buffer

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))

	result, err := RunWizard(strings.NewReader(input), &out, dir)
	require.NoError(t, err)

	// All collectors enabled by default (except github — no remote).
	assert.True(t, result.Collectors["todos"])
	assert.True(t, result.Collectors["gitlog"])
	assert.True(t, result.Collectors["patterns"])
	assert.True(t, result.Collectors["lotteryrisk"])
	assert.False(t, result.Collectors["github"]) // no GitHub remote detected
	assert.True(t, result.Collectors["dephealth"])
	assert.True(t, result.Collectors["vuln"])

	// Default thresholds.
	assert.Equal(t, 1000, result.GitDepth)
	assert.Equal(t, "90d", result.GitSince)
	assert.Equal(t, 500, result.LargeFileThresh)
	assert.Equal(t, 80, result.LotteryThreshold)

	// Tokens skipped.
	assert.False(t, result.GitHubTokenValid)
	assert.False(t, result.AnthropicKeyValid)

	// Output should contain welcome and detection.
	output := out.String()
	assert.Contains(t, output, "Welcome to stringer init!")
	assert.Contains(t, output, "Detected: Go project")
}

func TestRunWizard_DisableCollectors(t *testing.T) {
	// Disable todos, gitlog; enable rest with defaults.
	lines := []string{
		"n", // todos: no
		"n", // gitlog: no
		"",  // patterns: default (yes)
		"",  // lotteryrisk: default (yes)
		"",  // github: default (no, no remote)
		"",  // dephealth: default (yes)
		"",  // vuln: default (yes)
		"",  // large_file_threshold: default
		"",  // lottery_risk_threshold: default
		"",  // anthropic key: skip
	}
	input := strings.Join(lines, "\n") + "\n"
	var out bytes.Buffer
	dir := t.TempDir()

	result, err := RunWizard(strings.NewReader(input), &out, dir)
	require.NoError(t, err)

	assert.False(t, result.Collectors["todos"])
	assert.False(t, result.Collectors["gitlog"])
	assert.True(t, result.Collectors["patterns"])
	assert.True(t, result.Collectors["lotteryrisk"])
}

func TestRunWizard_CustomThresholds(t *testing.T) {
	lines := []string{
		"",    // todos: default
		"",    // gitlog: default
		"",    // patterns: default
		"",    // lotteryrisk: default
		"",    // github: default
		"",    // dephealth: default
		"",    // vuln: default
		"500", // git_depth
		"6m",  // git_since
		"300", // large_file_threshold
		"60",  // lottery_risk_threshold
		"",    // anthropic key: skip
	}
	input := strings.Join(lines, "\n") + "\n"
	var out bytes.Buffer
	dir := t.TempDir()

	result, err := RunWizard(strings.NewReader(input), &out, dir)
	require.NoError(t, err)

	assert.Equal(t, 500, result.GitDepth)
	assert.Equal(t, "6m", result.GitSince)
	assert.Equal(t, 300, result.LargeFileThresh)
	assert.Equal(t, 60, result.LotteryThreshold)
}

func TestRunWizard_InvalidIntFallsBackToDefault(t *testing.T) {
	lines := []string{
		"",       // todos
		"",       // gitlog
		"",       // patterns
		"",       // lotteryrisk
		"",       // github
		"",       // dephealth
		"",       // vuln
		"notnum", // git_depth: invalid
		"",       // git_since: default
		"abc",    // large_file_threshold: invalid
		"",       // lottery_risk_threshold: default
		"",       // anthropic key: skip
	}
	input := strings.Join(lines, "\n") + "\n"
	var out bytes.Buffer
	dir := t.TempDir()

	result, err := RunWizard(strings.NewReader(input), &out, dir)
	require.NoError(t, err)

	assert.Equal(t, 1000, result.GitDepth)       // falls back to default
	assert.Equal(t, 500, result.LargeFileThresh) // falls back to default
}

func TestRunWizard_SkipsThresholdsForDisabledCollectors(t *testing.T) {
	// Disable gitlog, patterns, lotteryrisk — threshold prompts should be skipped.
	lines := []string{
		"",  // todos: default
		"n", // gitlog: no
		"n", // patterns: no
		"n", // lotteryrisk: no
		"",  // github: default
		"",  // dephealth: default
		"",  // vuln: default
		"",  // anthropic key: skip
	}
	input := strings.Join(lines, "\n") + "\n"
	var out bytes.Buffer
	dir := t.TempDir()

	result, err := RunWizard(strings.NewReader(input), &out, dir)
	require.NoError(t, err)

	// Should not have prompted for thresholds.
	output := out.String()
	assert.NotContains(t, output, "Git history depth")
	assert.NotContains(t, output, "Large file threshold")
	assert.NotContains(t, output, "Lottery risk ownership")

	// Defaults still set (but won't be used since collectors disabled).
	assert.Equal(t, 1000, result.GitDepth)
}

func TestPromptYesNo(t *testing.T) {
	tests := []struct {
		input    string
		dflt     bool
		expected bool
	}{
		{"y\n", false, true},
		{"yes\n", false, true},
		{"Y\n", false, true},
		{"n\n", true, false},
		{"no\n", true, false},
		{"N\n", true, false},
		{"\n", true, true},
		{"\n", false, false},
		{"garbage\n", true, true},
		{"garbage\n", false, false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q_default=%v", strings.TrimSpace(tt.input), tt.dflt), func(t *testing.T) {
			scanner := bufioScanner(tt.input)
			got := promptYesNo(scanner, tt.dflt)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestPromptString(t *testing.T) {
	tests := []struct {
		input    string
		dflt     string
		expected string
	}{
		{"hello\n", "default", "hello"},
		{"\n", "default", "default"},
		{"  spaced  \n", "default", "spaced"},
	}

	for _, tt := range tests {
		scanner := bufioScanner(tt.input)
		got := promptString(scanner, tt.dflt)
		assert.Equal(t, tt.expected, got)
	}
}

func TestPromptInt(t *testing.T) {
	tests := []struct {
		input    string
		dflt     int
		expected int
	}{
		{"42\n", 10, 42},
		{"\n", 10, 10},
		{"abc\n", 10, 10},
		{"0\n", 10, 0}, // 0 is a valid parsed value
	}

	for _, tt := range tests {
		scanner := bufioScanner(tt.input)
		got := promptInt(scanner, tt.dflt)
		assert.Equal(t, tt.expected, got)
	}
}

func TestValidateGitHubToken_Success(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	httpClient = &mockHTTPClient{StatusCode: http.StatusOK}

	assert.True(t, validateGitHubToken("ghp_test123"))
}

func TestValidateGitHubToken_Failure(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	httpClient = &mockHTTPClient{StatusCode: http.StatusUnauthorized}

	assert.False(t, validateGitHubToken("bad_token"))
}

func TestValidateGitHubToken_NetworkError(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	httpClient = &mockHTTPClient{Err: fmt.Errorf("connection refused")}

	assert.False(t, validateGitHubToken("any_token"))
}

func TestPromptYesNo_EOF(t *testing.T) {
	// Empty reader — scanner.Scan() returns false immediately.
	scanner := bufio.NewScanner(strings.NewReader(""))
	assert.True(t, promptYesNo(scanner, true), "should return default true on EOF")
	assert.False(t, promptYesNo(scanner, false), "should return default false on EOF")
}

func TestPromptString_EOF(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader(""))
	assert.Equal(t, "fallback", promptString(scanner, "fallback"))
}

func TestPromptInt_EOF(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader(""))
	assert.Equal(t, 42, promptInt(scanner, 42))
}

func TestValidateAnthropicKey_NetworkError(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	httpClient = &mockHTTPClient{Err: fmt.Errorf("connection refused")}

	assert.False(t, validateAnthropicKey("sk-ant-any"))
}

func TestValidateAnthropicKey_Success(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	httpClient = &mockHTTPClient{StatusCode: http.StatusOK}

	assert.True(t, validateAnthropicKey("sk-ant-test123"))
}

func TestValidateAnthropicKey_Failure(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	httpClient = &mockHTTPClient{StatusCode: http.StatusUnauthorized}

	assert.False(t, validateAnthropicKey("bad_key"))
}

func TestRunWizard_WithGitHub(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	// Mock HTTP client returns OK for both GitHub and Anthropic key validation.
	httpClient = &mockHTTPClient{StatusCode: http.StatusOK}

	dir := initGitRepo(t, "https://github.com/octocat/hello-world.git")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))

	lines := []string{
		"",            // todos: default (yes)
		"",            // gitlog: default (yes)
		"",            // patterns: default (yes)
		"",            // lotteryrisk: default (yes)
		"",            // github: default (yes, remote detected)
		"",            // dephealth: default (yes)
		"",            // vuln: default (yes)
		"",            // git_depth: default
		"",            // git_since: default
		"",            // large_file_threshold: default
		"",            // lottery_risk_threshold: default
		"ghp_test",    // github token
		"sk-ant-test", // anthropic key
	}
	input := strings.Join(lines, "\n") + "\n"
	var out bytes.Buffer

	result, err := RunWizard(strings.NewReader(input), &out, dir)
	require.NoError(t, err)

	assert.True(t, result.Collectors["github"])
	assert.True(t, result.GitHubTokenValid)
	assert.True(t, result.AnthropicKeyValid)

	output := out.String()
	assert.Contains(t, output, "GitHub: octocat/hello-world")
	assert.Contains(t, output, "valid!")
}

func TestRunWizard_InvalidTokens(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	// Mock HTTP client returns Unauthorized for both tokens.
	httpClient = &mockHTTPClient{StatusCode: http.StatusUnauthorized}

	dir := initGitRepo(t, "https://github.com/octocat/hello-world.git")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))

	lines := []string{
		"", "", "", "", "", "", "", // 7 collectors: all defaults
		"", "", "", "", // 4 thresholds: defaults
		"bad_token", // github token
		"bad_key",   // anthropic key
	}
	input := strings.Join(lines, "\n") + "\n"
	var out bytes.Buffer

	result, err := RunWizard(strings.NewReader(input), &out, dir)
	require.NoError(t, err)

	assert.False(t, result.GitHubTokenValid)
	assert.False(t, result.AnthropicKeyValid)
	assert.Contains(t, out.String(), "invalid or expired")
}

func TestRunWizard_AnthropicKeyOnly(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	httpClient = &mockHTTPClient{StatusCode: http.StatusOK}

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))

	lines := []string{
		"", "", "", "", "", "", "", // 7 collectors: all defaults (no github)
		"", "", "", "", // 4 thresholds
		"sk-ant-valid", // anthropic key
	}
	input := strings.Join(lines, "\n") + "\n"
	var out bytes.Buffer

	result, err := RunWizard(strings.NewReader(input), &out, dir)
	require.NoError(t, err)
	assert.True(t, result.AnthropicKeyValid)
}

func TestRunWizard_NoGitHubLanguageOnly(t *testing.T) {
	// Covers the branch: analysis.Language != "" but no GitHub remote.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.22\n"), 0o600))

	input := strings.Repeat("\n", 20)
	var out bytes.Buffer

	_, err := RunWizard(strings.NewReader(input), &out, dir)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Detected: Go project")
}

func TestDefaultCollectors_GitHubDetected(t *testing.T) {
	defs := defaultCollectors(true)
	found := false
	for _, c := range defs {
		if c.Name == "github" {
			found = true
			assert.True(t, c.Default, "github should be enabled when remote detected")
		}
	}
	assert.True(t, found, "github collector should be in the list")
}

func TestDefaultCollectors_NoGitHub(t *testing.T) {
	defs := defaultCollectors(false)
	for _, c := range defs {
		if c.Name == "github" {
			assert.False(t, c.Default, "github should be disabled when no remote")
		}
	}
}

// bufioScanner creates a bufio.Scanner from a string for test use.
func bufioScanner(input string) *bufio.Scanner {
	return bufio.NewScanner(strings.NewReader(input))
}
