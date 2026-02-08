# 010: Shared Signal ID Generation

**Status:** Accepted
**Date:** 2026-02-08
**Context:** O4: Claude Code Tasks Formatter (stringer-cqo) — tasks formatter needs deterministic IDs, same algorithm as beads formatter

## Problem

The beads formatter generates deterministic content-based IDs via `BeadsFormatter.generateID()`. The tasks formatter now also needs deterministic IDs for Claude Code TaskCreate compatibility. Duplicating the hashing logic would violate DRY and risk the two formatters diverging.

## Decision

Extract the ID generation logic into a package-level `signalID(sig, prefix)` function in `internal/output/signalid.go`. Both formatters delegate to this shared function:

- **BeadsFormatter.generateID** applies convention prefix overrides, then calls `signalID`
- **TasksFormatter.signalToTask** calls `signalID` directly with the `"str-"` prefix

## Consequences

- **Positive:** Single source of truth for ID generation; adding new formatters that need IDs is trivial
- **Positive:** All existing beads tests pass unchanged — `generateID` behavior is identical
- **Negative:** None significant — this is a pure DRY extraction with no behavioral change
