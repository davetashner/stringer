# 014: Dead Code Detector Design

**Status:** Accepted
**Date:** 2026-02-21
**Context:** stringer-7el (C8: Dead Code Detector). Adding dead code detection as a new signal source to identify unused functions and types.

## Problem

Stringer detects complexity hotspots and churn patterns but does not identify code that is never referenced — dead code. Unused functions and types are maintenance debt: they confuse readers, increase build times, and rot over time. Detecting dead code provides actionable cleanup signals.

Key questions:
1. How should we detect unused symbols without AST parsing?
2. What confidence levels are appropriate given false-positive risk?
3. How do we handle exported symbols that may be used by external consumers?

## Options

### Option A: AST-based dead code analysis

Use `go/ast`, tree-sitter, or language-specific tools for precise symbol resolution and usage tracking.

**Pros:**
- Accurate symbol resolution including imports and qualified references
- Handles shadowing, overloading, and namespaces correctly

**Cons:**
- Requires AST parser per language — massive dependency surface
- Overkill for archaeological signals where ~80% accuracy suffices
- Doesn't match stringer's zero-external-tooling philosophy

### Option B: Regex heuristic + in-memory reference search

Two-pass algorithm: extract symbol definitions via regex, then search all cached file contents for word-boundary references.

**Pros:**
- Zero external dependencies
- Reuses existing `langSpecs`/`extToSpec` from complexity collector for function detection
- ~80% accuracy is acceptable for identifying cleanup candidates
- Fast with `strings.Contains` pre-filter before regex matching
- Easy to extend to new languages

**Cons:**
- Cannot resolve imports or qualified references precisely
- May miss references in string interpolation, reflection, or codegen
- False positives for exported symbols used by external packages

### Option C: External tool integration (deadcode, vulture, etc.)

Shell out to language-specific dead code tools.

**Pros:**
- Leverages battle-tested, accurate tools

**Cons:**
- Requires tools to be installed
- Different output formats per tool
- Doesn't match stringer's self-contained approach

## Recommendation

**Option B: Regex heuristic + in-memory reference search.**

Consistent with DR-013's philosophy — ~80% accuracy is sufficient for archaeological signals. Lower confidence for exported symbols mitigates false-positive risk.

### Algorithm

1. **Extract** — Walk source files, use regex to build a symbol index (name, file, line, visibility)
2. **Search** — For each symbol, scan all cached file contents for word-boundary matches. If count==1 in def file and 0 elsewhere, it's dead code. If only referenced in test files, flag with lower confidence.

### Signal kinds

- `unused-function` — function/method defined but never referenced elsewhere
- `unused-type` — type/class/struct defined but never referenced elsewhere

### Skip list

Never flag: `main`, `init`, `Test*`, `Benchmark*`, `Example*`, dunder methods, framework lifecycle methods (`constructor`, `render`, `componentDidMount`, etc.), names <= 2 chars, symbols in test files, generated/binary files.

### Confidence tiers

| Context | Confidence |
|---------|-----------|
| Go unexported, zero refs | 0.7 |
| Go exported in `internal/` | 0.6 |
| Rust non-pub | 0.6 |
| Other unexported | 0.5 |
| Other exported | 0.4 |
| Only referenced in test files | 0.3 |
| Go exported in public pkg | 0.3 |

### Performance guards

- File count cap: 10,000 (skip with warning if exceeded)
- `strings.Contains` fast pre-filter before regex match
- Context cancellation checks in both walk and search loops

### Languages

Go, Python, JS/TS, Java, Rust, Ruby — same as complexity collector.

## Decision

Option B accepted. Regex heuristic with in-memory reference search, conservative confidence tiers for exported symbols.

## Amendment 2026-09-18 (stringer-nxx.3): context rules for symbols that are alive without a by-name reference

The September 2026 benchmark showed the reference search flagging symbols that are alive through mechanisms it cannot observe: tokio's `fn bitand` inside `impl BitAnd for Ready` and `#[test]` fns inside `#[cfg(test)] mod tests` (180 signals), flask's `@bp.before_app_request` handlers and 30/43 signals under `tests/type_check/`, django's 1,153/1,491 signals in `tests/`, and public accessors such as tokio's `writer_mut` or laravel's `setHasher` that exist for downstream consumers. The algorithm is unchanged; the extraction pass now applies these rules before a symbol enters the index:

- **Test code is skipped, not extracted.** Test classification is the shared `isTestFile` plus any `test`/`tests`/`__tests__`/`spec`/`testdata` path component (the same rule the complexity collector adopted in DR-024), plus Rust `#[test]`/`#[bench]`/`#[tokio::test]` fns and `#[cfg(test)]` modules/impls in production files. Test helpers, fixtures and URL-dispatched views are discovered by the harness, not called by name. `collectors.deadcode.include_tests: true` restores them tagged `test-file`; a test-file symbol referenced from any other test file then counts as referenced.
- **Rust trait methods are never extracted.** A brace-depth scan (string and comment stripped) marks the bodies of `impl Trait for Type` and `trait Name` blocks; their fns are dispatched through the trait. Inherent `impl Type` methods are still candidates. `_`-prefixed fns are Rust's explicit intentionally-unused convention (`fn _assert_kinds()`) and are skipped.
- **Decorator-registered definitions are skipped.** A `@…` (Python/Java/Kotlin/JS/TS/Scala/Swift), `#[…]` (PHP 8, Rust) or `[…]` (C#) line directly above the definition (blank lines, comments and multi-line argument continuations allowed) marks it as registered by a framework: routes, handlers, fixtures, tests, FFI exports, `@Override` interface implementations. A small allowlist of plain decorators that only wrap (`@property`, `@lru_cache`, `@Deprecated`, `#[inline]`, `#[allow]`, `#[cfg]`, …) does not count, so a decorated-but-unused `@property` is still reported.
- **Public API of a library is capped at 0.3 and tagged `public-api`.** A repository is a library when a manifest says so (Cargo `[lib]`, package.json `main`/`exports` without `bin`, pyproject `[project]` without `[project.scripts]`, composer `"type": "library"`, `setup.py`) or when it has no application entry point (`main.go`, `cmd/`, `bin/`, `src/main.rs`, `src/bin/`, root `manage.py`/`app.py`/`main.py`/`__main__.py`, `artisan`, `Program.cs`, Cargo `[[bin]]`, package.json `bin`). A manifest library marker wins over an entry point. Visibility now comes from the declaration line (Rust `pub`, Java/Swift `public`/`open`, PHP/Scala not `private`/`protected`, Elixir not `defp`, JS/TS `export` or class member) rather than only from the name; private symbols keep their existing tiers. The 0.3 tier matches the existing "Go exported in public pkg" row, which is the same judgement.
- **Ruby/Elixir predicate and bang names** (`valid?`, `save!`, `def ready?`) previously never matched `\bname\b` because `\b` after `?` needs a following word byte, so they always read as dead. The trailing boundary is dropped for names ending in `?`/`!`: `\bvalid\?` matches `obj.valid?` and `valid?(x)` but not the distinct method `valid`.

On tokio (`git clone --depth 1`, 2026-09-18): 180 deadcode signals → 55, of which 49 are `public-api` at 0.3; the remaining 0.6 findings are `#[allow(dead_code)]` fns the authors acknowledge as unused, a commented-out fn and a `quote!`-generated enum. Reference-search cost is unchanged (the index benchmarks are within noise).

## Amendment 2026-09-18 (stringer-jfh.2): library public API is suppressed by default

Capping public symbols at 0.3 did not fix the volume problem: on laravel/framework the amendment above took deadcode from 1,208 to 1,203 signals, because nearly every finding in a library *is* a public symbol, and a 0.3 signal still counts toward every total, report and dashboard the reader sees. A library's exported surface exists for downstream consumers the reference search cannot observe, so as a default it carries no actionable information. In a repository classified as a library (same rule as above: manifest marker, or no application entry point), exported symbols that look unreferenced are now **not emitted at all**; the count is recorded in `DeadCodeMetrics.PublicSuppressed` (with `DeadCodeMetrics.IsLibrary`) and the collector logs one INFO line, `deadcode: N public symbols not reported (library repo; set collectors.deadcode.include_public_api to see them)`. Setting `collectors.deadcode.include_public_api: true` restores the previous behaviour exactly: the 0.3 tier and the `public-api` tag. The composer rule is corrected at the same time: a `composer.json` with no `type` key is a library (Composer's documented default; laravel/framework has no `type` and a `bin/` of release scripts, so the amendment above never classified it as one), while an explicit `project`, `metapackage` or `composer-plugin` type is not. Application repositories (entry point present, no library manifest) are unchanged, and `internal/`, private, `defp`, non-`pub` and non-`export` symbols keep their tiers in both kinds of repository. The judgement follows the test-code rule above: skipping with an opt-in escape hatch beats down-weighting when the down-weighted findings would still be the majority of the output.
