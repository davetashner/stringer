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

func TestMatchFuncStart_CSharp(t *testing.T) {
	spec := extToSpec[".cs"]
	tests := []struct {
		line string
		want string
	}{
		{"    public void Run()", "Run"},
		{"    public async Task<Dictionary<string, List<int>>> LoadAsync(int id)", "LoadAsync"},
		{"    private static int[] Build(string[] args)", "Build"},
		{"    protected internal override string Render() => _name;", "Render"},
		{"    public ItemService(ILogger<ItemService> logger)", "ItemService"},
		{"    public T Get<T>(string key) where T : class", "Get"},
		{"    public virtual int? Count(bool exact)", "Count"},
		{"    partial void OnLoaded();", "OnLoaded"},
		{"    public (List<string> Main, List<string> Sub) GetFilters(", "GetFilters"},
		{"    private (BaseItemEntity BaseItem, string[] Key)? GetItem(SqliteDataReader reader)", "GetItem"},
		{"    private (HashSet<(int Season, int Episode)> Keys, Dictionary<(int S, int E), Episode> Updatable) GetExisting(Series s)", "GetExisting"},
		{"    (int a, int b) = Split(x);", ""},
		{"    if (x > 0) Handle(x);", ""},
		{"    Task<Foo> GetAsync(int id);", "GetAsync"},
		// Not functions: statements and type declarations.
		{"    if (x > 0)", ""},
		{"    else if (y)", ""},
		{"    foreach (var item in items)", ""},
		{"    while (true)", ""},
		{"    switch (kind)", ""},
		{"    catch (Exception ex)", ""},
		{"    using (var stream = File.OpenRead(path))", ""},
		{"    return Compute(x);", ""},
		{"    await LoadAsync(id);", ""},
		{"    new Foo(1),", ""},
		{"    throw new InvalidOperationException(msg);", ""},
		{"public record Person(string Name, int Age);", ""},
		{"public sealed class Foo(int x)", ""},
		{"    private readonly ILogger _logger;", ""},
		{"    public string Name { get; set; }", ""},
		{"    [HttpGet(\"{id}\")]", ""},
	}
	for _, tt := range tests {
		name, _ := matchFuncStart(tt.line, spec, 1)
		assert.Equal(t, tt.want, name, "line: %s", tt.line)
	}
}

func TestExtractBraceBody_StopsAtSemicolonBeforeBrace(t *testing.T) {
	lines := strings.Split(`    public int Double(int x) => x * 2;
    public int Triple(int x)
    {
        return x * 3;
    }`, "\n")

	body, endIdx := extractBraceBody(lines, 0, ".cs")
	assert.Nil(t, body, "expression-bodied member has no brace body")
	assert.Equal(t, 0, endIdx)

	body, endIdx = extractBraceBody(lines, 1, ".cs")
	assert.Equal(t, []string{"    {", "        return x * 3;"}, body)
	assert.Equal(t, 4, endIdx)
}

func TestExtractBraceBody_IgnoresBracesInStrings(t *testing.T) {
	lines := strings.Split(`    public string Open()
    {
        var s = "{";
        return s + @"}";
    }
    public void Next() { }`, "\n")

	body, endIdx := extractBraceBody(lines, 0, ".cs")
	assert.Equal(t, 4, endIdx, "braces inside string literals must not extend the body")
	assert.Len(t, body, 3)
}

func TestExtractFunctions_CSharp_ExpressionBodiedDoesNotSwallow(t *testing.T) {
	lines := strings.Split(`namespace Demo;

public class Calc
{
    public int Double(int x) => x * 2;

    public int Classify(int x)
    {
        if (x > 0)
        {
            if (x > 10)
            {
                return 2;
            }
            return 1;
        }
        return 0;
    }
}`, "\n")

	funcs := extractFunctions(lines, "Calc.cs", extToSpec[".cs"], 1)
	names := make([]string, 0, len(funcs))
	for _, f := range funcs {
		names = append(names, f.FuncName)
	}
	assert.Equal(t, []string{"Classify"}, names, "Double has no body; Classify must be found on its own")
	assert.Equal(t, 7, funcs[0].StartLine)
	assert.Equal(t, 2, funcs[0].Branches)
	assert.Equal(t, 3, funcs[0].MaxNesting, "opening brace is the base indent, so the inner if sits at depth 3")
}

func TestStripStringsAndComments_CSharpVerbatim(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{`var p = @"C:\temp\" + x; if (y) {`, `var p = @"" + x; if (y) {`},
		{`var s = @"say ""if"" now"; while (z)`, `var s = @""; while (z)`},
		{`var t = $@"{a} if {b}"; for`, `var t = $@""; for`},
		{`var u = @$"{a}"; foreach`, `var u = @$""; foreach`},
		{`var v = $"{x}"; // if trailing`, `var v = $""; `},
		{`var c = '{'; if (x)`, `var c = ''; if (x)`},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, stripStringsAndComments(tt.line, ".cs"), "line: %s", tt.line)
	}
}

