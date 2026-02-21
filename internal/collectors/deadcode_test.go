// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/davetashner/stringer/internal/signal"
)

func TestDeadCodeCollector_Name(t *testing.T) {
	c := &DeadCodeCollector{}
	assert.Equal(t, "deadcode", c.Name())
}

func TestDeadCode_UnusedGoFunc(t *testing.T) {
	dir := t.TempDir()

	goCode := `package main

func usedFunc() int {
	return 42
}

func unusedHelper() string {
	return "never called"
}

func main() {
	usedFunc()
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(goCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	// unusedHelper should be detected.
	found := false
	for _, sig := range signals {
		if strings.Contains(sig.Title, "unusedHelper") {
			found = true
			assert.Equal(t, "deadcode", sig.Source)
			assert.Equal(t, "unused-function", sig.Kind)
			assert.Contains(t, sig.Tags, "dead-code")
			assert.Contains(t, sig.Tags, "cleanup-candidate")
			// Unexported Go func → 0.7 confidence.
			assert.InDelta(t, 0.7, sig.Confidence, 0.01)
			break
		}
	}
	assert.True(t, found, "expected unusedHelper to be detected as dead code")

	// usedFunc should NOT be detected (referenced in main).
	for _, sig := range signals {
		assert.NotContains(t, sig.Title, "usedFunc")
	}
}

func TestDeadCode_UsedFunc_NotFlagged(t *testing.T) {
	dir := t.TempDir()

	// Two files: one defines, the other uses.
	lib := `package mylib

func ProcessData(items []int) int {
	sum := 0
	for _, i := range items {
		sum += i
	}
	return sum
}
`
	caller := `package mylib

func DoWork() {
	ProcessData([]int{1, 2, 3})
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib.go"), []byte(lib), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "caller.go"), []byte(caller), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, sig := range signals {
		assert.NotContains(t, sig.Title, "ProcessData",
			"ProcessData is used in caller.go and should not be flagged")
	}
}

func TestDeadCode_SameFileReference(t *testing.T) {
	dir := t.TempDir()

	// helper is called within the same file.
	goCode := `package main

func helper() int {
	return 42
}

func caller() int {
	return helper()
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(goCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, sig := range signals {
		assert.NotContains(t, sig.Title, "helper",
			"helper is referenced in the same file and should not be flagged")
	}
}

func TestDeadCode_TestOnlyReference(t *testing.T) {
	dir := t.TempDir()

	lib := `package mylib

func InternalHelper() string {
	return "test only"
}
`
	testFile := `package mylib

import "testing"

func TestInternalHelper(t *testing.T) {
	InternalHelper()
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib.go"), []byte(lib), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib_test.go"), []byte(testFile), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	found := false
	for _, sig := range signals {
		if strings.Contains(sig.Title, "InternalHelper") {
			found = true
			assert.Contains(t, sig.Tags, "test-only-reference")
			assert.InDelta(t, 0.3, sig.Confidence, 0.01)
			break
		}
	}
	assert.True(t, found, "expected InternalHelper to be flagged as test-only reference")
}

func TestDeadCode_TypeDetection(t *testing.T) {
	dir := t.TempDir()

	goCode := `package main

type UsedStruct struct {
	Name string
}

type unusedConfig struct {
	Value int
}

func main() {
	_ = UsedStruct{Name: "test"}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "types.go"), []byte(goCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	foundUnused := false
	for _, sig := range signals {
		if strings.Contains(sig.Title, "unusedConfig") {
			foundUnused = true
			assert.Equal(t, "unused-type", sig.Kind)
			break
		}
	}
	assert.True(t, foundUnused, "expected unusedConfig to be detected")

	// UsedStruct should NOT be flagged (referenced in main).
	for _, sig := range signals {
		assert.NotContains(t, sig.Title, "UsedStruct")
	}
}

func TestDeadCode_TypeDetection_Python(t *testing.T) {
	dir := t.TempDir()

	pyCode := `class UsedClass:
    pass

class _UnusedHelper:
    pass

def main():
    obj = UsedClass()
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.py"), []byte(pyCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	foundUnused := false
	for _, sig := range signals {
		if strings.Contains(sig.Title, "_UnusedHelper") {
			foundUnused = true
			assert.Equal(t, "unused-type", sig.Kind)
			break
		}
	}
	assert.True(t, foundUnused, "expected _UnusedHelper to be detected")
}

