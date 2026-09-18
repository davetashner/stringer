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

func TestDeadCode_CSharp(t *testing.T) {
	dir := t.TempDir()

	service := `using System;

namespace Demo.Services;

public class ItemService : IDisposable
{
    private readonly Repo _repo = new();

    public ItemService(Repo repo) => _repo = repo;

    public int Count() => _repo.Size();

    private int Unused(int x)
    {
        return x * 2;
    }

    private static string FormatLabel(string s) => s.Trim();

    public void Dispose()
    {
        _repo.Dispose();
    }

    public override string ToString() => "ItemService";

    public override bool Equals(object? o) => o is ItemService;

    public override int GetHashCode() => 1;

    [HttpGet("{id}")]
    public IActionResult GetItem(int id)
    {
        return Ok(id);
    }
}

public sealed class Repo
{
    public int Size() => 0;
    public void Dispose() { }
}

internal record Orphan(string Name);

public interface IForgotten
{
    void Poke();
}

public static class Program
{
    public static void Main(string[] args)
    {
        var svc = new ItemService(new Repo());
        Console.WriteLine(svc.Count());
    }
}
`
	tests := `namespace Demo.Tests;

public class ItemServiceTests
{
    [Fact]
    public void CountIsZero()
    {
        var svc = new ItemService(new Repo());
        Assert.Equal(0, svc.Count());
    }
}
`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "tests"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "ItemService.cs"), []byte(service), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tests", "ItemServiceTests.cs"), []byte(tests), 0o600))
	// An entry point marks the fixture as an application, so public symbols keep the 0.4 tier
	// rather than the library public-api cap.
	program := "namespace App;\n\npublic static class Program\n{\n    public static void Main() { }\n}\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "Program.cs"), []byte(program), 0o600))

	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, signal.CollectorOpts{})
	require.NoError(t, err)

	byName := make(map[string]signal.RawSignal)
	for _, sig := range signals {
		name := sig.Title[strings.LastIndex(sig.Title, " ")+1:]
		byName[name] = sig
	}

	// Private unused method: flagged at the private-symbol confidence.
	unused, ok := byName["Unused"]
	require.True(t, ok, "expected Unused to be flagged; got %v", byName)
	assert.Equal(t, "unused-function", unused.Kind)
	assert.Equal(t, filepath.Join("src", "ItemService.cs"), unused.FilePath)
	assert.Equal(t, 13, unused.Line)
	assert.InDelta(t, 0.5, unused.Confidence, 0.001)

	// Private expression-bodied helper is also dead.
	label, ok := byName["FormatLabel"]
	require.True(t, ok, "expected FormatLabel to be flagged")
	assert.InDelta(t, 0.5, label.Confidence, 0.001)

	// Unused types: record and interface, public confidence.
	orphan, ok := byName["Orphan"]
	require.True(t, ok, "expected Orphan record to be flagged")
	assert.Equal(t, "unused-type", orphan.Kind)
	assert.InDelta(t, 0.4, orphan.Confidence, 0.001)
	_, ok = byName["IForgotten"]
	assert.True(t, ok, "expected IForgotten interface to be flagged")

	// Never dead: conventions, attributed actions, and referenced symbols.
	for _, name := range []string{"Main", "Dispose", "ToString", "Equals", "GetHashCode", "GetItem", "Count", "ItemService", "Repo", "Size", "Program"} {
		_, flagged := byName[name]
		assert.False(t, flagged, "%s must not be flagged", name)
	}
}

func TestCSharpNeverDead(t *testing.T) {
	tests := []struct {
		name  string
		sym   string
		lines []string
		want  bool
	}{
		{"Main", "Main", []string{"public static void Main()"}, true},
		{"Dispose", "Dispose", []string{"public void Dispose()"}, true},
		{"Program class", "Program", []string{"public static class Program"}, true},
		{"Fact above", "Runs", []string{"    [Fact]", "    public void Runs()"}, true},
		{"Theory with data", "Runs", []string{"    [Theory]", "    [InlineData(1)]", "    public void Runs(int x)"}, true},
		{"Test attribute suffix", "Runs", []string{"    [TestAttribute]", "    public void Runs()"}, true},
		{"HttpPost with route", "Save", []string{"    [HttpPost(\"items/{id}\")]", "    [Authorize]", "    public IActionResult Save()"}, true},
		{"attribute after blank and comment", "Get", []string{"    [Route(\"x\")]", "", "    // doc", "    public IActionResult Get()"}, true},
		{"combined list", "Runs", []string{"    [Trait(\"a\", \"b\"), Fact]", "    public void Runs()"}, true},
		{"unrelated attribute", "Old", []string{"    [Obsolete]", "    private void Old()"}, false},
		{"no attribute", "Old", []string{"    private void Old()"}, false},
		{"attribute beyond a code line", "Old", []string{"    [Fact]", "    public void A() { }", "    private void Old()"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, csharpNeverDead(tt.sym, tt.lines, len(tt.lines)-1))
		})
	}
}

func TestCSharpExported(t *testing.T) {
	assert.False(t, csharpExported("    private void Old()"))
	assert.False(t, csharpExported("    private static readonly int X = 1;"))
	assert.False(t, csharpExported("    private protected void Mixed()"))
	assert.True(t, csharpExported("    public void New()"))
	assert.True(t, csharpExported("    internal void Same()"))
	assert.True(t, csharpExported("    protected void Sub()"))
	assert.True(t, csharpExported("    void Implicit()"))
	assert.True(t, csharpExported("    public void privateKey()"), "identifier containing private is not the modifier")
}

func TestExtractSymbols_CSharpTypes(t *testing.T) {
	content := `namespace A;
public class Alpha { }
internal sealed class Beta { }
public readonly struct Gamma { }
public interface IDelta { }
public enum Epsilon { A, B }
public record Zeta(int X);
public record struct Eta(int X);
public abstract partial class Theta { }
private class Iota { }
`
	syms := extractSymbols(content, "A.cs", ".cs", false)
	var types []string
	exported := map[string]bool{}
	for _, s := range syms {
		if s.Kind == "unused-type" {
			types = append(types, s.Name)
			exported[s.Name] = s.Exported
		}
	}
	assert.Equal(t, []string{"Alpha", "Beta", "Gamma", "IDelta", "Epsilon", "Zeta", "Eta", "Theta", "Iota"}, types)
	assert.True(t, exported["Alpha"])
	assert.False(t, exported["Iota"])
	for _, s := range syms {
		assert.NotEqual(t, "unused-function", s.Kind, "record primary constructors are not functions: %s", s.Name)
	}
}
