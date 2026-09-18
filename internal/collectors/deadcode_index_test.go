// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// legacyIsDeadSymbol is a verbatim copy of the pre-index reference search
// (v1.10.0 deadcode.go isDeadSymbol): for every file, strings.Contains
// pre-filter then a `\bname\b` regexp over the whole content. It is the
// oracle the indexed implementation must agree with.
func legacyIsDeadSymbol(c *DeadCodeCollector, sym *symbolDef, files []fileContents) (dead bool, testOnly bool) {
	pat := c.wordBoundary(sym.Name)
	foundInNonTest := false
	foundInTest := false

	for i := range files {
		fc := &files[i]
		if !strings.Contains(fc.content, sym.Name) {
			continue
		}
		if fc.relPath == sym.FilePath {
			if len(pat.FindAllStringIndex(fc.content, -1)) > 1 {
				return false, false
			}
			continue
		}
		if pat.MatchString(fc.content) {
			if fc.isTest {
				foundInTest = true
			} else {
				foundInNonTest = true
			}
		}
	}

	if foundInNonTest {
		return false, false
	}
	if foundInTest {
		return false, true
	}
	return true, false
}

func newIndexCollector() *DeadCodeCollector {
	return &DeadCodeCollector{regexCache: make(map[string]*regexp.Regexp)}
}

// assertIndexMatchesLegacy checks every symbol against both implementations.
func assertIndexMatchesLegacy(t *testing.T, files []fileContents, symbols []symbolDef) {
	t.Helper()
	c := newIndexCollector()
	idx := buildSymbolIndex(files, symbols)
	for i := range symbols {
		sym := &symbols[i]
		wantDead, wantTestOnly := legacyIsDeadSymbol(c, sym, files)
		gotDead, gotTestOnly := c.isDeadSymbol(sym, idx)
		assert.Equal(t, wantDead, gotDead, "dead mismatch for %q in %s", sym.Name, sym.FilePath)
		assert.Equal(t, wantTestOnly, gotTestOnly, "testOnly mismatch for %q in %s", sym.Name, sym.FilePath)
	}
}

