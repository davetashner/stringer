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
