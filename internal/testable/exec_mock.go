package testable

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// MockCommandExecutor is a test double for CommandExecutor.
// It can simulate git not found, command failures, and predetermined outputs.
type MockCommandExecutor struct {
	// LookPathErr, when non-nil, is returned by LookPath for any file.
	LookPathErr error

	// LookPathResult is returned as the path when LookPathErr is nil.
	LookPathResult string

	// CommandOutputs maps a command key (e.g., "git blame --porcelain") to the
	// stdout that the resulting exec.Cmd should produce. The key is built from
	// the command name and all arguments joined by spaces.
	CommandOutputs map[string]string

	// CommandErrors maps a command key to an error message. When set, the
	// resulting exec.Cmd will fail with that message written to stderr.
	CommandErrors map[string]string

	// DefaultOutput is returned when no key matches in CommandOutputs.
	DefaultOutput string

	// DefaultError, when non-empty, makes every unmatched command fail.
	DefaultError string

	// Calls records the command keys that were invoked, for assertion purposes.
	Calls []string
}

// LookPath returns the configured result or error.
func (m *MockCommandExecutor) LookPath(_ string) (string, error) {
	if m.LookPathErr != nil {
		return "", m.LookPathErr
	}
	if m.LookPathResult != "" {
		return m.LookPathResult, nil
	}
	return "/usr/bin/git", nil
}

// CommandContext returns an *exec.Cmd that, when executed, produces the
// pre-configured output or error. It uses "echo" / "false" shell commands to
// simulate the behaviour without running the real binary.
func (m *MockCommandExecutor) CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	key := name + " " + strings.Join(args, " ")
	m.Calls = append(m.Calls, key)

	// Check for a matching error first.
	if m.CommandErrors != nil {
		if errMsg, ok := m.CommandErrors[key]; ok {
			// Use a command that writes to stderr and exits non-zero.
			cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("echo %q >&2; exit 1", errMsg)) //nolint:gosec // test helper
			return cmd
		}
	}

	// Check for a matching output.
	if m.CommandOutputs != nil {
		if out, ok := m.CommandOutputs[key]; ok {
			cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("printf '%%s' %q", out)) //nolint:gosec // test helper
			return cmd
		}
	}

	// Fall back to defaults.
	if m.DefaultError != "" {
		cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("echo %q >&2; exit 1", m.DefaultError)) //nolint:gosec // test helper
		return cmd
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("printf '%%s' %q", m.DefaultOutput)) //nolint:gosec // test helper
	return cmd
}
