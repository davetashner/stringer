# 025: Aggregator Exemption for Fan-Out Coupling Signals

**Status:** Accepted (pre-authorized in session, 2026-08-27)
**Date:** 2026-08-27
**Context:** stringer-3v0, stringer-h51 — of three high-coupling beads filed on davetashner/sandtable, two were false by construction: a component gallery whose purpose (enforced by a test) is importing every component, and the composition root of a single-page app. The third cleared the threshold at ten imports — unremarkable for a React card component.

## Problem

Fan-out is a real signal about modules in the *middle* of the import graph. A module whose role is aggregation — an entry point, a gallery, a barrel — has wide imports as its job, and an absolute threshold of 10 flags ordinary component-per-file code.

## Options

Considered per the bead, cheapest first; adopted a combination:

1. **Per-path exemption config** — `coupling_exempt_patterns` glob list under the coupling collector, so a repo states "gallery and entry points are exempt" once. Adopted.
2. **Automatic role recognition** — adopted two cheap, safe rules:
   - *Entry points:* modules with in-degree 0 in the intra-project graph, counting only non-test importers (a test importing its subject does not make the subject mid-graph). Covers `main`, app roots, galleries, scripts.
   - *Barrels:* modules whose basename is `index`, `mod`, or `__init__`.
   Build-config entry detection (vite/webpack/package.json) was deferred: the in-degree rule already covers those files in practice.
3. **Relative (repo-scaled) threshold** — deferred. The absolute default moved 10 → 15 instead; component-per-file ecosystems clear 10 trivially. Repo-relative ranking remains open if 15 proves wrong.
4. **Rich bead body** — every fan-out signal now carries WHAT (count vs threshold), WHY, ACTION, a DISMISS naming the auto-exemptions and the config key, and CONTEXT. Circular-dependency signals already had descriptive bodies.

## Consequences

- Exempted modules are counted in `CouplingMetrics.ExemptedAggregators` so the report can show what was suppressed rather than silently dropping it.
- A genuinely problematic god-module that nothing imports *yet* (dead aggregate) will not be flagged by fan-out; the deadcode collector is the right owner for that shape.
- Threshold change 10 → 15 may suppress previously-reported signals; noted in the changelog.
