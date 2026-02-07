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
