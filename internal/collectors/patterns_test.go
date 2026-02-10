package collectors

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/davetashner/stringer/internal/testable"
)

// --- Large file detection tests ---

func TestLargeFileDetected(t *testing.T) {
	dir := t.TempDir()

	// Create a file with 1100 lines (above the 1000-line threshold).
	content := strings.Repeat("package main\n", 1100)
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
	assert.Contains(t, largeFileSignals[0].Title, "1100 lines")
	assert.Equal(t, 0, largeFileSignals[0].Line)
	assert.GreaterOrEqual(t, largeFileSignals[0].Confidence, 0.4)
	assert.LessOrEqual(t, largeFileSignals[0].Confidence, 0.8)
	assert.Contains(t, largeFileSignals[0].Tags, "large-file")
}

func TestLargeFileNotDetectedUnderThreshold(t *testing.T) {
	dir := t.TempDir()

	// Create a file with exactly 1000 lines (at threshold, not over).
	content := strings.Repeat("package main\n", 1000)
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

func TestLargeFileCustomThreshold(t *testing.T) {
	dir := t.TempDir()

	// Create a file with 600 lines — below the default 1000-line threshold
	// but above a custom 500-line threshold.
	content := strings.Repeat("package main\n", 600)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "medium.go"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		LargeFileThreshold: 500,
	})
	require.NoError(t, err)

	var largeFileSignals []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "large-file" {
			largeFileSignals = append(largeFileSignals, s)
		}
	}

	require.Len(t, largeFileSignals, 1)
	assert.Contains(t, largeFileSignals[0].Title, "600 lines")
	assert.Contains(t, largeFileSignals[0].Description, "500-line threshold")
}

func TestLargeFileCustomThresholdNotTriggered(t *testing.T) {
	dir := t.TempDir()

	// 600 lines, default threshold of 1000 — should NOT trigger.
	content := strings.Repeat("package main\n", 600)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "medium.go"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "large-file" {
			t.Errorf("unexpected large-file signal with default threshold: %s", s.Title)
		}
	}
}

func TestLargeFileConfidenceScaling(t *testing.T) {
	tests := []struct {
		name        string
		lines       int
		threshold   int
		wantMinConf float64
		wantMaxConf float64
	}{
		{name: "just_over_default", lines: 1010, threshold: 1000, wantMinConf: 0.4, wantMaxConf: 0.45},
		{name: "1.5x_default", lines: 1500, threshold: 1000, wantMinConf: 0.55, wantMaxConf: 0.65},
		{name: "2x_default", lines: 2000, threshold: 1000, wantMinConf: 0.79, wantMaxConf: 0.80},
		{name: "3x_default_capped", lines: 3000, threshold: 1000, wantMinConf: 0.80, wantMaxConf: 0.80},
		{name: "custom_threshold", lines: 510, threshold: 500, wantMinConf: 0.4, wantMaxConf: 0.45},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := largeFileConfidence(tt.lines, tt.threshold)
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
	bigContent := strings.Repeat("package dep\n", 1100)
	require.NoError(t, os.WriteFile(filepath.Join(vendorDir, "big.go"), []byte(bigContent), 0o600))

	// Create a normal large file that should be detected.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(strings.Repeat("package main\n", 1100)), 0o600))

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

	bigContent := strings.Repeat("package gen\n", 1100)
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
		{name: "rb_test_prefix", path: "test_model.rb", want: true},
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
	goContent := strings.Repeat("package main\n", 1100)
	pyContent := strings.Repeat("# python\n", 1100)
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
	mdContent := strings.Repeat("# heading\n", 1100)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte(mdContent), 0o600))

	txtContent := strings.Repeat("some text\n", 1100)
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

// --- countLines edge case tests ---

func TestCountLines_NonexistentFile(t *testing.T) {
	_, err := countLines("/nonexistent/path/to/file.go")
	assert.Error(t, err, "nonexistent file should return error")
}

func TestCountLines_SingleLineNoNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "one.txt")
	require.NoError(t, os.WriteFile(path, []byte("no newline"), 0o600))

	count, err := countLines(path)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// --- hasTestCounterpart edge case tests ---

