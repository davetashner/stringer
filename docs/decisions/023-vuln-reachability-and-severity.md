# 023: Vulnerability Reachability and Severity Scoring

**Status:** Accepted (pre-authorized in session, 2026-08-27)
**Date:** 2026-08-27
**Context:** stringer-kgr, stringer-bqg, stringer-g9b — evaluation of stringer 1.8.6 output against davetashner/sandtable (TypeScript/React) showed the vuln collector inverting triage: four of five filed beads were dev-only, two at P1, while the one production-reachable advisory (image-size 0.7.5, nested under texture-compressor) was missed entirely. Severity ordering was unrelated to CVSS score.

## Problem

Three related accuracy defects in the vuln collector:

1. **Lost findings.** `parseNpmLockDeps` deduplicated packages by name only, so a lockfile holding the same package at two versions (nested dependencies) queried only one — chosen by map iteration order, i.e. nondeterministically. On sandtable this dropped the production-reachable vulnerable version and kept the dev-only one.
2. **Lost reachability.** The lockfile's `dev` flag was parsed and discarded, so dev-only advisories ranked equal to production ones and no bead said which it was.
3. **Wrong severity.** `severityFromCVSS` derived severity from CIA impact flags alone ("any H → high"), ignoring the base score. A network DoS (7.5) and a full compromise (9.8) tied; a moderate advisory could outrank a high one after flag-flattening.

Additionally, partial OSV failures (unfetchable details, unpaginated `querybatch` results, total network failure) were silent or near-silent, so an incomplete security scan looked complete.

## Options

**Severity:** (a) keep flag heuristic; (b) parse the numeric score OSV sometimes provides; (c) compute the CVSS v3.x base score from the vector per the FIRST spec. Chose **(c)**: deterministic, testable against published scores, works for every advisory carrying a vector, and the flag heuristic remains as fallback for unparseable vectors.

**Confidence bands** (drive P1–P4 via the ≥0.8/≥0.6/≥0.4 mapping): critical 0.95 → P1, high 0.85 → P1, medium 0.65 → P2, low 0.45 → P3, none 0.30 → P4, no-data 0.80 unchanged (avoids reshuffling ecosystems whose advisories lack vectors).

**Dev discount:** (a) separate priority track; (b) confidence multiplier; (c) flat −0.2 with floor 0.3. Chose **(c)**: one band down, so dev+high (0.65 → P2) sorts below prod+high (0.85 → P1) and above prod+medium only on ties — simple, explainable in the bead body. When the same name+version is reachable both ways, production wins.

**Dedup key:** name+version (parser) and vulnID+ecosystem+name+version (client). Each installed instance of a vulnerable version is a distinct finding.

**Partial coverage:** `QueryBatch` now returns `OSVQueryResult{Details, FailedFetches, Truncated}`; `querybatch` pagination (`next_page_token`) is followed up to 5 pages per query; total failure sets `VulnMetrics.Unavailable` and logs at Warn. Scans stay non-fatal offline, but never silently.

## Consequences

- npm gains full reachability awareness; other ecosystems with a dev distinction (Cargo `dev-dependencies`, Poetry/uv groups, Gemfile groups) parse as production until given the same treatment (follow-up bead).
- Severity labels now include "critical" and "none"; consumers of `VulnEntry.Severity` see the new values.
- The npm-audit reconciliation eval (stringer-kgr proposal 2) remains open — this record covers the mechanism fixes, not the eval harness.
