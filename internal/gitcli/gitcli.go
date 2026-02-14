// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

// Package gitcli provides native git CLI execution for blame operations.
// It shells out to the system git binary for blame (which uses packfile indexes
// and runs in milliseconds) while the rest of stringer uses go-git for commit
// iteration, branch listing, and repo detection. See DR-011 for rationale.
package gitcli

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/davetashner/stringer/internal/testable"
)

// DefaultTimeout is the per-file blame timeout per DR-011.
const DefaultTimeout = 5 * time.Second

// executor is the package-level CommandExecutor used by Available and Exec.
// It defaults to the real os/exec implementation.
var executor testable.CommandExecutor = testable.DefaultExecutor()

// SetExecutor replaces the package-level CommandExecutor. Pass nil to restore
// the default production executor. This is intended for testing.
func SetExecutor(e testable.CommandExecutor) {
	if e == nil {
		executor = testable.DefaultExecutor()
		return
	}
	executor = e
}

// BlameLine holds attribution data for a single source line.
type BlameLine struct {
	AuthorName string
	AuthorTime time.Time
}

// commitInfo caches author metadata for a single commit SHA.
type commitInfo struct {
	authorName string
	authorTime time.Time
}

// Available returns nil if git is on PATH, or an error otherwise.
func Available() error {
	_, err := executor.LookPath("git")
	if err != nil {
		return fmt.Errorf("git not found on PATH: %w", err)
	}
	return nil
}

// Exec runs git with the given arguments in repoDir and returns combined stdout.
func Exec(ctx context.Context, repoDir string, args ...string) (string, error) {
	cmd := executor.CommandContext(ctx, "git", args...)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// BlameSingleLine runs `git blame --porcelain -L <line>,<line> -- <relPath>`
// and returns the attribution for that line.
func BlameSingleLine(ctx context.Context, repoDir, relPath string, line int) (*BlameLine, error) {
	lineSpec := fmt.Sprintf("%d,%d", line, line)
	out, err := Exec(ctx, repoDir, "blame", "--porcelain", "-L", lineSpec, "--", relPath)
	if err != nil {
		return nil, err
	}

	lines, err := parsePorcelainBlame(out)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("no blame output for %s:%d", relPath, line)
	}
	return &lines[0], nil
}

// BlameFile runs `git blame --porcelain -- <relPath>` and returns one
// BlameLine per source line.
func BlameFile(ctx context.Context, repoDir, relPath string) ([]BlameLine, error) {
	out, err := Exec(ctx, repoDir, "blame", "--porcelain", "--", relPath)
	if err != nil {
		return nil, err
	}
	return parsePorcelainBlame(out)
}

// parsePorcelainBlame parses the output of `git blame --porcelain`.
//
// Porcelain format consists of blocks, one per source line:
//   - Header line: <sha> <orig-line> <final-line> [<num-lines>]
//   - On first occurrence of a SHA: metadata lines (author, author-time, etc.)
//   - Content line: TAB-prefixed actual source line
//
// When a SHA repeats, the metadata block is omitted (abbreviated block) and
// only the header + content lines appear.
func parsePorcelainBlame(output string) ([]BlameLine, error) {
	cache := make(map[string]*commitInfo)
	var result []BlameLine

	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := scanner.Text()

		// Look for SHA header line.
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		sha := fields[0]
		if !isHexSHA(sha) {
			continue
		}

		// Parse metadata lines for this block (if first occurrence of SHA).
		info, cached := cache[sha]
		if !cached {
			info = &commitInfo{}
			cache[sha] = info
		}

		for scanner.Scan() {
			mline := scanner.Text()

			// Content line starts with a tab — end of this block.
			if strings.HasPrefix(mline, "\t") {
				break
			}

			if strings.HasPrefix(mline, "author ") {
				info.authorName = strings.TrimPrefix(mline, "author ")
			} else if strings.HasPrefix(mline, "author-time ") {
				ts, err := strconv.ParseInt(strings.TrimPrefix(mline, "author-time "), 10, 64)
				if err == nil {
					info.authorTime = time.Unix(ts, 0)
				}
			}
		}

		result = append(result, BlameLine{
			AuthorName: info.authorName,
			AuthorTime: info.authorTime,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning blame output: %w", err)
	}

	return result, nil
}

// LastCommitTime returns the author time of the most recent commit that touched
// the given path (file or directory). Returns zero time if no commits are found.
func LastCommitTime(ctx context.Context, repoDir, path string) (time.Time, error) {
	out, err := Exec(ctx, repoDir, "log", "-1", "--format=%aI", "--", path)
	if err != nil {
		return time.Time{}, err
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, out)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing commit time %q: %w", out, err)
	}
	return t, nil
}

