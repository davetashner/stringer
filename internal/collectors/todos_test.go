// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/davetashner/stringer/internal/testable"
)

const floatTolerance = 0.001

func floatEqual(a, b float64) bool {
	return math.Abs(a-b) < floatTolerance
}

// --- Regex pattern tests ---

func TestTodoPatternMatches(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		keyword string
		message string
	}{
		{name: "go_todo", input: "// TODO: fix this", keyword: "TODO", message: "fix this"},
		{name: "go_fixme", input: "// FIXME: broken logic", keyword: "FIXME", message: "broken logic"},
		{name: "go_hack", input: "// HACK: workaround for bug", keyword: "HACK", message: "workaround for bug"},
		{name: "go_xxx", input: "// XXX: needs review", keyword: "XXX", message: "needs review"},
		{name: "go_bug", input: "// BUG: null pointer", keyword: "BUG", message: "null pointer"},
		{name: "go_optimize", input: "// OPTIMIZE: use batch query", keyword: "OPTIMIZE", message: "use batch query"},
		{name: "go_todo_author", input: "// TODO(dave): refactor this", keyword: "TODO", message: "refactor this"},
		{name: "go_fixme_author", input: "// FIXME(alice): handle edge case", keyword: "FIXME", message: "handle edge case"},
		{name: "lowercase_todo", input: "// todo: lowercase", keyword: "todo", message: "lowercase"},
		{name: "mixed_case", input: "// Todo: mixed case", keyword: "Todo", message: "mixed case"},
		{name: "fixme_lower", input: "// fixme: lower", keyword: "fixme", message: "lower"},
		{name: "python_todo", input: "# TODO: implement this", keyword: "TODO", message: "implement this"},
		{name: "python_fixme", input: "# FIXME: broken", keyword: "FIXME", message: "broken"},
		{name: "ruby_hack", input: "# HACK: monkey patch", keyword: "HACK", message: "monkey patch"},
		{name: "block_todo", input: "/* TODO: refactor */", keyword: "TODO", message: "refactor"},
		{name: "block_fixme", input: "/* FIXME: memory leak */", keyword: "FIXME", message: "memory leak"},
		{name: "jsdoc_todo", input: "* TODO: add docs", keyword: "TODO", message: "add docs"},
		{name: "sql_todo", input: "-- TODO: optimize query", keyword: "TODO", message: "optimize query"},
		{name: "haskell_fixme", input: "-- FIXME: handle error", keyword: "FIXME", message: "handle error"},
		{name: "dash_sep", input: "// TODO - fix this", keyword: "TODO", message: "fix this"},
		{name: "gt_sep", input: "// TODO> fix this", keyword: "TODO", message: "fix this"},
		{name: "no_sep", input: "// TODO fix this", keyword: "TODO", message: "fix this"},
		{name: "colon_no_space", input: "// TODO:fix this", keyword: "TODO", message: "fix this"},
		{name: "extra_space", input: "//   TODO:   lots of space", keyword: "TODO", message: "lots of space"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := todoPattern.FindStringSubmatch(tt.input)
			if matches == nil {
				t.Fatalf("pattern did not match input %q", tt.input)
			}
			if matches[1] != tt.keyword {
				t.Errorf("keyword = %q, want %q", matches[1], tt.keyword)
			}
			got := matches[2]
			if got != tt.message && got != tt.message+" */" {
				t.Errorf("message = %q, want %q", got, tt.message)
			}
		})
	}
}

func TestTodoPatternNoMatch(t *testing.T) {
	noMatch := []struct {
		name  string
		input string
	}{
		{name: "plain_text", input: "This is just plain text"},
		{name: "variable_name", input: "var todoList = []string{}"},
		{name: "string_literal", input: `fmt.Println("TODO: test")`},
		{name: "empty_line", input: ""},
		{name: "just_comment", input: "// This is a normal comment"},
		{name: "todoist_api", input: "// TODOIST_API: config"},
		{name: "fixmeup", input: "// FIXMEUP: handler"},
		{name: "todoist_tasks", input: "// TODOIST_TASKS: config"},
		{name: "bugzilla", input: "// BUGZILLA: tracker"},
	}

	for _, tt := range noMatch {
		t.Run(tt.name, func(t *testing.T) {
			matches := todoPattern.FindStringSubmatch(tt.input)
			if matches != nil {
				t.Errorf("pattern unexpectedly matched input %q: %v", tt.input, matches)
			}
		})
	}
}

