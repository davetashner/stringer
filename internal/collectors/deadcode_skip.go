// Copyright 2026 The Stringer Authors
// SPDX-License-Identifier: MIT

package collectors

import (
	"path/filepath"
	"regexp"
	"strings"
)

// This file holds the context rules that keep the deadcode collector from
// flagging symbols that are alive through a mechanism the reference search
// cannot see: trait dispatch, framework registration by decorator, test
// harness discovery, and consumption by downstream users of a library
// (stringer-nxx.3).

// rustTraitImplHeader matches the opening line of `impl Trait for Type` or
// of a `trait Name` definition: methods in both are called through the
// trait. `\sfor\s+[^<\s]` excludes HRTB bounds such as `for<'a> Fn(&'a T)`.
var rustTraitImplHeader = regexp.MustCompile(
	`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:unsafe\s+)?(?:impl\b[^{]*\sfor\s+[^<\s]|trait\s+\w+)`)

// rustBlockHeader matches a `mod name` or inherent `impl` line whose body
// becomes test code when a #[cfg(test)] attribute precedes it.
var rustBlockHeader = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:mod\s+\w+|(?:unsafe\s+)?impl\b)`)

// rustLineContext records, per line, whether the line lies inside a trait
// or `impl Trait for Type` block, or inside a #[cfg(test)] module or impl.
type rustLineContext struct {
	traitImpl []bool
	cfgTest   []bool
}

// scanRustContext walks a Rust file once with a brace-depth counter and
// marks the lines enclosed by trait impl blocks and #[cfg(test)] blocks.
// A header opens a region at the next `{`; the region closes with the
// matching `}`. Braces inside string literals and comments are ignored.
func scanRustContext(lines []string) rustLineContext {
	type region struct {
		trait bool
		depth int
	}
	rc := rustLineContext{traitImpl: make([]bool, len(lines)), cfgTest: make([]bool, len(lines))}
	var stack []region
	depth := 0
	pendingTrait, pendingTest := false, false

	for i, raw := range lines {
		line := stripStringsAndComments(raw, ".rs")
		if !pendingTrait && !pendingTest {
			switch {
			case rustTraitImplHeader.MatchString(line):
				pendingTrait = true
			case rustBlockHeader.MatchString(line) && precededByRustTestAttr(lines, i):
				pendingTest = true
			}
		}
		for j := 0; j < len(line); j++ {
			switch line[j] {
			case '{':
				depth++
				if pendingTrait || pendingTest {
					stack = append(stack, region{trait: pendingTrait, depth: depth})
					pendingTrait, pendingTest = false, false
				}
			case '}':
				if n := len(stack); n > 0 && stack[n-1].depth == depth {
					stack = stack[:n-1]
				}
				depth--
			}
		}
		if strings.HasSuffix(strings.TrimSpace(line), ";") {
			pendingTrait, pendingTest = false, false // `mod foo;` has no body
		}
		for _, r := range stack {
			if r.trait {
				rc.traitImpl[i] = true
			} else {
				rc.cfgTest[i] = true
			}
		}
	}
	return rc
}

// decoratorMarker returns the prefix that introduces a decorator, attribute
// or annotation line for ext, or "" when the language has none.
func decoratorMarker(ext string) string {
	switch ext {
	case ".py", ".java", ".kt", ".js", ".jsx", ".ts", ".tsx", ".scala", ".swift":
		return "@"
	case ".php", ".rs":
		return "#["
	case ".cs":
		return "["
	}
	return ""
}

// plainDecorators are decorators that only wrap or annotate the definition
// (lowercased, without module path or arguments). Everything else is
// treated as a framework registration: routes, handlers, fixtures, tests,
// FFI exports, interface overrides.
var plainDecorators = map[string]bool{
	// Python builtins / stdlib.
	"property": true, "staticmethod": true, "classmethod": true, "cached_property": true,
	"cache": true, "lru_cache": true, "wraps": true, "overload": true, "setter": true,
	"getter": true, "deleter": true, "dataclass": true, "total_ordering": true,
	"final": true, "unique": true, "runtime_checkable": true, "contextmanager": true,
	"asynccontextmanager": true, "singledispatch": true,
	// Java / C# / PHP annotations that never register anything.
	"deprecated": true, "suppresswarnings": true, "safevarargs": true,
	"functionalinterface": true, "nullable": true, "nonnull": true, "notnull": true,
	"obsolete": true, "serializable": true, "pure": true, "returntypewillchange": true,
	// Rust attributes that are not registrations.
	"inline": true, "allow": true, "warn": true, "deny": true, "forbid": true,
	"expect": true, "must_use": true, "cfg": true, "cfg_attr": true, "doc": true,
	"track_caller": true, "cold": true, "target_feature": true, "rustfmt": true,
	"non_exhaustive": true, "repr": true, "derive": true, "automatically_derived": true,
}

// decoratorName extracts the bare name of a decorator line:
// "@bp.before_app_request(x)" → "before_app_request", "#[tokio::test]" →
// "test", "[Fact]" → "Fact", "#[\Attribute]" → "Attribute".
func decoratorName(trimmed, marker string) string {
	s := strings.TrimPrefix(trimmed, marker)
	end := 0
	for end < len(s) && (isWordByte(s[end]) || s[end] == '.' || s[end] == ':' || s[end] == '\\') {
		end++
	}
	s = s[:end]
	if i := strings.LastIndexAny(s, ".:\\"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// indentWidth returns the number of leading whitespace bytes of line.
func indentWidth(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

// precededByDecorator reports whether the definition at idx carries a
// registering decorator/attribute/annotation: on the line itself (Swift
// `@objc func`) or on the lines directly above it, allowing blank lines,
// comments, plain decorators and the continuation lines of a multi-line
// decorator argument list (deeper indented, or starting with `)`/`]`).
func precededByDecorator(lines []string, idx int, ext string) bool {
	marker := decoratorMarker(ext)
	if marker == "" {
		return false
	}
	registering := func(trimmed string) bool {
		return !plainDecorators[strings.ToLower(decoratorName(trimmed, marker))]
	}
	if t := strings.TrimSpace(lines[idx]); strings.HasPrefix(t, marker) && registering(t) {
		return true
	}
	defIndent := indentWidth(lines[idx])
	for j := idx - 1; j >= 0 && j >= idx-12; j-- {
		trimmed := strings.TrimSpace(lines[j])
		switch {
		case strings.HasPrefix(trimmed, marker):
			if registering(trimmed) {
				return true
			}
		case trimmed == "" || commentLinePattern.MatchString(lines[j]):
		case strings.HasPrefix(trimmed, ")") || strings.HasPrefix(trimmed, "]") || indentWidth(lines[j]) > defIndent:
		default:
			return false
		}
	}
	return false
}

// symbolExported refines the name-based isExported with the modifiers on
// the declaration line, so that visibility reflects what a downstream
// consumer of the package could actually reach.
func symbolExported(name, line, ext string) bool {
	switch ext {
	case ".rs":
		return strings.Contains(line, "pub ")
	case ".java", ".swift":
		return strings.Contains(line, "public ") || strings.Contains(line, "open ")
	case ".php", ".scala":
		return !strings.Contains(line, "private ") && !strings.Contains(line, "protected ")
	case ".ex", ".exs":
		t := strings.TrimSpace(line)
		return !strings.HasPrefix(t, "defp ") && !strings.HasPrefix(t, "defmacrop ")
	case ".js", ".jsx", ".ts", ".tsx":
		if strings.Contains(line, "private ") {
			return false
		}
		// Un-exported top-level declarations are module-private; indented
		// declarations are class members whose reach is unknown.
		return strings.Contains(line, "export ") || indentWidth(line) > 0
	}
	return isExported(name, ext)
}

// deadCodeEntryFiles are file names that mark an application entry point
// anywhere in the tree; deadCodeRootEntryNames only count at the repo root.
var (
	deadCodeEntryFiles     = map[string]bool{"main.go": true, "Program.cs": true}
	deadCodeRootEntryNames = map[string]bool{
		"__main__.py": true, "manage.py": true, "app.py": true, "main.py": true,
		"artisan": true, "cmd": true, "bin": true,
	}
)

// isEntryPointPath reports whether relPath (a file or directory seen during
// the walk) marks the repository as an application rather than a library.
func isEntryPointPath(relPath string, isDir bool) bool {
	rel := filepath.ToSlash(relPath)
	if isDir {
		return deadCodeRootEntryNames[rel] || rel == "src/bin"
	}
	base := filepath.Base(rel)
	if deadCodeEntryFiles[base] || rel == "src/main.rs" {
		return true
	}
	return !strings.Contains(rel, "/") && deadCodeRootEntryNames[base]
}

// composerType matches a top-level composer.json "type" value. Composer's
// documented default when the key is absent is "library", so a manifest
// without one (laravel/framework, most packages) is a library too.
var composerType = regexp.MustCompile(`"type"\s*:\s*"([^"]*)"`)

