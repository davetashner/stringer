// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/davetashner/stringer/internal/signal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeDeadCodeFixture writes files into dir and runs the collector.
func writeDeadCodeFixture(t *testing.T, files map[string]string, opts signal.CollectorOpts) []signal.RawSignal {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, opts)
	require.NoError(t, err)
	return signals
}

// findSignal returns the signal whose title mentions name, or nil.
func findSignal(signals []signal.RawSignal, name string) *signal.RawSignal {
	for i := range signals {
		if strings.HasSuffix(signals[i].Title, ": "+name) {
			return &signals[i]
		}
	}
	return nil
}

func TestDeadCode_TestDirectoriesSkipped(t *testing.T) {
	files := map[string]string{
		"pkg/core.py":           "def core_thing():\n    return 1\n",
		"tests/helpers.py":      "def make_client():\n    return 1\n\ndef unused_helper():\n    return 2\n",
		"tests/test_views.py":   "from helpers import make_client\n\ndef test_x():\n    make_client()\n",
		"tests/type_check/x.py": "def typed_view():\n    return 3\n",
		"app.py":                "print(1)\n",
	}
	signals := writeDeadCodeFixture(t, files, signal.CollectorOpts{})
	assert.NotNil(t, findSignal(signals, "core_thing"), "production symbol still flagged")
	assert.Nil(t, findSignal(signals, "unused_helper"), "tests/ helper skipped by default")
	assert.Nil(t, findSignal(signals, "typed_view"), "tests/type_check/ skipped by default")

	signals = writeDeadCodeFixture(t, files, signal.CollectorOpts{IncludeTests: true})
	sig := findSignal(signals, "unused_helper")
	require.NotNil(t, sig, "include_tests restores test-file symbols")
	assert.Contains(t, sig.Tags, "test-file")
	assert.Nil(t, findSignal(signals, "make_client"), "test helper used by another test file is referenced")
	assert.Nil(t, findSignal(signals, "test_x"))
}

func TestDeadCode_RustTraitImplAndTests(t *testing.T) {
	rs := `use std::ops::BitAnd;

pub struct Ready(u8);

impl BitAnd for Ready {
    type Output = Ready;
    fn bitand(self, other: Ready) -> Ready {
        Ready(self.0 & other.0)
    }
}

impl<T> Clone for Wrapper<T>
where
    T: Clone,
{
    fn clone(&self) -> Self { unreachable!() }
}

impl Ready {
    pub fn writer_mut(&mut self) -> &mut u8 { &mut self.0 }
    fn private_helper(&self) -> u8 { self.0 }
}

fn takes_hrtb(f: impl for<'a> Fn(&'a str)) { f("x") }

#[test]
fn kills_on_drop_if_specified() {}

#[tokio::test]
async fn async_case() {}

#[cfg(test)]
mod tests {
    struct Harness;
    fn helper_in_tests() {}
    #[test]
    fn case() { helper_in_tests() }
}

mod other;
fn after_mod_decl() {}

pub trait AsyncSeekExt {
    fn stream_position(&mut self) -> u64 { 0 }
}

fn _assert_kinds() {}
`
	files := map[string]string{"src/lib.rs": rs, "Cargo.toml": "[package]\nname = \"x\"\n[[bin]]\nname = \"x\"\n"}
	signals := writeDeadCodeFixture(t, files, signal.CollectorOpts{})

	for _, name := range []string{"bitand", "clone", "kills_on_drop_if_specified", "async_case", "helper_in_tests", "Harness", "case", "stream_position", "_assert_kinds"} {
		assert.Nil(t, findSignal(signals, name), "%s should be skipped", name)
	}
	for _, name := range []string{"writer_mut", "private_helper", "takes_hrtb", "after_mod_decl", "AsyncSeekExt"} {
		assert.NotNil(t, findSignal(signals, name), "%s should still be flagged", name)
	}

	signals = writeDeadCodeFixture(t, files, signal.CollectorOpts{IncludeTests: true})
	assert.NotNil(t, findSignal(signals, "Harness"), "include_tests restores #[cfg(test)] symbols")
	assert.Nil(t, findSignal(signals, "helper_in_tests"), "referenced within the module")
	assert.Nil(t, findSignal(signals, "bitand"), "trait impl skip does not depend on include_tests")
}