func TestDeadCode_TypeDetection_Java(t *testing.T) {
	dir := t.TempDir()

	javaCode := `public class UsedClass {
    public void doWork() {}
}

class UnusedInternalClass {
    void helper() {}
}
`
	caller := `public class Main {
    public static void main(String[] args) {
        UsedClass uc = new UsedClass();
    }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "UsedClass.java"), []byte(javaCode), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Main.java"), []byte(caller), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	foundUnused := false
	for _, sig := range signals {
		if strings.Contains(sig.Title, "UnusedInternalClass") {
			foundUnused = true
			assert.Equal(t, "unused-type", sig.Kind)
			break
		}
	}
	assert.True(t, foundUnused, "expected UnusedInternalClass to be detected")
}

func TestDeadCode_TypeDetection_Rust(t *testing.T) {
	dir := t.TempDir()

	rsCode := `pub struct UsedStruct {
    pub name: String,
}

struct UnusedConfig {
    value: i32,
}

fn main() {
    let _s = UsedStruct { name: "test".to_string() };
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.rs"), []byte(rsCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	foundUnused := false
	for _, sig := range signals {
		if strings.Contains(sig.Title, "UnusedConfig") {
			foundUnused = true
			assert.Equal(t, "unused-type", sig.Kind)
			// Non-pub Rust → 0.6 confidence.
			assert.InDelta(t, 0.6, sig.Confidence, 0.01)
			break
		}
	}
	assert.True(t, foundUnused, "expected UnusedConfig to be detected")
}

func TestDeadCode_SkipList(t *testing.T) {
	dir := t.TempDir()

	goCode := `package main

func main() {}
func init() {}
func TestSomething() {}
func BenchmarkFoo() {}
func ExampleBar() {}

func ab() int { return 1 }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(goCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	// None of these should be flagged.
	for _, sig := range signals {
		assert.NotContains(t, sig.Title, "main")
		assert.NotContains(t, sig.Title, "init")
		assert.NotContains(t, sig.Title, "TestSomething")
		assert.NotContains(t, sig.Title, "BenchmarkFoo")
		assert.NotContains(t, sig.Title, "ExampleBar")
		assert.NotContains(t, sig.Title, "ab") // len <= 2
	}
}

func TestDeadCode_SkipDunderMethods(t *testing.T) {
	dir := t.TempDir()

	pyCode := `class MyClass:
    def __init__(self):
        pass

    def __str__(self):
        return "MyClass"

    def unused_method(self):
        pass
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.py"), []byte(pyCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, sig := range signals {
		assert.NotContains(t, sig.Title, "__init__")
		assert.NotContains(t, sig.Title, "__str__")
	}
}

func TestDeadCode_MultiLanguage(t *testing.T) {
	dir := t.TempDir()

	goCode := `package main

func unusedGoFunc() {}
`
	pyCode := `def unused_python_func():
    pass
`
	jsCode := `function unusedJsFunc() {
    return 42;
}
`
	rsCode := `fn unused_rust_func() -> i32 {
    42
}
`
	rbCode := `def unused_ruby_func
  42
end
`

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(goCode), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.py"), []byte(pyCode), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.js"), []byte(jsCode), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib.rs"), []byte(rsCode), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.rb"), []byte(rbCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, sig := range signals {
		// Extract function name from title "Unused function: <name>".
		parts := strings.SplitN(sig.Title, ": ", 2)
		if len(parts) == 2 {
			names[parts[1]] = true
		}
	}

	assert.True(t, names["unusedGoFunc"], "expected Go unused func")
	assert.True(t, names["unused_python_func"], "expected Python unused func")
	assert.True(t, names["unusedJsFunc"], "expected JS unused func")
	assert.True(t, names["unused_rust_func"], "expected Rust unused func")
	assert.True(t, names["unused_ruby_func"], "expected Ruby unused func")
}

func TestDeadCode_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\nfunc main() {}\n"), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	c := &DeadCodeCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{})
	assert.Error(t, err)
}

