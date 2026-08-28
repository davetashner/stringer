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

## Consequences

- Scores drop for flat/JSX-heavy code and hold for nested code; signal titles change (nesting added), so signal IDs change and delta scans will report these as new once.
- Indentation-derived depth is a heuristic: unindented (minified) code reads as flat — acceptable, since minified files are mostly excluded as generated.
- Tree-sitter adoption would collapse the two analysis tiers entirely; this record does not preclude it.
