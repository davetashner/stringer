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

// goTableTest is a table-driven Go test whose case table and loop push
// cyclomatic well past the AST threshold (stringer-nxx.5: gin's
// TestTreeFindCaseInsensitivePath was reported at 0.90).
const goTableTest = `package main

import "testing"

func TestTreeFind(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"a", "A", true},
		{"b", "B", true},
		{"c", "C", false},
	}
	for _, tt := range tests {
		got, ok := find(tt.in)
		if ok != tt.ok {
			t.Errorf("ok mismatch for %s", tt.in)
		}
		if got != tt.want && tt.ok {
			t.Errorf("want %s got %s", tt.want, got)
		}
		if tt.in == "" {
			t.Fatal("empty")
		} else if len(tt.in) > 1 {
			t.Fatal("long")
		}
		switch tt.in {
		case "a":
			continue
		case "b":
			break
		}
		for i := 0; i < 3; i++ {
			if i == 2 || ok {
				break
			}
		}
	}
}
`

// jsDescribeSuite mirrors express's test/app.js: a mocha describe callback
// full of nested it callbacks and branches.
const jsDescribeSuite = `describe('app.render', function(){
  it('should render', function(done){
    for (var i = 0; i < 10; i++) {
      if (i % 2) {
        if (i > 5 && i < 9) {
          done();
        } else if (i === 3) {
          done();
        }
      } else if (i === 4 || i === 6) {
        while (i < 8) {
          i++;
        }
      }
    }
  })
  it('should fail', function(done){
    if (done) {
      if (done.length) {
        switch (done.length) {
          case 1: break;
          case 2: break;
        }
      }
    }
  })
})
`

// javaSmallMethod is the kafka shape from the benchmark: 16 lines, 4
// branches (nested three deep), score 7.3, confidence 0.42 under the old
// floor of 6.
const javaSmallMethod = `public class Handler {
    public int handle(int x, int y) {
        int result = 0;
        for (int i = 0; i < 3; i++) {
            if (x > i) {
                if (y > i) {
                    result += i;
                }
            }
        }
        if (result > 100) {
            result = 100;
        }
        result += x;
        result += y;
        result += 1;
        result += 2;
        return result;
    }
}
`

// rustInlineTests is a src/ file with an inline #[cfg(test)] module.
const rustInlineTests = `pub fn add(a: i32, b: i32) -> i32 {
    a + b
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn exercises_add() {
        for i in 0..10 {
            if i % 2 == 0 {
                if add(i, 1) > 5 && i < 9 {
                    assert!(true);
                } else if i == 4 {
                    assert!(true);
                }
            } else if i == 3 || i == 7 {
                while i < 8 {
                    break;
                }
            }
        }
    }

    #[test]
    fn plain_test() {
        for i in 0..10 {
            if i % 2 == 0 {
                if add(i, 1) > 5 && i < 9 {
                    assert!(true);
                } else if i == 4 {
                    assert!(true);
                }
            } else if i == 3 || i == 7 {
                while i < 8 {
                    break;
                }
            }
        }
    }
}
`

func collectComplexity(t *testing.T, dir string, opts signal.CollectorOpts) []signal.RawSignal {
	t.Helper()
	c := &ComplexityCollector{}
	signals, err := c.Collect(context.Background(), dir, opts)
	require.NoError(t, err)
	return filterByKind(signals, "complex-function")
}

func writeFixture(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
}

func TestComplexityCollector_GoTableTestSkippedByDefault(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "tree_test.go", goTableTest)

	sigs := collectComplexity(t, dir, signal.CollectorOpts{})
	assert.Empty(t, sigs, "table-driven _test.go must not be a complexity hot spot")

	// include_tests restores the finding, tagged so it can be filtered.
	sigs = collectComplexity(t, dir, signal.CollectorOpts{IncludeTests: true})
	require.Len(t, sigs, 1)
	assert.Contains(t, sigs[0].Title, "TestTreeFind")
	assert.Contains(t, sigs[0].Tags, "test-file")
	assert.Contains(t, sigs[0].Tags, "ast-analyzed")
}

