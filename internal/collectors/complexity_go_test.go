// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- SA4.1: Go AST function visitor tests ---

func TestGoAST_EmptyFunction(t *testing.T) {
	src := []byte(`package p
func empty() {}
`)
	infos, err := analyzeGoSource(src)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "empty", infos[0].Name)
	assert.Equal(t, "", infos[0].Receiver)
	assert.False(t, infos[0].IsExported)
}

func TestGoAST_ExportedFunction(t *testing.T) {
	src := []byte(`package p
func DoWork() {
	x := 1
	_ = x
}
`)
	infos, err := analyzeGoSource(src)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "DoWork", infos[0].Name)
	assert.True(t, infos[0].IsExported)
}

func TestGoAST_MethodWithPointerReceiver(t *testing.T) {
	src := []byte(`package p
type Server struct{}
func (s *Server) Handle() {
	return
}
`)
	infos, err := analyzeGoSource(src)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "Handle", infos[0].Name)
	assert.Equal(t, "(*Server)", infos[0].Receiver)
	assert.True(t, infos[0].IsExported)
}

func TestGoAST_MethodWithValueReceiver(t *testing.T) {
	src := []byte(`package p
type Foo struct{}
func (f Foo) Bar() {
	return
}
`)
	infos, err := analyzeGoSource(src)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "Bar", infos[0].Name)
	assert.Equal(t, "Foo", infos[0].Receiver)
}

func TestGoAST_MultipleFunctions(t *testing.T) {
	src := []byte(`package p
func first() {}
func second() {}
func third() {}
`)
	infos, err := analyzeGoSource(src)
	require.NoError(t, err)
	require.Len(t, infos, 3)
	assert.Equal(t, "first", infos[0].Name)
	assert.Equal(t, "second", infos[1].Name)
	assert.Equal(t, "third", infos[2].Name)
}

func TestGoAST_FunctionLiteralsSkipped(t *testing.T) {
	// Function literals (closures) should not appear as top-level entries.
	src := []byte(`package p
func outer() {
	f := func() {
		return
	}
	_ = f
}
`)
	infos, err := analyzeGoSource(src)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "outer", infos[0].Name)
}

func TestGoAST_VariadicParams(t *testing.T) {
	src := []byte(`package p
func variadic(args ...int) {
	_ = args
}
`)
	infos, err := analyzeGoSource(src)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "variadic", infos[0].Name)
}

func TestGoAST_SyntaxError(t *testing.T) {
	src := []byte(`package p
func broken( {
`)
	_, err := analyzeGoSource(src)
	assert.Error(t, err)
}

func TestGoAST_AnalyzeGoFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "example.go")
	src := `package p

func Hello() {
	return
}
`
	require.NoError(t, os.WriteFile(path, []byte(src), 0o600))

	infos, err := analyzeGoFile(path)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "Hello", infos[0].Name)
	assert.True(t, infos[0].IsExported)
}

func TestGoAST_AnalyzeGoFile_NonExistent(t *testing.T) {
	_, err := analyzeGoFile("/nonexistent/file.go")
	assert.Error(t, err)
}

func TestGoAST_AnalyzeGoFile_SyntaxError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.go")
	require.NoError(t, os.WriteFile(path, []byte("not go code at all"), 0o600))

	_, err := analyzeGoFile(path)
	assert.Error(t, err)
}

func TestGoAST_LineNumbers(t *testing.T) {
	src := []byte(`package p

func first() {
	return
}

func second() {
	return
}
`)
	infos, err := analyzeGoSource(src)
	require.NoError(t, err)
	require.Len(t, infos, 2)
	assert.Equal(t, 3, infos[0].StartLine)
	assert.Equal(t, 5, infos[0].EndLine)
	assert.Equal(t, 7, infos[1].StartLine)
	assert.Equal(t, 9, infos[1].EndLine)
}

func TestGoAST_BodyLineCount(t *testing.T) {
	src := []byte(`package p
func multiline() {
	a := 1
	b := 2
	c := 3
	_ = a + b + c
}
`)
	infos, err := analyzeGoSource(src)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, 4, infos[0].Lines) // 4 body lines between braces
}

func TestGoAST_Goroutine(t *testing.T) {
	src := []byte(`package p
func withGoroutine() {
	go func() {
		return
	}()
}
`)
	infos, err := analyzeGoSource(src)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "withGoroutine", infos[0].Name)
}

// --- SA4.2: Cyclomatic complexity tests ---

// Helper to parse a single function and compute cyclomatic complexity.
func parseCyclomatic(t *testing.T, src string) int {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	require.NoError(t, err)
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return cyclomaticComplexity(fset, fn)
		}
	}
	t.Fatal("no function found")
	return 0
}

func TestCyclomatic_EmptyFunction(t *testing.T) {
	cc := parseCyclomatic(t, `package p; func f() {}`)
	assert.Equal(t, 1, cc)
}

