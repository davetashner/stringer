package redact

import (
	"os"
	"testing"
)

func TestString_RedactsKnownEnvVars(t *testing.T) {
	const secret = "ghp_TESTSECRETVALUE1234567890" //nolint:gosec // fake test credential
	t.Setenv("GITHUB_TOKEN", secret)

	input := "error: auth failed with token ghp_TESTSECRETVALUE1234567890 for repo"
	got := String(input)

	if got == input {
		t.Error("expected secret to be redacted, but string was unchanged")
	}
	if expected := "error: auth failed with token [REDACTED] for repo"; got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}

func TestString_NoSecretSetIsNoop(t *testing.T) {
	// Ensure env var is unset for this test.
	os.Unsetenv("GITHUB_TOKEN") //nolint:errcheck // test cleanup

	input := "some normal error message"
	got := String(input)

	if got != input {
		t.Errorf("expected no change, got %q", got)
	}
}

func TestString_ShortValuesIgnored(t *testing.T) {
	// Values under 4 chars could cause false-positive redaction.
	t.Setenv("GITHUB_TOKEN", "abc")

	input := "abc is in the string abc"
	got := String(input)

	if got != input {
		t.Errorf("expected no redaction for short values, got %q", got)
	}
}

func TestString_MultipleSecrets(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token-aaaa")
	t.Setenv("ANTHROPIC_API_KEY", "test-token-bbbb")

	input := "tokens: test-token-aaaa and test-token-bbbb"
	got := String(input)

	expected := "tokens: [REDACTED] and [REDACTED]"
	if got != expected {
		t.Errorf("got %q, want %q", got, expected)
	}
}
