// Package gitcli provides native git CLI execution for blame operations.
// It shells out to the system git binary for blame (which uses packfile indexes
// and runs in milliseconds) while the rest of stringer uses go-git for commit
// iteration, branch listing, and repo detection. See DR-011 for rationale.
package gitcli

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// DefaultTimeout is the per-file blame timeout per DR-011.
const DefaultTimeout = 5 * time.Second

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
	_, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("git not found on PATH: %w", err)
	}
	return nil
}

// Exec runs git with the given arguments in repoDir and returns combined stdout.
func Exec(ctx context.Context, repoDir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // args are controlled by callers within this package
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