func TestCyclomatic_SingleIf(t *testing.T) {
	cc := parseCyclomatic(t, `package p
func f(x int) {
	if x > 0 {
		return
	}
}`)
	assert.Equal(t, 2, cc) // base 1 + if
}

func TestCyclomatic_IfElseIf_Else(t *testing.T) {
	cc := parseCyclomatic(t, `package p
func f(x int) {
	if x > 0 {
		return
	} else if x < 0 {
		return
	} else {
		return
	}
}`)
	assert.Equal(t, 3, cc) // base 1 + if + else-if (which is another if)
}

func TestCyclomatic_SwitchWithCases(t *testing.T) {
	cc := parseCyclomatic(t, `package p
func f(x int) {
	switch x {
	case 1:
		return
	case 2:
		return
	case 3:
		return
	default:
		return
	}
}`)
	assert.Equal(t, 4, cc) // base 1 + 3 cases (default excluded)
}

func TestCyclomatic_NestedIfInFor(t *testing.T) {
	cc := parseCyclomatic(t, `package p
func f(items []int) {
	for _, v := range items {
		if v > 0 {
			return
		}
	}
}`)
	assert.Equal(t, 3, cc) // base 1 + range-for + if
}

func TestCyclomatic_LogicalOperators(t *testing.T) {
	cc := parseCyclomatic(t, `package p
func f(a, b, c bool) {
	if a && b || c {
		return
	}
}`)
	assert.Equal(t, 4, cc) // base 1 + if + && + ||
}

func TestCyclomatic_ForLoop(t *testing.T) {
	cc := parseCyclomatic(t, `package p
func f() {
	for i := 0; i < 10; i++ {
		return
	}
}`)
	assert.Equal(t, 2, cc) // base 1 + for
}

func TestCyclomatic_SelectWithCases(t *testing.T) {
	cc := parseCyclomatic(t, `package p
func f(ch1, ch2 chan int) {
	select {
	case <-ch1:
		return
	case <-ch2:
		return
	default:
		return
	}
}`)
	assert.Equal(t, 3, cc) // base 1 + 2 comm cases (default excluded)
}

func TestCyclomatic_MultipleReturns(t *testing.T) {
	cc := parseCyclomatic(t, `package p
func f(x int) int {
	if x > 10 {
		return 1
	}
	if x > 5 {
		return 2
	}
	return 0
}`)
	assert.Equal(t, 3, cc) // base 1 + 2 ifs
}

// --- SA4.3: Cognitive complexity tests ---

func parseCognitive(t *testing.T, src string) int {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	require.NoError(t, err)
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return cognitiveComplexity(fset, fn)
		}
	}
	t.Fatal("no function found")
	return 0
}

func TestCognitive_LinearFunction(t *testing.T) {
	cc := parseCognitive(t, `package p
func f() {
	x := 1
	y := 2
	_ = x + y
}`)
	assert.Equal(t, 0, cc)
}

func TestCognitive_SingleIf(t *testing.T) {
	cc := parseCognitive(t, `package p
func f(x int) {
	if x > 0 {
		return
	}
}`)
	assert.Equal(t, 1, cc) // +1 for if (nesting 0)
}

func TestCognitive_NestedIfInIf(t *testing.T) {
	cc := parseCognitive(t, `package p
func f(x, y int) {
	if x > 0 {
		if y > 0 {
			return
		}
	}
}`)
	assert.Equal(t, 3, cc) // +1 (outer if, nesting 0) + 1+1 (inner if, nesting 1) = 3
}

func TestCognitive_ForWithNestedIf(t *testing.T) {
	cc := parseCognitive(t, `package p
func f(items []int) {
	for _, v := range items {
		if v > 0 {
			return
		}
	}
}`)
	assert.Equal(t, 3, cc) // +1 (for at nesting 0) + 1+1 (if at nesting 1) = 3
}

func TestCognitive_IfElseIfElse(t *testing.T) {
	cc := parseCognitive(t, `package p
func f(x int) {
	if x > 0 {
		return
	} else if x < 0 {
		return
	} else {
		return
	}
}`)
	assert.Equal(t, 3, cc) // +1 (if) + 1 (else if) + 1 (else) = 3
}

func TestCognitive_LogicalOps_SameOperator(t *testing.T) {
	cc := parseCognitive(t, `package p
func f(a, b, c bool) {
	if a && b && c {
		return
	}
}`)
	assert.Equal(t, 2, cc) // +1 (if) + 1 (&& sequence counts once) = 2
}

func TestCognitive_LogicalOps_MixedOperators(t *testing.T) {
	cc := parseCognitive(t, `package p
func f(a, b, c bool) {
	if a && b || c {
		return
	}
}`)
	assert.Equal(t, 3, cc) // +1 (if) + 1 (&&) + 1 (|| switch) = 3
}