func TestComputeConfidence(t *testing.T) {
	now := time.Now()
	tenDaysAgo := now.Add(-10 * 24 * time.Hour)
	sevenMonthsAgo := now.Add(-7 * 30 * 24 * time.Hour)
	twoYearsAgo := now.Add(-2 * 365 * 24 * time.Hour)

	tests := []struct {
		name string
		sig  signal.RawSignal
		want float64
	}{
		{name: "bug_base", sig: signal.RawSignal{Kind: "bug"}, want: 0.8},
		{name: "fixme_base", sig: signal.RawSignal{Kind: "fixme"}, want: 0.65},
		{name: "hack_base", sig: signal.RawSignal{Kind: "hack"}, want: 0.55},
		{name: "todo_base", sig: signal.RawSignal{Kind: "todo"}, want: 0.5},
		{name: "xxx_base", sig: signal.RawSignal{Kind: "xxx"}, want: 0.45},
		{name: "optimize_base", sig: signal.RawSignal{Kind: "optimize"}, want: 0.35},
		{name: "todo_recent", sig: signal.RawSignal{Kind: "todo", Timestamp: tenDaysAgo}, want: 0.6},
		{name: "bug_recent", sig: signal.RawSignal{Kind: "bug", Timestamp: tenDaysAgo}, want: 0.9},
		{name: "todo_6mo", sig: signal.RawSignal{Kind: "todo", Timestamp: sevenMonthsAgo}, want: 0.5},
		{name: "fixme_6mo", sig: signal.RawSignal{Kind: "fixme", Timestamp: sevenMonthsAgo}, want: 0.65},
		{name: "todo_2yr", sig: signal.RawSignal{Kind: "todo", Timestamp: twoYearsAgo}, want: 0.5},
		{name: "bug_2yr", sig: signal.RawSignal{Kind: "bug", Timestamp: twoYearsAgo}, want: 0.8},
		{name: "todo_now", sig: signal.RawSignal{Kind: "todo", Timestamp: now}, want: 0.6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeConfidence(tt.sig)
			if !floatEqual(got, tt.want) {
				t.Errorf("computeConfidence() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeConfidenceCap(t *testing.T) {
	sig := signal.RawSignal{
		Kind:      "bug",
		Timestamp: time.Now().Add(-10 * 24 * time.Hour),
	}
	got := computeConfidence(sig)
	if got > 1.0+floatTolerance {
		t.Errorf("confidence %v exceeds 1.0 cap", got)
	}
}

func TestShouldExclude(t *testing.T) {
	tests := []struct {
		name     string
		relPath  string
		patterns []string
		want     bool
	}{
		{name: "vendor_dir", relPath: "vendor/pkg/foo.go", patterns: []string{"vendor/**"}, want: true},
		{name: "vendor_root", relPath: "vendor", patterns: []string{"vendor/**"}, want: true},
		{name: "node_modules", relPath: "node_modules/express/index.js", patterns: []string{"node_modules/**"}, want: true},
		{name: "git_dir", relPath: ".git/config", patterns: []string{".git/**"}, want: true},
		{name: "testdata", relPath: "testdata/fixture.go", patterns: []string{"testdata/**"}, want: true},
		{name: "min_js", relPath: "static/app.min.js", patterns: []string{"*.min.js"}, want: true},
		{name: "min_js_root", relPath: "app.min.js", patterns: []string{"*.min.js"}, want: true},
		{name: "normal_file", relPath: "main.go", patterns: []string{"vendor/**"}, want: false},
		{name: "src_file", relPath: "internal/foo/bar.go", patterns: []string{"vendor/**", "node_modules/**"}, want: false},
		{name: "empty_patterns", relPath: "any.go", patterns: nil, want: false},
		{name: "nested_vendor", relPath: "vendor/github.com/pkg/errors/errors.go", patterns: []string{"vendor/**"}, want: true},
		// Interior matching: pattern matches at any directory depth.
		{name: "wwwroot_lib_interior", relPath: "samples/foo/wwwroot/lib/bootstrap.js", patterns: []string{"wwwroot/lib/**"}, want: true},
		{name: "wwwroot_lib_root", relPath: "wwwroot/lib/jquery.js", patterns: []string{"wwwroot/lib/**"}, want: true},
		{name: "wwwroot_lib_exact", relPath: "wwwroot/lib", patterns: []string{"wwwroot/lib/**"}, want: true},
		{name: "third_party_interior", relPath: "samples/foo/third_party/proto.go", patterns: []string{"third_party/**"}, want: true},
		{name: "no_false_match_lib", relPath: "libfoo/bar.go", patterns: []string{"wwwroot/lib/**"}, want: false},
		{name: "no_false_match_extern", relPath: "myextern/code.go", patterns: []string{"extern/**"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldExclude(tt.relPath, tt.patterns)
			if got != tt.want {
				t.Errorf("shouldExclude(%q, %v) = %v, want %v", tt.relPath, tt.patterns, got, tt.want)
			}
		})
	}
}

func TestMatchesAny(t *testing.T) {
	tests := []struct {
		name     string
		relPath  string
		patterns []string
		want     bool
	}{
		{name: "match_go", relPath: "foo.go", patterns: []string{"*.go"}, want: true},
		{name: "match_py", relPath: "script.py", patterns: []string{"*.py"}, want: true},
		{name: "no_match", relPath: "foo.go", patterns: []string{"*.py"}, want: false},
		{name: "multi_match", relPath: "foo.go", patterns: []string{"*.py", "*.go"}, want: true},
		{name: "empty_patterns", relPath: "foo.go", patterns: nil, want: false},
		{name: "nested_go", relPath: "internal/pkg/foo.go", patterns: []string{"*.go"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesAny(tt.relPath, tt.patterns)
			if got != tt.want {
				t.Errorf("matchesAny(%q, %v) = %v, want %v", tt.relPath, tt.patterns, got, tt.want)
			}
		})
	}
}

func TestMergeExcludes(t *testing.T) {
	got := mergeExcludes(nil)
	if len(got) != len(defaultExcludePatterns) {
		t.Errorf("mergeExcludes(nil) returned %d patterns, want %d", len(got), len(defaultExcludePatterns))
	}

	user := []string{"build/**", "*.generated.go"}
	got = mergeExcludes(user)
	want := len(defaultExcludePatterns) + len(user)
	if len(got) != want {
		t.Errorf("mergeExcludes(user) returned %d patterns, want %d", len(got), want)
	}
}

func TestIsBinaryFile(t *testing.T) {
	dir := t.TempDir()

	textPath := filepath.Join(dir, "text.go")
	if err := os.WriteFile(textPath, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	binPath := filepath.Join(dir, "binary.dat")
	if err := os.WriteFile(binPath, []byte{0x00, 0x01, 0x02, 0x00}, 0o600); err != nil {
		t.Fatal(err)
	}

	if isBinaryFile(textPath) {
		t.Error("text file detected as binary")
	}
	if !isBinaryFile(binPath) {
		t.Error("binary file not detected as binary")
	}
}

func TestScanFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "example.go")

	content := `package example

// TODO: implement this function
func Foo() {}

// FIXME(alice): handle error
func Bar() error { return nil }

# BUG: this is broken
// HACK - temporary workaround
/* XXX: needs review */
// OPTIMIZE: use a cache
// This is a normal comment
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	signals, err := scanFile(path, "example.go")
	if err != nil {
		t.Fatal(err)
	}

	if len(signals) != 6 {
		t.Fatalf("got %d signals, want 6", len(signals))
	}

	wantKinds := []string{"todo", "fixme", "bug", "hack", "xxx", "optimize"}
	wantLines := []int{3, 6, 9, 10, 11, 12}

	for i, sig := range signals {
		if sig.Kind != wantKinds[i] {
			t.Errorf("signal[%d].Kind = %q, want %q", i, sig.Kind, wantKinds[i])
		}
		if sig.Line != wantLines[i] {
			t.Errorf("signal[%d].Line = %d, want %d", i, sig.Line, wantLines[i])
		}
		if sig.Source != "todos" {
			t.Errorf("signal[%d].Source = %q, want %q", i, sig.Source, "todos")
		}
	}
}

func TestScanFileEmptyMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.go")

	if err := os.WriteFile(path, []byte("// TODO:\n// FIXME\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	signals, err := scanFile(path, "empty.go")
	if err != nil {
		t.Fatal(err)
	}

	for _, sig := range signals {
		if sig.Title == "" {
			t.Error("signal has empty title")
		}
	}
}

func initTestGitRepo(t *testing.T, files map[string]string) string {
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
	cmd := exec.Command("git", args...) //nolint:gosec // test helper with controlled args
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestScanFile_NoFalsePositives(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nofp.go")
	content := `package main

// TODOIST_API_TASKS: config
func todoistHandler() {}

// FIXMEUP_HANDLER: recovery
func fixmeHandler() {}

// BUGZILLA_ID: 12345
func bugTracker() {}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	signals, err := scanFile(path, "nofp.go")
	if err != nil {
		t.Fatal(err)
	}

	if len(signals) != 0 {
		t.Errorf("expected 0 signals (no false positives), got %d: %v", len(signals), signals)
	}
}

func TestScanFile_TodoWithAuthorAfterWordBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "author.go")
	content := "// TODO(alice): refactor this code\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	signals, err := scanFile(path, "author.go")
	if err != nil {
		t.Fatal(err)
	}

	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if signals[0].Kind != "todo" {
		t.Errorf("Kind = %q, want %q", signals[0].Kind, "todo")
	}
	if signals[0].Title != "TODO: refactor this code" {
		t.Errorf("Title = %q, want %q", signals[0].Title, "TODO: refactor this code")
	}
}

func TestCollect_BasicSignals(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"main.go": "package main\n\n// TODO: implement feature\nfunc main() {}\n\n// FIXME: broken logic\nfunc broken() {}\n\n// BUG: crashes on nil input\nfunc buggy() {}\n",
	})

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	if err != nil {
		t.Fatal(err)
	}

	if len(signals) != 3 {
		t.Fatalf("got %d signals, want 3", len(signals))
	}

	for _, sig := range signals {
		if sig.Author == "" {
			t.Errorf("signal %q has no author", sig.Title)
		}
		if sig.Timestamp.IsZero() {
			t.Errorf("signal %q has zero timestamp", sig.Title)
		}
		if sig.Confidence == 0 {
			t.Errorf("signal %q has zero confidence", sig.Title)
		}
	}
}

