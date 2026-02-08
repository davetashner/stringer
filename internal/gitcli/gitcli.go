// Package gitcli provides native git command execution for blame and log
// operations that are too slow in go-git's pure-Go implementation.
package gitcli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DefaultTimeout is the per-command timeout for git operations.
const DefaultTimeout = 5 * time.Second

// BlameLine holds authorship data for a single line of a blamed file.
type BlameLine struct {
	AuthorName string
	AuthorTime time.Time
	LineNumber int
}

// BlameResult holds the full blame output for a file.
type BlameResult struct {
	Lines []BlameLine
}

// Run executes a git command in repoDir and returns its stdout.
func Run(ctx context.Context, repoDir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // args are controlled by callers within this package
	cmd.Dir = repoDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// BlameFile runs git blame --porcelain on the entire file and returns
// authorship data for every line.
func BlameFile(ctx context.Context, repoDir, relPath string) (*BlameResult, error) {
	out, err := Run(ctx, repoDir, "blame", "--porcelain", "--", relPath)
	if err != nil {
		return nil, err
	}
	return ParseBlamePortcelain(out)
}

// BlameOneLine runs git blame --porcelain on a single line and returns
// the author name and author time.
func BlameOneLine(ctx context.Context, repoDir, relPath string, line int) (string, time.Time, error) {
	lineSpec := fmt.Sprintf("%d,%d", line, line)
	out, err := Run(ctx, repoDir, "blame", "--porcelain", "-L", lineSpec, "--", relPath)
	if err != nil {
		return "", time.Time{}, err
	}

	result, err := ParseBlamePortcelain(out)
	if err != nil {
		return "", time.Time{}, err
	}
	if len(result.Lines) == 0 {
		return "", time.Time{}, fmt.Errorf("no blame data for %s:%d", relPath, line)
	}

	bl := result.Lines[0]
	return bl.AuthorName, bl.AuthorTime, nil
}

// commitMeta holds per-commit metadata parsed from porcelain output.
type commitMeta struct {
	AuthorName string
	AuthorTime time.Time
}

// ParseBlamePortcelain parses git blame --porcelain output into a BlameResult.
//
// Porcelain format: each line group starts with a header
// "<hash> <orig-line> <final-line> [<group-size>]", followed by key-value
// headers (only on first occurrence of each commit hash), then a tab-prefixed
// content line. We track commit metadata by hash since porcelain only emits
// full headers on the first occurrence per commit.
func ParseBlamePortcelain(data []byte) (*BlameResult, error) {
	commits := make(map[string]*commitMeta)
	var lines []BlameLine

	scanner := bytes.Split(data, []byte("\n"))
	var currentHash string
	var currentLine int

	i := 0
	for i < len(scanner) {
		line := string(scanner[i])

		// Skip empty lines at the end.
		if line == "" {
			i++
			continue
		}

		// Content line: starts with a tab.
		if strings.HasPrefix(line, "\t") {
			if currentHash != "" {
				meta := commits[currentHash]
				if meta != nil {
					lines = append(lines, BlameLine{
						AuthorName: meta.AuthorName,
						AuthorTime: meta.AuthorTime,
						LineNumber: currentLine,
					})
				}
			}
			i++
			continue
		}

		// Try to parse as a commit header: "<40-hex-hash> <orig> <final> [count]"
		parts := strings.Fields(line)
		if len(parts) >= 3 && len(parts[0]) == 40 && isHex(parts[0]) {
			currentHash = parts[0]
			if n, err := strconv.Atoi(parts[2]); err == nil {
				currentLine = n
			}
			// Initialize commit metadata if first time seeing this hash.
			if commits[currentHash] == nil {
				commits[currentHash] = &commitMeta{}
			}
			i++
			continue
		}

		// Key-value header line.
		if strings.HasPrefix(line, "author ") {
			if meta := commits[currentHash]; meta != nil {
				meta.AuthorName = strings.TrimPrefix(line, "author ")
			}
		} else if strings.HasPrefix(line, "author-time ") {
			if meta := commits[currentHash]; meta != nil {
				if ts, err := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64); err == nil {
					meta.AuthorTime = time.Unix(ts, 0)
				}
			}
		}

		i++
	}

	return &BlameResult{Lines: lines}, nil
}

// isHex reports whether s consists entirely of hexadecimal characters.
func isHex(s string) bool {
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return len(s) > 0
}