func TestHasTestCounterpart_GoTestExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "handler.go"), []byte("package main\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "handler_test.go"), []byte("package main\n"), 0o600))

	assert.True(t, hasTestCounterpart(filepath.Join(dir, "handler.go"), "handler.go", dir, nil))
}

func TestHasTestCounterpart_GoTestMissing(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "handler.go"), []byte("package main\n"), 0o600))

	assert.False(t, hasTestCounterpart(filepath.Join(dir, "handler.go"), "handler.go", dir, nil))
}

func TestHasTestCounterpart_TSTestExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.ts"), []byte("// ts\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.test.ts"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(filepath.Join(dir, "app.ts"), "app.ts", dir, nil))
}

func TestHasTestCounterpart_TSSpecExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.ts"), []byte("// ts\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.spec.ts"), []byte("// spec\n"), 0o600))

	assert.True(t, hasTestCounterpart(filepath.Join(dir, "app.ts"), "app.ts", dir, nil))
}

func TestHasTestCounterpart_PythonTestPrefixExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "handler.py"), []byte("# py\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test_handler.py"), []byte("# test\n"), 0o600))

	assert.True(t, hasTestCounterpart(filepath.Join(dir, "handler.py"), "handler.py", dir, nil))
}

func TestHasTestCounterpart_PythonTestSuffixExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "handler.py"), []byte("# py\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "handler_test.py"), []byte("# test\n"), 0o600))

	assert.True(t, hasTestCounterpart(filepath.Join(dir, "handler.py"), "handler.py", dir, nil))
}

func TestHasTestCounterpart_RubySpecExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "model.rb"), []byte("# rb\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "model_spec.rb"), []byte("# spec\n"), 0o600))

	assert.True(t, hasTestCounterpart(filepath.Join(dir, "model.rb"), "model.rb", dir, nil))
}

func TestHasTestCounterpart_RubyTestExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "model.rb"), []byte("# rb\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "model_test.rb"), []byte("# test\n"), 0o600))

	assert.True(t, hasTestCounterpart(filepath.Join(dir, "model.rb"), "model.rb", dir, nil))
}

func TestHasTestCounterpart_JavaTestExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Foo.java"), []byte("// java\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "FooTest.java"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(filepath.Join(dir, "Foo.java"), "Foo.java", dir, nil))
}

func TestHasTestCounterpart_JavaSpecExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Foo.java"), []byte("// java\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "FooSpec.java"), []byte("// spec\n"), 0o600))

	assert.True(t, hasTestCounterpart(filepath.Join(dir, "Foo.java"), "Foo.java", dir, nil))
}

func TestHasTestCounterpart_KotlinTestExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Bar.kt"), []byte("// kt\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "BarTest.kt"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(filepath.Join(dir, "Bar.kt"), "Bar.kt", dir, nil))
}

func TestHasTestCounterpart_KotlinSpecExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Bar.kt"), []byte("// kt\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "BarSpec.kt"), []byte("// spec\n"), 0o600))

	assert.True(t, hasTestCounterpart(filepath.Join(dir, "Bar.kt"), "Bar.kt", dir, nil))
}

func TestHasTestCounterpart_JSTestExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.js"), []byte("// js\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.test.js"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(filepath.Join(dir, "app.js"), "app.js", dir, nil))
}

func TestHasTestCounterpart_JSSpecExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.js"), []byte("// js\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.spec.js"), []byte("// spec\n"), 0o600))

	assert.True(t, hasTestCounterpart(filepath.Join(dir, "app.js"), "app.js", dir, nil))
}

func TestHasTestCounterpart_JSXTestExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "component.jsx"), []byte("// jsx\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "component.test.jsx"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(filepath.Join(dir, "component.jsx"), "component.jsx", dir, nil))
}