func TestSymbolIndex_MatchesLegacy_Fixtures(t *testing.T) {
	files := []fileContents{
		{relPath: "a.go", content: "package a\n\nfunc Foo() {}\nfunc FooBar() { Foo() }\nfunc foo_bar() {}\nfunc Lonely() {}\n"},
		{relPath: "b.go", content: "package a\n\nfunc other() { FooBar(); x := foo_bar }\nvar _ = Zed\n"},
		{relPath: "a_test.go", content: "package a\n\nfunc TestLonely(t *T) { Lonely() }\nfunc TestQux(t *T) { Qux() }\n", isTest: true},
		{relPath: "c.go", content: "package a\n\nfunc Qux() {}\nfunc Zed() {}\nfunc Twice() {}\nvar _ = Twice\n"},
		// Substring traps: Foo inside FooBar/MyFoo/foo_bar must not count.
		{relPath: "d.go", content: "package a\n\nfunc MyFoo() { FooBarBaz(); _foo_bar_() }\nvar fooBar = 1\n"},
		// Non-ASCII around identifiers: Go's \b is ASCII-only, so "éAccent"
		// still matches "Accent" at a boundary while "Accenté" does too.
		{relPath: "e.go", content: "package a\n\n// naïve comment éAccent Accenté 日本語Kanji\nfunc Accent() {}\nfunc Kanji() {}\nfunc Naive() {}\n"},
		// Ruby predicate/bang methods and an Elixir dotted module name.
		{relPath: "r.rb", content: "class K\n  def valid?\n  end\n  def save!\n  end\n  def check\n    valid?\n  end\nend\n"},
		{relPath: "s.rb", content: "K.new.save!\nK.new.validx\nsave\n"},
		{relPath: "m.ex", content: "defmodule Foo.Bar do\nend\ndefmodule Foo.Baz do\nend\ndefmodule Solo.Mod do\nend\n"},
		{relPath: "n.ex", content: "Foo.Bar.hello()\nFoo .Baz\nSolo.Modx\n"},
		{relPath: "n_test.exs", content: "Solo.Mod.run()\n", isTest: true},
		// Empty file and a file with only punctuation.
		{relPath: "empty.go", content: ""},
		{relPath: "punct.go", content: "!!! ??? ... <=> ___\n"},
	}
	symbols := []symbolDef{
		{Name: "Foo", FilePath: "a.go"},
		{Name: "FooBar", FilePath: "a.go"},
		{Name: "foo_bar", FilePath: "a.go"},
		{Name: "Lonely", FilePath: "a.go"},
		{Name: "Qux", FilePath: "c.go"},
		{Name: "Zed", FilePath: "c.go"},
		{Name: "Twice", FilePath: "c.go"},
		{Name: "MyFoo", FilePath: "d.go"},
		{Name: "Accent", FilePath: "e.go"},
		{Name: "Kanji", FilePath: "e.go"},
		{Name: "Naive", FilePath: "e.go"},
		{Name: "valid?", FilePath: "r.rb"},
		{Name: "save!", FilePath: "r.rb"},
		{Name: "check", FilePath: "r.rb"},
		{Name: "Foo.Bar", FilePath: "m.ex"},
		{Name: "Foo.Baz", FilePath: "m.ex"},
		{Name: "Solo.Mod", FilePath: "m.ex"},
		// Symbols that appear nowhere, or only as substrings.
		{Name: "Missing", FilePath: "a.go"},
		{Name: "oo", FilePath: "a.go"},
		{Name: "___", FilePath: "punct.go"},
		{Name: "<=>", FilePath: "punct.go"},
		{Name: "?..", FilePath: "punct.go"},
		// A symbol whose defining file is not in the file set.
		{Name: "Foo", FilePath: "ghost.go"},
	}
	assertIndexMatchesLegacy(t, files, symbols)

	// Spot-check a few expected outcomes so the oracle itself is exercised.
	c := newIndexCollector()
	idx := buildSymbolIndex(files, symbols)
	check := func(name, file string, wantDead, wantTestOnly bool) {
		t.Helper()
		dead, testOnly := c.isDeadSymbol(&symbolDef{Name: name, FilePath: file}, idx)
		assert.Equal(t, wantDead, dead, "%s dead", name)
		assert.Equal(t, wantTestOnly, testOnly, "%s testOnly", name)
	}
	check("Foo", "a.go", false, false)    // called in a.go twice
	check("Lonely", "a.go", false, true)  // only a_test.go
	check("Qux", "c.go", false, true)     // only a_test.go
	check("Zed", "c.go", false, false)    // referenced in b.go
	check("Twice", "c.go", false, false)  // used twice in same file
	check("MyFoo", "d.go", true, false)   // never referenced
	check("Accent", "e.go", false, false) // ASCII boundary next to é
	check("Kanji", "e.go", false, false)  // ASCII boundary next to CJK
	check("Naive", "e.go", true, false)   // "naïve" is not "Naive"
	// Predicate/bang names drop the trailing `\b` (stringer-nxx.3): valid?
	// is called inside r.rb, save! from s.rb, and "validx"/"save" are not
	// references.
	check("valid?", "r.rb", false, false)
	check("save!", "r.rb", false, false)
	check("Foo.Bar", "m.ex", false, false) // Foo.Bar.hello() in n.ex
	check("Foo.Baz", "m.ex", true, false)  // "Foo .Baz" is not Foo.Baz
	check("Solo.Mod", "m.ex", false, true) // only n_test.exs
	check("Missing", "a.go", true, false)  // nowhere
	check("<=>", "punct.go", true, false)  // \b<=>\b needs word bytes around it
}