// NumstatCommit holds parsed data from a single commit in git log --numstat output.
type NumstatCommit struct {
	SHA        string
	Author     string
	AuthorTime time.Time
	Files      []string
}

// LogNumstat runs `git log --numstat --format=...` and returns structured
// per-commit data. maxCount limits the number of commits returned.
// If since is non-empty, it is passed as --since=<value>.
func LogNumstat(ctx context.Context, repoDir string, maxCount int, since string) ([]NumstatCommit, error) {
	args := []string{
		"log",
		"--numstat",
		"--format=format:%H|%aN|%aI",
		fmt.Sprintf("--max-count=%d", maxCount),
	}
	if since != "" {
		args = append(args, "--since="+since)
	}

	out, err := Exec(ctx, repoDir, args...)
	if err != nil {
		return nil, err
	}
	return parseNumstatLog(out)
}

// parseNumstatLog parses the output of `git log --numstat --format='format:%H|%aN|%aI'`.
//
// Format:
//
//	<sha>|<author>|<iso-date>
//	<added>\t<removed>\t<filepath>
//	                                    ← blank line separates commits
func parseNumstatLog(output string) ([]NumstatCommit, error) {
	var commits []NumstatCommit
	scanner := bufio.NewScanner(strings.NewReader(output))

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Try to parse as a header line: SHA|Author|Date
		parts := strings.SplitN(line, "|", 3)
		if len(parts) == 3 && isHexSHA(parts[0]) {
			t, _ := time.Parse(time.RFC3339, strings.TrimSpace(parts[2]))
			commit := NumstatCommit{
				SHA:        parts[0],
				Author:     parts[1],
				AuthorTime: t,
			}

			// Read numstat lines until blank line or next header.
			for scanner.Scan() {
				fline := scanner.Text()
				if fline == "" {
					break
				}
				// Numstat lines: <added>\t<removed>\t<filepath>
				fields := strings.SplitN(fline, "\t", 3)
				if len(fields) != 3 {
					break // not a numstat line
				}
				filePath := fields[2]
				// Handle renames: "old => new" or "{old => new}/path"
				if strings.Contains(filePath, " => ") {
					filePath = extractRenameDest(filePath)
				}
				commit.Files = append(commit.Files, filePath)
			}

			commits = append(commits, commit)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning numstat output: %w", err)
	}

	return commits, nil
}

// extractRenameDest extracts the destination path from a git rename notation.
// Handles both "old => new" and "prefix/{old => new}/suffix" formats.
func extractRenameDest(s string) string {
	// Check for brace notation: "prefix/{old => new}/suffix"
	braceStart := strings.Index(s, "{")
	braceEnd := strings.Index(s, "}")
	if braceStart >= 0 && braceEnd > braceStart {
		prefix := s[:braceStart]
		suffix := s[braceEnd+1:]
		inner := s[braceStart+1 : braceEnd]
		if arrowIdx := strings.Index(inner, " => "); arrowIdx >= 0 {
			newPart := inner[arrowIdx+4:]
			return prefix + newPart + suffix
		}
	}

	// Simple notation: "old => new"
	if arrowIdx := strings.Index(s, " => "); arrowIdx >= 0 {
		return s[arrowIdx+4:]
	}

	return s
}

// isHexSHA returns true if s looks like a full or abbreviated git SHA (hex string, >= 4 chars).
func isHexSHA(s string) bool {
	if len(s) < 4 {
		return false
	}
	for _, r := range []byte(s) {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
