# 024: Nesting-Weighted Complexity for Regex-Analyzed Languages

**Status:** Accepted (pre-authorized in session, 2026-08-27)
**Date:** 2026-08-27
**Context:** stringer-t98, stringer-sby, stringer-h51 — evaluation of 43 complexity beads on davetashner/sandtable (TypeScript/React) found 25 (58%) pointing at one schema-validator file whose functions are flat lists of independent one-line guards. The old regex score (`lines/50 + branch count`) cannot distinguish twenty flat guards from four conditions nested four deep; Go's AST path already can.

## Problem

Non-Go languages get token counting where complexity is about structure:

1. No nesting term — the score *is* the branch count.
2. `&&`/`||` counted as branches everywhere, including JSX conditional rendering (`{cond && <X/>}`), React's declarative "show this when" idiom.
3. Trailing comments and string literals counted — `doThing() // if this fails, retry` and `"retry if unclear"` each contribute a branch.
4. The bead body was a bare `Location:` line — no metrics, no rationale, no dismiss criteria.

## Options

**Nesting source:** (a) tree-sitter/AST per language — correct but a large dependency and per-language work (remains the long-term fix, tracked separately); (b) indentation-derived depth. Chose **(b)** for now: depth = 1 + (indent − base)/unit, where unit is the smallest observed indent step (≥2 columns, fallback 4), capped at 10. Continuation-line indentation is kept out of the reported nesting by tracking max depth only on lines that carry control-flow keywords.

**Weighting:** cognitive-complexity shape — a branch keyword at depth *d* costs *d* (flat guards cost 1 each, exactly the old behavior; nesting is superlinear in aggregate because deeper branches imply their enclosing branches). Logical operators cost a flat 1 — they are conditions, not structure — and 0.5 in `.jsx`/`.tsx` files, where distinguishing JSX expressions from logic without a parser is impractical; the discount is documented rather than hidden.

**Flat-function confidence cap:** when max nesting ≤ 2, confidence is capped at 0.55 (P3). A validator or dispatch table written as a flat rule list is often the clearest form of that code; the cap keeps such findings visible without letting them claim P1/P2, and the bead body says explicitly that the finding is dismissible. On sandtable this demotes all 25 validator beads while `TourProvider` (genuinely tangled, produced a shipped bug) keeps its top rank.

**Counting hygiene:** a line-local quote-state scan strips string contents and trailing `//`, `#` (Python/Ruby/Elixir), and `/* */` comments before matching. Rust exempts `'` (lifetimes would read as unterminated char literals).

**Bead bodies:** every complexity signal now carries WHAT (the metrics), WHY, ACTION, DISMISS, and CONTEXT (threshold + config key), per stringer-h51. DISMISS is tailored: flat functions are told they are probably fine.

## Amendment (same day, from acceptance testing)

The DR-013 confidence bands (0.8 at score 15) were calibrated for raw branch counts; depth-weighted scores run roughly 2× higher, which pushed ordinary loop+guard+condition validators (nesting 3) to P1 — 16 P1 findings in one file on the eval repo. Bands recalibrated to the AST path's cognitive/30 shape: 0.8 at 30, 0.6 at 18, 0.4 at 6. After recalibration the eval repo's validator file retains two P1s (its two genuinely densest functions) and the top hotspot (TourProvider, 97.5) keeps its rank.

## Amendment (2026-09-18, stringer-nxx.5: test code and the non-Go floor)

The September 2026 benchmark on v1.10.0 showed two further sources of noise. First, test code was scored like production code: express reported 28 of 39 complexity signals in `test/` (mocha `describe`/`it` callbacks titled "Complex function: describe (score 108.0, 302 lines)" at 0.80), gin flagged `TestTreeFindCaseInsensitivePath` at 0.90, kafka had 1,869 of 5,208 in `src/test`, kubernetes 8,622 of 24,140 in `_test.go`. Table-driven tests and nested suite callbacks score high on every metric and are never refactor candidates. Second, the regex floor of 6 (confidence 0.40) let ordinary 16-line, 4-branch methods through: 3,137 of kafka's 5,208 complexity signals were below 0.5 confidence.

Decisions:

- **Test code is skipped, not down-weighted.** Down-weighting (0.5× confidence, 2× threshold) would keep thousands of low-priority findings in large repos and still require a reader to dismiss them; skipping is the behaviour the duplication collector's `test-only` tag already steers users towards, and `collectors.complexity.include_tests: true` restores the findings, tagged `test-file`. Test detection reuses the shared language-aware `isTestFile` classifier and adds three complexity-specific rules: any path component in `test`, `tests`, `__tests__`, `spec`, `testdata` (express keeps its suite in `test/app.js`, which no naming convention catches); JS/TS `describe`/`it`/`test`/hook callbacks anywhere (the JS regex reads `describe('x', function() {` as a function named `describe`); and Rust fns under a `#[test]`/`#[tokio::test]`/`#[bench]`/`#[cfg(test)]` attribute, so inline `mod tests` blocks in `src/` count. The directory rule will also skip test-support libraries such as `django/test/`; that is accepted and the flag is the escape hatch.
- **JS/TS test callbacks are named by their string literal** — `describe("app.render") callback` — so the title is readable when `include_tests` is on.
- **The regex floor rises from 6 to 12.** Under the bands above (0.4 at 6, 0.6 at 18) a score of 12 is exactly confidence 0.5, so nothing below 0.5 is emitted by default for regex-analyzed languages. Go's AST path keeps its cyclomatic floor of 6 unchanged. A single `min_complexity_score` still overrides both paths; setting it to 6 restores the previous non-Go behaviour.

On the stringer repo itself (Go only) this removes the 66 of 363 complexity signals that lived in `_test.go` and leaves the remaining 297 unchanged.

## Consequences

- Scores drop for flat/JSX-heavy code and hold for nested code; signal titles change (nesting added), so signal IDs change and delta scans will report these as new once.
- Indentation-derived depth is a heuristic: unindented (minified) code reads as flat — acceptable, since minified files are mostly excluded as generated.
- Tree-sitter adoption would collapse the two analysis tiers entirely; this record does not preclude it.