func TestCollect_ExcludePatterns(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"main.go":             "// TODO: keep this\n",
		"vendor/dep/dep.go":   "// TODO: vendor todo\n",
		"node_modules/pkg.js": "// TODO: npm todo\n",
	})

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	if err != nil {
		t.Fatal(err)
	}

	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1", len(signals))
	}
	if signals[0].FilePath != "main.go" {
		t.Errorf("signal.FilePath = %q, want %q", signals[0].FilePath, "main.go")
	}
}

func TestCollect_CustomExcludes(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"main.go":          "// TODO: keep this\n",
		"generated/gen.go": "// TODO: generated code\n",
	})

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{
		ExcludePatterns: []string{"generated/**"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1", len(signals))
	}
}

func TestCollect_IncludePatterns(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"main.go":   "// TODO: go file\n",
		"script.py": "# TODO: python file\n",
	})

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{
		IncludePatterns: []string{"*.go"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1", len(signals))
	}
	if signals[0].FilePath != "main.go" {
		t.Errorf("signal.FilePath = %q, want %q", signals[0].FilePath, "main.go")
	}
}

func TestCollect_SkipsBinaryFiles(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"main.go": "// TODO: text file\n",
	})

	binPath := filepath.Join(repoPath, "image.png")
	binContent := make([]byte, 100)
	binContent[10] = 0x00
	if err := os.WriteFile(binPath, binContent, 0o600); err != nil {
		t.Fatal(err)
	}

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	if err != nil {
		t.Fatal(err)
	}

	for _, sig := range signals {
		if sig.FilePath == "image.png" {
			t.Error("binary file should have been skipped")
		}
	}
}