func TestAnalyzeBody_CSharpOperators(t *testing.T) {
	lines := strings.Split(`        foreach (var item in items)
        {
            var name = item?.Name ?? "unknown";
            var label = item.Count > 0 ? "some" : "none";
            int? maybe = null;
            total ??= 0;
        }`, "\n")

	got := analyzeBody(lines, ".cs")
	// foreach (branch) + ?. + ?? + ternary + ??= (4 conditional operators).
	assert.Equal(t, 5, got.Branches)
	assert.InDelta(t, 1.0+4.0, got.WeightedBranches, 0.001)

	// The same body in Java counts none of the C# operators and no foreach.
	javaGot := analyzeBody(lines, ".java")
	assert.Equal(t, 0, javaGot.Branches)
}

func TestComplexityCollector_CSharp(t *testing.T) {
	dir := t.TempDir()

	csCode := `using System;
using System.Collections.Generic;

namespace Demo.Services
{
    public class ItemService
    {
        private readonly Dictionary<string, int> _cache = new();

        public int Simple(int x) => x + 1;

        public string Describe(Item item) => $"{item.Name} ({item.Id})";

        public async Task<List<string>> ProcessAsync(IEnumerable<Item> items, bool strict)
        {
            var result = new List<string>();
            foreach (var item in items)
            {
                if (item == null)
                {
                    continue;
                }
                if (item.Count > 0 && item.Enabled)
                {
                    foreach (var child in item.Children)
                    {
                        if (child.IsValid || !strict)
                        {
                            switch (child.Kind)
                            {
                                case Kind.A:
                                    result.Add(child.Name ?? "a");
                                    break;
                                case Kind.B:
                                    if (child.Weight > 10)
                                    {
                                        result.Add(child.Name);
                                    }
                                    break;
                                default:
                                    result.Add(child?.Label ?? "x");
                                    break;
                            }
                        }
                        else if (child.Retry)
                        {
                            try
                            {
                                await child.LoadAsync();
                            }
                            catch (Exception ex) when (ex is TimeoutException)
                            {
                                result.Add(@"timeout {");
                            }
                        }
                    }
                }
                while (result.Count > 100)
                {
                    result.RemoveAt(0);
                }
            }
            return result;
        }

        public void Dispose()
        {
            _cache.Clear();
        }
    }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ItemService.cs"), []byte(csCode), 0o600))

	c := &ComplexityCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	complexSigs := filterByKind(signals, "complex-function")
	require.Len(t, complexSigs, 1, "only ProcessAsync should clear the default floor")
	sig := complexSigs[0]
	assert.Contains(t, sig.Title, "ProcessAsync")
	assert.Equal(t, "ItemService.cs", sig.FilePath)
	assert.Equal(t, 14, sig.Line)
	assert.GreaterOrEqual(t, sig.Confidence, 0.5)
	assert.Contains(t, sig.Tags, "refactor-candidate")
	assert.NotContains(t, sig.Tags, "test-file")

	m, ok := c.Metrics().(*ComplexityMetrics)
	require.True(t, ok)
	assert.Equal(t, 1, m.FilesAnalyzed)
	// Simple and Describe are expression-bodied (no body lines); Dispose is
	// too short. Only ProcessAsync is measured.
	require.Len(t, m.Functions, 1)
	assert.Greater(t, m.Functions[0].MaxNesting, 3)
}

func TestComplexityCollector_CSharp_TestProjectSkipped(t *testing.T) {
	dir := t.TempDir()

	testCode := `namespace Demo.Tests;
public class ItemServiceTests
{
    [Fact]
    public void Nested()
    {
        if (a) { if (b) { if (c) { if (d) { if (e) { x(); } } } } }
        if (a) { if (b) { if (c) { if (d) { if (e) { x(); } } } } }
        if (a) { if (b) { if (c) { if (d) { if (e) { x(); } } } } }
        if (a) { if (b) { if (c) { if (d) { if (e) { x(); } } } } }
        if (a) { if (b) { if (c) { if (d) { if (e) { x(); } } } } }
    }
}
`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tests", "Demo.Tests"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tests", "Demo.Tests", "ItemServiceTests.cs"), []byte(testCode), 0o600))

	c := &ComplexityCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{MinComplexityScore: 1})
	require.NoError(t, err)
	assert.Empty(t, filterByKind(signals, "complex-function"), "test project code is skipped by default")

	signals, err = c.Collect(context.Background(), dir, signal.CollectorOpts{MinComplexityScore: 1, IncludeTests: true})
	require.NoError(t, err)
	sigs := filterByKind(signals, "complex-function")
	require.Len(t, sigs, 1)
	assert.Contains(t, sigs[0].Tags, "test-file")
}
