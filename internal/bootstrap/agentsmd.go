package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	markerStart = "<!-- stringer:init:start -->"
	markerEnd   = "<!-- stringer:init:end -->"
)

// agentSnippet is the content inserted between the markers in AGENTS.md.
const agentSnippet = `## Stringer Integration

[Stringer](https://github.com/davetashner/stringer) mines this repository for
actionable work items — TODOs, git history patterns, lottery-risk areas, and
GitHub issues — and outputs them as structured signals.

### Quick Start

` + "```" + `bash
# Scan for work items (outputs Beads JSONL by default)
stringer scan .

# Generate a repository health report
stringer report .

# Generate/update AGENTS.md documentation
stringer docs .

# Import signals into your beads backlog
stringer scan . | bd import
` + "```" + `

### Configuration

Edit ` + "`.stringer.yaml`" + ` in the repo root to customize collectors, output
format, and filtering. Run ` + "`stringer init --force`" + ` to regenerate defaults.
`

// AppendAgentSnippet appends the stringer integration snippet to AGENTS.md.
// It uses marker comments for idempotency:
//   - If AGENTS.md has the markers: skip (already present)
//   - If AGENTS.md exists, no markers: append snippet
//   - If no AGENTS.md: create minimal file with snippet
func AppendAgentSnippet(repoPath string) (Action, error) {
	agentsPath := filepath.Join(repoPath, "AGENTS.md")

	content, err := os.ReadFile(agentsPath) //nolint:gosec // repo-local file
	if err != nil && !os.IsNotExist(err) {
		return Action{}, fmt.Errorf("reading AGENTS.md: %w", err)
	}

	existingContent := string(content)

	// Already has our markers — skip.
	if strings.Contains(existingContent, markerStart) {
		return Action{
			File:        "AGENTS.md",
			Operation:   "skipped",
			Description: "stringer section already present",
		}, nil
	}

	wrapped := markerStart + "\n" + agentSnippet + markerEnd + "\n"

	if os.IsNotExist(err) {
		// No AGENTS.md — create one.
		newContent := "# AGENTS.md\n\n" + wrapped
		if writeErr := os.WriteFile(agentsPath, []byte(newContent), 0o644); writeErr != nil { //nolint:gosec // documentation file
			return Action{}, fmt.Errorf("creating AGENTS.md: %w", writeErr)
		}
		return Action{
			File:        "AGENTS.md",
			Operation:   "created",
			Description: "created with stringer integration section",
		}, nil
	}

	// Existing AGENTS.md — append snippet.
	separator := "\n"
	if !strings.HasSuffix(existingContent, "\n") {
		separator = "\n\n"
	}
	newContent := existingContent + separator + wrapped
	if writeErr := os.WriteFile(agentsPath, []byte(newContent), 0o644); writeErr != nil { //nolint:gosec // documentation file
		return Action{}, fmt.Errorf("updating AGENTS.md: %w", writeErr)
	}

	return Action{
		File:        "AGENTS.md",
		Operation:   "updated",
		Description: "appended stringer integration section",
	}, nil
}