func TestScanRustContext(t *testing.T) {
	lines := strings.Split(`impl Foo for Bar {
    fn a() {}
}
impl Bar {
    fn b() { let s = "{"; }
}
#[cfg(test)]
impl Bar {
    fn c() {}
}
fn d() {}
`, "\n")
	rc := scanRustContext(lines)
	assert.True(t, rc.traitImpl[1])
	assert.False(t, rc.traitImpl[4], "inherent impl is not a trait impl")
	assert.False(t, rc.traitImpl[10], "brace in string literal does not leak the region")
	assert.True(t, rc.cfgTest[8])
	assert.False(t, rc.cfgTest[10])
}

func TestDeadCode_DecoratedFunctionsSkipped(t *testing.T) {
	files := map[string]string{
		"app.py": `from flask import Blueprint

bp = Blueprint("auth", __name__)

@bp.before_app_request
def load_logged_in_user():
    pass

@app.errorhandler(
    404,
)
def not_found(e):
    pass

@property
def plain_property(self):
    return 1

@functools.lru_cache(maxsize=8)
def cached_but_unused():
    return 2

def after_decorated():
    return call(
        x,
    )

def trailing_plain():
    return 3
`,
		"Ctl.java": `public class Ctl {
    @GetMapping("/x")
    public String route() { return "x"; }

    @Override
    public String toString() { return "Ctl"; }

    @Deprecated
    public String oldMethod() { return "old"; }

    @RequestMapping(
        value = "/y"
    )
    public String multiLine() { return "y"; }
}
`,
		"Route.php": `<?php
class Route {
    #[\Symfony\Component\Routing\Attribute\Route('/x')]
    public function index() {}

    #[Deprecated]
    public function legacy() {}

    public function plainMethod() {}
}
`,
		"ctl.ts": `export class Ctl {
    @Get()
    findAll() { return []; }

    helper() { return 1; }
}
`,
		"View.swift": `class View {
    @objc func handleTap() {}
    @IBAction
    func pressed() {}
    func plainSwift() {}
}
`,
		"main.go": "package main\nfunc main() {}\n",
	}
	signals := writeDeadCodeFixture(t, files, signal.CollectorOpts{})

	skipped := []string{"load_logged_in_user", "not_found", "route", "toString", "multiLine", "index", "findAll", "handleTap", "pressed"}
	for _, name := range skipped {
		assert.Nil(t, findSignal(signals, name), "%s is decorator-registered", name)
	}
	kept := []string{"plain_property", "cached_but_unused", "after_decorated", "trailing_plain", "oldMethod", "legacy", "plainMethod", "helper", "plainSwift"}
	for _, name := range kept {
		assert.NotNil(t, findSignal(signals, name), "%s should still be flagged", name)
	}
}

func TestPrecededByDecorator(t *testing.T) {
	cs := strings.Split("public class T {\n    [Fact]\n    public void Runs() {}\n    [Obsolete]\n    public void Old() {}\n}\n", "\n")
	assert.True(t, precededByDecorator(cs, 2, ".cs"))
	assert.False(t, precededByDecorator(cs, 4, ".cs"), "[Obsolete] is plain")
	assert.False(t, precededByDecorator([]string{"@x", "func f() {}"}, 1, ".go"), "Go has no decorators")

	// A decorated previous function must not leak onto the next definition.
	py := strings.Split("@fixture\ndef a():\n    return x(1)\n\ndef b():\n    pass\n", "\n")
	assert.True(t, precededByDecorator(py, 1, ".py"))
	assert.False(t, precededByDecorator(py, 4, ".py"))

	// Comments between decorator and definition are tolerated.
	py2 := strings.Split("@app.route('/')\n# handler\ndef index():\n    pass\n", "\n")
	assert.True(t, precededByDecorator(py2, 2, ".py"))
}

