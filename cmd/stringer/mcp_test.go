// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMCPCmd_IsRegistered(t *testing.T) {
	// Verify the mcp command is registered as a subcommand of root.
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Use == "mcp" {
			found = true
			break
		}
	}
	assert.True(t, found, "mcp command should be registered on rootCmd")
}

func TestMCPServeCmd_IsRegistered(t *testing.T) {
	// Verify the serve subcommand is registered under mcp.
	found := false
	for _, cmd := range mcpCmd.Commands() {
		if cmd.Use == "serve" {
			found = true
			break
		}
	}
	assert.True(t, found, "serve command should be registered on mcpCmd")
}

func TestMCPServeCmd_RejectsArgs(t *testing.T) {
	// cobra.NoArgs should reject positional arguments.
	err := mcpServeCmd.Args(mcpServeCmd, []string{"extra"})
	assert.Error(t, err)
}
