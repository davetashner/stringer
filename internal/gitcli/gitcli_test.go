package gitcli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestParseBlamePortcelain_SingleCommit(t *testing.T) {
	// Synthetic porcelain output: one commit, two lines.
	data := []byte(
		"abc1234567890abc1234567890abc123456789ab 1 1 2\n" +
			"author Alice\n" +
			"author-mail <alice@example.com>\n" +
			"author-time 1700000000\n" +
			"author-tz +0000\n" +
			"committer Alice\n" +
			"committer-mail <alice@example.com>\n" +
			"committer-time 1700000000\n" +
			"committer-tz +0000\n" +
			"summary initial commit\n" +
			"filename main.go\n" +
			"\tpackage main\n" +
			"abc1234567890abc1234567890abc123456789ab 2 2\n" +
			"\tfunc main() {}\n",
	)

	result, err := ParseBlamePortcelain(data)
	if err != nil {
		t.Fatalf("ParseBlamePortcelain() error: %v", err)
	}
	if len(result.Lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(result.Lines))
	}

	for i, bl := range result.Lines {
		if bl.AuthorName != "Alice" {
			t.Errorf("line %d: AuthorName = %q, want %q", i, bl.AuthorName, "Alice")
		}
		if bl.AuthorTime != time.Unix(1700000000, 0) {
			t.Errorf("line %d: AuthorTime = %v, want %v", i, bl.AuthorTime, time.Unix(1700000000, 0))
		}
		if bl.LineNumber != i+1 {
			t.Errorf("line %d: LineNumber = %d, want %d", i, bl.LineNumber, i+1)
		}
	}
}

func TestParseBlamePortcelain_MultiCommit(t *testing.T) {
	// Two different commits: line 1 by Alice, line 2 by Bob.
	data := []byte(
		"aaaa234567890abc1234567890abc123456789ab 1 1 1\n" +
			"author Alice\n" +
			"author-time 1700000000\n" +
			"committer Alice\n" +
			"committer-time 1700000000\n" +
			"summary first\n" +
			"filename f.go\n" +
			"\tline one\n" +
			"bbbb234567890abc1234567890abc123456789ab 2 2 1\n" +
			"author Bob\n" +
			"author-time 1700001000\n" +
			"committer Bob\n" +
			"committer-time 1700001000\n" +
			"summary second\n" +
			"filename f.go\n" +
			"\tline two\n",
	)

	result, err := ParseBlamePortcelain(data)
	if err != nil {
		t.Fatalf("ParseBlamePortcelain() error: %v", err)
	}
	if len(result.Lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(result.Lines))
	}
	if result.Lines[0].AuthorName != "Alice" {
		t.Errorf("line 0: AuthorName = %q, want %q", result.Lines[0].AuthorName, "Alice")
	}
	if result.Lines[1].AuthorName != "Bob" {
		t.Errorf("line 1: AuthorName = %q, want %q", result.Lines[1].AuthorName, "Bob")
	}
}

func TestParseBlamePortcelain_RepeatedHash(t *testing.T) {
	// Same commit hash repeated without full headers (second occurrence).
	data := []byte(
		"cccc234567890abc1234567890abc123456789ab 1 1 3\n" +
			"author Charlie\n" +
			"author-time 1700002000\n" +
			"committer Charlie\n" +
			"committer-time 1700002000\n" +
			"summary all lines\n" +
			"filename f.go\n" +
			"\tline one\n" +
			"cccc234567890abc1234567890abc123456789ab 2 2\n" +
			"\tline two\n" +
			"cccc234567890abc1234567890abc123456789ab 3 3\n" +
			"\tline three\n",
	)

	result, err := ParseBlamePortcelain(data)
	if err != nil {
		t.Fatalf("ParseBlamePortcelain() error: %v", err)
	}
	if len(result.Lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(result.Lines))
	}
	for i, bl := range result.Lines {
		if bl.AuthorName != "Charlie" {
			t.Errorf("line %d: AuthorName = %q, want %q", i, bl.AuthorName, "Charlie")
		}
	}
}

func TestParseBlamePortcelain_Empty(t *testing.T) {
	result, err := ParseBlamePortcelain([]byte{})
	if err != nil {
		t.Fatalf("ParseBlamePortcelain() error: %v", err)
	}
	if len(result.Lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(result.Lines))
	}
}

// initTestRepo creates a git repo with a committed file for integration tests.
func initTestRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test Author")

	for relPath, content := range files {
		absPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", relPath)
	}

	runGit(t, dir, "commit", "-m", "initial commit")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // test helper
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestBlameOneLine_Integration(t *testing.T) {
	dir := initTestRepo(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})

	author, ts, err := BlameOneLine(context.Background(), dir, "main.go", 1)
	if err != nil {
		t.Fatalf("BlameOneLine() error: %v", err)
	}
	if author != "Test Author" {
		t.Errorf("author = %q, want %q", author, "Test Author")
	}
	if ts.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestBlameFile_Integration(t *testing.T) {
	dir := initTestRepo(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})

	result, err := BlameFile(context.Background(), dir, "main.go")
	if err != nil {
		t.Fatalf("BlameFile() error: %v", err)
	}
	if len(result.Lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(result.Lines))
	}
	for i, bl := range result.Lines {
		if bl.AuthorName != "Test Author" {
			t.Errorf("line %d: AuthorName = %q, want %q", i, bl.AuthorName, "Test Author")
		}
		if bl.LineNumber != i+1 {
			t.Errorf("line %d: LineNumber = %d, want %d", i, bl.LineNumber, i+1)
		}
	}
}

func TestBlameFile_EmptyRepo(t *testing.T) {
	dir := t.TempDir()

	cmd := exec.Command("git", "init") //nolint:gosec // test
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	_, err := BlameFile(context.Background(), dir, "any.go")
	if err == nil {
		t.Error("expected error for empty repo with no HEAD")
	}
}

func TestBlameFile_NonexistentFile(t *testing.T) {
	dir := initTestRepo(t, map[string]string{
		"exists.go": "package main\n",
	})

	_, err := BlameFile(context.Background(), dir, "nonexistent.go")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestRun_InvalidDir(t *testing.T) {
	_, err := Run(context.Background(), "/nonexistent/directory", "status")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestIsHex(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"abc123", true},
		{"ABC123", true},
		{"0123456789abcdef", true},
		{"xyz", false},
		{"", false},
		{"abc123g", false},
	}
	for _, tt := range tests {
		if got := isHex(tt.input); got != tt.want {
			t.Errorf("isHex(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