func TestDeadCode_ExcludePatterns(t *testing.T) {
	dir := t.TempDir()

	vendorDir := filepath.Join(dir, "vendor")
	require.NoError(t, os.MkdirAll(vendorDir, 0o750))

	goCode := `package vendor
func unusedVendorFunc() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(vendorDir, "lib.go"), []byte(goCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals, "vendor files should be excluded")
}

func TestDeadCode_Metrics(t *testing.T) {
	dir := t.TempDir()

	goCode := `package main

func usedFunc() int { return 42 }
func unusedFunc() int { return 0 }

func main() { usedFunc() }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(goCode), 0o600))

	c := &DeadCodeCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	assert.NotNil(t, c.Metrics())
	m := c.Metrics().(*DeadCodeMetrics)
	assert.Equal(t, 1, m.FilesAnalyzed)
	assert.GreaterOrEqual(t, m.SymbolsFound, 2) // at least usedFunc + unusedFunc (main skipped)
	assert.GreaterOrEqual(t, m.DeadSymbols, 1)
	assert.False(t, m.SkippedCapExceeded)
}

func TestDeadCode_ExportedGoInInternal(t *testing.T) {
	dir := t.TempDir()

	internalDir := filepath.Join(dir, "internal", "pkg")
	require.NoError(t, os.MkdirAll(internalDir, 0o750))

	goCode := `package pkg

func UnusedExported() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(internalDir, "lib.go"), []byte(goCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	found := false
	for _, sig := range signals {
		if strings.Contains(sig.Title, "UnusedExported") {
			found = true
			// Exported in internal/ → 0.6 confidence.
			assert.InDelta(t, 0.6, sig.Confidence, 0.01)
			break
		}
	}
	assert.True(t, found, "expected UnusedExported to be detected in internal/")
}

func TestDeadCode_ExportedGoPublicPackage(t *testing.T) {
	dir := t.TempDir()

	pkgDir := filepath.Join(dir, "pkg")
	require.NoError(t, os.MkdirAll(pkgDir, 0o750))

	goCode := `package pkg

func UnusedPublicExport() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(pkgDir, "lib.go"), []byte(goCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	found := false
	for _, sig := range signals {
		if strings.Contains(sig.Title, "UnusedPublicExport") {
			found = true
			// Exported in public package → 0.3 confidence.
			assert.InDelta(t, 0.3, sig.Confidence, 0.01)
			break
		}
	}
	assert.True(t, found, "expected UnusedPublicExport to be detected")
}

func TestDeadCode_GeneratedFileSkipped(t *testing.T) {
	dir := t.TempDir()

	goCode := "// Code generated by stringer; DO NOT EDIT.\npackage main\nfunc unusedGen() {}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gen.go"), []byte(goCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals, "generated files should be skipped")
}

func TestDeadCode_BinaryFileSkipped(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "binary.go"),
		[]byte("package main\x00\x00\x00\nfunc unused() {}\n"), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestDeadCode_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestDeadCode_MinConfidenceFilter(t *testing.T) {
	dir := t.TempDir()

	// Public package exported func has confidence 0.3.
	goCode := `package pkg

func UnusedPublic() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib.go"), []byte(goCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		MinConfidence: 0.5, // Should filter out 0.3 confidence signals.
	})
	require.NoError(t, err)

	for _, sig := range signals {
		assert.NotContains(t, sig.Title, "UnusedPublic",
			"low confidence signals should be filtered by MinConfidence")
	}
}

func TestDeadCode_TestFilesNotScanned(t *testing.T) {
	dir := t.TempDir()

	// Symbols defined in test files should not be extracted.
	testCode := `package main

func helperForTests() int {
	return 42
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main_test.go"), []byte(testCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, sig := range signals {
		assert.NotContains(t, sig.Title, "helperForTests",
			"symbols from test files should not be extracted")
	}
}

