# 011: Native Git for Blame and Ownership

**Status:** Accepted
**Date:** 2026-02-08
**Context:** stringer-dv3 (P0 blame performance). Supersedes the blame portion of DR-002. The eval harness run against kubernetes/kubectl (768 files, 1000-commit depth) revealed that go-git's in-memory blame hangs for 60+ minutes and consumes 2.8 GB RAM. httpie/cli (321 files) takes 45 seconds. This makes stringer unusable on any non-trivial repository.

## Problem

go-git's `Blame()` implementation walks the full commit graph in memory — O(commits × lines) per file. For the `todos` collector (blame per TODO line) and `lotteryrisk` collector (blame per source file for ownership), this is catastrophically slow on repos with history deeper than a few hundred commits.

DR-002 chose go-git for self-contained binary UX and noted: *"If go-git's blame performance proves insufficient on large repos, we can revisit."* That time has arrived.

## Options

### Option A: Shell out to native git for blame/ownership only

Replace go-git blame with `git blame --porcelain` (todos) and `git log --numstat` (lotteryrisk). Keep go-git for everything else (commit iteration in gitlog, branch listing, repo detection).

**Pros:**
- Native `git blame` uses packfile indexes — runs in milliseconds where go-git takes minutes
- Targeted change — only two collectors affected, rest of codebase unchanged
- `git` is guaranteed to be on PATH (stringer scans git repos)
- Porcelain output format is stable and locale-independent
- go-git remains available as fallback when git is not on PATH

**Cons:**
- Adds runtime dependency on git CLI for blame operations
- Two git interaction patterns in the codebase (go-git for some, CLI for others)
- Need to handle path escaping, subprocess errors, and timeout

### Option B: Replace go-git entirely with native git

Move all git operations to CLI: `git log`, `git diff`, `git blame`, `git branch`.

**Pros:**
- Single interaction pattern — all git ops use the same exec layer
- Removes go-git dependency (~5 MB binary size reduction)
- Full git feature parity for any future collectors

**Cons:**
- Much larger change surface — every collector and many tests affected
- Lose type-safe commit/tree/diff APIs that go-git provides
- go-git's commit iteration and diff work fine — no performance issue there
- Would need to build a git output parsing layer for structured data (commits, diffs, trees)

### Option C: Add blame caching or limit blame depth

Keep go-git but cache blame results and/or limit blame to the most recent N commits.

**Pros:**
- No new dependency or interaction pattern
- Minimal code change

**Cons:**
- Caching doesn't help first-run performance (the common case)
- Limiting depth produces incorrect author attribution
- Doesn't fix the fundamental O(commits × lines) complexity
- lotteryrisk's commit-walk diffing has the same scaling problem, unrelated to blame

## Recommendation

**Option A: Shell out to native git for blame/ownership only.**

This is the minimal change that fixes the P0 performance issue. The hybrid approach (go-git for structured data, native git for heavy operations) is a pragmatic split based on where each tool excels. go-git is good at walking commit objects and building diffs in-process; native git is good at blame and log aggregation because it uses on-disk indexes.

### Implementation plan

**todos collector (`enrichWithBlame`):**
- Replace `blameFile()` with `git blame --porcelain -L <line>,<line> <relPath>`
- Parse porcelain output for `author` and `author-time` fields
- Keep mtime fallback (from PR #68) when blame fails
- Add 5-second per-file timeout via `context.WithTimeout`

**lotteryrisk collector (ownership analysis):**
- Replace commit-walk diffing with `git log --numstat --format='%H|%aN|%aI'`
- Parse output to build the same `fileChanges` and `dirOwnership` maps
- Preserve exponential decay weighting and lottery risk scoring (math stays the same)
- Replace per-file blame ownership with `git log --follow --format='%aN' -- <file>` for author attribution

**Shared concerns:**
- Add a `gitExec(ctx, repoDir, args...)` helper in a new `internal/gitcli` package
- Validate git is on PATH at scan start; skip blame-dependent features with a warning if not
- All exec calls use `exec.CommandContext` with configurable timeout

**Files affected:** ~4 source files, ~4 test files, ~200-250 lines changed.

## Decision

Option A accepted. Shell out to native git for blame (todos) and ownership analysis (lotteryrisk). Keep go-git for commit iteration, branch listing, and repo detection.
