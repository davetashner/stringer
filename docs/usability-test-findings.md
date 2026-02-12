# Stringer Usability Test Findings

**Date**: 2026-02-09
**Version tested**: v0.5.1 (dev build from `feat/dephealth-collector`)
**Tester persona**: AI coding agent helping a user generate a backlog from an existing repository
**Test repositories**:

| Repo | Shape | Signals found | Duration |
|------|-------|--------------|----------|
| spf13/cobra | CLI library (small, focused) | 19 | 1.2s |
| junegunn/fzf | CLI tool (large, mature) | 98 | 3.6s |
| labstack/echo | Web framework (medium) | 29 | 2.7s |

---

## Executive Summary

Stringer delivers genuine value — the report command is outstanding, error messages are excellent, and the init-to-scan path works. But several issues undermine first-run experience: **false-positive BUG detection from changelogs is the most damaging signal quality issue**, progress logging drowns the actual output, `--min-confidence 2.0` is silently accepted, and the lottery risk labeling is inverted from what users expect. These are all fixable, and most are P2 or lower.

---

## Phase 1: Discovery

### F1. Help text doesn't guide first-time users to `init`
**Severity: P3** | **Category: Onboarding**

`stringer --help` lists commands alphabetically. A new user sees `completion` first, `init` buried in the middle, and `scan` near the bottom. There's no "Getting Started" hint.

**Expected**: Something like "Run `stringer init` to get started, then `stringer scan .` to generate issues."
**Actual**: Just an alphabetical command list.
**Recommendation**: Add a footer to `--help` with a quick-start flow, or reorder commands by workflow.

---

## Phase 2: Init

### F2. Generated config is identical for all repos — no project detection
**Severity: P3** | **Category: Init**

