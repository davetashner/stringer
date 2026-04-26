# 022: Opportunity-Axis Surfacing — Reframing Existing Signals as Candidate Work

**Status:** Proposed
**Date:** 2026-04-26
**Context:** stringer-w1d (epic). Originated from the question: can the same signal pipeline that surfaces debt also surface candidate features and tooling improvements? A multi-phase deep-dive (signal taxonomy, product framing, architecture, simplicity, external research) produced consolidated artifacts under `.review/` (gitignored) and the conservative recommendation captured here. Children: stringer-w1d.1 (Layer 0), stringer-w1d.2 (Layer 1), stringer-w1d.3 (Layer 4), stringer-w1d.4 (external user study).

## Problem

Today every collector emits a `RawSignal` that maps to a Beads issue of type `bug | task | chore`. The implicit framing is uniformly debt-shaped: "go fix this." But several existing collectors already produce signals whose subject is something to *build*, *extract*, or *elevate* rather than something to fix:

- `apidrift` `unimplemented-route` — an OpenAPI route specified with no handler is a specced-but-unbuilt feature.
- `github` `github-feature` — an open issue labelled `enhancement` is a stated feature request.
- `duplication` `code-clone` across packages — a shared abstraction the codebase hasn't extracted.
- `configdrift` `inconsistent-defaults` / `env-var-drift` — motivates a config-schema or validation layer.
- `coupling` `circular-dependency` and `high-coupling` — extraction targets.
- `dephealth` `local-replace` (persisted `go.mod` replace directives) — publishable-module candidates.
- `complexity` `complex-function` — concrete extraction targets.
- `apidrift` `undocumented-route` — lowest-friction "new" documentation features.
- `docstale` `doc-code-drift` and `broken-doc-link` — doc-generation tooling motivators.

These are already collected. What is missing is a lens that says *"these particular signals are also opportunity candidates."* The question for this DR is *how* to add that lens without (a) introducing new false-positive surface, (b) breaking the strict-semver / additive-only contract, (c) eroding Stringer's deterministic, confidence-scored, low-FP brand, and (d) drifting into the "AI tells you what to build" category that external research (Gartner Apr 2026 agent-washing; MIT 2025 95% GenAI pilot failure) shows is structurally crowded with bad products.

## Options

Six architectural surfaces were considered. Full file-level sketches in `.review/architecture.md`.

### Option A — New collector(s) + new bead `type` (e.g., `feature` or `enhancement`)

A dedicated `opportunities` collector emitting new kinds (`extraction-candidate`, etc.) and a new bead `type` value so downstream tracks them distinctly.

**Pros:** Bead type makes the axis first-class. Cleanest semantic separation.

**Cons:** **Breaking-change.** New `type` value mutates the Beads JSONL contract enum (`bug | task | chore`). Requires a major version bump under strict-semver. Cross-collector synthesis (the most useful opportunities) requires aggregation that the `Collector` interface does not provide.

### Option B — Post-aggregation interpreter pass (`internal/pipeline/interpret.go`)

A new pipeline stage runs after `Pipeline.Run()` and dedup, walks the aggregated signal list, and synthesizes new opportunity rows by joining across collectors.

**Pros:** Enables genuinely cross-collector opportunities (e.g., `extraction-candidate` = high churn + high coupling + low lottery risk in the same directory). Architecturally clean — mirrors `internal/pipeline/enrich.go`.

**Cons:** This is the layer where external precision research shows the floor collapses (JExtract: 22–42% precision for Extract Method recommendations). New pipeline stage = larger architectural surface than this DR wants for a v1 ship. Ships nothing on its own without rule content.

### Option C — New top-level command `stringer opportunities`

A second CLI verb that runs a parallel pipeline configuration emphasizing opportunity signals.

**Pros:** Distinct UX surface. Easier to position separately from `scan`.

**Cons:** Two commands to maintain. Adds CLI surface for a narrow lens. Most signal sources are shared with `scan` — duplication of execution path.

### Option D — New MCP tool `opportunities`

A fourth MCP tool alongside `scan / report / context / docs`.

**Pros:** Ergonomic for agent consumers; explicit in tool naming.

**Cons:** New tool is committed surface (additive-with-caveats — agents will start depending on it). Premature without the underlying logic existing in the scan path first.

### Option E — New report section (`internal/report/opportunities.go`)

A new `report.Section` (peer of `lotteryrisk`, `churn`, `coverage`, etc.) that renders opportunity signals from the existing `ScanResult`.

**Pros:** Pure read-model layered on existing data. Zero schema change. Ships as one new file plus a registration line. Lowest possible blast radius.

**Cons:** Read-only — does not affect the JSONL output that backlog-seeding consumers use. Without a label injected upstream (Option F), Beads consumers see no difference.

### Option F — New `Kind` strings on existing collectors + label injection