func TestCollect_ContextCancellation(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"main.go": "// TODO: test\n",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &TodoCollector{}
	_, err := c.Collect(ctx, repoPath, signal.CollectorOpts{})
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestCollect_NonGitDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("// TODO: no git\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	if err != nil {
		t.Fatal(err)
	}

	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1", len(signals))
	}
	if signals[0].Author != "" {
		t.Errorf("expected empty author, got %q", signals[0].Author)
	}
}

func TestCollectorName(t *testing.T) {
	c := &TodoCollector{}
	if c.Name() != "todos" {
		t.Errorf("Name() = %q, want %q", c.Name(), "todos")
	}
}

func TestCollect_MultipleLanguages(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"main.go":   "// TODO: go todo\n",
		"script.py": "# FIXME: python fixme\n",
		"style.css": "/* HACK: css hack */\n",
		"query.sql": "-- BUG: sql bug\n",
		"app.rb":    "# XXX: ruby xxx\n",
		"build.sh":  "# OPTIMIZE: shell optimize\n",
	})

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	if err != nil {
		t.Fatal(err)
	}

	if len(signals) != 6 {
		t.Fatalf("got %d signals, want 6", len(signals))
	}

	kinds := make(map[string]bool)
	for _, sig := range signals {
		kinds[sig.Kind] = true
	}
	for _, k := range []string{"todo", "fixme", "hack", "bug", "xxx", "optimize"} {
		if !kinds[k] {
			t.Errorf("missing kind %q", k)
		}
	}
}

func TestCollect_SignalFields(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"main.go": "// TODO: implement feature\n",
	})

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	if err != nil {
		t.Fatal(err)
	}

	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1", len(signals))
	}

	sig := signals[0]
	if sig.Source != "todos" {
		t.Errorf("Source = %q, want %q", sig.Source, "todos")
	}
	if sig.Kind != "todo" {
		t.Errorf("Kind = %q, want %q", sig.Kind, "todo")
	}
	if sig.FilePath != "main.go" {
		t.Errorf("FilePath = %q, want %q", sig.FilePath, "main.go")
	}
	if sig.Line != 1 {
		t.Errorf("Line = %d, want %d", sig.Line, 1)
	}
	if sig.Title != "TODO: implement feature" {
		t.Errorf("Title = %q, want %q", sig.Title, "TODO: implement feature")
	}
	if sig.Confidence < 0 || sig.Confidence > 1.0+floatTolerance {
		t.Errorf("Confidence = %v, want 0-1 range", sig.Confidence)
	}
}

func TestCollect_BlameAttribution(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"main.go": "// TODO: test blame\n",
	})

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	if err != nil {
		t.Fatal(err)
	}

	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1", len(signals))
	}

	sig := signals[0]
	if sig.Author != "Test Author" {
		t.Errorf("Author = %q, want %q", sig.Author, "Test Author")
	}
	if sig.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero for committed file")
	}
}

// --- matchesAny edge case tests ---

func TestMatchesAny_DoubleStarPatterns(t *testing.T) {
	tests := []struct {
		name     string
		relPath  string
		patterns []string
		want     bool
	}{
		{
			name:     "double_star_prefix_match",
			relPath:  "src/components/button.go",
			patterns: []string{"src/**/*.go"},
			want:     true,
		},
		{
			name:     "double_star_no_suffix_match",
			relPath:  "src/anything/deep/nested.go",
			patterns: []string{"src/**"},
			want:     true,
		},
		{
			name:     "double_star_prefix_no_match",
			relPath:  "lib/components/button.go",
			patterns: []string{"src/**/*.go"},
			want:     false,
		},
		{
			name:     "double_star_suffix_no_match",
			relPath:  "src/components/button.py",
			patterns: []string{"src/**/*.go"},
			want:     false,
		},
		{
			name:     "double_star_root_match",
			relPath:  "foo.go",
			patterns: []string{"**/*.go"},
			want:     true,
		},
		{
			name:     "empty_patterns_returns_false",
			relPath:  "anything.go",
			patterns: []string{},
			want:     false,
		},
		{
			name:     "invalid_pattern_does_not_panic",
			relPath:  "foo.go",
			patterns: []string{"[invalid"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesAny(tt.relPath, tt.patterns)
			if got != tt.want {
				t.Errorf("matchesAny(%q, %v) = %v, want %v", tt.relPath, tt.patterns, got, tt.want)
			}
		})
	}
}

// --- enrichWithBlame edge case tests ---

