// Package testable provides interfaces for mocking external dependencies
// such as exec.Command and exec.LookPath in tests.
package testable

import (
	"context"
	"os/exec"
)

// CommandExecutor abstracts exec.LookPath and exec.CommandContext so that
// callers (e.g., gitcli) can be tested without a real git binary.
type CommandExecutor interface {
	// LookPath searches for an executable named file in the directories
	// named by the PATH environment variable.
	LookPath(file string) (string, error)

	// CommandContext returns an *exec.Cmd configured to run name with the
	// given arguments. The provided context is used for cancellation.
	CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd
}

// RealCommandExecutor is the production implementation that delegates to the
// os/exec package.
type RealCommandExecutor struct{}

// LookPath wraps exec.LookPath.
func (r *RealCommandExecutor) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// CommandContext wraps exec.CommandContext.
func (r *RealCommandExecutor) CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...) //nolint:gosec // args are controlled by callers
}

// DefaultExecutor returns a production CommandExecutor backed by the os/exec
// package.
func DefaultExecutor() CommandExecutor {
	return &RealCommandExecutor{}
}