func TestComplexityCollector_JSDescribeInTestDirSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "test/app.js", jsDescribeSuite)

	sigs := collectComplexity(t, dir, signal.CollectorOpts{MinComplexityScore: 1})
	assert.Empty(t, sigs, "describe callbacks under test/ must be suppressed")

	sigs = collectComplexity(t, dir, signal.CollectorOpts{MinComplexityScore: 1, IncludeTests: true})
	require.Len(t, sigs, 1)
	assert.Contains(t, sigs[0].Title, `describe("app.render") callback`)
	assert.Contains(t, sigs[0].Tags, "test-file")
	assert.NotContains(t, sigs[0].Title, "Complex function: describe (")
}

func TestComplexityCollector_JSDescribeOutsideTestDirIsStillTest(t *testing.T) {
	dir := t.TempDir()
	// Not a test-named file and not in a test dir: the describe call alone
	// marks it as test code.
	writeFixture(t, dir, "suite.js", jsDescribeSuite)

	sigs := collectComplexity(t, dir, signal.CollectorOpts{MinComplexityScore: 1})
	assert.Empty(t, sigs)

	sigs = collectComplexity(t, dir, signal.CollectorOpts{MinComplexityScore: 1, IncludeTests: true})
	require.Len(t, sigs, 1)
	assert.Contains(t, sigs[0].Title, `describe("app.render") callback`)
}

func TestComplexityCollector_JavaSmallMethodBelowRegexFloor(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "src/main/java/Handler.java", javaSmallMethod)

	// Default floor (12) suppresses the 16-line/4-branch method.
	sigs := collectComplexity(t, dir, signal.CollectorOpts{})
	assert.Empty(t, sigs, "sub-0.5 confidence signal must not be emitted by default")

	// The old floor restores it: config override for previous behaviour.
	sigs = collectComplexity(t, dir, signal.CollectorOpts{MinComplexityScore: 6})
	require.Len(t, sigs, 1)
	assert.Contains(t, sigs[0].Title, "handle")
	assert.Less(t, sigs[0].Confidence, 0.5)
	assert.Contains(t, sigs[0].Title, "score 7.3")
	assert.Contains(t, sigs[0].Description, "fires at score ≥ 6.0")
	assert.Contains(t, sigs[0].Description, "include_tests")
}

func TestComplexityCollector_JavaTestRootSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "clients/src/test/java/HandlerFixture.java", javaSmallMethod)

	sigs := collectComplexity(t, dir, signal.CollectorOpts{MinComplexityScore: 1})
	assert.Empty(t, sigs, "nested src/test/java must be treated as test code")
}

func TestComplexityCollector_RegexFloorDefaultIsHalfConfidence(t *testing.T) {
	// A regex score of exactly the default floor maps to confidence 0.5,
	// so nothing below 0.5 is emitted unless min_complexity_score is lowered.
	assert.InDelta(t, 0.5, complexityConfidence(defaultMinRegexScore), 1e-9)
	assert.Less(t, complexityConfidence(defaultMinRegexScore-0.1), 0.5)
}

func TestComplexityCollector_GoASTThresholdUnchanged(t *testing.T) {
	dir := t.TempDir()
	// Cyclomatic 6 (five branches) in a production Go file must still fire
	// at the unchanged Go default even though the regex floor rose to 12.
	writeFixture(t, dir, "main.go", `package main

func route(x int) int {
	if x == 1 {
		return 1
	}
	if x == 2 {
		return 2
	}
	if x == 3 {
		return 3
	}
	if x == 4 {
		return 4
	}
	if x == 5 {
		return 5
	}
	return 0
}
`)

	sigs := collectComplexity(t, dir, signal.CollectorOpts{})
	require.Len(t, sigs, 1)
	assert.Contains(t, sigs[0].Title, "route")
	assert.NotContains(t, sigs[0].Tags, "test-file")
}