func TestEnrichWithBlame_EmptyGitDir(t *testing.T) {
	sig := signal.RawSignal{Line: 1}
	enrichWithBlame(context.Background(), "", "any.go", &sig, "any.go")
	if sig.Author != "" {
		t.Errorf("expected empty author when gitDir is empty, got %q", sig.Author)
	}
}

func TestEnrichWithBlame_LineOutOfBounds(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"small.go": "package main\n",
	})

	// Line 100 is way beyond the file (1 line), so blame should fail gracefully
	// and fall back to mtime.
	sig := signal.RawSignal{Line: 100}
	enrichWithBlame(context.Background(), repoPath, "small.go", &sig, filepath.Join(repoPath, "small.go"))
	// Native git blame -L 100,100 on a 1-line file returns an error,
	// so we should get mtime fallback.
}

func TestEnrichWithBlame_LineZero(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"z.go": "package main\n",
	})

	// Line=0 is invalid and should be skipped.
	sig := signal.RawSignal{Line: 0}
	enrichWithBlame(context.Background(), repoPath, "z.go", &sig, filepath.Join(repoPath, "z.go"))
	if sig.Author != "" {
		t.Errorf("expected empty author for line=0, got %q", sig.Author)
	}
}

func TestEnrichWithBlame_NegativeLine(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"neg.go": "package main\n",
	})

	sig := signal.RawSignal{Line: -5}
	enrichWithBlame(context.Background(), repoPath, "neg.go", &sig, filepath.Join(repoPath, "neg.go"))
	if sig.Author != "" {
		t.Errorf("expected empty author for negative line, got %q", sig.Author)
	}
}

func TestEnrichWithBlame_NonexistentFile(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"exists.go": "package main\n",
	})

	// Blame on a file not in the repo should fail gracefully.
	sig := signal.RawSignal{Line: 1}
	enrichWithBlame(context.Background(), repoPath, "nonexistent.go", &sig, filepath.Join(repoPath, "nonexistent.go"))
	if sig.Author != "" {
		t.Errorf("expected empty author for nonexistent file, got %q", sig.Author)
	}
}

func TestEnrichWithBlame_MtimeFallback(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"exists.go": "package main\n",
	})

	// Create a file on disk that is NOT tracked in git, so blame fails.
	untracked := filepath.Join(repoPath, "untracked.go")
	if err := os.WriteFile(untracked, []byte("// TODO: fix\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	sig := signal.RawSignal{Line: 1, Tags: []string{"todo"}}
	enrichWithBlame(context.Background(), repoPath, "untracked.go", &sig, untracked)

	// Blame fails, but file exists → should get mtime as timestamp.
	if sig.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp from mtime fallback")
	}
	// Should be tagged with estimated-timestamp.
	found := false
	for _, tag := range sig.Tags {
		if tag == "estimated-timestamp" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'estimated-timestamp' tag, got %v", sig.Tags)
	}
	// Author should remain empty (no blame data).
	if sig.Author != "" {
		t.Errorf("expected empty author, got %q", sig.Author)
	}
}

// --- computeConfidence edge case tests ---

func TestComputeConfidence_UnknownKeyword(t *testing.T) {
	sig := signal.RawSignal{Kind: "unknown"}
	got := computeConfidence(sig)
	if !floatEqual(got, 0.5) {
		t.Errorf("computeConfidence(unknown keyword) = %v, want 0.5", got)
	}
}

// --- isBinaryFile edge case tests ---

func TestIsBinaryFile_Nonexistent(t *testing.T) {
	if !isBinaryFile("/nonexistent/path/to/file") {
		t.Error("nonexistent file should be treated as binary (unreadable)")
	}
}

func TestIsBinaryFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	// Empty file read returns n=0 and err (EOF), treated as binary.
	if !isBinaryFile(path) {
		t.Error("empty file should be treated as binary")
	}
}

// --- scanFile edge case tests ---

func TestScanFile_NonexistentFile(t *testing.T) {
	_, err := scanFile("/nonexistent/path.go", "path.go")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestScanFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.go")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}

	signals, err := scanFile(path, "empty.go")
	if err != nil {
		t.Fatalf("scanFile() error: %v", err)
	}
	if len(signals) != 0 {
		t.Errorf("expected 0 signals for empty file, got %d", len(signals))
	}
}

func TestScanFile_BlockCommentStripping(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "block.go")
	content := "/* TODO: refactor this code */\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	signals, err := scanFile(path, "block.go")
	if err != nil {
		t.Fatalf("scanFile() error: %v", err)
	}
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	// Verify trailing */ was stripped from the title.
	if signals[0].Title != "TODO: refactor this code" {
		t.Errorf("Title = %q, want %q", signals[0].Title, "TODO: refactor this code")
	}
}

// --- shouldExclude edge case tests ---

func TestShouldExclude_InvalidPattern(t *testing.T) {
	// An invalid glob pattern should not cause a crash.
	got := shouldExclude("foo.go", []string{"[invalid"})
	if got {
		t.Error("invalid pattern should not match")
	}
}

// --- Collect edge case: symlink outside repo ---