func TestHasTestCounterpart_TSXSpecExists(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "component.tsx"), []byte("// tsx\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "component.spec.tsx"), []byte("// spec\n"), 0o600))

	assert.True(t, hasTestCounterpart(filepath.Join(dir, "component.tsx"), "component.tsx", dir, nil))
}

func TestHasTestCounterpart_UnknownExtension(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("key: val\n"), 0o600))

	assert.False(t, hasTestCounterpart(filepath.Join(dir, "config.yaml"), "config.yaml", dir, nil))
}

// --- isTestFile edge case tests ---

func TestIsTestFile_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "tsx_test", path: "component.test.tsx", want: true},
		{name: "tsx_spec", path: "component.spec.tsx", want: true},
		{name: "jsx_test", path: "component.test.jsx", want: true},
		{name: "jsx_spec", path: "component.spec.jsx", want: true},
		{name: "test_file_itself", path: "Test.java", want: false},         // Just "Test.java" with no prefix
		{name: "spec_file_itself", path: "Spec.java", want: false},         // Just "Spec.java" with no prefix
		{name: "nested_path", path: "src/pkg/handler_test.go", want: true}, // nested path
		{name: "deep_ts_spec", path: "src/components/button.spec.ts", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTestFile(tt.path)
			assert.Equal(t, tt.want, got, "isTestFile(%q)", tt.path)
		})
	}
}

// --- isUnderTestRoot ---

func TestIsUnderTestRoot(t *testing.T) {
	roots := []string{"tests", "test", "spec"}
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"under_tests", "tests/test_handler.py", true},
		{"under_test", "test/handler_test.go", true},
		{"under_spec", "spec/model_spec.rb", true},
		{"not_under_root", "src/handler.go", false},
		{"prefix_match_only", "testing/handler.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isUnderTestRoot(tt.path, roots), "isUnderTestRoot(%q)", tt.path)
		})
	}
}

func TestIsUnderTestRoot_EmptyRoots(t *testing.T) {
	assert.False(t, isUnderTestRoot("tests/foo.py", nil))
}

// --- isGeneratedFile ---

func TestIsGeneratedFile(t *testing.T) {
	dir := t.TempDir()

	// _string.go suffix → generated.
	stringerFile := filepath.Join(dir, "kind_string.go")
	require.NoError(t, os.WriteFile(stringerFile, []byte("package main\n"), 0o600))
	assert.True(t, isGeneratedFile(stringerFile))

	// "Code generated" header → generated.
	codegenFile := filepath.Join(dir, "gen.go")
	require.NoError(t, os.WriteFile(codegenFile, []byte("// Code generated by stringer; DO NOT EDIT.\npackage main\n"), 0o600))
	assert.True(t, isGeneratedFile(codegenFile))

	// Normal source file → not generated.
	normalFile := filepath.Join(dir, "handler.go")
	require.NoError(t, os.WriteFile(normalFile, []byte("package main\nfunc Handle() {}\n"), 0o600))
	assert.False(t, isGeneratedFile(normalFile))
}

// --- Patterns Collector: symlink outside repo ---

func TestPatterns_SymlinkOutsideRepoSkipped(t *testing.T) {
	dir := t.TempDir()

	// Create a normal large source file.
	goContent := strings.Repeat("package main\n", 1100)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(goContent), 0o600))

	// Create an outside file.
	outsideDir := t.TempDir()
	outsidePath := filepath.Join(outsideDir, "external.go")
	bigContent := strings.Repeat("package ext\n", 1100)
	require.NoError(t, os.WriteFile(outsidePath, []byte(bigContent), 0o600))

	// Create a symlink inside the dir pointing outside.
	symlinkPath := filepath.Join(dir, "external_link.go")
	if err := os.Symlink(outsidePath, symlinkPath); err != nil {
		t.Skip("symlinks not supported on this OS")
	}

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.FilePath == "external_link.go" {
			t.Error("symlink pointing outside repo should be skipped")
		}
	}
}

// --- largeFileConfidence edge case tests ---

func TestLargeFileConfidence_AtThreshold(t *testing.T) {
	// At exactly the threshold+1 (1001), confidence should be just above 0.4.
	conf := largeFileConfidence(1001, defaultLargeFileThreshold)
	assert.GreaterOrEqual(t, conf, 0.4)
	assert.Less(t, conf, 0.41)
}