Existing collectors continue emitting their existing kinds; a small whitelist controls which kinds carry an additional `stringer-opportunity` label, applied centrally in the formatter.

**Pros:** Fully additive — no new pipeline stage, no new collector, no new bead type, no new CLI flag, no new MCP tool. New `Kind` values (when collectors gain them in later layers) and a single new label string. Hash-stable: dedup formula unchanged. Affects every output that already labels signals (Beads JSONL, JSON, Tasks, SARIF).

**Cons:** Bound to single-collector signals only. Cross-collector opportunities require Option B's aggregation surface — explicitly out of scope for v1.

## Recommendation

**Option E + Option F, layered, additive-only. Cross-collector synthesis (Option B) deferred behind an explicit user-request gate. Options A and C declined.**

Rationale:

1. **Smallest possible surface that delivers value.** The whitelist + central label injection (Option F) costs roughly: one new package (`internal/opportunity/`), one three-line edit in `internal/output/beads.go` (`buildLabels`), one new report section (Option E). No new bead type, no new pipeline stage, no new CLI flag, no new MCP tool.
2. **Strict-semver-additive.** No change to `signal.RawSignal` fields. No change to the SHA-256 dedup hash formula (`Source+Kind+FilePath+Line+Title`). No change to the Beads JSONL `type` enum. New `Kind` strings (when added in later layers) take new hashes under the existing formula. New `labels` array values only.
3. **Brand-preserving.** Centralised label injection enforced from a single function in the formatter, gated by a published whitelist of existing deterministic kinds. No file under `internal/opportunity/` or `internal/report/opportunities*` may import `internal/llm` or `internal/analysis` — a CI-level import check enforces this.
4. **Composable with existing UX.** Opportunity signals use the same `str-XXXX` ID format; existing `stringer baseline suppress` works unchanged. `--delta` correctly reports an `unimplemented-route` as removed once it gets implemented and the spec/handler align.
5. **External-precision-aligned.** The whitelist is sized to the precision shape Arcan published (architectural smell detection: 100% precision, 60–66% recall — better to miss real opportunities than to surface false ones). DECOR's 8% precision and JExtract's 22–42% precision define the band Stringer must stay above; the recommendation's quantitative gate is ≤20% expected false-positive rate per kind.

## Decision

[To be filled in by a developer after review. The product judgements that determine the shape of the implementation are captured below as "Decisions captured during proposal." If the maintainer accepts as-stated, change Status to Accepted in the same PR that lands stringer-w1d.2 (Layer 1).]

## Layered rollout

The recommendation is sequenced as four additive layers. Each is independently shippable; each except Layer 0 is gated on the previous layer's evidence.

### Layer 0 — Pre-existing correctness fix (stringer-w1d.1)

**Scope.** `internal/collectors/github.go:305-310` currently overwrites `Kind = "github-stale-issue"` unconditionally on the stale path, silently destroying the original `github-feature` (or `github-bug`) classification. Fix: preserve the original `Kind`, append `"stale"` to `Tags`, multiply `Confidence` by 0.7.

**Why Layer 0.** Independently warranted regardless of opportunity-axis rollout. The bug masks a meaningful share of real `github-feature` signals today.

**Semver.** Additive. Existing `github-stale-issue` baseline suppressions see a one-time delta spike (new hashes); call out in release notes.

**Dependencies.** None. Ships before Layer 1.

### Layer 1 — Opportunity label + 13-kind whitelist + report section (stringer-w1d.2)

**Scope.** Three net-new files and three small edits.

Files created:
- `internal/opportunity/kinds.go` — exported `map[string]Descriptor` seeded with the Layer 1 whitelist; exported `IsOpportunityKind(kind string) bool`.
- `internal/opportunity/kinds_test.go` — (a) drift test: every whitelist entry corresponds to a kind currently emitted by a registered collector; (b) `TestWhitelistExcludesNeverKinds`: negative assertion on the Bottom-5 (`vulnerable-dependency`, `committed-secret`, `merge-conflict-marker`, `yanked-dependency` / `retracted-version`, `github-review-todo`).
- `internal/report/opportunities.go` — new `report.Section` registered via `init()`; reads `ScanResult.Signals`; renders a grouped counts table + top-5-per-kind by confidence.
- `internal/report/opportunities_test.go` — golden rendering test + integration test invoking `stringer report --sections opportunities`.

Files modified:
- `internal/output/beads.go` `buildLabels` (~L222-238) — three lines: `if opportunity.IsOpportunityKind(sig.Kind) { labels = append(labels, "stringer-opportunity") }`. The label is injected centrally off `Kind`, never sprinkled through collectors.
- `internal/collectors/todos.go` — populate `Description` with the TODO body (precision improvement; addresses the taxonomy gap where the body was previously empty).
- `AGENTS.md` "Key Design Decisions" — add the opportunity-axis rule (deterministic-only; central injection off `Kind`; opportunity path forbidden from importing `internal/llm` or `internal/analysis`; opportunity signals capped at P3 unless promoted).
- `README.md` — add the "Repository Intelligence" framing paragraph in "Why Stringer?" alongside the existing "codebase archaeology" tagline.