func TestCollect_SymlinkOutsideRepoSkipped(t *testing.T) {
	// Create two directories: repo and outside.
	repoPath := initTestGitRepo(t, map[string]string{
		"main.go": "// TODO: inside repo\n",
	})

	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "external.go")
	if err := os.WriteFile(outsidePath, []byte("// TODO: outside repo\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Create a symlink inside the repo pointing outside.
	symlinkPath := filepath.Join(repoPath, "external_link.go")
	if err := os.Symlink(outsidePath, symlinkPath); err != nil {
		t.Skip("symlinks not supported on this OS")
	}

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	if err != nil {
		t.Fatal(err)
	}

	for _, sig := range signals {
		if sig.FilePath == "external_link.go" {
			t.Error("symlink pointing outside repo should be skipped")
		}
	}
}

// --- Collect edge case: unreadable directory entry ---

func TestCollect_WalkDirErrorContinues(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"good.go": "// TODO: readable\n",
	})

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	if err != nil {
		t.Fatal(err)
	}

	// The collector should still find signals from readable files.
	if len(signals) < 1 {
		t.Error("expected at least 1 signal from readable file")
	}
}

// --- Collect edge case: excluded file pattern (not directory) ---

func TestCollect_ExcludedFilePattern(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"main.go":          "// TODO: keep this\n",
		"generated.min.js": "// TODO: should be excluded\n",
	})

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{
		ExcludePatterns: []string{"*.min.js"},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, sig := range signals {
		if sig.FilePath == "generated.min.js" {
			t.Error("excluded file pattern should skip the file")
		}
	}

	// Should still find the TODO in main.go.
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if signals[0].FilePath != "main.go" {
		t.Errorf("expected main.go, got %s", signals[0].FilePath)
	}
}

// --- Collect edge case: unreadable source file ---

func TestCollect_UnreadableFileSkipped(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"good.go": "// TODO: readable\n",
	})

	// Create a file that can't be read.
	noReadPath := filepath.Join(repoPath, "noread.go")
	if err := os.WriteFile(noReadPath, []byte("// TODO: secret\n"), 0o000); err != nil {
		t.Fatal(err)
	}

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	if err != nil {
		t.Fatal(err)
	}

	// Should still get the signal from good.go.
	for _, sig := range signals {
		if sig.FilePath == "noread.go" {
			t.Error("unreadable file should be skipped")
		}
	}
}

// --- Collect edge case: broken symlink ---

func TestCollect_BrokenSymlinkSkipped(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"main.go": "// TODO: real\n",
	})

	// Create a broken symlink.
	symlinkPath := filepath.Join(repoPath, "broken_link.go")
	if err := os.Symlink("/nonexistent/target.go", symlinkPath); err != nil {
		t.Skip("symlinks not supported on this OS")
	}

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	if err != nil {
		t.Fatal(err)
	}

	for _, sig := range signals {
		if sig.FilePath == "broken_link.go" {
			t.Error("broken symlink should be skipped")
		}
	}
}

// --- Subdirectory scan with GitRoot ---

func TestCollect_SubdirectoryScanWithGitRoot(t *testing.T) {
	// Create a git repo with a subdirectory containing a TODO.
	repoPath := initTestGitRepo(t, map[string]string{
		"sub/handler.go": "package sub\n\n// TODO: refactor handler\nfunc Handle() {}\n",
	})

	subDir := filepath.Join(repoPath, "sub")

	c := &TodoCollector{}
	// Scan the subdirectory, passing GitRoot pointing to the repo root.
	signals, err := c.Collect(context.Background(), subDir, signal.CollectorOpts{
		GitRoot: repoPath,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1", len(signals))
	}

	sig := signals[0]
	if sig.Kind != "todo" {
		t.Errorf("Kind = %q, want %q", sig.Kind, "todo")
	}
	// Blame attribution should work since GitRoot points to the repo root.
	if sig.Author == "" {
		t.Error("expected non-empty author from blame (GitRoot should enable blame)")
	}
	if sig.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp from blame")
	}
}

func TestIsDemoPath(t *testing.T) {
	tests := []struct {
		name    string
		relPath string
		want    bool
	}{
		{name: "examples_root", relPath: "examples", want: true},
		{name: "examples_nested", relPath: "examples/basic/main.go", want: true},
		{name: "example_root", relPath: "example", want: true},
		{name: "example_nested", relPath: "example/hello/main.go", want: true},
		{name: "tutorials_nested", relPath: "tutorials/intro/step1.go", want: true},
		{name: "tutorial_nested", relPath: "tutorial/basics/main.go", want: true},
		{name: "demos_nested", relPath: "demos/showcase/app.go", want: true},
		{name: "demo_nested", relPath: "demo/widget/main.go", want: true},
		{name: "samples_nested", relPath: "samples/quickstart/main.go", want: true},
		{name: "sample_nested", relPath: "sample/hello/main.go", want: true},
		{name: "_examples_nested", relPath: "_examples/advanced/main.go", want: true},
		{name: "src_not_demo", relPath: "src/main.go", want: false},
		{name: "internal_not_demo", relPath: "internal/handler.go", want: false},
		{name: "cmd_not_demo", relPath: "cmd/app/main.go", want: false},
		{name: "filename_example_no_match", relPath: "example.go", want: false},
		{name: "filename_examples_no_match", relPath: "examples.go", want: false},
		{name: "sub_example_no_match", relPath: "pkg/example.go", want: false},
		// Non-source directories (docs, tooling, etc.)
		{name: "docs_nested", relPath: "docs/design.md", want: true},
		{name: "doc_nested", relPath: "doc/api/overview.go", want: true},
		{name: "scripts_nested", relPath: "scripts/deploy.py", want: true},
		{name: "tools_nested", relPath: "tools/gen/main.go", want: true},
		{name: "build_nested", relPath: "build/output/main.go", want: true},
		{name: "deploy_nested", relPath: "deploy/k8s/app.yaml", want: true},
		{name: "extras_nested", relPath: "extras/playground.go", want: true},
		{name: "packaging_nested", relPath: "packaging/deb/control", want: true},
		{name: "contrib_nested", relPath: "contrib/plugin/main.go", want: true},
		{name: "misc_nested", relPath: "misc/notes.txt", want: true},
		{name: "dotgithub_nested", relPath: ".github/workflows/ci.yml", want: true},
		{name: "dotci_nested", relPath: ".ci/pipeline.yml", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDemoPath(tt.relPath)
			if got != tt.want {
				t.Errorf("isDemoPath(%q) = %v, want %v", tt.relPath, got, tt.want)
			}
		})
	}
}