func TestLargeFileConfidence_ExtremelyLarge(t *testing.T) {
	// 10000 lines should cap at 0.8.
	conf := largeFileConfidence(10000, defaultLargeFileThreshold)
	assert.InDelta(t, 0.8, conf, 0.001)
}

// --- Patterns Collect: context cancellation during ratio loop ---

func TestPatterns_ContextCancelledDuringRatioCheck(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "pkg")
	require.NoError(t, os.MkdirAll(subdir, 0o750))

	// Create enough files to trigger the ratio check.
	for i := 0; i < 5; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(subdir, fmt.Sprintf("file%d.go", i)),
			[]byte("package pkg\n"), 0o600))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := &PatternsCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{})
	assert.Error(t, err, "cancelled context should return error")
}

// --- Patterns Collect: excluded file (not directory) ---

func TestPatterns_ExcludedFilePattern(t *testing.T) {
	dir := t.TempDir()

	// Create a large .go file and a large .generated.go file.
	goContent := strings.Repeat("package main\n", 1100)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(goContent), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "code.generated.go"), []byte(goContent), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		ExcludePatterns: []string{"*.generated.go"},
	})
	require.NoError(t, err)

	for _, s := range signals {
		if strings.Contains(s.FilePath, "generated") {
			t.Errorf("excluded file should be skipped: %s", s.FilePath)
		}
	}

	// The non-excluded file should still be detected.
	var found bool
	for _, s := range signals {
		if s.Kind == "large-file" && s.FilePath == "main.go" {
			found = true
			break
		}
	}
	assert.True(t, found, "non-excluded large file should be detected")
}

// --- Patterns Collect: broken symlink ---

func TestPatterns_BrokenSymlinkSkipped(t *testing.T) {
	dir := t.TempDir()

	// Create a normal file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte(strings.Repeat("package main\n", 1100)), 0o600))

	// Create a broken symlink (points to nonexistent target).
	symlinkPath := filepath.Join(dir, "broken_link.go")
	if err := os.Symlink("/nonexistent/target.go", symlinkPath); err != nil {
		t.Skip("symlinks not supported on this OS")
	}

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	// Should not panic or error; the broken symlink is silently skipped.
	for _, s := range signals {
		if s.FilePath == "broken_link.go" {
			t.Error("broken symlink should be skipped")
		}
	}
}

// --- Patterns Collect: unreadable file ---

func TestPatterns_UnreadableFileSkipped(t *testing.T) {
	dir := t.TempDir()

	// Create a normal large file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte(strings.Repeat("package main\n", 1100)), 0o600))

	// Create a file with no read permission.
	noReadPath := filepath.Join(dir, "noread.go")
	require.NoError(t, os.WriteFile(noReadPath, []byte(strings.Repeat("package main\n", 1100)), 0o000))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	// The unreadable file should be silently skipped; the collector should
	// still find the large-file signal from main.go.
	var found bool
	for _, s := range signals {
		if s.Kind == "large-file" && s.FilePath == "main.go" {
			found = true
		}
		if s.FilePath == "noread.go" {
			t.Error("unreadable file should be skipped")
		}
	}
	assert.True(t, found, "expected large-file signal for main.go")
}

// --- Patterns Collect: multiple directories with different ratios ---

func TestPatterns_MultipleDirRatios(t *testing.T) {
	dir := t.TempDir()

	// Create dir1 with 4 source files and 0 tests (low ratio).
	dir1 := filepath.Join(dir, "dir1")
	require.NoError(t, os.MkdirAll(dir1, 0o750))
	for i := 0; i < 4; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(dir1, fmt.Sprintf("f%d.go", i)),
			[]byte("package dir1\n"), 0o600))
	}

	// Create dir2 with 3 source files and 1 test (good ratio).
	dir2 := filepath.Join(dir, "dir2")
	require.NoError(t, os.MkdirAll(dir2, 0o750))
	for i := 0; i < 3; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(dir2, fmt.Sprintf("f%d.go", i)),
			[]byte("package dir2\n"), 0o600))
	}
	require.NoError(t, os.WriteFile(
		filepath.Join(dir2, "f0_test.go"),
		[]byte("package dir2\n"), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	var ratioSignals []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "low-test-ratio" {
			ratioSignals = append(ratioSignals, s)
		}
	}

	// Only dir1 should have a low-test-ratio signal.
	require.Len(t, ratioSignals, 1)
	assert.Equal(t, "dir1", ratioSignals[0].FilePath)
}