// TestSymbolIndex_MatchesLegacy_Randomized fuzzes both implementations with
// seeded random content drawn from an alphabet that mixes word bytes,
// punctuation, and multi-byte UTF-8, plus symbol names that include the
// non-word bytes real extractors can produce (? ! .).
func TestSymbolIndex_MatchesLegacy_Randomized(t *testing.T) {
	rng := rand.New(rand.NewSource(42)) //nolint:gosec // deterministic test fixture

	words := []string{"Foo", "FooBar", "foo_bar", "foo", "Bar", "x1", "_x", "É", "日本", "valid", "save", "Mod", "a", "ab"}
	glue := []string{" ", "\n", ".", "?", "!", "(", ")", "é", "、", "", "__", "-", "9"}
	names := []string{"Foo", "FooBar", "foo_bar", "foo", "Bar", "x1", "_x", "valid?", "save!", "Foo.Bar", "Mod.Mod", "a.b", "ab", "?.!", "É"}

	for iter := 0; iter < 300; iter++ {
		nFiles := 1 + rng.Intn(6)
		files := make([]fileContents, nFiles)
		for i := range files {
			var sb strings.Builder
			for n := rng.Intn(40); n > 0; n-- {
				sb.WriteString(words[rng.Intn(len(words))])
				sb.WriteString(glue[rng.Intn(len(glue))])
			}
			files[i] = fileContents{
				relPath: fmt.Sprintf("f%d.go", i),
				content: sb.String(),
				isTest:  rng.Intn(3) == 0,
			}
		}
		var symbols []symbolDef
		for n := 1 + rng.Intn(8); n > 0; n-- {
			symbols = append(symbols, symbolDef{
				Name:     names[rng.Intn(len(names))],
				FilePath: files[rng.Intn(nFiles)].relPath,
			})
		}
		assertIndexMatchesLegacy(t, files, symbols)
	}
}

func TestSymbolIndex_Tokenizer(t *testing.T) {
	for _, b := range []byte("abcXYZ019_") {
		assert.True(t, isWordByte(b), "%q", b)
	}
	for _, b := range []byte(" .?!-\n\t\xc3\xa9") {
		assert.False(t, isWordByte(b), "%q", b)
	}

	assert.True(t, isWordOnly("Foo_1"))
	assert.False(t, isWordOnly(""))
	assert.False(t, isWordOnly("valid?"))
	assert.False(t, isWordOnly("É"))

	assert.Equal(t, []string{"Foo", "Bar"}, nameSegments("Foo.Bar"))
	assert.Equal(t, []string{"valid"}, nameSegments("valid?"))
	assert.Equal(t, []string{"caf"}, nameSegments("café"))
	assert.Nil(t, nameSegments("<=>"))
	assert.Nil(t, nameSegments(""))
	assert.Equal(t, []string{"a", "b", "c"}, nameSegments("..a.b..c"))
}

func TestSymbolIndex_Build(t *testing.T) {
	files := []fileContents{
		{relPath: "a.go", content: "Foo Foo Bar"},
		{relPath: "b.go", content: "Foo"},
		{relPath: "c.go", content: "Baz"},
	}
	idx := buildSymbolIndex(files, []symbolDef{{Name: "Foo"}, {Name: "Bar.Qux"}, {Name: "Foo"}})

	assert.Equal(t, []fileOcc{{file: 0, count: 2}, {file: 1, count: 1}}, idx.occ["Foo"])
	assert.Equal(t, []fileOcc{{file: 0, count: 1}}, idx.occ["Bar"])
	// Segment of a dotted name is indexed even when it never occurs.
	_, ok := idx.occ["Qux"]
	assert.True(t, ok)
	assert.Nil(t, idx.occ["Qux"])
	// Tokens that are not symbol names are not indexed.
	_, ok = idx.occ["Baz"]
	assert.False(t, ok)
}

