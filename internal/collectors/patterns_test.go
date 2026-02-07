package collectors

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

// --- Large file detection tests ---

func TestLargeFileDetected(t *testing.T) {
	dir := t.TempDir()

	// Create a file with 600 lines (above the 500-line threshold).
	content := strings.Repeat("package main\n", 600)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.go"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	var largeFileSignals []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "large-file" {
			largeFileSignals = append(largeFileSignals, s)
		}
	}

	require.Len(t, largeFileSignals, 1)
	assert.Equal(t, "patterns", largeFileSignals[0].Source)
	assert.Equal(t, "big.go", largeFileSignals[0].FilePath)
	assert.Contains(t, largeFileSignals[0].Title, "600 lines")
	assert.Equal(t, 0, largeFileSignals[0].Line)
	assert.GreaterOrEqual(t, largeFileSignals[0].Confidence, 0.4)
	assert.LessOrEqual(t, largeFileSignals[0].Confidence, 0.8)
	assert.Contains(t, largeFileSignals[0].Tags, "large-file")
	assert.Contains(t, largeFileSignals[0].Tags, "stringer-generated")
}

func TestLargeFileNotDetectedUnderThreshold(t *testing.T) {
	dir := t.TempDir()

	// Create a file with exactly 500 lines (at threshold, not over).
	content := strings.Repeat("package main\n", 500)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ok.go"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "large-file" {
			t.Errorf("unexpected large-file signal for file at threshold: %s", s.Title)
		}
	}
}

func TestLargeFileConfidenceScaling(t *testing.T) {
	tests := []struct {
		name        string
		lines       int
		wantMinConf float64
		wantMaxConf float64
	}{
		{name: "just_over", lines: 510, wantMinConf: 0.4, wantMaxConf: 0.45},
		{name: "1.5x", lines: 750, wantMinConf: 0.55, wantMaxConf: 0.65},
		{name: "2x", lines: 1000, wantMinConf: 0.79, wantMaxConf: 0.80},
		{name: "3x_capped", lines: 1500, wantMinConf: 0.80, wantMaxConf: 0.80},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := largeFileConfidence(tt.lines)
			assert.GreaterOrEqual(t, conf, tt.wantMinConf, "confidence too low for %d lines", tt.lines)
			assert.LessOrEqual(t, conf, tt.wantMaxConf, "confidence too high for %d lines", tt.lines)
		})
	}
}

// --- Missing test detection tests ---

func TestMissingTestDetected(t *testing.T) {
	dir := t.TempDir()

	// Create a source file with enough lines (>=20) but no test counterpart.
	content := strings.Repeat("func foo() {}\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "handler.go"), []byte("package main\n"+content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	var missingTestSignals []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "missing-tests" {
			missingTestSignals = append(missingTestSignals, s)
		}
	}

	require.Len(t, missingTestSignals, 1)
	assert.Equal(t, "patterns", missingTestSignals[0].Source)
	assert.Equal(t, "handler.go", missingTestSignals[0].FilePath)
	assert.Contains(t, missingTestSignals[0].Title, "handler.go")
	assert.InDelta(t, missingTestConfidence, missingTestSignals[0].Confidence, 0.001)
	assert.Contains(t, missingTestSignals[0].Tags, "missing-tests")
}

func TestMissingTestNotReportedWhenTestExists(t *testing.T) {
	dir := t.TempDir()

	// Create source + test file pair.
	content := strings.Repeat("func foo() {}\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "handler.go"), []byte("package main\n"+content), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "handler_test.go"), []byte("package main\n"), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" && s.FilePath == "handler.go" {
			t.Error("missing-tests signal should not be reported when test file exists")
		}
	}
}

func TestMissingTestNotReportedForSmallFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a small source file (< 20 lines) with no test counterpart.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "const.go"), []byte("package main\n\nconst X = 1\n"), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" && s.FilePath == "const.go" {
			t.Error("missing-tests signal should not be reported for small files")
		}
	}
}

