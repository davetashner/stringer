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

func TestComplexityCollector_Name(t *testing.T) {
	c := &ComplexityCollector{}
	assert.Equal(t, "complexity", c.Name())
}

func TestMatchFuncStart_Go(t *testing.T) {
	spec := extToSpec[".go"]
	tests := []struct {
		line string
		want string
	}{
		{"func main() {", "main"},
		{"func (s *Server) Handle(w http.ResponseWriter, r *http.Request) {", "Handle"},
		{"func processFile(path string) error {", "processFile"},
		{"// func commented() {", ""},
		{"var x = 5", ""},
	}
	for _, tt := range tests {
		name, _ := matchFuncStart(tt.line, spec, 1)
		assert.Equal(t, tt.want, name, "line: %s", tt.line)
	}
}

func TestMatchFuncStart_Python(t *testing.T) {
	spec := extToSpec[".py"]
	tests := []struct {
		line string
		want string
	}{
		{"def process_file(path):", "process_file"},
		{"    def inner(x):", "inner"},
		{"class Foo:", ""},
	}
	for _, tt := range tests {
		name, _ := matchFuncStart(tt.line, spec, 1)
		assert.Equal(t, tt.want, name, "line: %s", tt.line)
	}
}

func TestMatchFuncStart_JavaScript(t *testing.T) {
	spec := extToSpec[".js"]
	tests := []struct {
		line string
		want string
	}{
		{"function processFile(path) {", "processFile"},
		{"async function fetchData() {", "fetchData"},
		{"const handler = (req, res) => {", "handler"},
		{"export function render() {", "render"},
	}
	for _, tt := range tests {
		name, _ := matchFuncStart(tt.line, spec, 1)
		assert.Equal(t, tt.want, name, "line: %s", tt.line)
	}
}

func TestMatchFuncStart_Java(t *testing.T) {
	spec := extToSpec[".java"]
	tests := []struct {
		line string
		want string
	}{
		{"    public void processFile(String path) {", "processFile"},
		{"    private static int calculate(int x) {", "calculate"},
		{"    protected List<String> getItems() {", "getItems"},
	}
	for _, tt := range tests {
		name, _ := matchFuncStart(tt.line, spec, 1)
		assert.Equal(t, tt.want, name, "line: %s", tt.line)
	}
}

func TestMatchFuncStart_Rust(t *testing.T) {
	spec := extToSpec[".rs"]
	tests := []struct {
		line string
		want string
	}{
		{"fn process_file(path: &str) -> Result<()> {", "process_file"},
		{"pub fn new() -> Self {", "new"},
		{"pub(crate) async fn handle(req: Request) -> Response {", "handle"},
	}
	for _, tt := range tests {
		name, _ := matchFuncStart(tt.line, spec, 1)
		assert.Equal(t, tt.want, name, "line: %s", tt.line)
	}
}

func TestMatchFuncStart_Ruby(t *testing.T) {
	spec := extToSpec[".rb"]
	tests := []struct {
		line string
		want string
	}{
		{"  def process_file(path)", "process_file"},
		{"  def valid?", "valid?"},
		{"  def save!", "save!"},
	}
	for _, tt := range tests {
		name, _ := matchFuncStart(tt.line, spec, 1)
		assert.Equal(t, tt.want, name, "line: %s", tt.line)
	}
}

func TestExtractBraceBody(t *testing.T) {
	lines := strings.Split(`func foo() {
	if x > 0 {
		return x
	}
	for i := 0; i < 10; i++ {
		sum += i
	}
	return sum
}`, "\n")

	body, endIdx := extractBraceBody(lines, 0)
	assert.Equal(t, 7, len(body), "expected 7 body lines")
	assert.Equal(t, 8, endIdx)
}

func TestExtractDedentBody(t *testing.T) {
	lines := strings.Split(`def process(data):
    if data is None:
        return
    for item in data:
        result.append(item)
    return result

def other():`, "\n")

	body, endIdx := extractDedentBody(lines, 0)
	// Body includes the blank line between functions (before dedent).
	assert.Equal(t, 6, len(body), "expected 6 body lines including trailing blank")
	assert.Equal(t, 6, endIdx)
}