// --- Parallel test tree tests ---

func TestHasTestCounterpart_ParallelTestTree(t *testing.T) {
	dir := t.TempDir()

	// Create src/handler.py (source file).
	srcDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "handler.py"), []byte("# source\n"), 0o600))

	// Create tests/src/test_handler.py (parallel test tree).
	testDir := filepath.Join(dir, "tests", "src")
	require.NoError(t, os.MkdirAll(testDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "test_handler.py"), []byte("# test\n"), 0o600))

	// With testRoots = ["tests"], should find the counterpart.
	assert.True(t, hasTestCounterpart(
		filepath.Join(srcDir, "handler.py"),
		"src/handler.py",
		dir,
		[]string{"tests"},
	))
}

func TestHasTestCounterpart_NoParallelRoots(t *testing.T) {
	dir := t.TempDir()

	// Create src/handler.py without any test file.
	srcDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "handler.py"), []byte("# source\n"), 0o600))

	// No test roots, no same-directory test file — should be false.
	assert.False(t, hasTestCounterpart(
		filepath.Join(srcDir, "handler.py"),
		"src/handler.py",
		dir,
		nil,
	))
}

func TestHasTestCounterpart_ParallelTestTreeStrippedRoot(t *testing.T) {
	dir := t.TempDir()

	// Create homeassistant/components/light/sensor.py (source file with
	// a top-level directory that is NOT mirrored in the test tree).
	srcDir := filepath.Join(dir, "homeassistant", "components", "light")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "sensor.py"), []byte("# source\n"), 0o600))

	// Create tests/components/light/test_sensor.py (first component stripped).
	testDir := filepath.Join(dir, "tests", "components", "light")
	require.NoError(t, os.MkdirAll(testDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "test_sensor.py"), []byte("# test\n"), 0o600))

	// With testRoots = ["tests"], should find the counterpart via stripping.
	assert.True(t, hasTestCounterpart(
		filepath.Join(srcDir, "sensor.py"),
		"homeassistant/components/light/sensor.py",
		dir,
		[]string{"tests"},
	))
}

// --- Maven/Gradle test tree tests ---

func TestHasTestCounterpart_MavenJavaTree(t *testing.T) {
	dir := t.TempDir()

	// src/main/java/com/example/Foo.java
	srcDir := filepath.Join(dir, "src", "main", "java", "com", "example")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Foo.java"), []byte("// java\n"), 0o600))

	// src/test/java/com/example/FooTest.java
	testDir := filepath.Join(dir, "src", "test", "java", "com", "example")
	require.NoError(t, os.MkdirAll(testDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "FooTest.java"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(srcDir, "Foo.java"),
		"src/main/java/com/example/Foo.java",
		dir,
		nil,
	))
}

func TestHasTestCounterpart_MavenKotlinTree(t *testing.T) {
	dir := t.TempDir()

	// src/main/kotlin/com/example/Bar.kt
	srcDir := filepath.Join(dir, "src", "main", "kotlin", "com", "example")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Bar.kt"), []byte("// kt\n"), 0o600))

	// src/test/kotlin/com/example/BarTest.kt
	testDir := filepath.Join(dir, "src", "test", "kotlin", "com", "example")
	require.NoError(t, os.MkdirAll(testDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "BarTest.kt"), []byte("// test\n"), 0o600))

	assert.True(t, hasTestCounterpart(
		filepath.Join(srcDir, "Bar.kt"),
		"src/main/kotlin/com/example/Bar.kt",
		dir,
		nil,
	))
}

