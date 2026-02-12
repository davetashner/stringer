package bootstrap

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/davetashner/stringer/internal/docs"
)

// WizardResult holds the user's choices from the interactive wizard.
type WizardResult struct {
	Collectors        map[string]bool
	GitDepth          int
	GitSince          string
	LargeFileThresh   int
	LotteryThreshold  int
	GitHubTokenValid  bool
	AnthropicKeyValid bool
}

// defaultCollectors returns the default set of collectors and whether they
// should be enabled by default given the repo analysis.
func defaultCollectors(hasGitHub bool) []struct {
	Name    string
	Default bool
	Desc    string
} {
	return []struct {
		Name    string
		Default bool
		Desc    string
	}{
		{"todos", true, "Scan for TODO/FIXME/HACK/BUG comments"},
		{"gitlog", true, "Detect reverts, high-churn files, stale branches"},
		{"patterns", true, "Find large files, missing tests"},
		{"lotteryrisk", true, "Identify single-contributor knowledge silos"},
		{"github", hasGitHub, "Import GitHub issues and PR review comments"},
		{"dephealth", true, "Check for archived/stale dependencies"},
		{"vuln", true, "Scan dependencies for known vulnerabilities"},
	}
}

// RunWizard runs the interactive init wizard, prompting the user for
// configuration choices. It returns WizardResult with all selections.
func RunWizard(r io.Reader, w io.Writer, repoPath string) (*WizardResult, error) {
	scanner := bufio.NewScanner(r)

	// 1. Welcome + detection.
	analysis, _ := docs.Analyze(repoPath)
	remote := DetectGitHubRemote(repoPath)
	hasGitHub := remote != nil

	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Welcome to stringer init!")
	_, _ = fmt.Fprintln(w)
	if analysis != nil && analysis.Language != "" {
		_, _ = fmt.Fprintf(w, "  Detected: %s project", analysis.Language)
		if hasGitHub {
			_, _ = fmt.Fprintf(w, " (GitHub: %s/%s)", remote.Owner, remote.Repo)
		}
		_, _ = fmt.Fprintln(w)
	} else if hasGitHub {
		_, _ = fmt.Fprintf(w, "  Detected: GitHub repo (%s/%s)\n", remote.Owner, remote.Repo)
	}
	_, _ = fmt.Fprintln(w)

	// 2. Collector selection.
	_, _ = fmt.Fprintln(w, "Which collectors would you like to enable?")
	_, _ = fmt.Fprintln(w)

	defs := defaultCollectors(hasGitHub)
	result := &WizardResult{
		Collectors:       make(map[string]bool),
		GitDepth:         1000,
		GitSince:         "90d",
		LargeFileThresh:  500,
		LotteryThreshold: 80,
	}

	for _, c := range defs {
		dflt := "Y/n"
		if !c.Default {
			dflt = "y/N"
		}
		_, _ = fmt.Fprintf(w, "  Enable %s? (%s) [%s] ", c.Name, c.Desc, dflt)
		result.Collectors[c.Name] = promptYesNo(scanner, c.Default)
	}
	_, _ = fmt.Fprintln(w)

	// 3. Key thresholds (only ask if relevant collectors are enabled).
	if result.Collectors["gitlog"] {
		_, _ = fmt.Fprintf(w, "  Git history depth (commits to analyze) [%d]: ", result.GitDepth)
		if v := promptInt(scanner, result.GitDepth); v > 0 {
			result.GitDepth = v
		}

		_, _ = fmt.Fprintf(w, "  Git history window (e.g. 90d, 6m, 1y) [%s]: ", result.GitSince)
		if v := promptString(scanner, result.GitSince); v != "" {
			result.GitSince = v
		}
	}

	if result.Collectors["patterns"] {
		_, _ = fmt.Fprintf(w, "  Large file threshold (lines) [%d]: ", result.LargeFileThresh)
		if v := promptInt(scanner, result.LargeFileThresh); v > 0 {
			result.LargeFileThresh = v
		}
	}

	if result.Collectors["lotteryrisk"] {
		_, _ = fmt.Fprintf(w, "  Lottery risk ownership threshold (%%) [%d]: ", result.LotteryThreshold)
		if v := promptInt(scanner, result.LotteryThreshold); v > 0 {
			result.LotteryThreshold = v
		}
	}
	_, _ = fmt.Fprintln(w)

	// 4. Token validation.
	if result.Collectors["github"] {
		_, _ = fmt.Fprintf(w, "  GitHub token (GITHUB_TOKEN) — enter to validate, or press Enter to skip: ")
		token := promptString(scanner, "")
		if token != "" {
			_, _ = fmt.Fprintf(w, "  Validating GitHub token... ")
			if validateGitHubToken(token) {
				_, _ = fmt.Fprintln(w, "valid!")
				result.GitHubTokenValid = true
			} else {
				_, _ = fmt.Fprintln(w, "invalid or expired. You can set GITHUB_TOKEN later.")
			}
		} else {
			_, _ = fmt.Fprintln(w, "  Skipped. Set GITHUB_TOKEN env var before running scans.")
		}
	}

	_, _ = fmt.Fprintf(w, "  Anthropic API key (ANTHROPIC_API_KEY) — for LLM features, or press Enter to skip: ")
	anthropicKey := promptString(scanner, "")
	if anthropicKey != "" {
		_, _ = fmt.Fprintf(w, "  Validating Anthropic key... ")
		if validateAnthropicKey(anthropicKey) {
			_, _ = fmt.Fprintln(w, "valid!")
			result.AnthropicKeyValid = true
		} else {
			_, _ = fmt.Fprintln(w, "invalid or expired. Set ANTHROPIC_API_KEY later for LLM features.")
		}
	} else {
		_, _ = fmt.Fprintln(w, "  Skipped. LLM features (clustering, priority inference) require ANTHROPIC_API_KEY.")
	}

	_, _ = fmt.Fprintln(w)
	return result, nil
}

