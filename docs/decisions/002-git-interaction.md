# 002: Git Interaction Method

**Status:** Proposed
**Date:** 2026-02-07
**Context:** F2 core types (stringer-0lx). Stringer reads git history (blame, log, diff) from target repositories. This decision determines how stringer interacts with git.

## Problem

Stringer needs to read git data (blame output, commit logs, diffs) from target repositories. Should it use a pure-Go git library or shell out to the `git` CLI?

## Options

### Option A: go-git (pure Go library)

**Pros:**
- No external dependency — stringer binary is self-contained
- Cross-platform without requiring git installation
- Type-safe API, testable without real git repos
- In-process performance for read operations

**Cons:**
- Incomplete git feature parity (blame support is basic, some edge cases with packed refs)
- Larger binary size (~5MB overhead)
- go-git's blame implementation is slow on large repos compared to native git
- Maintenance risk — go-git has had periods of slow development

### Option B: Shell out to git CLI

**Pros:**
- Full feature parity — anything git can do, stringer can parse
- git blame is fast and battle-tested
- Smaller binary
- Users already have git installed (they're running stringer on git repos)

**Cons:**
- Requires git on PATH — adds runtime dependency
- Output parsing is fragile (locale-sensitive, format changes between versions)
- Harder to test (need real git repos or mock the exec layer)
- Security considerations for subprocess execution

### Option C: go-git with CLI fallback

**Pros:**
- Works without git installed (go-git handles common cases)
- Falls back to CLI for operations where go-git is weak (blame on large repos)
- Best of both worlds for correctness

**Cons:**
- Two code paths to maintain and test
- Complex error handling (which path failed? should we retry with the other?)
- Users get inconsistent behavior depending on whether git is installed

## Recommendation

**Option A: go-git.** Stringer is a read-only tool that needs blame, log, and diff — all supported by go-git. The self-contained binary is a strong UX win (no "please install git" errors). For MVP, the TODO collector only needs blame for author/date attribution, which go-git handles adequately. If go-git's blame performance proves insufficient on large repos, we can revisit with Option C post-MVP.

## Decision

[Pending acceptance]