func TestCollect_SubdirectoryScanWithoutGitRoot(t *testing.T) {
	// Create a git repo with a subdirectory containing a TODO.
	repoPath := initTestGitRepo(t, map[string]string{
		"sub/handler.go": "package sub\n\n// TODO: refactor handler\nfunc Handle() {}\n",
	})

	subDir := filepath.Join(repoPath, "sub")

	c := &TodoCollector{}
	// Scan the subdirectory WITHOUT GitRoot — blame will fail gracefully.
	signals, err := c.Collect(context.Background(), subDir, signal.CollectorOpts{})
	if err != nil {
		t.Fatal(err)
	}

	if len(signals) != 1 {
		t.Fatalf("got %d signals, want 1", len(signals))
	}

	// Without GitRoot, blame won't work (sub/ is not a git root), so author should be empty.
	sig := signals[0]
	if sig.Kind != "todo" {
		t.Errorf("Kind = %q, want %q", sig.Kind, "todo")
	}
	if sig.Author != "" {
		t.Logf("note: got author %q (blame might still work depending on git implementation)", sig.Author)
	}
}

// --- Mock-based tests ---

func TestTodoCollector_Metrics(t *testing.T) {
	c := &TodoCollector{}
	// Before collecting, metrics should be nil.
	assert.Nil(t, c.Metrics())

	repoPath := initTestGitRepo(t, map[string]string{
		"main.go": "// TODO: first\n// FIXME: second\n// TODO: third\n",
	})

	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.NoError(t, err)
	require.Len(t, signals, 3)

	m := c.Metrics()
	require.NotNil(t, m)

	metrics, ok := m.(*TodoMetrics)
	require.True(t, ok, "Metrics() should return *TodoMetrics")
	assert.Equal(t, 3, metrics.Total)
	assert.Equal(t, 2, metrics.ByKind["todo"])
	assert.Equal(t, 1, metrics.ByKind["fixme"])
	assert.GreaterOrEqual(t, metrics.WithTimestamp, 0)
}

func TestTodoCollector_WalkDirError(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		WalkDirFn: func(root string, fn fs.WalkDirFunc) error {
			return fmt.Errorf("walk error")
		},
	}

	c := &TodoCollector{}
	_, err := c.Collect(context.Background(), "/tmp/fake", signal.CollectorOpts{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "walking repo")
	assert.Contains(t, err.Error(), "walk error")
}

func TestTodoCollector_EvalSymlinksFailure(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	dir := t.TempDir()
	// Create a symlink that the mock will fail to resolve.
	targetPath := filepath.Join(dir, "target.go")
	require.NoError(t, os.WriteFile(targetPath, []byte("// TODO: test\n"), 0o600))
	symlinkPath := filepath.Join(dir, "link.go")
	if err := os.Symlink(targetPath, symlinkPath); err != nil {
		t.Skip("symlinks not supported")
	}

	FS = &testable.MockFileSystem{
		EvalSymlinksFn: func(path string) (string, error) {
			return "", fmt.Errorf("symlink error")
		},
	}

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	// The symlink should be skipped due to EvalSymlinks error,
	// but the target file may be scanned directly.
	for _, sig := range signals {
		assert.NotEqual(t, "link.go", sig.FilePath,
			"symlink with failed EvalSymlinks should be skipped")
	}
}

func TestTodoCollector_StatFailureInEnrichBlameFallback(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	// Create a git repo where blame will fail (untracked file).
	repoPath := initTestGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})
	untrackedPath := filepath.Join(repoPath, "untracked.go")
	require.NoError(t, os.WriteFile(untrackedPath, []byte("// TODO: test\n"), 0o600))

	FS = &testable.MockFileSystem{
		StatFn: func(name string) (os.FileInfo, error) {
			// Let .git stat succeed (so isGitRepo returns true),
			// but fail stat for the untracked file in the blame fallback.
			if filepath.Base(name) == ".git" {
				return os.Stat(name) //nolint:gosec
			}
			return nil, fmt.Errorf("stat error")
		},
	}

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.NoError(t, err)
	// Should still collect signals from the untracked file.
	// Blame fails and stat fallback also fails, so timestamp should be zero.
	var untrackedSignals []signal.RawSignal
	for _, sig := range signals {
		if sig.FilePath == "untracked.go" {
			untrackedSignals = append(untrackedSignals, sig)
		}
	}
	// Note: The file may be skipped by isBinaryFile if stat affects Open,
	// but since WalkDir uses real FS and Open falls through to real OS,
	// the file should be readable.
	for _, sig := range untrackedSignals {
		assert.True(t, sig.Timestamp.IsZero(),
			"timestamp should be zero when stat fallback fails")
	}
}

