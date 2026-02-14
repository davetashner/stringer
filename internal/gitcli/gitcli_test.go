// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package gitcli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAvailable(t *testing.T) {
	if err := Available(); err != nil {
		t.Fatalf("git should be available on PATH: %v", err)
	}
}

func TestExec_BasicCommand(t *testing.T) {
	out, err := Exec(context.Background(), ".", "--version")
	if err != nil {
		t.Fatalf("Exec(git --version) error: %v", err)
	}
	if out == "" {
		t.Error("expected non-empty git version output")
	}
}

func TestExec_InvalidCommand(t *testing.T) {
	_, err := Exec(context.Background(), ".", "not-a-real-command")
	if err == nil {
		t.Error("expected error for invalid git command")
	}
}

// initTestRepo creates a git repo with committed files and returns the directory path.
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

func TestBlameSingleLine(t *testing.T) {
	dir := initTestRepo(t, map[string]string{
		"hello.go": "package main\n\nfunc main() {}\n",
	})

	bl, err := BlameSingleLine(context.Background(), dir, "hello.go", 1)
	if err != nil {
		t.Fatalf("BlameSingleLine error: %v", err)
	}
	if bl.AuthorName != "Test Author" {
		t.Errorf("AuthorName = %q, want %q", bl.AuthorName, "Test Author")
	}
	if bl.AuthorTime.IsZero() {
		t.Error("AuthorTime should not be zero")
	}
	if time.Since(bl.AuthorTime) > 10*time.Minute {
		t.Errorf("AuthorTime too far in past: %v", bl.AuthorTime)
	}
}