// promptYesNo reads a yes/no response. Empty input returns the default.
func promptYesNo(scanner *bufio.Scanner, defaultVal bool) bool {
	if !scanner.Scan() {
		return defaultVal
	}
	input := strings.TrimSpace(strings.ToLower(scanner.Text()))
	switch input {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		return defaultVal
	}
}

// promptString reads a string response. Empty input returns the default.
func promptString(scanner *bufio.Scanner, defaultVal string) string {
	if !scanner.Scan() {
		return defaultVal
	}
	input := strings.TrimSpace(scanner.Text())
	if input == "" {
		return defaultVal
	}
	return input
}

// promptInt reads an integer response. Empty or invalid input returns the default.
func promptInt(scanner *bufio.Scanner, defaultVal int) int {
	if !scanner.Scan() {
		return defaultVal
	}
	input := strings.TrimSpace(scanner.Text())
	if input == "" {
		return defaultVal
	}
	var v int
	if _, err := fmt.Sscanf(input, "%d", &v); err != nil {
		return defaultVal
	}
	return v
}

// validateGitHubToken tests a GitHub token by calling the /user endpoint.
func validateGitHubToken(token string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close() //nolint:errcheck // validation only
	return resp.StatusCode == http.StatusOK
}

// validateAnthropicKey tests an Anthropic API key by listing models.
func validateAnthropicKey(key string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/v1/models", nil)
	if err != nil {
		return false
	}
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close() //nolint:errcheck // validation only
	return resp.StatusCode == http.StatusOK
}

// httpClient is the HTTP client used for token validation.
// Tests can replace this to avoid real network calls.
var httpClient HTTPClient = &http.Client{Timeout: 10 * time.Second}

// HTTPClient is an interface for HTTP requests, allowing test injection.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}
