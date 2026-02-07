# Bootstrapping a Beads Backlog with Claude Code

A guide for seeding a project's beads backlog from a structured plan using Claude Code.

## Prerequisites

- [bd CLI](https://github.com/steveyegge/beads) installed
- Claude Code with `--dangerously-skip-permissions` (or approve prompts as they come)

## Step 1: Write the Plan

Create a structured plan document with:

- **Epics** grouped by phase (e.g., Phase 0: Foundation, Phase 1: Core Features)
- **Stories** as numbered children under each epic (e.g., F1.1, F1.2)
- **Priorities** per phase (P0 = highest)
- **Labels** for cross-cutting concerns (e.g., `collector`, `cli`, `testing`)
- **A dependency graph** showing which epics block which

The plan should be detailed enough that each story is independently actionable by an agent. Include acceptance criteria in story descriptions.

## Step 2: Initialize Beads

```bash
bd init
```

This creates the `.beads/` directory and database.

## Step 3: Prompt Claude Code

Give Claude the plan and a prompt like:

> Implement the following plan: [paste plan]

Claude will:

1. **Create epics** using `bd create "Title" -t epic -p <priority> -l <labels> -d <description> --silent`
2. **Create stories** as children using `bd create "Title" -t task --parent <epic-id> --silent`
3. **Wire dependencies** using `bd dep <blocker> --blocks <blocked>`
4. **Verify** with `bd dep cycles` and `bd ready`

## Key `bd` Patterns

```bash
# Create an epic, capture its ID
ID=$(bd create "Epic Title" -t epic -p 0 -l "label1,label2" -d "Description" --silent)

# Create a child story under that epic
bd create "Story Title" -t task -p 0 --parent "$ID" -d "Description" --silent

# Wire a blocking dependency (A blocks B)
bd dep <blocker-id> --blocks <blocked-id>

# Check for dependency cycles
bd dep cycles

# See what's ready to work on (unblocked)
bd ready

# See what depends on an issue
bd dep list <id> --direction up

# Verify total count
bd count
```

## Tips

- **Use `--silent`** to get only the issue ID back — essential for scripting parent-child relationships
- **Create epics before stories** so you have parent IDs for `--parent`
- **Wire dependencies at the epic level**, not story level — keeps the graph manageable
- **Child stories** automatically get `.N` suffixes (e.g., `stringer-aks.1`)
- **Run `bd dep cycles`** after wiring to catch mistakes
- **Run `bd ready`** to verify only the right starting work is surfaced
- **Commit the `.beads/` directory** so the backlog is versioned with the repo

## What You Get

After bootstrapping, agents can:

```bash
bd ready --json        # Find unblocked work
bd show <id>           # Read full story details
bd close <id>          # Mark work done (unblocks dependents)
```

The dependency graph ensures agents work in the right order — completing a foundation epic automatically unblocks the next wave of work.

## Example Backlog Shape

```
Phase 0: Foundation (P0, blocks everything)
  F1: Scaffold ──┐
  F2: Types      ├── All block Phase 1
  F3: Config     │
  F4: CLI        │
  F5: Pipeline ──┘

Phase 1 (parallel tracks after Phase 0):
  Track A: Collectors  ── C1, C2, C3 (independent)
  Track B: Formatters  ── O1, O2, O3 (independent)
  Track C: CLI Commands ── depends on collectors + formatters
  Track D: Testing     ── depends on types

Phase 2+: Later phases depend on Phase 1
```

This structure supports 6-8 agents working in parallel once Phase 0 completes.

## Persona Review Passes

After the initial backlog is bootstrapped, run it through expert persona reviews to stress-test scope, ordering, and completeness. Each pass examines the backlog from a specialized perspective and proposes concrete modifications.

### Pass 1: Expert CLI Designer

**Reviewer prompt:** "You are an expert CLI designer. Examine the beads backlog and assess how well planned this CLI project is."

**What was caught:**

| Finding | Severity | Action Taken |
|---------|----------|--------------|
| No defined MVP or release milestones — 249 issues with no clear "you can stop here" boundary | High | Defined v0.1.0 MVP scope (~27 tasks) with a single goal: `stringer scan . \| bd import -i -` |
| Foundation over-engineered before any feature code — F5 Pipeline Runner had parallel execution, 3 error modes, dedup before a single collector existed | High | Simplified F5 to 2 tasks (sequential execution + basic validation). Moved parallelism/dedup/error modes to new F5-2 epic |
| F1-CI had 12 tasks including GoReleaser, Homebrew tap, SAST, and badges — release infrastructure for a tool with zero functionality | Medium | Trimmed to 5 tasks (build/test/fmt/lint/vet). Reparented 14 tasks to new F1-CI-2: CI Hardening epic |
| F3 (Config) blocking F4 (CLI) — forced config system before any CLI could be built, but hardcoded defaults work fine for MVP | Medium | Deferred F3 to P3, removed as blocker of F4 |
| No TODO collector, beads formatter, or scan command in the backlog — the three things needed for the tool to actually work | High | Created C1 (4 tasks), O1 (3 tasks), CLI1 (4 tasks) epics with proper dependency wiring |
| T1 Test Framework had mock GitHub server and golden file infra before any collector existed | Low | Trimmed to 2 tasks (testify + fixtures). Closed premature test infra tasks |
| Missing `stringer version` command | Low | Added as F4.5 |
| 3 orphaned CLI1 tasks (--min-confidence, --git-depth, --churn-threshold) with no parent epic and dependent on non-existent collectors | Low | Closed with "deferred to post-MVP" |
| All priorities were 0 — no signal about build order within a phase | Low | New MVP epics set to P1, deferred epics set to P3 |

**Estimated quality increase:** The initial backlog was comprehensive in vision but lacked execution focus. The main risk was spending 50+ tasks on scaffolding, CI, and abstractions before writing a single collector or formatter — the pieces that make the tool actually useful. The review compressed the critical path from ~50 open tasks to ~27 MVP-scoped tasks, introduced a clear milestone definition, and ensured the dependency graph reflects what's truly needed to ship a working tool. Estimated reduction in time-to-first-working-version: **40-60%**.

### Pass 2: End User (Beads Adopter on a Legacy Codebase)

**Reviewer prompt:** "You are an end user who is familiar with beads and wants to start using beads on a complicated legacy codebase. Evaluate the repository README and other project assets and provide an analysis of how clear it is what the problem is and how stringer aims to solve it. Propose additional questions we might ask a potential user to influence the product design."

**What was caught:**

| Finding | Severity | Action Taken |
|---------|----------|--------------|
| No maturity signal anywhere — README reads as if the tool is finished and ready to use, but zero code exists. Users who discover vaporware after investing time evaluating feel burned. | High | Created UX1.1: Add pre-alpha status banner to README |
| No guidance on expected output volume — a user scanning a 50k-line legacy codebase has no idea if they'll get 20 beads or 500. Fear of backlog flooding is the #1 barrier to trying the tool. | High | Created UX1.2: Add "What to Expect" section with realistic output guidance |
| No "start small" recipe for nervous adopters — the README shows the full-power happy path but doesn't guide users who want to dip a toe in with a narrow, safe first scan | Medium | Created UX1.3: Add "Start Small" quickstart for legacy codebase adopters |
| Stringer-generated bead lifecycle is hand-waved — README says "idempotent" but doesn't explain what happens on re-scan after fixing TODOs, whether manual edits to stringer beads survive, or the interim story before `--delta` ships | Medium | Created UX1.4: Document stringer-generated bead lifecycle |
| Confidence scoring is a black box — users will filter on `--min-confidence` without understanding what the number means. No documentation of heuristics per collector. | Medium | Created UX1.5: Document noise management strategy. Updated stringer-vkt (C1: TODO Collector) description to flag this as a requirement. |
| No target user personas defined — the tool is being built for "someone" but product decisions (default filters, output volume, LLM requirement) depend on who that someone is | Medium | Created UX2.1: Define target user personas |
| No user interview guide or research plan — 14 product-shaping questions identified (noise tolerance, LLM willingness, codebase profiles, competitive positioning) with no mechanism to collect answers | Low | Created UX2.2: Draft user interview guide |
| Open question: how many beads is too many? No research on noise tolerance thresholds to inform default `--min-confidence` and whether a `--max-issues` cap is needed | Medium | Created UX2.3: Validate noise tolerance thresholds |
| Open question: is `--no-llm` the real MVP? The LLM clustering demo is the most compelling feature in the README but requires an API key and per-scan cost. Unclear if the value prop collapses without it. | Medium | Created UX2.4: Validate LLM-optional positioning |
| Language-agnostic pattern detection will produce false positives on real codebases (generated protobuf files flagged as "large", vendor/ meaning different things per ecosystem) | Low | Updated stringer-8s5 (FUT2: Language Plugins) description with user research note |
| Delta scanning (`--delta`) listed as "coming soon" but is critical for ongoing use — without it, users must choose between wiping/rescanning or never running stringer again after bootstrap | Low | Updated stringer-7ws (A1: Delta Scanning) description with lifecycle urgency note |
| Decision records missing from agent workflow — agents make architectural choices (libraries, patterns, formats) without documenting trade-offs for developer review | Medium | Added Decision Records section to AGENTS.md and CLAUDE.md. Created `docs/decisions/` directory. |

**Estimated quality increase:** Pass 1 fixed the build plan — what to build and in what order. Pass 2 fixes the product story — why someone would adopt stringer and whether they'd trust it on a real codebase. The backlog previously had zero user-facing work items; all 269 issues were implementation tasks. The two new epics (UX1: 5 tasks, UX2: 4 tasks) are entirely unblocked and can run in parallel with scaffolding, meaning product clarity improves without delaying code. The decision records process adds a lightweight governance layer that prevents agents from baking in architectural choices that should be reviewed by developers. Most critically, the noise management and "What to Expect" documentation addresses the #1 adoption risk — that a user's first scan floods their backlog and they never try stringer again. Estimated reduction in first-user-abandonment risk: **30-50%**.

### Pass 3: Software Engineer (Architecture, Security, Testability, Adoption)

**Reviewer prompt:** "You are an expert software engineer. Examine the beads backlog and provide an assessment of how thorough the plan is and how clear the implementation would be. Include an assessment of architecture, security, testability, tool choice, likelihood to be used by other engineers, and other relevant assessment areas."

**What was caught:**

| Finding | Severity | Action Taken |
|---------|----------|--------------|
| Three pairs of duplicate epics — C1, O1, and CLI1 each existed in both "MVP" and "full" versions with disconnected dependency chains. Agents would encounter two competing paths for the same work. | Critical | Closed original epics (stringer-bsf, stringer-koj, stringer-bnp) and all 25 children. Kept MVP versions (stringer-vkt, stringer-r1v, stringer-dov). Rewired 11 downstream dependents (A3 docs, LLM1, CONTEXT.md, pre-closed beads, validate, GitHub output, CI integration, monorepo, delta scanning, documentation, integration tests) to the MVP epics. |
| F3 (Config, deferred P3) still blocked all non-MVP collectors — C2 git log, C3 patterns, C4 GitHub, C5 bus factor couldn't start until the deferred config system shipped | High | Removed stringer-a80 as blocker from stringer-cnk, stringer-rw2, stringer-lmo, stringer-20r. Collectors work with hardcoded defaults; config layers in later. |
| No MVP milestone issue — the goal "stringer scan . \| bd import -i -" existed in docs but had no trackable issue with acceptance criteria | High | Created stringer-gac: "MVP v0.1.0: End-to-end scan-to-import works" with 5 acceptance criteria (real repo scan, bd import pipe, bead verification, idempotency, dry-run). Blocked by CLI1 (stringer-dov). |
| F1 scaffold over-decomposed — 5 tasks for ~30 minutes of work (init module, Makefile, golangci-lint, LICENSE/.gitignore, pre-commit hooks) | Medium | Closed 5 tasks (.1-.5), created 2 consolidated tasks: F1.A "Initialize Go project structure" (stringer-aks.6) and F1.B "Set up build tooling and hooks" (stringer-aks.7). |
| F2 core types over-decomposed — 6 tasks for ~50 lines of Go (one task per struct/interface definition) | Medium | Closed 6 tasks (.1-.6), created 2 consolidated tasks: F2.A "Define core domain types" (stringer-0lx.7) and F2.B "Define interfaces and collector registry" (stringer-0lx.8). |
| F1-CI over-decomposed — 5 separate tasks for what is naturally a single workflow YAML file | Medium | Closed 5 tasks (.1-.5), created 1 consolidated task: F1-CI.A "Create GitHub Actions CI workflow" (stringer-7kb.20). |
| No security tasks — no input path validation, no token leak prevention, no JSONL output sanitization against injection from crafted TODO comments | High | Created S1: Security Hardening epic (stringer-6iv) with 3 tasks: path validation (.1), API token leak prevention (.2), output sanitization (.3). Blocked by F2, blocks MVP milestone. |
| No decision records exist despite AGENTS.md mandating them — foundational choices (Go version, go-git vs git CLI, module path) would be made without documented trade-offs | Medium | Created F1.C "Write decision records for foundational choices" (stringer-aks.8) under F1 scaffold. Covers 001-go-version, 002-git-interaction, 003-module-path. |
| T1 (Test Framework) blocked by F2 (Core Types) — but testify setup and fixtures directory don't depend on domain types at all | Medium | Removed F2 dependency from T1. T1 is now unblocked, runs in parallel with F1 as a third concurrent track. |
| bd import round-trip test was P2 and buried under a non-MVP epic — despite being the #1 acceptance criterion for the entire MVP | High | Created T1.6 "bd import round-trip integration test" (stringer-j5s.6) at P1 under the unblocked T1 epic. Tests scan → JSONL → bd import → verify → re-scan idempotency. |
| No competitive analysis — existing tools (leasot, fixme, SonarQube TODO rules, CodeClimate) not evaluated for differentiation or gaps | Low | Created UX1.6 "Competitive analysis of TODO scanner tools" (stringer-4qs.6) under the unblocked UX1 epic. |
| `RawSignal.Confidence` semantics vary across collectors (keyword severity vs file size heuristic) — will confuse users filtering on `--min-confidence` | Low | Noted as architectural concern. UX1.5 (noise management docs) already covers user-facing aspect; implementation should normalize scoring or document per-collector semantics. No new issue needed. |
| Package layout ambiguity — AGENTS.md says `internal/collectors/` but task descriptions mention `internal/types/` or `internal/signal/` | Low | Covered by new F1.C decision records task — module structure will be documented before implementation. |

**Backlog impact:**

| Metric | Before Pass 3 | After Pass 3 | Delta |
|--------|---------------|--------------|-------|
| Open issues | 273 | 246 | -27 |
| Closed issues | 21 | 48 | +27 |
| Duplicate epics | 3 pairs (6 epics) | 0 | -6 epics |
| MVP tasks (actionable) | ~27 (across 9 epics) | ~28 (across 11 epics) | +1 task, +2 epics (S1, milestone) |
| Unblocked parallel tracks | 2 (F1, UX1) | 3 (F1, T1, UX1) | +1 track |
| Security tasks | 0 | 3 | +3 |
| Tasks per F1/F2/CI epics | 16 (5+6+5) | 5 (2+2+1) | -11 (consolidation) |

**Estimated quality increase:** Passes 1 and 2 fixed scope and product story. Pass 3 fixes structural integrity — the backlog is now internally consistent (no duplicate paths), security-aware, and right-sized at the task level. The most impactful change was eliminating the three duplicate epic pairs, which would have confused any agent or contributor trying to navigate the dependency graph. The second most impactful was unblocking T1, which means test infrastructure can be built in parallel with the scaffold rather than sequentially after it. The consolidated F1/F2/CI tasks reduce ceremony by ~70% for the foundation phase — instead of 16 PRs for boilerplate, it's 5 PRs that each deliver a meaningful, reviewable unit of work. The new security epic ensures that path traversal, token leakage, and output injection are addressed before v0.1.0, not discovered after. Estimated reduction in "first implementation session" friction: **30-40%**.

### Pass 4: CLI Agent (Claude Code Usability Assessment)

**Reviewer prompt:** "You are a Claude Code agent. Assess how easy to use you would find the tool being proposed in the beads backlog. We will update the backlog with changes you propose including decision records."

**What was caught:**

| Finding | Severity | Action Taken |
|---------|----------|--------------|
| No exit codes defined — agents and scripts have no way to distinguish partial failure from total failure from bad arguments | High | Created CLI1.5: Define exit codes (0=success, 1=bad args, 2=partial, 3=total failure) [stringer-dov.5] |
| No progress reporting on stderr — long scans on large repos look like hangs to both agents and users | High | Created CLI1.6: Implement stderr progress reporting [stringer-dov.6] |
| `--dry-run` output format undefined — agents need machine-readable scan metadata (signal counts per collector, duration) to make programmatic decisions | Medium | Created CLI1.7: Add `--json` mode to `--dry-run` output [stringer-dov.7] |
| No output volume safety cap — an agent piping stringer output directly to `bd import` on a large repo could flood the backlog with hundreds of beads | Medium | Created CLI1.8: Add `--max-issues` safety cap [stringer-dov.8] |
| Error messages not specified — vague errors like "collector failed" force agents to guess recovery actions instead of taking the suggested fix | High | Created CLI1.9: Define actionable error message format (what/why/fix) [stringer-dov.9] |
| MVP lifecycle story missing — no documentation of what happens when TODOs are fixed and stringer is re-run. Agents may re-scan and assume old beads auto-resolve (they don't). | Medium | Created UX1.7: Document MVP lifecycle (no delta scanning) [stringer-4qs.7] |
| `--min-confidence` semantics undefined — agents asked to "show important stuff" have no way to map intent to a threshold value | Medium | Drafted DR-004: Confidence scoring semantics. Recommends named presets (`--min-confidence=high`) alongside float values. Accepted. [stringer-vkt.5] |
| No CI check for stale documentation — interface changes in Go code could silently drift from AGENTS.md | Low | Created F1-CI.2: Add CI check for stale documentation [stringer-7kb.21] |

**Overall assessment: B+.** Strongest areas: one-liner UX (`stringer scan . | bd import -i -`), read-only safety guarantee, idempotent output, composable collector selection, and `--dry-run` for safe preview. Weakest areas: undefined error contract, no exit codes, opaque confidence scoring, and missing lifecycle documentation. All gaps addressed with 8 new backlog items.

**Backlog impact:**

| Metric | Before Pass 4 | After Pass 4 | Delta |
|--------|---------------|--------------|-------|
| Open issues | 246 | 254 | +8 |
| CLI1 children | 4 | 9 | +5 |
| UX1 children | 6 | 7 | +1 |
| F1-CI children | 16 | 17 | +1 |
| C1 children | 4 | 5 | +1 (DR-004) |
| Decision records | 0 drafted | 1 (004-confidence-scoring, accepted) | +1 |

**Estimated quality increase:** Passes 1-3 fixed what to build, why to build it, and structural integrity. Pass 4 fixes the agent-facing contract — the interface between stringer and the tools that will invoke it most often. Exit codes, structured errors, and progress reporting are table-stakes for any CLI tool used in automation, but are frequently omitted from initial plans focused on human users. The `--max-issues` cap and lifecycle documentation address the most likely agent-driven failure mode: flooding a beads backlog on first scan and leaving orphaned beads on re-scan. DR-004 (confidence scoring presets) bridges the gap between human intent ("show me the important stuff") and machine precision (`--min-confidence=0.7`). Estimated reduction in agent-integration friction: **40-50%**.