func TestExtractKeywordBody(t *testing.T) {
	lines := strings.Split(`def process(data)
  if data.nil?
    return
  end
  data.each do |item|
    result << item
  end
  result
end`, "\n")

	body, endIdx := extractKeywordBody(lines, 0)
	assert.Equal(t, 7, len(body), "expected 7 body lines")
	assert.Equal(t, 8, endIdx)
}

func TestCountBranches(t *testing.T) {
	lines := []string{
		"	if x > 0 {",
		"		// if this is a comment",
		"		for i := range items {",
		"			if y > 0 || z > 0 {",
		"				switch mode {",
		"				case 1:",
		"				case 2:",
		"			}",
		"		}",
		"	}",
	}
	count := countBranches(lines)
	// if + for + if + || + switch + case + case = 7
	assert.Equal(t, 7, count)
}

func TestCountBranches_SkipsComments(t *testing.T) {
	lines := []string{
		"// if this is commented",
		"# elif this too",
		"  if real_code:",
	}
	count := countBranches(lines)
	assert.Equal(t, 1, count)
}

func TestComplexityConfidence(t *testing.T) {
	tests := []struct {
		score float64
		want  float64
	}{
		{20.0, 0.8},
		{15.0, 0.8},
		{8.0, 0.6},
		{11.5, 0.7},
		{6.0, 0.5},
		{7.0, 0.55},
		{5.0, 0.5},
	}
	for _, tt := range tests {
		got := complexityConfidence(tt.score)
		assert.InDelta(t, tt.want, got, 0.01, "score=%.1f", tt.score)
	}
}

func TestCompositeScore(t *testing.T) {
	// 200 lines, 15 branches → 200/50 + 15 = 4.0 + 15 = 19.0
	score := float64(200)/50.0 + float64(15)
	assert.InDelta(t, 19.0, score, 0.01)

	// 50 lines, 0 branches → 1.0
	score = float64(50)/50.0 + float64(0)
	assert.InDelta(t, 1.0, score, 0.01)

	// 50 lines, 10 branches → 11.0
	score = float64(50)/50.0 + float64(10)
	assert.InDelta(t, 11.0, score, 0.01)
}

func TestComplexityCollector_EndToEnd(t *testing.T) {
	dir := t.TempDir()

	// Create a Go file with a complex function.
	goCode := `package main

func simpleFunc() {
	return
}

func complexFunc(items []int) int {
	sum := 0
	for _, item := range items {
		if item > 0 {
			sum += item
		} else if item < -10 {
			sum -= item
		}
		switch {
		case item == 0:
			continue
		case item > 100:
			break
		}
		if sum > 1000 || sum < -1000 {
			return sum
		}
	}
	for i := 0; i < len(items); i++ {
		if items[i] > sum && items[i] < sum*2 {
			sum = items[i]
		}
	}
	return sum
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(goCode), 0o600))

	c := &ComplexityCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		MinComplexityScore: 3.0, // Lower threshold for test
	})
	require.NoError(t, err)

	// Should detect complexFunc but not simpleFunc.
	complexSigs := filterByKind(signals, "complex-function")
	require.NotEmpty(t, complexSigs, "expected at least one complex-function signal")

	found := false
	for _, sig := range complexSigs {
		if strings.Contains(sig.Title, "complexFunc") {
			found = true
			assert.Equal(t, "complexity", sig.Source)
			assert.Contains(t, sig.Tags, "complexity")
			assert.Contains(t, sig.Tags, "refactor-candidate")
			assert.True(t, sig.Confidence >= 0.5)
			break
		}
	}
	assert.True(t, found, "expected complexFunc signal")

	// simpleFunc should not appear.
	for _, sig := range complexSigs {
		assert.NotContains(t, sig.Title, "simpleFunc")
	}

	// Metrics should be populated.
	assert.NotNil(t, c.Metrics())
	m := c.Metrics().(*ComplexityMetrics)
	assert.Equal(t, 1, m.FilesAnalyzed)
	assert.GreaterOrEqual(t, m.FunctionsFound, 1)
}

func TestComplexityCollector_Python(t *testing.T) {
	dir := t.TempDir()

	pyCode := `def simple():
    return 1

def complex_function(data):
    result = []
    for item in data:
        if item > 0:
            result.append(item)
        elif item < -10:
            result.append(-item)
        else:
            pass
        for sub in item.children:
            if sub.valid and sub.active:
                result.append(sub)
            elif sub.pending or sub.deferred:
                pass
        while len(result) > 100:
            result.pop()
    return result
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.py"), []byte(pyCode), 0o600))

	c := &ComplexityCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		MinComplexityScore: 3.0,
	})
	require.NoError(t, err)

	complexSigs := filterByKind(signals, "complex-function")
	found := false
	for _, sig := range complexSigs {
		if strings.Contains(sig.Title, "complex_function") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected complex_function signal")
}