func TestHasTestCounterpart_MavenTreeMissing(t *testing.T) {
	dir := t.TempDir()

	// src/main/java/com/example/Foo.java — no test counterpart
	srcDir := filepath.Join(dir, "src", "main", "java", "com", "example")
	require.NoError(t, os.MkdirAll(srcDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "Foo.java"), []byte("// java\n"), 0o600))

	assert.False(t, hasTestCounterpart(
		filepath.Join(srcDir, "Foo.java"),
		"src/main/java/com/example/Foo.java",
		dir,
		nil,
	))
}

func TestMavenTestDir(t *testing.T) {
	tests := []struct {
		name    string
		relPath string
		want    string
		ok      bool
	}{
		{"java", "src/main/java/com/example/Foo.java", filepath.FromSlash("src/test/java/com/example"), true},
		{"kotlin", "src/main/kotlin/com/Bar.kt", filepath.FromSlash("src/test/kotlin/com"), true},
		{"scala", "src/main/scala/Baz.scala", filepath.FromSlash("src/test/scala"), true},
		{"not_maven", "lib/handler.go", "", false},
		{"src_but_not_main", "src/handler.go", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := mavenTestDir(tt.relPath)
			assert.Equal(t, tt.ok, ok)
			if ok {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestDetectTestRoots(t *testing.T) {
	dir := t.TempDir()

	// Create "tests" and "__tests__" directories.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tests"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "__tests__"), 0o750))

	c := &PatternsCollector{}
	c.detectTestRoots(dir)

	assert.True(t, c.testRootsInit)
	assert.Contains(t, c.testRoots, "tests")
	assert.Contains(t, c.testRoots, "__tests__")
	assert.NotContains(t, c.testRoots, "test")
	assert.NotContains(t, c.testRoots, "spec")
}

func TestDetectTestRoots_OnlyRunsOnce(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tests"), 0o750))

	c := &PatternsCollector{}
	c.detectTestRoots(dir)
	assert.Len(t, c.testRoots, 1)

	// Create another directory — calling again should not update.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "test"), 0o750))
	c.detectTestRoots(dir)
	assert.Len(t, c.testRoots, 1, "detectTestRoots should only run once")
}

// --- Timestamp enrichment tests ---

func TestPatterns_TimestampsEnriched(t *testing.T) {
	// Use a git repo so enrichTimestamps can call git log.
	_, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n" + strings.Repeat("func f() {}\n", 1100),
	})

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	largeFile := filterByKind(signals, "large-file")
	require.NotEmpty(t, largeFile, "should detect large file")

	for _, sig := range largeFile {
		assert.False(t, sig.Timestamp.IsZero(), "large-file signal should have non-zero timestamp")
		assert.WithinDuration(t, time.Now(), sig.Timestamp, 10*time.Minute,
			"timestamp should be recent for a freshly committed file")
	}
}

// --- Demo path filtering tests ---

func TestPatterns_MissingTestsSuppressedInExamples(t *testing.T) {
	dir := t.TempDir()
	exDir := filepath.Join(dir, "examples", "basic")
	require.NoError(t, os.MkdirAll(exDir, 0o750))

	// Create a source file with enough lines in examples/ — should NOT produce missing-tests.
	content := strings.Repeat("func foo() {}\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(exDir, "main.go"), []byte("package main\n"+content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "missing-tests" {
			t.Errorf("missing-tests signal should be suppressed in examples/, got: %s", s.FilePath)
		}
	}
}

func TestPatterns_MissingTestsIncludeDemoPathsOptIn(t *testing.T) {
	dir := t.TempDir()
	exDir := filepath.Join(dir, "examples", "basic")
	require.NoError(t, os.MkdirAll(exDir, 0o750))

	content := strings.Repeat("func foo() {}\n", 25)
	require.NoError(t, os.WriteFile(filepath.Join(exDir, "main.go"), []byte("package main\n"+content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		IncludeDemoPaths: true,
	})
	require.NoError(t, err)

	var missingTests []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "missing-tests" {
			missingTests = append(missingTests, s)
		}
	}
	require.NotEmpty(t, missingTests, "IncludeDemoPaths=true should re-enable missing-tests in examples/")
}

func TestPatterns_LowTestRatioSuppressedInExamples(t *testing.T) {
	dir := t.TempDir()
	exDir := filepath.Join(dir, "examples")
	require.NoError(t, os.MkdirAll(exDir, 0o750))

	// Create 5 source files in examples/ — should NOT produce low-test-ratio.
	for i := 0; i < 5; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(exDir, fmt.Sprintf("file%d.go", i)),
			[]byte("package examples\n"), 0o600))
	}

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, s := range signals {
		if s.Kind == "low-test-ratio" && strings.HasPrefix(s.FilePath, "examples") {
			t.Errorf("low-test-ratio signal should be suppressed in examples/, got: %s", s.FilePath)
		}
	}
}