// manifestKind inspects the package manifests at the repo root and reports
// whether one declares the repository a library (Cargo `[lib]`,
// package.json `main`/`exports` without `bin`, pyproject `[project]` without
// `[project.scripts]`, composer `"type": "library"` or no `type` (the
// composer default), a `setup.py`) or an
// application (Cargo `[[bin]]`, package.json `bin`).
func manifestKind(repoPath string) (lib, app bool) {
	read := func(name string) (string, bool) {
		c, err := readFileContent(filepath.Join(repoPath, name))
		return c, err == nil
	}
	if c, ok := read("Cargo.toml"); ok {
		lib = lib || strings.Contains(c, "[lib]")
		app = app || strings.Contains(c, "[[bin]]")
	}
	if c, ok := read("package.json"); ok {
		hasBin := strings.Contains(c, `"bin"`)
		app = app || hasBin
		lib = lib || (!hasBin && (strings.Contains(c, `"main"`) || strings.Contains(c, `"exports"`)))
	}
	if c, ok := read("pyproject.toml"); ok {
		hasScripts := strings.Contains(c, "[project.scripts]") || strings.Contains(c, "[project.gui-scripts]")
		lib = lib || (!hasScripts && strings.Contains(c, "[project]"))
	}
	if c, ok := read("composer.json"); ok {
		m := composerType.FindStringSubmatch(c)
		lib = lib || m == nil || m[1] == "library"
	}
	if _, err := FS.Stat(filepath.Join(repoPath, "setup.py")); err == nil {
		lib = true
	}
	return lib, app
}