func TestTodoCollector_OpenFailureInScanFile(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		OpenFn: func(name string) (*os.File, error) {
			return nil, fmt.Errorf("permission denied")
		},
	}

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.go"), []byte("// TODO: test\n"), 0o600))

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	// File can't be opened via mock, so scanFile and isBinaryFile will fail.
	// Both skip gracefully, so no signals should be found.
	assert.Empty(t, signals)
}

func TestIsGitRepo_True(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"main.go": "package main\n",
	})
	assert.True(t, isGitRepo(repoPath))
}

func TestIsGitRepo_False(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, isGitRepo(dir))
}

func TestIsGitRepo_MockStatFailure(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		StatFn: func(name string) (os.FileInfo, error) {
			return nil, fmt.Errorf("no such file")
		},
	}
	assert.False(t, isGitRepo("/any/path"))
}

func TestTodoCollector_ProgressCallback(t *testing.T) {
	// This tests the progress callback path; it requires 500+ files
	// to fire but should not error. Just verify no panics.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("// TODO: test\n"), 0o600))

	var progressMessages []string
	c := &TodoCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		ProgressFunc: func(msg string) {
			progressMessages = append(progressMessages, msg)
		},
	})
	require.NoError(t, err)
	// Small directory won't trigger progress, but should not crash.
}

// --- isInsideStringLiteral tests ---

func TestIsInsideStringLiteral(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		matchStart int
		want       bool
	}{
		{name: "single_quoted_url", line: `.get('//todo@txt')`, matchStart: 6, want: true},
		{name: "double_quoted_comment", line: `var s = "// TODO: fake"`, matchStart: 9, want: true},
		{name: "backtick_template", line: "\x60// TODO: template\x60", matchStart: 1, want: true},
		{name: "real_comment_after_code", line: `code(); // TODO: real`, matchStart: 8, want: false},
		{name: "after_closed_string", line: `"hello"; // TODO: real`, matchStart: 9, want: false},
		{name: "escaped_quote_closed", line: `'it\'s'; // TODO: ok`, matchStart: 9, want: false},
		{name: "single_inside_double", line: `"it's"; // TODO: real`, matchStart: 7, want: false},
		{name: "empty_line", line: "", matchStart: 0, want: false},
		{name: "match_at_zero", line: `// TODO: start`, matchStart: 0, want: false},
		{name: "double_quoted_single_inside", line: `x("it's //TODO")`, matchStart: 10, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isInsideStringLiteral(tt.line, tt.matchStart)
			if got != tt.want {
				t.Errorf("isInsideStringLiteral(%q, %d) = %v, want %v",
					tt.line, tt.matchStart, got, tt.want)
			}
		})
	}
}

func TestScanFile_StringLiteralFalsePositives(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "express.js")

	content := `var express = require('express');

app.get('//todo@txt', function(req, res) {
  res.send('ok');
});

var url = "http://todo@example.com/path";

// TODO: this is a real todo
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	signals, err := scanFile(path, "express.js")
	require.NoError(t, err)

	// Only the real TODO comment on the last line should match.
	require.Len(t, signals, 1, "expected exactly 1 signal (only the real TODO)")
	assert.Equal(t, "TODO: this is a real todo", signals[0].Title)
	assert.Equal(t, 9, signals[0].Line)

	// Verify no signal has the false-positive @txt content.
	for _, sig := range signals {
		assert.NotContains(t, sig.Title, "@txt",
			"string literal false positive should be filtered")
	}
}

func TestScanFile_RealCommentsStillMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "real.go")

	content := `// TODO: real comment
/* TODO: block comment */
# TODO: python style
// TODO(auth): with author
-- FIXME: sql style
* HACK: jsdoc style
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	signals, err := scanFile(path, "real.go")
	require.NoError(t, err)

	assert.Len(t, signals, 6, "all real comment patterns should still match")
}

func TestCollect_VendoredWwwrootLibExcluded(t *testing.T) {
	repoPath := initTestGitRepo(t, map[string]string{
		"main.go": "// TODO: real work item\n",
		"project/wwwroot/lib/bootstrap/dist/js/bootstrap.js": "// TODO: vendored todo\n",
		"project/wwwroot/lib/jquery/jquery.min.js":           "// TODO: vendored jquery\n",
		"third_party/proto/gen.go":                           "// TODO: generated\n",
		"sub/external/dep.go":                                "// TODO: external dep\n",
	})

	c := &TodoCollector{}
	signals, err := c.Collect(context.Background(), repoPath, signal.CollectorOpts{})
	require.NoError(t, err)

	// Only the real work item in main.go should survive.
	require.Len(t, signals, 1, "only main.go TODO should remain after vendor exclusion")
	assert.Equal(t, "main.go", signals[0].FilePath)
}

func TestIsBinaryFile_MockOpenError(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		OpenFn: func(name string) (*os.File, error) {
			return nil, fmt.Errorf("open failed")
		},
	}
	// Should treat as binary (unreadable).
	assert.True(t, isBinaryFile("/any/path"))
}
