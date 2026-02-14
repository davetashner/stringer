// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// initTestRepo creates a small isolated git repository in t.TempDir() suitable
// for integration tests. It contains Go source files with TODO/FIXME/HACK
// markers, a large function (for patterns collector), a go.mod (for context
// detection), and multiple commits by different authors (for gitlog/churn/
// lottery-risk analysis). Returns the repo directory path.
//
// Using this instead of repoRoot(t) avoids scanning the entire stringer
// repository (which triggers git blame on every file and takes minutes).
func initTestRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	// Resolve symlinks so paths match what stringer resolves internally
	// (e.g., macOS /var -> /private/var).
	var err error
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}

	// go.mod so project detection works (stringer context).
	writeTestFile(t, dir, "go.mod", "module testrepo\n\ngo 1.22\n")

	// internal/ directory triggers "Go Internal Packages" pattern detection.
	writeTestFile(t, dir, "internal/core/core.go", "package core\n")

	// --- Commit 1: initial files by author A ---
	writeTestFile(t, dir, "main.go", `package main

import "fmt"

func main() {
	// TODO: Add proper CLI argument parsing
	fmt.Println("hello world")

	// FIXME: This will panic on nil input
	process(nil)
}

// process handles the input data.
func process(data []byte) {
	// HACK: Temporary workaround until upstream fixes the API
	if data == nil {
		return
	}
	fmt.Println(string(data))
}
`)

	writeTestFile(t, dir, "util.go", `package main

import "strings"

// TODO: Refactor this utility function
func normalize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	return s
}
`)

	runGitCmd(t, dir, "init")
	runGitCmd(t, dir, "config", "user.email", "test@test.com")
	runGitCmd(t, dir, "config", "user.name", "Test")
	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "-c", "user.name=Alice", "-c", "user.email=alice@test.com",
		"commit", "-m", "Initial commit")

	// --- Commit 2: add a large function (patterns collector) by author B ---
	writeTestFile(t, dir, "handler.go", `package main

import "fmt"

// handleRequest is a long function that should trigger the patterns collector.
func handleRequest(method string, path string, body []byte) error {
	if method == "" {
		return fmt.Errorf("empty method")
	}
	if path == "" {
		return fmt.Errorf("empty path")
	}
	if body == nil {
		body = []byte{}
	}
	fmt.Println("method:", method)
	fmt.Println("path:", path)
	fmt.Println("body length:", len(body))
	if method == "GET" {
		fmt.Println("handling GET")
	}
	if method == "POST" {
		fmt.Println("handling POST")
	}
	if method == "PUT" {
		fmt.Println("handling PUT")
	}
	if method == "DELETE" {
		fmt.Println("handling DELETE")
	}
	if method == "PATCH" {
		fmt.Println("handling PATCH")
	}
	if method == "OPTIONS" {
		fmt.Println("handling OPTIONS")
	}
	if method == "HEAD" {
		fmt.Println("handling HEAD")
	}
	result := fmt.Sprintf("%s %s processed", method, path)
	fmt.Println(result)
	if len(body) > 0 {
		fmt.Println("body:", string(body))
	}
	if len(body) > 1024 {
		fmt.Println("large body")
	}
	if len(body) > 4096 {
		fmt.Println("very large body")
	}
	if path == "/health" {
		fmt.Println("health check ok")
	}
	if path == "/ready" {
		fmt.Println("ready check ok")
	}
	if path == "/metrics" {
		fmt.Println("metrics endpoint")
	}
	return nil
}
`)

	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "-c", "user.name=Bob", "-c", "user.email=bob@test.com",
		"commit", "-m", "Add request handler")

	// --- Commit 3: modify main.go (creates churn) by author A ---
	writeTestFile(t, dir, "main.go", `package main

import "fmt"

func main() {
	// TODO: Add proper CLI argument parsing
	fmt.Println("hello world v2")

	// FIXME: This will panic on nil input
	process(nil)
}

// process handles the input data.
func process(data []byte) {
	// HACK: Temporary workaround until upstream fixes the API
	if data == nil {
		return
	}
	fmt.Println(string(data))
}
`)

	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "-c", "user.name=Alice", "-c", "user.email=alice@test.com",
		"commit", "-m", "Update greeting")

	// --- Commit 4: add another file by author C (lottery risk: 3 authors) ---
	writeTestFile(t, dir, "config.go", `package main

// TODO: Load config from file
var defaultPort = 8080
`)

	runGitCmd(t, dir, "add", ".")
	runGitCmd(t, dir, "-c", "user.name=Charlie", "-c", "user.email=charlie@test.com",
		"commit", "-m", "Add config")

	return dir
}

// writeTestFile creates a file (and any necessary parent directories) under dir.
func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", parent, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
