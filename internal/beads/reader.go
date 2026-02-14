// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package beads

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Bead represents a single issue from the beads backlog.
type Bead struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	Type      string   `json:"type,omitempty"`
	IssueType string   `json:"issue_type,omitempty"`
	Priority  int      `json:"priority"`
	Labels    []string `json:"labels,omitempty"`
}

// BeadsDir is the standard directory for beads data.
const BeadsDir = ".beads"

// IssuesFile is the standard filename for beads issues.
const IssuesFile = "issues.jsonl"

// LoadBeads reads and parses the .beads/issues.jsonl file from a repo path.
// Returns nil, nil if the file does not exist.
func LoadBeads(repoPath string) ([]Bead, error) {
	path := filepath.Join(repoPath, BeadsDir, IssuesFile)

	f, err := os.Open(path) //nolint:gosec // path constructed from validated repo path
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open beads file: %w", err)
	}
	defer f.Close() //nolint:errcheck // best-effort close on read-only file

	var beads []Bead
	scanner := bufio.NewScanner(f)
	// Increase buffer for large JSONL lines.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var b Bead
		if err := json.Unmarshal(line, &b); err != nil {
			return nil, fmt.Errorf("parse bead at line %d: %w", lineNum, err)
		}
		beads = append(beads, b)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read beads file: %w", err)
	}

	return beads, nil
}