func TestDecoratorName(t *testing.T) {
	tests := []struct{ line, marker, want string }{
		{"@bp.before_app_request", "@", "before_app_request"},
		{"@app.route('/x')", "@", "route"},
		{"@property", "@", "property"},
		{"#[tokio::test]", "#[", "test"},
		{"#[cfg(test)]", "#[", "cfg"},
		{`#[\Attribute\Route('/x')]`, "#[", "Route"},
		{"[Fact]", "[", "Fact"},
		{"@", "@", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, decoratorName(tt.line, tt.marker), tt.line)
	}
}

func TestSymbolExported(t *testing.T) {
	tests := []struct {
		name, line, ext string
		want            bool
	}{
		{"f", "pub fn f()", ".rs", true},
		{"f", "pub(crate) fn f()", ".rs", false},
		{"f", "fn f()", ".rs", false},
		{"f", "    public void f()", ".java", true},
		{"f", "    void f()", ".java", false},
		{"f", "    open func f()", ".swift", true},
		{"f", "    func f()", ".swift", false},
		{"f", "    function f()", ".php", true},
		{"f", "    private function f()", ".php", false},
		{"f", "  def f", ".scala", true},
		{"f", "  protected def f", ".scala", false},
		{"f", "  def f do", ".ex", true},
		{"f", "  defp f do", ".ex", false},
		{"f", "export function f()", ".ts", true},
		{"f", "function f()", ".ts", false},
		{"f", "    f() {", ".ts", true},
		{"f", "    private f() {", ".ts", false},
		{"F", "func F()", ".go", true},
		{"_f", "def _f():", ".py", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, symbolExported(tt.name, tt.line, tt.ext), "%s %q", tt.ext, tt.line)
	}
}

func TestIsEntryPointPath(t *testing.T) {
	tests := []struct {
		rel   string
		isDir bool
		want  bool
	}{
		{"cmd", true, true},
		{"bin", true, true},
		{"src/bin", true, true},
		{"pkg/cmd", true, false},
		{"main.go", false, true},
		{"cmd/x/main.go", false, true},
		{"src/Program.cs", false, true},
		{"src/main.rs", false, true},
		{"manage.py", false, true},
		{"artisan", false, true},
		{"src/flask/__main__.py", false, false},
		{"lib.rs", false, false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, isEntryPointPath(tt.rel, tt.isDir), tt.rel)
	}
}

func TestManifestKind(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		lib   bool
		app   bool
	}{
		{"none", nil, false, false},
		{"cargo lib", map[string]string{"Cargo.toml": "[package]\n[lib]\n"}, true, false},
		{"cargo bin", map[string]string{"Cargo.toml": "[package]\n[[bin]]\n"}, false, true},
		{"npm lib", map[string]string{"package.json": `{"main": "index.js"}`}, true, false},
		{"npm exports", map[string]string{"package.json": `{"exports": {}}`}, true, false},
		{"npm bin", map[string]string{"package.json": `{"main": "x", "bin": "cli.js"}`}, false, true},
		{"pyproject lib", map[string]string{"pyproject.toml": "[project]\nname = \"x\"\n"}, true, false},
		{"pyproject scripts", map[string]string{"pyproject.toml": "[project]\n[project.scripts]\nx = \"x:main\"\n"}, false, false},
		{"composer lib", map[string]string{"composer.json": `{"type" : "library"}`}, true, false},
		{"composer project", map[string]string{"composer.json": `{"type": "project"}`}, false, false},
		{"composer default type", map[string]string{"composer.json": `{"name": "laravel/framework", "require": {}}`}, true, false},
		{"composer plugin", map[string]string{"composer.json": `{"type": "composer-plugin"}`}, false, false},
		{"setup.py", map[string]string{"setup.py": "from setuptools import setup\n"}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range tt.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o600))
			}
			lib, app := manifestKind(dir)
			assert.Equal(t, tt.lib, lib, "lib")
			assert.Equal(t, tt.app, app, "app")
		})
	}
}

// collectDeadCodeMetrics writes files into a temp dir, runs the collector
// and returns both the signals and the metrics.
func collectDeadCodeMetrics(t *testing.T, files map[string]string, opts signal.CollectorOpts) ([]signal.RawSignal, *DeadCodeMetrics) {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	c := &DeadCodeCollector{}
	signals, err := c.Collect(context.Background(), dir, opts)
	require.NoError(t, err)
	return signals, c.Metrics().(*DeadCodeMetrics)
}