func TestPatterns_LowTestRatioIncludeDemoPathsOptIn(t *testing.T) {
	dir := t.TempDir()
	exDir := filepath.Join(dir, "examples")
	require.NoError(t, os.MkdirAll(exDir, 0o750))

	for i := 0; i < 5; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(exDir, fmt.Sprintf("file%d.go", i)),
			[]byte("package examples\n"), 0o600))
	}

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		IncludeDemoPaths: true,
	})
	require.NoError(t, err)

	var ratioSignals []signal.RawSignal
	for _, s := range signals {
		if s.Kind == "low-test-ratio" && strings.HasPrefix(s.FilePath, "examples") {
			ratioSignals = append(ratioSignals, s)
		}
	}
	require.NotEmpty(t, ratioSignals, "IncludeDemoPaths=true should re-enable low-test-ratio in examples/")
}

func TestPatterns_TimestampsGracefulWithoutGit(t *testing.T) {
	// Non-git directory: timestamps should remain zero without errors.
	dir := t.TempDir()
	content := strings.Repeat("package main\n", 1100)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.go"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	largeFile := filterByKind(signals, "large-file")
	require.NotEmpty(t, largeFile)

	// Timestamps should be zero since there's no git repo.
	for _, sig := range largeFile {
		assert.True(t, sig.Timestamp.IsZero(), "timestamp should be zero without git")
	}
}

// --- Mock-based tests ---

func TestPatternsCollector_Metrics(t *testing.T) {
	c := &PatternsCollector{}
	// Before collecting, metrics should be nil.
	assert.Nil(t, c.Metrics())

	dir := t.TempDir()
	content := strings.Repeat("package main\n", 1100)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.go"), []byte(content), 0o600))

	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	m := c.Metrics()
	require.NotNil(t, m)

	metrics, ok := m.(*PatternsMetrics)
	require.True(t, ok, "Metrics() should return *PatternsMetrics")
	assert.Equal(t, 1, metrics.LargeFiles)
}

