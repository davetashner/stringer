# 005: GitHub API Integration

**Status:** Accepted
**Date:** 2026-02-07
**Context:** C4: GitHub Collector (stringer-20r) — import issues, PRs, and review comments from GitHub

## Problem

Stringer needs to collect signals from GitHub (open issues, PRs, review comments). How should we authenticate, detect the repo, and interact with the GitHub API?

## Options

### Option A: `google/go-github` + `GITHUB_TOKEN` env var

**Pros:**
- Most popular Go GitHub client (16k+ stars), well-maintained
- Handles pagination, rate limit headers, and type safety out of the box
- `GITHUB_TOKEN` is the standard env var used by `gh` CLI and GitHub Actions
- Supports all needed endpoints (issues, PRs, reviews, review comments)

**Cons:**
- Adds a new dependency (~moderate transitive deps)
- Only supports token auth (no GitHub App support)

### Option B: Raw `net/http` + GitHub REST API

**Pros:**
- Zero new dependencies
- Full control over requests

**Cons:**
- Must implement pagination, rate limiting, response parsing manually
- Much more code to write and maintain
- Easy to introduce bugs in edge cases (Link header parsing, etc.)

### Option C: `shurcooL/githubv4` (GraphQL)

**Pros:**
- Can fetch issues + PRs + review comments in fewer requests
- More efficient for nested data

**Cons:**
- GraphQL adds complexity
- Less familiar to most Go developers
- Rate limiting works differently (point-based)

## Recommendation

**Option A: `google/go-github/v68`**. The library handles pagination and rate limits correctly, which are the two hardest parts of GitHub API integration. The `GITHUB_TOKEN` env var is universally supported. For remote detection, parse the origin URL from go-git (supports both HTTPS and SSH formats).

## Decision

Accepted. Use `google/go-github/v68` with `GITHUB_TOKEN` env var authentication. Auto-detect owner/repo from git remote origin URL. Missing token gracefully returns empty signals with a log warning. Non-GitHub remotes skip gracefully.