func TestDeadCode_LibraryPublicAPI_SuppressedByDefault(t *testing.T) {
	rs := "pub fn reader_pin_mut() {}\nfn private_fn() {}\npub struct Exported;\n"
	// No entry point and no manifest: treated as a library. Public symbols
	// are counted, not reported; private ones keep their tier.
	signals, m := collectDeadCodeMetrics(t, map[string]string{"src/lib.rs": rs}, signal.CollectorOpts{})
	assert.Nil(t, findSignal(signals, "reader_pin_mut"), "public fn suppressed")
	assert.Nil(t, findSignal(signals, "Exported"), "public type suppressed")
	priv := findSignal(signals, "private_fn")
	require.NotNil(t, priv)
	assert.InDelta(t, 0.6, priv.Confidence, 0.01)
	assert.NotContains(t, priv.Tags, "public-api")
	assert.True(t, m.IsLibrary)
	assert.Equal(t, 2, m.PublicSuppressed)
	assert.Equal(t, 1, m.DeadSymbols, "suppressed symbols are not dead-symbol findings")

	// Cargo [lib] wins over an entry point.
	signals, m = collectDeadCodeMetrics(t, map[string]string{
		"src/lib.rs": rs, "src/main.rs": "fn main() {}\n", "Cargo.toml": "[package]\n[lib]\n",
	}, signal.CollectorOpts{})
	assert.Nil(t, findSignal(signals, "reader_pin_mut"))
	assert.True(t, m.IsLibrary)
	assert.Equal(t, 2, m.PublicSuppressed)
}

func TestDeadCode_LibraryPublicAPI_OptIn(t *testing.T) {
	rs := "pub fn reader_pin_mut() {}\nfn private_fn() {}\npub struct Exported;\n"
	signals, m := collectDeadCodeMetrics(t, map[string]string{"src/lib.rs": rs},
		signal.CollectorOpts{IncludePublicAPI: true})
	pub := findSignal(signals, "reader_pin_mut")
	require.NotNil(t, pub)
	assert.InDelta(t, 0.3, pub.Confidence, 0.01)
	assert.Contains(t, pub.Tags, "public-api")
	typ := findSignal(signals, "Exported")
	require.NotNil(t, typ)
	assert.Contains(t, typ.Tags, "public-api")
	priv := findSignal(signals, "private_fn")
	require.NotNil(t, priv)
	assert.InDelta(t, 0.6, priv.Confidence, 0.01)
	assert.NotContains(t, priv.Tags, "public-api")
	assert.True(t, m.IsLibrary)
	assert.Equal(t, 0, m.PublicSuppressed, "nothing suppressed when opted in")
	assert.Equal(t, 3, m.DeadSymbols)

	// Cargo [lib] wins over an entry point.
	signals, _ = collectDeadCodeMetrics(t, map[string]string{
		"src/lib.rs": rs, "src/main.rs": "fn main() {}\n", "Cargo.toml": "[package]\n[lib]\n",
	}, signal.CollectorOpts{IncludePublicAPI: true})
	assert.Contains(t, findSignal(signals, "reader_pin_mut").Tags, "public-api")
}

func TestDeadCode_ApplicationUnchanged(t *testing.T) {
	rs := "pub fn reader_pin_mut() {}\nfn private_fn() {}\npub struct Exported;\n"
	files := map[string]string{"src/lib.rs": rs, "src/main.rs": "fn main() {}\n"}
	// An entry point without a library manifest keeps the 0.4 tier and
	// suppresses nothing, with or without the flag.
	for _, include := range []bool{false, true} {
		signals, m := collectDeadCodeMetrics(t, files, signal.CollectorOpts{IncludePublicAPI: include})
		pub := findSignal(signals, "reader_pin_mut")
		require.NotNil(t, pub, "include=%v", include)
		assert.InDelta(t, 0.4, pub.Confidence, 0.01)
		assert.NotContains(t, pub.Tags, "public-api")
		assert.False(t, m.IsLibrary)
		assert.Equal(t, 0, m.PublicSuppressed)
		assert.Equal(t, 3, m.DeadSymbols)
	}
}