func TestComplexityCollector_MultipleFunctionsInFile(t *testing.T) {
	dir := t.TempDir()

	goCode := `package main

func funcA(x int) int {
	if x > 0 {
		for i := 0; i < x; i++ {
			if i%2 == 0 || i%3 == 0 {
				x += i
			}
			switch i {
			case 1:
				x++
			case 2:
				x--
			}
		}
	}
	return x
}

func funcB(items []string) []string {
	var result []string
	for _, item := range items {
		if len(item) > 0 && item[0] != '#' {
			result = append(result, item)
		}
		if len(item) > 100 || strings.Contains(item, "special") {
			continue
		}
		for _, ch := range item {
			if ch == '\n' {
				break
			}
		}
		switch len(item) {
		case 0:
			continue
		case 1:
			result = append(result, item+item)
		}
	}
	return result
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "multi.go"), []byte(goCode), 0o600))

	c := &ComplexityCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		MinComplexityScore: 3.0,
	})
	require.NoError(t, err)

	complexSigs := filterByKind(signals, "complex-function")
	assert.GreaterOrEqual(t, len(complexSigs), 2, "expected at least 2 complex function signals")
}

func TestComplexityCollector_ExcludePatterns(t *testing.T) {
	dir := t.TempDir()

	// Create a file in a vendor directory.
	vendorDir := filepath.Join(dir, "vendor")
	require.NoError(t, os.MkdirAll(vendorDir, 0o750))

	goCode := `package vendor
func complexVendor(x int) int {
	if x > 0 {
		for i := 0; i < 10; i++ {
			if i > 5 {
				switch i {
				case 6:
				case 7:
				case 8:
				}
			}
		}
	}
	return x
}
`
	require.NoError(t, os.WriteFile(filepath.Join(vendorDir, "lib.go"), []byte(goCode), 0o600))

	c := &ComplexityCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		MinComplexityScore: 1.0,
	})
	require.NoError(t, err)
	assert.Empty(t, signals, "vendor files should be excluded")
}

func TestComplexityCollector_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o600))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	c := &ComplexityCollector{}
	_, err := c.Collect(ctx, dir, signal.CollectorOpts{})
	assert.Error(t, err)
}

func TestComplexityCollector_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "empty.go"), []byte(""), 0o600))

	c := &ComplexityCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestComplexityCollector_BinaryFileSkipped(t *testing.T) {
	dir := t.TempDir()
	// Write a binary file with null bytes.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "binary.go"), []byte("package main\x00\x00\x00"), 0o600))

	c := &ComplexityCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)
	assert.Empty(t, signals)
}

func TestComplexityCollector_GeneratedFileSkipped(t *testing.T) {
	dir := t.TempDir()
	goCode := "// Code generated by stringer; DO NOT EDIT.\npackage main\nfunc big() {\n"
	for i := 0; i < 100; i++ {
		goCode += "\tif true { }\n"
	}
	goCode += "}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gen.go"), []byte(goCode), 0o600))

	c := &ComplexityCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		MinComplexityScore: 1.0,
	})
	require.NoError(t, err)
	assert.Empty(t, signals, "generated files should be skipped")
}

func TestComplexityCollector_Rust(t *testing.T) {
	dir := t.TempDir()

	rsCode := `pub fn process(data: &[i32]) -> Vec<i32> {
    let mut result = Vec::new();
    for item in data {
        if *item > 0 {
            result.push(*item);
        } else if *item < -10 {
            result.push(-*item);
        }
        match *item {
            0 => continue,
            1..=10 => result.push(*item * 2),
            _ => {}
        }
        while result.len() > 100 {
            result.pop();
        }
    }
    result
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "lib.rs"), []byte(rsCode), 0o600))

	c := &ComplexityCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		MinComplexityScore: 3.0,
	})
	require.NoError(t, err)

	complexSigs := filterByKind(signals, "complex-function")
	found := false
	for _, sig := range complexSigs {
		if strings.Contains(sig.Title, "process") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected process function signal from Rust file")
}

