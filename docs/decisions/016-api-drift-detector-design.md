# 016: API Contract Drift Detector Design

**Status:** Accepted
**Date:** 2026-02-21
**Context:** stringer-c9e (C10: API Contract Drift Detector). Adding detection of drift between OpenAPI/Swagger specs and route handler registrations in code.

## Problem

Projects with OpenAPI/Swagger specs often drift from actual code routes over time. Routes get added without updating the spec (undocumented), spec routes never get implemented, or API versions in code lag behind what the spec declares. This drift is a common source of bugs, broken documentation, and client confusion.

Key questions:
1. Should we parse specs with a full OpenAPI library or use regex?
2. Which route registration patterns should we detect?
3. How do we normalize different param syntaxes for comparison?

## Options

### Option A: Full OpenAPI/Swagger parser library

Use a Go OpenAPI parser (e.g., `kin-openapi`) for spec parsing and AST-based route extraction.

**Pros:**
- Accurate spec parsing including `$ref` resolution
- Handles edge cases like allOf/oneOf schemas

**Cons:**
- Heavy dependency for a heuristic tool
- Doesn't match stringer's zero-external-dependency approach for collectors
- AST-based route extraction would need per-framework parsers

### Option B: Regex-based line-by-line scanning

Walk spec files for `paths:` sections (YAML) or `"paths"` objects (JSON), extract route paths via regex. Walk source files for framework-specific handler registration patterns.

**Pros:**
- Zero external dependencies
- Consistent with DR-013, DR-014, DR-015 approach
- Reuses existing helpers (`mergeExcludes`, `shouldExclude`, `enrichTimestamps`)
- Good enough for the pragmatic "drift detector" use case
- Handles Go, JS/TS, and Python frameworks

**Cons:**
- Won't resolve `$ref` or deeply nested spec structures
- May miss routes registered via metaprogramming

## Recommendation

**Option B: Regex-based line-by-line scanning.**

Stringer is a heuristic signal detector, not a spec validator. The goal is surfacing obvious drift — undocumented routes, unimplemented spec paths, stale API versions — not full spec compliance. Regex scanning catches the vast majority of real-world route declarations.

### Signal types

| Kind | Detection | Confidence |
|------|-----------|------------|
| `undocumented-route` | Route in code but not in spec | 0.6 |
| `unimplemented-route` | Route in spec but no handler found | 0.5 |
| `stale-api-version` | Code routes use older version prefix than spec declares | 0.7 |

### Route normalization

All parameter styles are normalized before comparison:
- `:id` (Express, Gin) → `{param}`
- `<id>` (Flask) → `{param}`
- `{id}` (OpenAPI) → `{param}`
- Trailing slashes stripped
- Paths lowercased

### Graceful no-op

If no spec file is found in the repository, the collector returns zero signals (no error). This makes it safe to enable globally.

### Guards

- Routes must start with `/` to filter false regex matches
- Django `re_path()` regex patterns (containing `^$?*+[(|`) are skipped
- Spec files discovered by well-known names: `openapi.yaml/json`, `swagger.yaml/json`, `api-spec.*`, `api.yaml/json`

## Decision

Option B accepted. Regex-based drift detection with three signal types and route normalization.