func TestDeadCode_TypeScript(t *testing.T) {
	dir := t.TempDir()

	tsCode := `export class UsedClass {
    constructor() {}
}

interface UnusedInterface {
    name: string;
}

type UnusedType = {
    value: number;
};

const x = new UsedClass();
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.ts"), []byte(tsCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	foundInterface := false
	foundType := false
	for _, sig := range signals {
		if strings.Contains(sig.Title, "UnusedInterface") {
			foundInterface = true
		}
		if strings.Contains(sig.Title, "UnusedType") {
			foundType = true
		}
		assert.NotContains(t, sig.Title, "UsedClass")
	}
	assert.True(t, foundInterface, "expected UnusedInterface to be detected")
	assert.True(t, foundType, "expected UnusedType to be detected")
}

func TestShouldSkipSymbol(t *testing.T) {
	tests := []struct {
		name string
		skip bool
	}{
		{"main", true},
		{"init", true},
		{"TestFoo", true},
		{"BenchmarkBar", true},
		{"ExampleBaz", true},
		{"ab", true},       // len <= 2
		{"x", true},        // len <= 2
		{"__init__", true}, // dunder
		{"__str__", true},  // dunder
		{"constructor", true},
		{"render", true},
		{"processData", false},
		{"HandleRequest", false},
		{"calculate", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.skip, shouldSkipSymbol(tt.name), "name=%s", tt.name)
	}
}

func TestDeadCode_IsTestFile(t *testing.T) {
	tests := []struct {
		path   string
		isTest bool
	}{
		{"foo_test.go", true},
		{"foo.test.js", true},
		{"foo.spec.ts", true},
		{"foo.test.tsx", true},
		{"test_foo.py", true},
		{"__tests__/foo.test.js", true},
		{"tests/test_foo.py", true}, // test_ prefix on .py
		{"foo.go", false},
		{"foo.js", false},
		{"app.py", false},
		{"testing.go", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.isTest, isTestFile(tt.path), "path=%s", tt.path)
	}
}

func TestIsExported(t *testing.T) {
	tests := []struct {
		name     string
		ext      string
		exported bool
	}{
		{"ProcessData", ".go", true},
		{"processData", ".go", false},
		{"_private", ".py", false},
		{"public_func", ".py", true},
		{"MyClass", ".java", true},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.exported, isExported(tt.name, tt.ext),
			"name=%s ext=%s", tt.name, tt.ext)
	}
}

func TestDeadCode_IncludePatterns(t *testing.T) {
	dir := t.TempDir()

	subDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(subDir, 0o750))

	// File in src/ should be analyzed.
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "lib.go"),
		[]byte("package lib\nfunc unusedInSrc() {}\n"), 0o600))
	// File outside src/ should be excluded.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "other.go"),
		[]byte("package other\nfunc unusedOutside() {}\n"), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		IncludePatterns: []string{"src/**"},
	})
	require.NoError(t, err)

	foundInSrc := false
	for _, sig := range signals {
		if strings.Contains(sig.Title, "unusedInSrc") {
			foundInSrc = true
		}
		assert.NotContains(t, sig.Title, "unusedOutside")
	}
	assert.True(t, foundInSrc, "expected signal from src/")
}

func TestDeadCode_RustVisibility(t *testing.T) {
	dir := t.TempDir()

	rsCode := `pub fn public_unused() -> i32 {
    42
}

fn private_unused() -> i32 {
    0
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib.rs"), []byte(rsCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	for _, sig := range signals {
		if strings.Contains(sig.Title, "public_unused") {
			assert.InDelta(t, 0.4, sig.Confidence, 0.01, "pub Rust func should have 0.4 confidence")
		}
		if strings.Contains(sig.Title, "private_unused") {
			assert.InDelta(t, 0.6, sig.Confidence, 0.01, "non-pub Rust func should have 0.6 confidence")
		}
	}
}

func TestDeadCode_ContextCancellationInSearchPhase(t *testing.T) {
	dir := t.TempDir()

	// Create enough files to get past the walk phase before cancellation
	// affects the search phase.
	for i := 0; i < 5; i++ {
		code := "package main\nfunc unused" + string(rune('A'+i)) + "Func() {}\n"
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "file"+string(rune('0'+i))+".go"),
			[]byte(code), 0o600))
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &DeadCodeCollector{}
	// Cancel after walk completes (we can't easily time this, so we just
	// verify the collector handles cancellation gracefully).
	cancel()
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{})
	// Should error due to cancellation.
	assert.Error(t, err)
}

func TestExtractSymbols_GoTypes(t *testing.T) {
	content := `package main

type MyStruct struct {
	Name string
}

type MyInterface interface {
	Method()
}

type myPrivate struct {
	x int
}
`
	syms := extractSymbols(content, "types.go", ".go")

	names := make(map[string]bool)
	for _, s := range syms {
		names[s.Name] = true
	}
	assert.True(t, names["MyStruct"], "expected MyStruct")
	assert.True(t, names["MyInterface"], "expected MyInterface")
	assert.True(t, names["myPrivate"], "expected myPrivate")
}