func TestComplexityCollector_Ruby(t *testing.T) {
	dir := t.TempDir()

	rbCode := `def process(data)
  result = []
  data.each do |item|
    if item > 0
      result << item
    elsif item < -10
      result << -item
    end
    case item
    when 0
      next
    when 1..10
      result << item * 2
    end
    while result.length > 100
      result.pop
    end
  end
  result
end
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.rb"), []byte(rbCode), 0o600))

	c := &ComplexityCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		MinComplexityScore: 3.0,
	})
	require.NoError(t, err)

	complexSigs := filterByKind(signals, "complex-function")
	found := false
	for _, sig := range complexSigs {
		if strings.Contains(sig.Title, "process") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected process function signal from Ruby file")
}

func TestComplexityCollector_MinFunctionLines(t *testing.T) {
	dir := t.TempDir()

	// A short function with branches — should be skipped with high minLines.
	goCode := `package main

func tiny(x int) int {
	if x > 0 {
		return x
	}
	return -x
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tiny.go"), []byte(goCode), 0o600))

	c := &ComplexityCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{
		MinFunctionLines:   100, // Very high threshold.
		MinComplexityScore: 0.1,
	})
	require.NoError(t, err)
	assert.Empty(t, signals, "tiny functions should be skipped with high minLines")
}

func TestCountNonBlank(t *testing.T) {
	lines := []string{
		"	x := 1",
		"",
		"	y := 2",
		"",
		"",
		"	return x + y",
	}
	assert.Equal(t, 3, countNonBlank(lines))
}

func TestLeadingSpaces(t *testing.T) {
	assert.Equal(t, 0, leadingSpaces("hello"))
	assert.Equal(t, 4, leadingSpaces("    hello"))
	assert.Equal(t, 4, leadingSpaces("\thello"))
	assert.Equal(t, 8, leadingSpaces("\t\thello"))
}

func TestExtractFunctions_JavaScriptArrow(t *testing.T) {
	spec := extToSpec[".js"]
	lines := strings.Split(`const handler = (req, res) => {
  if (req.method === 'GET') {
    for (const item of items) {
      if (item.valid && item.active) {
        res.send(item);
      }
    }
  } else if (req.method === 'POST') {
    switch (req.body.type) {
    case 'create':
      break;
    case 'update':
      break;
    }
  }
};`, "\n")

	funcs := extractFunctions(lines, "handler.js", spec, 1)
	require.NotEmpty(t, funcs)
	assert.Equal(t, "handler", funcs[0].FuncName)
	assert.True(t, funcs[0].Branches > 0)
}

func TestExtractFunctions_JavaMethod(t *testing.T) {
	spec := extToSpec[".java"]
	lines := strings.Split(`    public List<String> process(List<String> items) {
        List<String> result = new ArrayList<>();
        for (String item : items) {
            if (item != null && !item.isEmpty()) {
                result.add(item);
            } else if (item == null) {
                continue;
            }
            switch (item.length()) {
            case 0:
                break;
            case 1:
                result.add(item + item);
                break;
            }
        }
        return result;
    }`, "\n")

	funcs := extractFunctions(lines, "Processor.java", spec, 1)
	require.NotEmpty(t, funcs)
	assert.Equal(t, "process", funcs[0].FuncName)
	assert.True(t, funcs[0].Branches > 0)
}