`.stringer.yaml` is byte-for-byte identical for cobra, fzf, and echo. Init doesn't detect:
- The `dephealth` collector is not listed in the config at all (even though it's a registered collector)
- Project-specific characteristics (e.g., exclude patterns for vendor dirs, Makefile targets, etc.)

The config is well-commented and the next-steps output is helpful, but the "auto-detection" feels thin.

**Recommendation**: Include all registered collectors in the generated config. Consider detecting vendor directories, test framework, or CI to suggest exclude patterns.

### F3. Init "next steps" mention MCP server before basic usage
**Severity: P3** | **Category: Onboarding**

The init output shows:
```
Next steps:
  1. Review .stringer.yaml and adjust settings
  2. Register MCP server: claude mcp add stringer -- stringer mcp serve
  3. Run: stringer scan .
  4. Import results: stringer scan . | bd import
```

Step 2 (MCP server) is advanced and only relevant to Claude Code users. A first-time user likely wants to go straight to step 3.

**Recommendation**: Put `stringer scan .` as step 2 and make MCP server a later "optional" step.

---

## Phase 3: First Scan

### F4. Progress logging to stderr is extremely verbose
**Severity: P2** | **Category: UX / Output**

A scan of cobra produces **48 lines of stderr logging** for 19 signals. The output looks like:
```
time=... level=INFO msg="gitlog: examined 100 commits"
time=... level=INFO msg="gitlog: examined 200 commits"
time=... level=INFO msg="gitlog: examined 300 commits"
... (10 lines for gitlog, 10 for lotteryrisk, 6 for collector complete)
```

When piping (`stringer scan . | bd import`), this is fine — stderr goes to terminal, stdout goes to pipe. But when just running `stringer scan .` in a terminal, the log lines and actual JSON output are interleaved and hard to parse visually.

The `-q` flag fixes this, but a new user doesn't know about it.

**Recommendation**: Either make `-q` the default (with `-v` for verbose), or use a progress bar/spinner instead of line-per-100-commits logging. At minimum, only show final summary by default.

### F5. False-positive BUG detection from CHANGELOG section headers
**Severity: P1** | **Category: Signal Quality**

The TODO collector matches `BUG:` in CHANGELOG.md section headers. In fzf, this produced **7 identical false positives**:

```json
{"title":"BUG: fixes", "description":"Location: CHANGELOG.md:2941"}
{"title":"BUG: fixes", "description":"Location: CHANGELOG.md:2953"}
{"title":"BUG: fixes", "description":"Location: CHANGELOG.md:2967"}
... (7 total)
```

In echo, it picked up markdown formatting as bugs:
```json
{"title":"BUG: ** fixes until **2026-12-31**"}
```

These are CHANGELOG section headers like `## Bug fixes` or `- Bug: fixes`, not actual code annotations.

**Recommendation**:
1. Exclude `CHANGELOG*`, `CHANGES*`, `HISTORY*` from the TODO collector by default
2. Require `BUG:` to appear as a comment marker (after `//`, `#`, `/*`, etc.), not in prose
3. At minimum, add these paths to the default exclude list in `.stringer.yaml`

### F6. "No test file found" for test files themselves (false positive)
**Severity: P2** | **Category: Signal Quality**

In fzf, the patterns collector flagged:
```
"No test file found for test/test_core.rb"
"No test file found for test/test_exec.rb"
"No test file found for test/test_filter.rb"
```

These ARE test files (Ruby test scripts). The collector's test-file detection heuristic only understands Go's `_test.go` convention and doesn't recognize other languages' test patterns.

Similarly, it flags `src/actiontype_string.go` (a `go generate` output) as missing tests.

**Recommendation**:
1. Recognize files already in `test/`, `tests/`, `spec/` directories as test files
2. Recognize common test naming patterns: `test_*.rb`, `*_spec.rb`, `*.test.js`, etc.
3. Skip generated files (those with `// Code generated` headers or `_string.go` suffix)

### F7. Lottery risk labels are inverted from user expectations
**Severity: P2** | **Category: Signal Quality**

Every directory in fzf shows `"Low lottery risk"` with a risk score of 1 and Junegunn Choi at 80-91%. The title says "low lottery risk" but the actual risk is **high** — the entire project depends on one person.

The number (1) is the lottery risk factor, where 1 = highest risk. But the title says "Low lottery risk" which reads as "this is fine." The report command handles this correctly (shows "CRITICAL" for lottery risk 1), but the scan signal title is misleading.

**Recommendation**: Rename to "High lottery risk" or "Single-contributor risk" for lottery risk 1-2. "Low lottery risk" should describe directories with many contributors.

### F8. Tasks format has redundant type prefixes in subjects
**Severity: P3** | **Category: Output Format**

The `tasks` format output produces subjects like:
```
"subject": "BUG: FIXME: Gt is unused by cobra..."
"subject": "TODO: TODO: this isn't quite right..."
```

The type prefix (`BUG:`, `TODO:`) is prepended by the formatter, and the original comment tag (`FIXME:`, `TODO:`) is already in the title, creating doubled prefixes.

**Recommendation**: Don't prepend the type to the subject when the title already contains the annotation keyword.

---

## Phase 4: Filtering & Tuning

### F9. `--min-confidence 2.0` silently accepted — runs full scan, returns 0
**Severity: P2** | **Category: Input Validation**

```bash
$ stringer scan . --min-confidence 2.0
# ... runs full scan (1.1s) ...
# confidence filter: before=19 after=0 min=2
# scan complete: issues=0
```

The help text says `(0.0-1.0)` but values outside this range are silently accepted. A typo like `--min-confidence 70` (forgetting to use decimal) would produce zero results with no warning.

**Recommendation**: Validate that `--min-confidence` is between 0.0 and 1.0 and exit with an error if out of range.

### F10. `--kind` filter still runs all collectors
**Severity: P3** | **Category: Performance**

```bash
$ stringer scan . --kind todo --dry-run
# gitlog: 10 signals, lotteryrisk: 10 signals, patterns: 42 signals, todos: 36 signals
# kind filter: before=98 after=13
```

When filtering by `--kind todo`, all collectors still run. The dry-run output shows 98 signals collected but only 13 survive the filter. For a kind like `todo` that only comes from the `todos` collector, this wastes time.

**Recommendation**: Consider optimizing by only running collectors that can produce the requested kinds. Low priority since scan is fast anyway.

### F11. Confidence filter log shows "before" and "after" but not what was removed
**Severity: P3** | **Category: UX**

```
confidence filter: before=19 after=14 min=0.7
```

Good — this tells you filtering happened. But if you wanted to know which signals were dropped (and why), there's no way to see that without comparing two runs.

**Recommendation**: In verbose mode, log the dropped signals. Low priority.

---

## Phase 5: Report Command

### F12. Report command is excellent
**Severity: N/A** | **Category: Delight**

The report output is the single best feature of the tool. Well-formatted, colored, actionable. The recommendations section distills signals into prioritized advice. The TODO age distribution with the histogram is particularly effective.

The cobra report correctly identified:
- `assets/` directory as CRITICAL lottery risk
- 4 reverts → "Review testing and code review processes"
- 6 TODOs all over 1 year → "Review and resolve or remove"

This is genuinely useful output that would help a team prioritize work.

### F13. Report vs Scan relationship unclear
**Severity: P3** | **Category: Discoverability**

A new user wouldn't know when to use `report` vs `scan`. They solve different problems:
- `scan` = generate importable backlog items
- `report` = get a health dashboard

But the help text doesn't clarify this distinction.

**Recommendation**: Add a brief comparison in the top-level help or in each command's description. E.g., "scan produces importable issues; report produces a health dashboard."

---

## Phase 6: Docs & Context

### F14. Context command correctly prompts for delta scan
**Severity: N/A** | **Category: Delight**

The `stringer context` output includes:
```
## Known Technical Debt

No scan data available. Run `stringer scan --delta .` to populate.
```

This is a nice breadcrumb that guides the user to run delta scan before context, without being an error.

### F15. Docs command detects Go 1.15 for cobra (go.mod says `go 1.15`)
**Severity: P3** | **Category: Signal Quality**

Cobra's `go.mod` says `go 1.15` (the minimum supported version), not the version the project actually uses. The docs output says "Go 1.15 (detected from go.mod)" which is technically correct but potentially misleading — cobra CI tests against Go 1.22+.

**Recommendation**: Consider also checking `.github/workflows/` for Go version matrix, or noting the distinction between minimum required version and actual CI version.

---

## Phase 7: Delta Scanning

### F16. Delta scan works correctly
**Severity: N/A** | **Category: Delight**

First run: `delta filter total=19 new=19` → outputs all signals, saves state.
Second run: `delta filter total=19 new=0` → `Delta scan summary: no changes` → clean exit.

The "no changes" message is clear. State persistence works. This is well-implemented.

---

## Phase 8: Error Handling

### F17. Error messages are excellent
**Severity: N/A** | **Category: Delight**

| Scenario | Message |
|----------|---------|
| Bad collector | `unknown collector: "nonexistent" (available: dephealth, github, gitlog, lotteryrisk, patterns, todos)` |
| Bad format | `unknown format: "badformat" (available: beads, json, markdown, tasks)` |
| Bad path | `cannot resolve path "/nonexistent/path" (lstat /nonexistent: no such file or directory)` |

Every error message includes what went wrong AND what the valid options are. This is better than most CLIs.

### F18. `-c github` without GITHUB_TOKEN silently produces 0 results
**Severity: P2** | **Category: Error Handling**

```bash
$ stringer scan . -c github
# GITHUB_TOKEN not set, skipping GitHub collector
# scan complete: issues=0
```

When a user *explicitly requests* the github collector with `-c github`, silently skipping it and producing 0 results is confusing. The log message appears on stderr but the exit code is 0.

**Recommendation**: When a collector is explicitly requested via `-c` but cannot run (no token, no go.mod for dephealth, etc.), warn more prominently or exit with a non-zero code. The current behavior is fine for *default* runs where github is enabled but optional.

---

## Phase 9: Cross-Cutting Concerns

### F19. Signal count varies wildly — no guidance on what's "normal"
**Severity: P3** | **Category: UX**

| Repo | Signals | Duration |
|------|---------|----------|
| cobra (small lib) | 19 | 1.2s |
| echo (medium framework) | 29 | 2.7s |
| fzf (large CLI) | 98 | 3.6s |

98 signals for fzf is overwhelming. A user would benefit from knowing a typical range and seeing a suggestion like "try `--min-confidence 0.7` to focus on high-confidence signals (54 of 98)."

**Recommendation**: After scan, if signal count exceeds a threshold (e.g., 50), print a hint like: `Tip: Use --min-confidence 0.7 to filter to N high-confidence signals.`

### F20. fzf scan: 26 "missing test" false positives for Ruby/shell test files
**Severity: P2** | **Category: Signal Quality** (also see F6)

Of fzf's 98 signals, **26 are "No test file found"** — and most are for Ruby test files, shell scripts, or generated code. This is 26% of all signals being noise.

Combined with the 7 false-positive BUGs from the CHANGELOG (F5), that's **33 of 98 signals (34%) being noise** for fzf.

For cobra (cleaner Go-only project), the false positive rate is much lower (~4 missing-tests / 19 = 21%).

---

## Summary Table

| ID | Severity | Category | Finding |
|----|----------|----------|---------|
| F5 | **P1** | Signal Quality | False-positive BUG detection from CHANGELOG section headers |
| F4 | **P2** | UX / Output | Progress logging to stderr is extremely verbose |
| F6 | **P2** | Signal Quality | "No test file found" flags test files as missing tests |
| F7 | **P2** | Signal Quality | Lottery risk labels inverted ("Low" for lottery risk 1) |
| F9 | **P2** | Input Validation | `--min-confidence 2.0` silently accepted, returns 0 results |
| F18 | **P2** | Error Handling | `-c github` without token silently produces 0 results |
| F20 | **P2** | Signal Quality | 34% false positive rate on polyglot repos (fzf) |
| F1 | **P3** | Onboarding | Help text doesn't guide first-time users |
| F2 | **P3** | Init | Config identical for all repos, dephealth missing from config |
| F3 | **P3** | Onboarding | Init next steps prioritize MCP over basic usage |
| F8 | **P3** | Output Format | Tasks format has redundant type prefixes |
| F10 | **P3** | Performance | `--kind` filter runs all collectors unnecessarily |
| F11 | **P3** | UX | Confidence filter doesn't show what was dropped |
| F13 | **P3** | Discoverability | Report vs Scan relationship unclear |
| F15 | **P3** | Signal Quality | Go version from go.mod may not reflect actual project version |
| F19 | **P3** | UX | No guidance on signal count or filtering when output is large |

### What Works Well

- **Report command** is the standout feature — well-formatted, actionable, colored
- **Error messages** are best-in-class (show what's wrong + valid options)
- **Delta scanning** works cleanly and reliably
- **Dry-run + JSON mode** is exactly right for pre-flight checks
- **Context command** gracefully handles missing scan data
- **Filtering** (`--min-confidence`, `--kind`, `-c`, `-x`) all work as expected
- **Performance** is good across all repo sizes (1-4 seconds)
- **Quiet mode** (`-q`) properly suppresses log noise

### Recommended Priority Order

1. **F5** (P1): Fix CHANGELOG false positives — easiest win for signal quality
2. **F9** (P2): Validate `--min-confidence` range — trivial fix, prevents confusion
3. **F6 + F20** (P2): Improve missing-test heuristics for polyglot repos
4. **F7** (P2): Fix lottery risk label inversion
5. **F4** (P2): Default to quieter output (or make `-q` default)
6. **F18** (P2): Warn/error when explicitly requested collector can't run