func TestMissingTestMultiLanguage(t *testing.T) {
	dir := t.TempDir()

	content := strings.Repeat("// line\n", 25)

	// JS: foo.js with foo.test.js → should NOT be flagged.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "foo.js"), []byte(content), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "foo.test.js"), []byte("// test\n"), 0o600))

	// Python: bar.py without test_bar.py → should be flagged.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bar.py"), []byte(content), 0o600))

	// Ruby: baz.rb with baz_spec.rb → should NOT be flagged.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "baz.rb"), []byte(content), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "baz_spec.rb"), []byte("# spec\n"), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	var missingTestSignals []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "missing-tests" {
			missingTestSignals = append(missingTestSignals, s)
		}
	}

	require.Len(t, missingTestSignals, 1)
	assert.Equal(t, "bar.py", missingTestSignals[0].FilePath)
}

// --- Test-to-source ratio tests ---

func TestLowTestRatioDetected(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "pkg")
	require.NoError(t, os.MkdirAll(subdir, 0o750))

	// Create 5 source files and 0 test files (ratio = 0%).
	for i := 0; i < 5; i++ {
		content := strings.Repeat("// code\n", 5)
		require.NoError(t, os.WriteFile(filepath.Join(subdir, fmt.Sprintf("file%d.go", i)), []byte("package pkg\n"+content), 0o600))
	}

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	var ratioSignals []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "low-test-ratio" {
			ratioSignals = append(ratioSignals, s)
		}
	}

	require.Len(t, ratioSignals, 1)
	assert.Equal(t, "patterns", ratioSignals[0].Source)
	assert.Equal(t, "pkg", ratioSignals[0].FilePath)
	assert.Contains(t, ratioSignals[0].Title, "0 test files / 5 source files")
	assert.InDelta(t, lowTestRatioConfidence, ratioSignals[0].Confidence, 0.001)
	assert.Contains(t, ratioSignals[0].Tags, "low-test-ratio")
}

func TestLowTestRatioNotDetectedWhenEnoughTests(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "pkg")
	require.NoError(t, os.MkdirAll(subdir, 0o750))

	// Create 3 source files and 1 test file (ratio ~33%, well above 10%).
	for i := 0; i < 3; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(subdir, fmt.Sprintf("file%d.go", i)), []byte("package pkg\n"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(subdir, "file0_test.go"), []byte("package pkg\n"), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "low-test-ratio" && s.FilePath == "pkg" {
			t.Error("low-test-ratio should not be reported when ratio is above threshold")
		}
	}
}

func TestLowTestRatioNotDetectedForSmallDirectories(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "small")
	require.NoError(t, os.MkdirAll(subdir, 0o750))

	// Create 2 source files (below minSourceFilesForRatio of 3).
	require.NoError(t, os.WriteFile(filepath.Join(subdir, "a.go"), []byte("package small\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(subdir, "b.go"), []byte("package small\n"), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "low-test-ratio" && s.FilePath == "small" {
			t.Error("low-test-ratio should not be reported for directories with fewer than 3 source files")
		}
	}
}

// --- Exclude pattern tests ---

func TestExcludedDirectoriesSkipped(t *testing.T) {
	dir := t.TempDir()

	// Create a large file inside vendor/ (default exclude).
	vendorDir := filepath.Join(dir, "vendor", "dep")
	require.NoError(t, os.MkdirAll(vendorDir, 0o750))
	bigContent := strings.Repeat("package dep\n", 600)
	require.NoError(t, os.WriteFile(filepath.Join(vendorDir, "big.go"), []byte(bigContent), 0o600))

	// Create a normal large file that should be detected.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(strings.Repeat("package main\n", 600)), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if strings.Contains(s.FilePath, "vendor") {
			t.Errorf("signal from excluded vendor directory: %s", s.FilePath)
		}
	}

	// Verify the non-excluded large file WAS detected.
	var found bool
	for _, s := range signals {
		if s.Kind == "large-file" && s.FilePath == "main.go" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected large-file signal for main.go")
}

func TestCustomExcludePatterns(t *testing.T) {
	dir := t.TempDir()
	genDir := filepath.Join(dir, "generated")
	require.NoError(t, os.MkdirAll(genDir, 0o750))

	bigContent := strings.Repeat("package gen\n", 600)
	require.NoError(t, os.WriteFile(filepath.Join(genDir, "gen.go"), []byte(bigContent), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		ExcludePatterns: []string{"generated/**"},
	})
	require.NoError(t, err)

	for _, s := range signals {
		if strings.Contains(s.FilePath, "generated") {
			t.Errorf("signal from custom-excluded directory: %s", s.FilePath)
		}
	}
}

