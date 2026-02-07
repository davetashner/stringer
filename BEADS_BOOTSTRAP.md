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