func TestPatternsCollector_MetricsWithTestRatios(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "pkg")
	require.NoError(t, os.MkdirAll(subdir, 0o750))

	// Create 3 source files and 1 test.
	for i := 0; i < 3; i++ {
		require.NoError(t, os.WriteFile(
			filepath.Join(subdir, fmt.Sprintf("file%d.go", i)),
			[]byte("package pkg\n"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(subdir, "file0_test.go"),
		[]byte("package pkg\n"), 0o600))

	c := &PatternsCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	m := c.Metrics()
	require.NotNil(t, m)
	metrics := m.(*PatternsMetrics)
	assert.NotEmpty(t, metrics.DirectoryTestRatios)

	// Find pkg directory ratio.
	var found bool
	for _, r := range metrics.DirectoryTestRatios {
		if r.Path == "pkg" {
			found = true
			assert.Equal(t, 3, r.SourceFiles)
			assert.Equal(t, 1, r.TestFiles)
			assert.InDelta(t, 1.0/3.0, r.Ratio, 0.01)
		}
	}
	assert.True(t, found, "pkg directory should appear in metrics")
}

func TestPatternsCollector_WalkDirError(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		WalkDirFn: func(root string, fn fs.WalkDirFunc) error {
			return fmt.Errorf("walk error")
		},
	}

	c := &PatternsCollector{}
	_, err := c.Collect(context.Background(), "/tmp/fake", signal.CollectorOpts{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "walking repo")
	assert.Contains(t, err.Error(), "walk error")
}

func TestPatternsCollector_EvalSymlinksFailure(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target.go")
	content := strings.Repeat("package main\n", 1100)
	require.NoError(t, os.WriteFile(targetPath, []byte(content), 0o600))

	symlinkPath := filepath.Join(dir, "link.go")
	if err := os.Symlink(targetPath, symlinkPath); err != nil {
		t.Skip("symlinks not supported")
	}

	FS = &testable.MockFileSystem{
		EvalSymlinksFn: func(path string) (string, error) {
			return "", fmt.Errorf("symlink error")
		},
	}

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	// The symlink should be skipped, only the target may be detected.
	for _, sig := range signals {
		assert.NotEqual(t, "link.go", sig.FilePath,
			"symlink with failed EvalSymlinks should be skipped")
	}
}

func TestPatternsCollector_OpenFailure(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		OpenFn: func(name string) (*os.File, error) {
			return nil, fmt.Errorf("permission denied")
		},
	}

	dir := t.TempDir()
	content := strings.Repeat("package main\n", 1100)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.go"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	// Can't open files via mock, so binary check fails and files are skipped.
	assert.Empty(t, signals)
}

func TestPatternsCollector_CountLinesOpenFailure(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	callCount := 0
	FS = &testable.MockFileSystem{
		OpenFn: func(name string) (*os.File, error) {
			callCount++
			// Let isBinaryFile succeed (first call) but countLines fail (second call).
			if callCount <= 1 {
				return os.Open(name) //nolint:gosec
			}
			return nil, fmt.Errorf("second open failed")
		},
	}

	dir := t.TempDir()
	content := strings.Repeat("package main\n", 1100)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.go"), []byte(content), 0o600))

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	// countLines fails, file is skipped — no signals.
	assert.Empty(t, signals)
}

func TestPatternsCollector_StatFailureInDetectTestRoots(t *testing.T) {
	oldFS := FS
	defer func() { FS = oldFS }()

	FS = &testable.MockFileSystem{
		StatFn: func(name string) (os.FileInfo, error) {
			// Make all stat calls fail — test roots won't be detected.
			return nil, fmt.Errorf("stat error")
		},
	}

	c := &PatternsCollector{}
	c.detectTestRoots("/tmp/fake")
	assert.True(t, c.testRootsInit)
	assert.Empty(t, c.testRoots)
}

func TestEnrichTimestamps_NonGitDir(t *testing.T) {
	dir := t.TempDir()
	signals := []signal.RawSignal{
		{FilePath: "some/path.go"},
	}
	enrichTimestamps(context.Background(), dir, signals)
	assert.True(t, signals[0].Timestamp.IsZero(),
		"timestamp should remain zero when git log fails")
}

func TestEnrichTimestamps_AlreadyHasTimestamp(t *testing.T) {
	now := time.Now()
	signals := []signal.RawSignal{
		{FilePath: "some/path.go", Timestamp: now},
	}
	enrichTimestamps(context.Background(), "/nonexistent", signals)
	assert.Equal(t, now, signals[0].Timestamp,
		"timestamp should not be overwritten when already set")
}

func TestPatternsCollector_GitRootForTimestamps(t *testing.T) {
	_, dir := initGoGitRepo(t, map[string]string{
		"main.go": "package main\n" + strings.Repeat("func f() {}\n", 1100),
	})

	c := &PatternsCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		GitRoot: dir,
	})
	require.NoError(t, err)

	largeFile := filterByKind(signals, "large-file")
	require.NotEmpty(t, largeFile)
	for _, sig := range largeFile {
		assert.False(t, sig.Timestamp.IsZero(),
			"signal should have non-zero timestamp when GitRoot is set")
	}
}