The Layer 1 whitelist (13 kinds), each with expected FP ≤ ~20% per `.review/research.md` calibration:

| # | Kind | Source collector | Reframing the label unlocks |
|:-:|------|------------------|-----------------------------|
| 1 | `unimplemented-route` | apidrift | Specced-but-unbuilt feature (the spec is the brief) |
| 2 | `circular-dependency` | coupling | Ring → candidate shared-package extraction |
| 3 | `github-feature` | github | Explicitly-filed feature request |
| 4 | `github-pr-approved` | github | Approved feature work awaiting merge |
| 5 | `inconsistent-defaults` | configdrift | Motivates a config-schema / validation layer |
| 6 | `env-var-drift` | configdrift | Undocumented config surface → typed schema candidate |
| 7 | `code-clone` | duplication | Exact cross-file clone → dedupe or extract helper |
| 8 | `local-replace` | dephealth | Persisted `go.mod` local replace → publishable module |
| 9 | `high-coupling` | coupling | Fan-out hotspot → facade + sub-packages |
| 10 | `complex-function` | complexity | Extraction target |
| 11 | `undocumented-route` | apidrift | Lowest-friction "new" documented endpoint |
| 12 | `doc-code-drift` | docstale | Persistent drift → doc-generation tooling motivator |
| 13 | `broken-doc-link` | docstale | Cluster → missing CI link-check gate |

Seven additional candidates (`near-clone`, `low-lottery-risk`, `stale-doc`, `large-file`, `stale-api-version`, `dead-config-key`, `review-concentration`) are deferred to Layer 2 with explicit per-kind promotion gates documented in `.review/synthesis.md`.

**Semver.** Fully additive. No new bead `type`. No new CLI flag. No new MCP tool. The opportunity DR flips Status: Proposed → Accepted in the same PR.

**Dependencies.** stringer-w1d.1 must land first so that the `github-feature` opportunity correctly survives staleness.

### Layer 2 — Promote deferred kinds (gated)

**Scope.** Promote each of the 7 deferred kinds to the whitelist by adding their per-kind co-occurrence / scope gates. Each promotion is independently shippable.

**Promotion gate.** Layer 1 acceptance-rate telemetry across the 10-repo benchmark in `README` "Real-World Results" shows per-kind acceptance ≥ 50% and defer-rate ≤ 30%.

**Semver.** Additive (new whitelist entries; the kinds themselves already exist as Stringer signal kinds).

### Layer 3 — Cross-collector synthesis (deferred, demand-gated)

**Scope.** Option B materialised: a new `internal/pipeline/interpret.go` post-aggregation stage that emits synthesized kinds from joins across collectors. The 14-rule catalog drafted in `.review/synthesis.md` (rules R-OPP-01 through R-OPP-14) defines candidate logic — `extraction-candidate`, `dedupe-library`, `testability-refactor`, etc.

**Gate for entry.** Explicit user request from a Tier-1 persona (staff-engineer RFC author, AI agent author, platform/DX team) in writing, after Layers 1–2 are live.

**Why deferred.** External research shows cross-collector synthesis is precisely where precision collapses into the JExtract 22–42% band. The simplicity review rejected this layer outright on grounds of brand erosion; the architectural review confirmed it requires a new pipeline stage. Keeping it parked preserves the door without paying its precision cost up front.

**Semver.** Additive at the signal level; borderline at the architecture level (new pipeline stage).

### Layer 4 — Extend baseline reasons (stringer-w1d.3)

**Scope.** `internal/baseline/baseline.go:39-54` currently defines three valid suppression reasons (`acknowledged`, `won't-fix`, `false-positive`). Add two:

- `deferred` — "real opportunity, not now"
- `out-of-scope` — "real opportunity, not for this project"

**Why this matters.** Layer 2 promotion telemetry depends on distinguishing "rejected" from "deferred." Conflating these collapses three useful signals (acceptance, taste, precision) into one fuzzy one. Mirrors CodeScene's three-state lifecycle (Planned Refactoring / Supervise / No Problem) without copying their vocabulary.

**Semver.** Additive-with-caveats. The `ValidateReason` error message text changes (lists 5 reasons instead of 3); scripts matching against the old enumerated text may see different output. Documented in release notes.

**Dependencies.** Ships alongside or after Layer 1. Useful immediately for any opportunity signal a user wants to defer.

## Decisions captured during proposal