func TestExtractSymbols_GoFunctions(t *testing.T) {
	content := `package main

func publicFunc() {}

func (s *Server) method() {}

func privateFunc() {}
`
	syms := extractSymbols(content, "funcs.go", ".go")

	names := make(map[string]bool)
	for _, s := range syms {
		names[s.Name] = true
	}
	assert.True(t, names["publicFunc"])
	assert.True(t, names["method"])
	assert.True(t, names["privateFunc"])
}

func TestExtractSymbols_RustTypes(t *testing.T) {
	content := `pub struct PublicStruct {
    name: String,
}

struct PrivateStruct {
    value: i32,
}

pub enum PublicEnum {
    A,
    B,
}

trait MyTrait {
    fn method(&self);
}
`
	syms := extractSymbols(content, "lib.rs", ".rs")

	symMap := make(map[string]symbolDef)
	for _, s := range syms {
		symMap[s.Name] = s
	}
	assert.True(t, symMap["PublicStruct"].Exported)
	assert.False(t, symMap["PrivateStruct"].Exported)
	assert.True(t, symMap["PublicEnum"].Exported)
	assert.False(t, symMap["MyTrait"].Exported) // no pub keyword
}

func TestDeadCode_RubyClass(t *testing.T) {
	dir := t.TempDir()

	rbCode := `class UsedClass
  def work
    42
  end
end

class UnusedClass
  def helper
    0
  end
end

obj = UsedClass.new
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.rb"), []byte(rbCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	foundUnused := false
	for _, sig := range signals {
		if strings.Contains(sig.Title, "UnusedClass") {
			foundUnused = true
			assert.Equal(t, "unused-type", sig.Kind)
		}
		assert.NotContains(t, sig.Title, "UsedClass")
	}
	assert.True(t, foundUnused, "expected UnusedClass to be detected")
}

func TestDeadCode_PHPClass(t *testing.T) {
	dir := t.TempDir()

	phpCode := `<?php
class UsedService {
    public function run() { return 42; }
}

class UnusedService {
    public function run() { return 0; }
}

$svc = new UsedService();
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.php"), []byte(phpCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	foundUnused := false
	for _, sig := range signals {
		if strings.Contains(sig.Title, "UnusedService") {
			foundUnused = true
			assert.Equal(t, "unused-type", sig.Kind)
		}
		assert.NotContains(t, sig.Title, "UsedService")
	}
	assert.True(t, foundUnused, "expected UnusedService to be detected")
}

func TestDeadCode_SwiftType(t *testing.T) {
	dir := t.TempDir()

	swiftCode := `struct UsedModel {
    var name: String
}

struct UnusedModel {
    var id: Int
}

let m = UsedModel(name: "test")
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "models.swift"), []byte(swiftCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	foundUnused := false
	for _, sig := range signals {
		if strings.Contains(sig.Title, "UnusedModel") {
			foundUnused = true
			assert.Equal(t, "unused-type", sig.Kind)
		}
		assert.NotContains(t, sig.Title, "UsedModel")
	}
	assert.True(t, foundUnused, "expected UnusedModel to be detected")
}

func TestDeadCode_ScalaType(t *testing.T) {
	dir := t.TempDir()

	scalaCode := `class UsedService {
  def run(): Int = 42
}

class UnusedService {
  def run(): Int = 0
}

val svc = new UsedService()
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "App.scala"), []byte(scalaCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	foundUnused := false
	for _, sig := range signals {
		if strings.Contains(sig.Title, "UnusedService") {
			foundUnused = true
			assert.Equal(t, "unused-type", sig.Kind)
		}
		assert.NotContains(t, sig.Title, "UsedService")
	}
	assert.True(t, foundUnused, "expected UnusedService to be detected")
}

func TestDeadCode_ElixirModule(t *testing.T) {
	dir := t.TempDir()

	exCode := `defmodule UsedServer do
  def start, do: :ok
end

defmodule UnusedServer do
  def start, do: :ok
end

UsedServer.start()
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.ex"), []byte(exCode), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	foundUnused := false
	for _, sig := range signals {
		if strings.Contains(sig.Title, "UnusedServer") {
			foundUnused = true
			assert.Equal(t, "unused-type", sig.Kind)
		}
		assert.NotContains(t, sig.Title, "UsedServer")
	}
	assert.True(t, foundUnused, "expected UnusedServer to be detected")
}