func TestBlameSingleLine_NonexistentFile(t *testing.T) {
	dir := initTestRepo(t, map[string]string{
		"exists.go": "package main\n",
	})

	_, err := BlameSingleLine(context.Background(), dir, "nonexistent.go", 1)
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestBlameFile(t *testing.T) {
	dir := initTestRepo(t, map[string]string{
		"multi.go": "line one\nline two\nline three\n",
	})

	lines, err := BlameFile(context.Background(), dir, "multi.go")
	if err != nil {
		t.Fatalf("BlameFile error: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d blame lines, want 3", len(lines))
	}

	for i, bl := range lines {
		if bl.AuthorName != "Test Author" {
			t.Errorf("line %d: AuthorName = %q, want %q", i+1, bl.AuthorName, "Test Author")
		}
		if bl.AuthorTime.IsZero() {
			t.Errorf("line %d: AuthorTime should not be zero", i+1)
		}
	}
}

func TestBlameFile_MultipleAuthors(t *testing.T) {
	dir := initTestRepo(t, map[string]string{
		"collab.go": "alice line\n",
	})

	// Reconfigure author and add a second line.
	runGit(t, dir, "config", "user.name", "Bob")
	runGit(t, dir, "config", "user.email", "bob@example.com")

	// Append a line and commit as Bob.
	path := filepath.Join(dir, "collab.go")
	if err := os.WriteFile(path, []byte("alice line\nbob line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "collab.go")
	runGit(t, dir, "commit", "-m", "bob adds line")

	lines, err := BlameFile(context.Background(), dir, "collab.go")
	if err != nil {
		t.Fatalf("BlameFile error: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}

	if lines[0].AuthorName != "Test Author" {
		t.Errorf("line 1 author = %q, want %q", lines[0].AuthorName, "Test Author")
	}
	if lines[1].AuthorName != "Bob" {
		t.Errorf("line 2 author = %q, want %q", lines[1].AuthorName, "Bob")
	}
}

func TestParsePorcelainBlame(t *testing.T) {
	// Synthetic porcelain output with full and abbreviated blocks.
	porcelain := `abc123def456abc123def456abc123def456abcd 1 1 2
author Alice
author-mail <alice@example.com>
author-time 1700000000
author-tz +0000
committer Alice
committer-mail <alice@example.com>
committer-time 1700000000
committer-tz +0000
summary initial commit
filename hello.go
	package main
abc123def456abc123def456abc123def456abcd 2 2
	func main() {}
def789012345def789012345def789012345def0 3 3 1
author Bob
author-mail <bob@example.com>
author-time 1700100000
author-tz +0000
committer Bob
committer-mail <bob@example.com>
committer-time 1700100000
committer-tz +0000
summary second commit
filename hello.go
	// added by bob
`

	lines, err := parsePorcelainBlame(porcelain)
	if err != nil {
		t.Fatalf("parsePorcelainBlame error: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}

	// Line 1: full block from Alice.
	if lines[0].AuthorName != "Alice" {
		t.Errorf("line 1 author = %q, want %q", lines[0].AuthorName, "Alice")
	}
	if lines[0].AuthorTime.Unix() != 1700000000 {
		t.Errorf("line 1 author-time = %d, want %d", lines[0].AuthorTime.Unix(), 1700000000)
	}

	// Line 2: abbreviated block (same SHA as line 1), should reuse Alice's metadata.
	if lines[1].AuthorName != "Alice" {
		t.Errorf("line 2 author = %q, want %q (reused from abbreviated block)", lines[1].AuthorName, "Alice")
	}

	// Line 3: full block from Bob.
	if lines[2].AuthorName != "Bob" {
		t.Errorf("line 3 author = %q, want %q", lines[2].AuthorName, "Bob")
	}
	if lines[2].AuthorTime.Unix() != 1700100000 {
		t.Errorf("line 3 author-time = %d, want %d", lines[2].AuthorTime.Unix(), 1700100000)
	}
}

func TestIsHexSHA(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"abc123def456abc123def456abc123def456abcd", true},
		{"abcd", true},
		{"0000", true},
		{"abc", false},  // too short
		{"ABCD", false}, // uppercase
		{"ghij", false}, // non-hex
		{"", false},
	}
	for _, tt := range tests {
		if got := isHexSHA(tt.input); got != tt.want {
			t.Errorf("isHexSHA(%q) = %v, want %v", tt.input, got, tt.want)
		}
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

func TestLastCommitTime(t *testing.T) {
	dir := initTestRepo(t, map[string]string{
		"hello.go": "package main\n\nfunc main() {}\n",
	})

	ts, err := LastCommitTime(context.Background(), dir, "hello.go")
	if err != nil {
		t.Fatalf("LastCommitTime error: %v", err)
	}
	if ts.IsZero() {
		t.Error("expected non-zero timestamp")
	}
	if time.Since(ts) > 10*time.Minute {
		t.Errorf("timestamp too far in past: %v", ts)
	}
}

func TestLastCommitTime_Directory(t *testing.T) {
	dir := initTestRepo(t, map[string]string{
		"src/a.go": "package src\n",
		"src/b.go": "package src\n",
	})

	ts, err := LastCommitTime(context.Background(), dir, "src")
	if err != nil {
		t.Fatalf("LastCommitTime error: %v", err)
	}
	if ts.IsZero() {
		t.Error("expected non-zero timestamp for directory")
	}
}

func TestLastCommitTime_NoCommits(t *testing.T) {
	dir := initTestRepo(t, map[string]string{
		"hello.go": "package main\n",
	})

	ts, err := LastCommitTime(context.Background(), dir, "nonexistent-path")
	if err != nil {
		t.Fatalf("LastCommitTime error: %v", err)
	}
	if !ts.IsZero() {
		t.Errorf("expected zero timestamp for nonexistent path, got %v", ts)
	}
}

func TestBlameSingleLine_ContextTimeout(t *testing.T) {
	dir := initTestRepo(t, map[string]string{
		"hello.go": "package main\n",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := BlameSingleLine(ctx, dir, "hello.go", 1)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

// --- LogNumstat tests ---

func TestLogNumstat(t *testing.T) {
	dir := initTestRepo(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})

	commits, err := LogNumstat(context.Background(), dir, 100, "")
	if err != nil {
		t.Fatalf("LogNumstat error: %v", err)
	}
	if len(commits) == 0 {
		t.Fatal("expected at least one commit")
	}
	if commits[0].Author != "Test Author" {
		t.Errorf("Author = %q, want %q", commits[0].Author, "Test Author")
	}
	if commits[0].AuthorTime.IsZero() {
		t.Error("AuthorTime should not be zero")
	}
	if len(commits[0].Files) == 0 {
		t.Error("expected at least one file in numstat")
	}
}

func TestLogNumstat_MultipleCommits(t *testing.T) {
	dir := initTestRepo(t, map[string]string{
		"a.go": "package main\n",
	})

	absPath := filepath.Join(dir, "b.go")
	if err := os.WriteFile(absPath, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "b.go")
	runGit(t, dir, "commit", "-m", "add b.go")

	commits, err := LogNumstat(context.Background(), dir, 100, "")
	if err != nil {
		t.Fatalf("LogNumstat error: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}
}

func TestLogNumstat_MaxCount(t *testing.T) {
	dir := initTestRepo(t, map[string]string{
		"a.go": "package main\n",
	})

	absPath := filepath.Join(dir, "b.go")
	if err := os.WriteFile(absPath, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "b.go")
	runGit(t, dir, "commit", "-m", "add b.go")

	commits, err := LogNumstat(context.Background(), dir, 1, "")
	if err != nil {
		t.Fatalf("LogNumstat error: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("got %d commits, want 1 (max-count)", len(commits))
	}
}

func TestLogNumstat_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")

	commits, err := LogNumstat(context.Background(), dir, 100, "")
	// Empty repo — git log returns error or empty output.
	if err == nil && len(commits) > 0 {
		t.Error("expected no commits for empty repo")
	}
}

func TestParseNumstatLog(t *testing.T) {
	output := "abc123def456abc123def456abc123def456abcd|Alice|2025-01-15T10:00:00+00:00\n" +
		"3\t0\tmain.go\n" +
		"5\t2\tlib/util.go\n" +
		"\n" +
		"def789012345def789012345def789012345def0|Bob|2025-01-14T09:00:00+00:00\n" +
		"1\t1\tREADME.md\n" +
		"\n"

	commits, err := parseNumstatLog(output)
	if err != nil {
		t.Fatalf("parseNumstatLog error: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}

	if commits[0].Author != "Alice" {
		t.Errorf("commit 0 author = %q, want %q", commits[0].Author, "Alice")
	}
	if len(commits[0].Files) != 2 {
		t.Errorf("commit 0 files = %d, want 2", len(commits[0].Files))
	}
	if commits[0].Files[0] != "main.go" {
		t.Errorf("commit 0 file 0 = %q, want %q", commits[0].Files[0], "main.go")
	}

	if commits[1].Author != "Bob" {
		t.Errorf("commit 1 author = %q, want %q", commits[1].Author, "Bob")
	}
	if len(commits[1].Files) != 1 {
		t.Errorf("commit 1 files = %d, want 1", len(commits[1].Files))
	}
}

func TestParseNumstatLog_Rename(t *testing.T) {
	output := "abc123def456abc123def456abc123def456abcd|Alice|2025-01-15T10:00:00+00:00\n" +
		"0\t0\told.go => new.go\n" +
		"\n"

	commits, err := parseNumstatLog(output)
	if err != nil {
		t.Fatalf("parseNumstatLog error: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("got %d commits, want 1", len(commits))
	}
	if len(commits[0].Files) != 1 || commits[0].Files[0] != "new.go" {
		t.Errorf("rename: got file %q, want %q", commits[0].Files[0], "new.go")
	}
}

func TestParseNumstatLog_BraceRename(t *testing.T) {
	output := "abc123def456abc123def456abc123def456abcd|Alice|2025-01-15T10:00:00+00:00\n" +
		"0\t0\tsrc/{old.go => new.go}\n" +
		"\n"

	commits, err := parseNumstatLog(output)
	if err != nil {
		t.Fatalf("parseNumstatLog error: %v", err)
	}
	if len(commits[0].Files) != 1 || commits[0].Files[0] != "src/new.go" {
		t.Errorf("brace rename: got file %q, want %q", commits[0].Files[0], "src/new.go")
	}
}

func TestParseNumstatLog_Empty(t *testing.T) {
	commits, err := parseNumstatLog("")
	if err != nil {
		t.Fatalf("parseNumstatLog error: %v", err)
	}
	if len(commits) != 0 {
		t.Errorf("empty input should return 0 commits, got %d", len(commits))
	}
}

func TestExtractRenameDest(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"old.go => new.go", "new.go"},
		{"src/{old.go => new.go}", "src/new.go"},
		{"src/{sub/old.go => sub/new.go}/file", "src/sub/new.go/file"},
		{"no-rename.go", "no-rename.go"},
	}
	for _, tt := range tests {
		got := extractRenameDest(tt.input)
		if got != tt.want {
			t.Errorf("extractRenameDest(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
