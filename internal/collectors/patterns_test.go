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