| ID | Question | Choice | Rationale |
|:-:|----------|--------|-----------|
| Q1 | Layer 1 whitelist size | **13 kinds (Arcan-shaped FP bar)** | Matches the 100%-precision-at-60%-recall shape Arcan published. 16-kind generous set sneaks in three high-FP kinds at default settings, contradicting the precision brand. 2-kind minimum is too narrow to move the needle on most repos. Expansion or retraction post-ship is a one-line whitelist edit. |
| Q2 | Category positioning | **Layered: keep "codebase archaeology" tagline; introduce "opportunity mode" framing as the new surface** | Preserves brand continuity. Maps cleanly onto the additive layered architecture. The README addition is one paragraph in "Why Stringer?", not a tagline rewrite. |
| Q3 | Layer 2 evidence | **Self-triage on the 10-repo benchmark with bias disclosure in release notes; external user study planned as a follow-up bead (stringer-w1d.4)** | External study would be more rigorous but depends on coordination outside the maintainer's control. Self-triage produces per-kind acceptance data fast enough to gate Layer 2; the bias is disclosed honestly. The follow-up bead protects the path to a higher-rigour future study. |
| Q4 | Layer 3 commitment | **Build only on explicit Tier-1 user request after Layers 1–2 are live** | Pre-committing to cross-collector synthesis commits engineering surface to the layer with the highest brand-erosion risk before any evidence justifies it. Indefinitely deferring forecloses a real value channel (platform/DX persona). The pull-based default keeps the option open at zero cost. |
| Q5 | Baseline reasons extension | **Add both `deferred` and `out-of-scope`** | Layer 2 promotion telemetry from Q3 requires distinguishing defer from reject. Both reasons are cheap to add now and additive-with-caveats under semver. |

## Guardrails

These are shipping gates, not recommendations.

1. **Three forbidden phrases.** The strings **"opportunity discovery"**, **"AI-powered"** (in the opportunity context), and **"roadmap"** must not appear in the README, MCP tool descriptors, or release notes. Enforced by a release-time review against the rendered text. Grounded in Gartner April 2026 agent-washing naming, MIT 2025 95% GenAI pilot failure data, and Sourcery's ~50%-noise market positioning (research.md §8).
2. **`--no-llm` strict-compatible.** No file under `internal/opportunity/` or `internal/report/opportunities*` may import `internal/llm` or `internal/analysis`. Enforced by a CI-level import check (the existing `Doc Staleness` workflow can be extended, or a small new test under `internal/opportunity/imports_test.go`).
3. **Opportunity priority cap.** Opportunity signals cap at P3 by default unless explicitly promoted via a maintainer-controlled flag in the descriptor. Prevents rule drift into the highest-urgency band.
4. **Negative-assertion test.** `TestWhitelistExcludesNeverKinds` ensures vulnerabilities, committed secrets, merge conflict markers, yanked dependencies, and review TODOs can never carry the `stringer-opportunity` label, regardless of future whitelist edits.
5. **Whitelist drift test.** `TestWhitelistDrift` walks `collector.List()` and fails if a whitelist entry has no emitter. Catches stale whitelist entries when a collector is removed or a kind is renamed.

## Risks

| # | Risk | Mitigation |
|:-:|------|-----------|
| 1 | Precision-brand erosion from the existence of an opportunity label | Layer 1 is purely additive over deterministic kinds; label injected centrally; confidence-to-priority mapping unchanged; per-kind FP bar enforced. |
| 2 | Volume drowning if the whitelist expands on noisy kinds | Arcan-shaped FP bar (≤20%) is the quantitative gate for every promotion. 7 candidates already deferred to Layer 2 on this basis. |
| 3 | "AI-powered roadmap" positioning contamination | Three-phrase shipping gate. README addition refuses demand-inference claims. `--no-llm` stays default. |
| 4 | Layer 0 fix introduces baseline-suppression delta for users with existing `github-stale-issue` suppressions | One-time spike documented in release notes. Users opt-in to re-suppress after reviewing the now-distinguishable stale-feature vs. stale-bug flavours. |
| 5 | Whitelist drift as new collectors ship | `TestWhitelistDrift` runs in the standard test suite; any new-collector PR that breaks it is caught pre-merge. |

## References

- `.review/synthesis.md` — combined view across specialists (gitignored)
- `.review/architecture.md` — six-option file-level sketches (gitignored)
- `.review/taxonomy.md` — 52-kind opportunity classification (gitignored)
- `.review/research.md` — academic + industry precedent, precision-recall calibration (gitignored)
- `.review/yagni.md` — adversarial simplicity critique (gitignored)
- `.review/pm.md` — persona + competitive analysis (gitignored)
- DR-004 — confidence scoring semantics (priority bands)
- DR-010 — shared signal ID generation (dedup hash inputs)
- AGENTS.md — Beads JSONL output contract; Breaking change surfaces; Adding a new collector