// --- Binary and empty file tests ---

func TestBinaryFilesSkipped(t *testing.T) {
	dir := t.TempDir()

	// Create a binary file with .go extension (unusual but tests binary detection).
	binContent := make([]byte, 600)
	binContent[10] = 0x00 // null byte → binary detection
	require.NoError(t, os.WriteFile(filepath.Join(dir, "binary.go"), binContent, 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.FilePath == "binary.go" {
			t.Errorf("binary file should have been skipped, got signal: %s", s.Title)
		}
	}
}

func TestEmptyFilesSkipped(t *testing.T) {
	dir := t.TempDir()

	// Create an empty .go file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.go"), []byte{}, 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.FilePath == "empty.go" && s.Kind == "large-file" {
			t.Error("empty file should not produce large-file signal")
		}
	}
}

// --- Context cancellation test ---

func TestContextCancellation(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &PatternsCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{})
	assert.Error(t, err, "expected error from cancelled context")
}

// --- Collector name test ---

func TestPatternsCollectorName(t *testing.T) {
	c := &PatternsCollector{}
	assert.Equal(t, "patterns", c.Name())
}

// --- isTestFile tests ---

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "go_test", path: "handler_test.go", want: true},
		{name: "go_source", path: "handler.go", want: false},
		{name: "js_test", path: "app.test.js", want: true},
		{name: "js_spec", path: "app.spec.js", want: true},
		{name: "js_source", path: "app.js", want: false},
		{name: "ts_test", path: "service.test.ts", want: true},
		{name: "ts_spec", path: "service.spec.ts", want: true},
		{name: "ts_source", path: "service.ts", want: false},
		{name: "py_test_prefix", path: "test_handler.py", want: true},
		{name: "py_test_suffix", path: "handler_test.py", want: true},
		{name: "py_source", path: "handler.py", want: false},
		{name: "rb_spec", path: "model_spec.rb", want: true},
		{name: "rb_test", path: "model_test.rb", want: true},
		{name: "rb_source", path: "model.rb", want: false},
		{name: "java_test", path: "FooTest.java", want: true},
		{name: "java_spec", path: "FooSpec.java", want: true},
		{name: "java_source", path: "Foo.java", want: false},
		{name: "kt_test", path: "BarTest.kt", want: true},
		{name: "kt_source", path: "Bar.kt", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTestFile(tt.path)
			assert.Equal(t, tt.want, got, "isTestFile(%q)", tt.path)
		})
	}
}

// --- countLines test ---

func TestCountLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	require.NoError(t, os.WriteFile(path, []byte("a\nb\nc\n"), 0o600))

	count, err := countLines(path)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestCountLinesEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	require.NoError(t, os.WriteFile(path, []byte{}, 0o600))

	count, err := countLines(path)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// --- Include patterns test ---

func TestIncludePatterns(t *testing.T) {
	dir := t.TempDir()

	// Create a large .go file and a large .py file.
	goContent := strings.Repeat("package main\n", 600)
	pyContent := strings.Repeat("# python\n", 600)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(goContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "script.py"), []byte(pyContent), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		IncludePatterns: []string{"*.go"},
	})
	require.NoError(t, err)

	for _, s := range signals {
		if s.FilePath == "script.py" {
			t.Errorf("signal from non-included file: %s", s.FilePath)
		}
	}

	// Verify .go file signal exists.
	var found bool
	for _, s := range signals {
		if s.Kind == "large-file" && s.FilePath == "main.go" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected large-file signal for main.go")
}

// --- Non-source extensions skipped test ---

func TestNonSourceExtensionsSkipped(t *testing.T) {
	dir := t.TempDir()

	// Create large .md and .txt files — these should be ignored.
	mdContent := strings.Repeat("# heading\n", 600)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte(mdContent), 0o600))

	txtContent := strings.Repeat("some text\n", 600)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(txtContent), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.FilePath == "README.md" || s.FilePath == "notes.txt" {
			t.Errorf("non-source file should be skipped: %s", s.FilePath)
		}
	}
}
