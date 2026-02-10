package bootstrap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// mcpConfig represents the structure of a .mcp.json file.
type mcpConfig struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

// mcpServerEntry is the stringer MCP server configuration.
type mcpServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// GenerateMCPConfig generates or updates .mcp.json with a stringer MCP server
// entry. It only acts when Claude Code is detected (presence of .claude/ directory).
func GenerateMCPConfig(repoPath string) (Action, error) {
	// Detect Claude Code by checking for .claude/ directory.
	claudeDir := filepath.Join(repoPath, ".claude")
	if _, err := FS.Stat(claudeDir); err != nil {
		if os.IsNotExist(err) {
			return Action{
				File:        ".mcp.json",
				Operation:   "skipped",
				Description: "Claude Code not detected (.claude/ directory not found)",
			}, nil
		}
		return Action{}, fmt.Errorf("checking .claude directory: %w", err)
	}

	mcpPath := filepath.Join(repoPath, ".mcp.json")

	// Check if .mcp.json already exists.
	existing, err := FS.ReadFile(mcpPath)
	if err != nil && !os.IsNotExist(err) {
		return Action{}, fmt.Errorf("reading .mcp.json: %w", err)
	}

	if err == nil {
		// File exists — check if stringer entry is already present.
		var cfg mcpConfig
		if jsonErr := json.Unmarshal(existing, &cfg); jsonErr != nil {
			return Action{}, fmt.Errorf("parsing .mcp.json: %w", jsonErr)
		}

		if _, ok := cfg.MCPServers["stringer"]; ok {
			return Action{
				File:        ".mcp.json",
				Operation:   "skipped",
				Description: "stringer MCP server already configured",
			}, nil
		}

		// Merge stringer entry into existing config.
		entry := mcpServerEntry{
			Command: "stringer",
			Args:    []string{"mcp", "serve"},
		}
		entryJSON, entryErr := json.Marshal(entry)
		if entryErr != nil {
			return Action{}, fmt.Errorf("marshaling MCP server entry: %w", entryErr)
		}
		cfg.MCPServers["stringer"] = entryJSON

		data, marshalErr := json.MarshalIndent(cfg, "", "  ")
		if marshalErr != nil {
			return Action{}, fmt.Errorf("marshaling .mcp.json: %w", marshalErr)
		}
		data = append(data, '\n')

		if writeErr := FS.WriteFile(mcpPath, data, 0o644); writeErr != nil {
			return Action{}, fmt.Errorf("writing .mcp.json: %w", writeErr)
		}

		return Action{
			File:        ".mcp.json",
			Operation:   "updated",
			Description: "added stringer MCP server entry",
		}, nil
	}

	// No .mcp.json — create new file.
	cfg := mcpConfig{
		MCPServers: map[string]json.RawMessage{},
	}
	entry := mcpServerEntry{
		Command: "stringer",
		Args:    []string{"mcp", "serve"},
	}
	entryJSON, entryErr := json.Marshal(entry)
	if entryErr != nil {
		return Action{}, fmt.Errorf("marshaling MCP server entry: %w", entryErr)
	}
	cfg.MCPServers["stringer"] = entryJSON

	data, marshalErr := json.MarshalIndent(cfg, "", "  ")
	if marshalErr != nil {
		return Action{}, fmt.Errorf("marshaling .mcp.json: %w", marshalErr)
	}
	data = append(data, '\n')

	if writeErr := FS.WriteFile(mcpPath, data, 0o644); writeErr != nil {
		return Action{}, fmt.Errorf("creating .mcp.json: %w", writeErr)
	}

	return Action{
		File:        ".mcp.json",
		Operation:   "created",
		Description: "created with stringer MCP server entry",
	}, nil
}