func TestComplexityCollector_RustTestAttrSkipped(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "src/lib.rs", rustInlineTests)

	sigs := collectComplexity(t, dir, signal.CollectorOpts{MinComplexityScore: 1})
	for _, s := range sigs {
		assert.NotContains(t, s.Title, "exercises_add")
		assert.NotContains(t, s.Title, "plain_test")
	}

	sigs = collectComplexity(t, dir, signal.CollectorOpts{MinComplexityScore: 1, IncludeTests: true})
	var names []string
	for _, s := range sigs {
		names = append(names, s.Title)
		if strings.Contains(s.Title, "exercises_add") || strings.Contains(s.Title, "plain_test") {
			assert.Contains(t, s.Tags, "test-file")
		}
	}
	assert.Len(t, sigs, 2, "both #[tokio::test] and #[test] fns emitted when include_tests is set: %v", names)
}

func TestComplexityCollector_MetricsExcludeSkippedTests(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "tree_test.go", goTableTest)
	writeFixture(t, dir, "suite.js", jsDescribeSuite)

	c := &ComplexityCollector{}
	_, err := c.Collect(context.Background(), dir, signal.CollectorOpts{MinComplexityScore: 1})
	require.NoError(t, err)
	m, ok := c.Metrics().(*ComplexityMetrics)
	require.True(t, ok)
	assert.Equal(t, 0, m.FunctionsFound)
	assert.Equal(t, 1, m.FilesAnalyzed, "the _test.go is skipped before analysis; suite.js is analyzed and filtered")
}

func TestIsComplexityTestFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"foo_test.go", true},
		{"internal/collectors/complexity.go", false},
		{"test/app.js", true},
		{"test/res.send.js", true},
		{"tests/test_views.py", true},
		{"django/test/client.py", true},
		{"src/__tests__/App.js", true},
		{"spec/models/user_spec.rb", true},
		{"lib/user.rb", false},
		{"clients/src/test/java/Foo.java", true},
		{"clients/src/main/java/Foo.java", false},
		{"pkg/testdata/gen.go", true},
		{"contest/scoring.py", false},
		{"attestation/verify.go", false},
		{"lib.rs", false},
		{"tokio/tests/rt_basic.rs", true},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, isComplexityTestFile(filepath.FromSlash(tt.path)), tt.path)
	}
}

func TestTestCallbackName(t *testing.T) {
	assert.Equal(t, `describe("app.render") callback`, testCallbackName("describe", `describe('app.render', function(){`))
	assert.Equal(t, `it("GET /") callback`, testCallbackName("it", `  it("GET /", async () => {`))
	assert.Equal(t, "beforeEach callback", testCallbackName("beforeEach", `beforeEach(function(){`))
	assert.Equal(t, "it callback", testCallbackName("it", `it('', function(){`))

	long := strings.Repeat("x", maxTestCallbackLabel+10)
	got := testCallbackName("test", "test('"+long+"', () => {")
	assert.Contains(t, got, "…")
	assert.Less(t, len(got), len(long)+20)
}

func TestPrecededByRustTestAttr(t *testing.T) {
	lines := []string{
		"#[test]",
		"#[allow(dead_code)]",
		"",
		"fn a() {",
		"fn b() {",
		"#[derive(Debug)]",
		"fn c() {",
		"#[cfg(test)]",
		"fn d() {",
		"#[bench]",
		"fn e() {",
		"let x = 1;",
		"let y = 2;",
		"let z = 3;",
		"fn f() {",
	}
	assert.True(t, precededByRustTestAttr(lines, 3), "attribute above another attribute and a blank")
	assert.False(t, precededByRustTestAttr(lines, 4), "previous line is code: attribute block does not extend past it")
	assert.False(t, precededByRustTestAttr(lines, 6), "derive is not a test attribute")
	assert.True(t, precededByRustTestAttr(lines, 8), "#[cfg(test)]")
	assert.True(t, precededByRustTestAttr(lines, 10), "#[bench]")
	assert.False(t, precededByRustTestAttr(lines, 14), "statements end the attribute block")
	assert.False(t, precededByRustTestAttr(lines, 0), "no preceding lines")
}

func TestComplexityTags(t *testing.T) {
	assert.Equal(t, []string{"complexity", "go"}, complexityTags(FunctionComplexity{}, "complexity", "go"))
	assert.Equal(t, []string{"complexity", "test-file"}, complexityTags(FunctionComplexity{IsTest: true}, "complexity"))
}