func TestSymbolIndex_CandidateFiles(t *testing.T) {
	files := []fileContents{
		{relPath: "0.ex", content: "Foo Bar"},
		{relPath: "1.ex", content: "Foo"},
		{relPath: "2.ex", content: "Bar Foo"},
		{relPath: "3.ex", content: "Bar"},
	}
	idx := buildSymbolIndex(files, []symbolDef{{Name: "Foo.Bar"}, {Name: "Foo.Nope"}})

	assert.Equal(t, []int{0, 2}, idx.candidateFiles("Foo.Bar"))
	assert.Empty(t, idx.candidateFiles("Foo.Nope"))
	// No word segments: every file is a candidate.
	assert.Equal(t, []int{0, 1, 2, 3}, idx.candidateFiles("<=>"))

	assert.True(t, hasFile([]fileOcc{{file: 1}, {file: 3}, {file: 7}}, 3))
	assert.False(t, hasFile([]fileOcc{{file: 1}, {file: 3}, {file: 7}}, 4))
	assert.False(t, hasFile(nil, 0))
}

// synthDeadCodeFixture builds nFiles source files each defining nSyms/nFiles
// symbols, with cross-file references so lookups exercise every branch.
func synthDeadCodeFixture(nFiles, nSyms int) ([]fileContents, []symbolDef) {
	files := make([]fileContents, 0, nFiles)
	symbols := make([]symbolDef, 0, nSyms)
	perFile := nSyms / nFiles
	if perFile == 0 {
		perFile = 1
	}
	for f := 0; f < nFiles; f++ {
		rel := fmt.Sprintf("pkg%d/file%d.go", f%50, f)
		var sb strings.Builder
		sb.WriteString("package p\n\nimport \"fmt\"\n\n")
		for s := 0; s < perFile && len(symbols) < nSyms; s++ {
			name := fmt.Sprintf("Symbol%dOf%d", s, f)
			fmt.Fprintf(&sb, "func %s(a, b int) int {\n\tfmt.Println(a, b)\n\treturn a + b\n}\n\n", name)
			symbols = append(symbols, symbolDef{Name: name, FilePath: rel, Kind: "unused-function", Language: ".go"})
		}
		// Reference a symbol from the previous file every third file, and pad
		// with filler identifiers so the content is realistically sized.
		if f%3 == 0 && f > 0 {
			fmt.Fprintf(&sb, "var _ = Symbol0Of%d\n", f-1)
		}
		for i := 0; i < 60; i++ {
			fmt.Fprintf(&sb, "// filler%d lorem ipsum dolor sit amet, consectetur adipiscing elit %d\n", i, f)
		}
		files = append(files, fileContents{relPath: rel, content: sb.String(), isTest: f%7 == 0})
	}
	return files, symbols
}

// benchSizes are (files, symbols) pairs: the bead's 2,000 x 200 case and a
// denser one closer to real repos, where the legacy cost grows with symbols.
var benchSizes = [][2]int{{2000, 200}, {2000, 2000}}

func BenchmarkDeadCodeReferenceSearch_Indexed(b *testing.B) {
	for _, sz := range benchSizes {
		files, symbols := synthDeadCodeFixture(sz[0], sz[1])
		b.Run(fmt.Sprintf("files=%d/symbols=%d", sz[0], sz[1]), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				c := newIndexCollector()
				idx := buildSymbolIndex(files, symbols)
				for j := range symbols {
					c.isDeadSymbol(&symbols[j], idx)
				}
			}
		})
	}
}

func BenchmarkDeadCodeReferenceSearch_Legacy(b *testing.B) {
	for _, sz := range benchSizes {
		files, symbols := synthDeadCodeFixture(sz[0], sz[1])
		b.Run(fmt.Sprintf("files=%d/symbols=%d", sz[0], sz[1]), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				c := newIndexCollector()
				for j := range symbols {
					legacyIsDeadSymbol(c, &symbols[j], files)
				}
			}
		})
	}
}

func TestSymbolIndex_SyntheticAgreesWithLegacy(t *testing.T) {
	files, symbols := synthDeadCodeFixture(100, 200)
	require.Len(t, symbols, 200)
	assertIndexMatchesLegacy(t, files, symbols)
}