func TestDeadCode_LibraryPublicAPI_GoAndPHP(t *testing.T) {
	files := map[string]string{
		"go.mod":                 "module example.com/lib\n",
		"lib.go":                 "package lib\n\nfunc Exported() {}\n\nfunc unexported() {}\n",
		"internal/x/x.go":        "package x\n\nfunc InternalExported() {}\n",
		"src/Hasher.php":         "<?php\nclass Hasher {\n    public function setHasher() {}\n    private function secret() {}\n}\n",
		"src/Events/Lockout.php": "<?php\nclass Lockout {}\n",
		"composer.json":          `{"type": "library"}`,
	}

	// Default: the four public symbols (Exported, Hasher, setHasher, Lockout) are
	// suppressed; internal/ and private symbols are unaffected.
	signals, m := collectDeadCodeMetrics(t, files, signal.CollectorOpts{})
	for _, name := range []string{"Exported", "Hasher", "setHasher", "Lockout"} {
		assert.Nil(t, findSignal(signals, name), name)
	}
	assert.Equal(t, 4, m.PublicSuppressed)
	assert.True(t, m.IsLibrary)

	sig := findSignal(signals, "unexported")
	require.NotNil(t, sig)
	assert.InDelta(t, 0.7, sig.Confidence, 0.01)
	assert.NotContains(t, sig.Tags, "public-api")

	sig = findSignal(signals, "InternalExported")
	require.NotNil(t, sig)
	assert.InDelta(t, 0.6, sig.Confidence, 0.01, "internal/ is not public API")
	assert.NotContains(t, sig.Tags, "public-api")

	sig = findSignal(signals, "secret")
	require.NotNil(t, sig)
	assert.InDelta(t, 0.5, sig.Confidence, 0.01)

	// Opt-in restores the public symbols at the 0.3 tier, tagged.
	signals, m = collectDeadCodeMetrics(t, files, signal.CollectorOpts{IncludePublicAPI: true})
	assert.Equal(t, 0, m.PublicSuppressed)
	for _, name := range []string{"Exported", "Hasher", "setHasher", "Lockout"} {
		sig = findSignal(signals, name)
		require.NotNil(t, sig, name)
		assert.InDelta(t, 0.3, sig.Confidence, 0.01, name)
		assert.Contains(t, sig.Tags, "public-api", name)
	}
}

func TestDeadCode_RubyElixirPredicateNames(t *testing.T) {
	signals := writeDeadCodeFixture(t, map[string]string{
		"lib/model.rb":  "class Model\n  def valid?\n    true\n  end\n  def save!\n    true\n  end\n  def stale?\n    false\n  end\nend\n",
		"lib/runner.rb": "m = Model.new\nm.save! if m.valid?\n",
		"lib/check.ex":  "defmodule Check do\n  def ready?(x), do: x\n  def lonely?(x), do: x\nend\n",
		"lib/use.ex":    "Check.ready?(1)\n",
		"bin/run":       "",
	}, signal.CollectorOpts{})

	for _, name := range []string{"valid?", "save!", "ready?"} {
		assert.Nil(t, findSignal(signals, name), "%s is referenced from another file", name)
	}
	for _, name := range []string{"stale?", "lonely?"} {
		assert.NotNil(t, findSignal(signals, name), "%s is unreferenced", name)
	}
}

func TestWordBoundary_PredicateNames(t *testing.T) {
	c := &DeadCodeCollector{regexCache: map[string]*regexp.Regexp{}}
	assert.True(t, c.wordBoundary("valid?").MatchString("x.valid?\n"))
	assert.False(t, c.wordBoundary("valid?").MatchString("x.valid\n"))
	assert.False(t, c.wordBoundary("valid?").MatchString("invalid?\n"))
	assert.True(t, c.wordBoundary("save!").MatchString("save!\n"))
	assert.False(t, c.wordBoundary("save").MatchString("saved\n"))
}