func TestCognitive_Switch(t *testing.T) {
	cc := parseCognitive(t, `package p
func f(x int) {
	switch x {
	case 1:
		return
	case 2:
		return
	default:
		return
	}
}`)
	assert.Equal(t, 1, cc) // +1 for switch (case/default don't add)
}

func TestCognitive_Select(t *testing.T) {
	cc := parseCognitive(t, `package p
func f(ch chan int) {
	select {
	case <-ch:
		return
	default:
		return
	}
}`)
	assert.Equal(t, 1, cc) // +1 for select
}

func TestCognitive_DeeplyNested(t *testing.T) {
	cc := parseCognitive(t, `package p
func f(x, y, z int) {
	if x > 0 {
		for i := 0; i < y; i++ {
			if z > 0 {
				return
			}
		}
	}
}`)
	// if (nesting 0) = 1
	// for (nesting 1) = 1 + 1 = 2
	// if (nesting 2) = 1 + 2 = 3
	// total = 1 + 2 + 3 = 6
	assert.Equal(t, 6, cc)
}

func TestCognitive_Goto(t *testing.T) {
	cc := parseCognitive(t, `package p
func f() {
	goto done
done:
	return
}`)
	assert.Equal(t, 1, cc) // +1 for goto at nesting 0
}

// --- SA4.4: Nesting depth tests ---

func parseNesting(t *testing.T, src string) int {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, 0)
	require.NoError(t, err)
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return maxNestingDepth(fset, fn)
		}
	}
	t.Fatal("no function found")
	return 0
}

func TestNesting_EmptyFunction(t *testing.T) {
	nd := parseNesting(t, `package p; func f() {}`)
	assert.Equal(t, 0, nd)
}

func TestNesting_SingleIf(t *testing.T) {
	nd := parseNesting(t, `package p
func f(x int) {
	if x > 0 {
		return
	}
}`)
	assert.Equal(t, 1, nd)
}

func TestNesting_ForIfSwitch(t *testing.T) {
	nd := parseNesting(t, `package p
func f(items []int) {
	for _, v := range items {
		if v > 0 {
			switch v {
			case 1:
				return
			}
		}
	}
}`)
	assert.Equal(t, 3, nd) // for=1, if=2, switch=3
}

func TestNesting_FuncLiteral(t *testing.T) {
	nd := parseNesting(t, `package p
func f() {
	go func() {
		if true {
			return
		}
	}()
}`)
	assert.Equal(t, 2, nd) // func lit=1, if=2
}

func TestNesting_DeeplyNested(t *testing.T) {
	nd := parseNesting(t, `package p
func f(a, b, c, d int) {
	if a > 0 {
		for i := 0; i < b; i++ {
			switch c {
			case 1:
				select {
				default:
					return
				}
			}
		}
	}
}`)
	assert.Equal(t, 4, nd) // if=1, for=2, switch=3, select=4
}

func TestNesting_ElseDoesNotIncrease(t *testing.T) {
	nd := parseNesting(t, `package p
func f(x int) {
	if x > 0 {
		return
	} else {
		return
	}
}`)
	assert.Equal(t, 1, nd) // else is at same level as if
}

func TestNesting_LinearFunction(t *testing.T) {
	nd := parseNesting(t, `package p
func f() {
	x := 1
	y := 2
	_ = x + y
}`)
	assert.Equal(t, 0, nd)
}

// --- Integration: analyzeGoSource populates all metrics ---

func TestGoAST_IntegrationMetrics(t *testing.T) {
	src := []byte(`package p

func complex(x, y int) int {
	if x > 0 {
		for i := 0; i < y; i++ {
			if i > x {
				return i
			}
		}
	}
	return 0
}
`)
	infos, err := analyzeGoSource(src)
	require.NoError(t, err)
	require.Len(t, infos, 1)

	info := infos[0]
	assert.Equal(t, "complex", info.Name)
	assert.True(t, info.IsExported == false)
	assert.Equal(t, 4, info.Cyclomatic) // base 1 + if + for + if
	assert.Equal(t, 6, info.Cognitive)  // if(0)=1 + for(1)=2 + if(2)=3 = 6
	assert.Equal(t, 3, info.MaxNesting) // if=1, for=2, if=3
	assert.Equal(t, 3, info.StartLine)
	assert.Equal(t, 12, info.EndLine)
}

func TestGoAST_ReceiverString(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "value receiver",
			src:  `package p; type T struct{}; func (t T) M() {}`,
			want: "T",
		},
		{
			name: "pointer receiver",
			src:  `package p; type T struct{}; func (t *T) M() {}`,
			want: "(*T)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			infos, err := analyzeGoSource([]byte(tt.src))
			require.NoError(t, err)
			require.Len(t, infos, 1)
			assert.Equal(t, tt.want, infos[0].Receiver)
		})
	}
}

func TestGoAST_IsExported(t *testing.T) {
	assert.True(t, isGoExported("Hello"))
	assert.False(t, isGoExported("hello"))
	assert.True(t, isGoExported("X"))
	assert.False(t, isGoExported(""))
}
