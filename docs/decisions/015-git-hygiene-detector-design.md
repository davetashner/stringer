# 015: Git Hygiene Detector Design

**Status:** Accepted
**Date:** 2026-02-21
**Context:** stringer-38h (C13: Git Hygiene Detector). Adding repository-level hygiene checks as a new signal source.

## Problem

Stringer detects code-level issues (TODOs, dead code, complexity) but misses repository-level hygiene problems that are trivially detectable and always actionable. Common git mistakes — large committed binaries, forgotten merge conflict markers, accidentally committed secrets, and mixed line endings — are high-signal, low-false-positive findings.

Key questions:
1. Which hygiene checks have the best signal-to-noise ratio?
2. How do we avoid flagging LFS-tracked binaries?
3. What confidence levels are appropriate for each check type?

## Options

### Option A: Shell out to external tools

Use `gitleaks` for secrets, custom scripts for binaries and conflicts.

**Pros:**
- Battle-tested secret detection

**Cons:**
- Requires external tool installation
- Different output formats to normalize
- Doesn't match stringer's self-contained approach

### Option B: Regex-based single-pass file scanner

Walk all files once, applying four checks per file: binary size, conflict markers, secret patterns, and line ending consistency.

**Pros:**
- Zero external dependencies
- Single walk pass for efficiency
- Reuses existing helpers (`isBinaryFile`, `shouldExclude`, `mergeExcludes`)
- Conservative secret patterns minimize false positives

**Cons:**
- Secret detection is not as comprehensive as gitleaks
- Line ending detection requires reading entire files

## Recommendation

**Option B: Regex-based single-pass file scanner.**

This is not a replacement for gitleaks — it's a lightweight early warning system that catches the most common patterns. Stringer already runs gitleaks as a pre-commit hook, so this collector catches secrets that slipped through or were committed before the hook was set up.

### Signal types

| Kind | Detection | Confidence |
|------|-----------|------------|
| `large-binary` | Binary file > 1 MB not tracked by LFS | 0.8 |
| `merge-conflict-marker` | `<<<<<<<`, `=======`, `>>>>>>>` in text files | 0.9 |
| `committed-secret` | AWS keys, GitHub tokens, generic key=value patterns | 0.6–0.7 |
| `mixed-line-endings` | File has both CRLF and LF (≥2 of each) | 0.7 |

### LFS handling

Parse `.gitattributes` for `filter=lfs` entries and skip matching files in the large-binary check. This avoids false positives for intentionally tracked large files.

### Secret patterns

Conservative set — not a full gitleaks replacement:
- AWS access key: `AKIA[0-9A-Z]{16}` (confidence 0.7)
- GitHub token: `gh[ps]_[A-Za-z0-9_]{36,}` (confidence 0.7)
- Generic: `(?i)(api[_-]?key|secret[_-]?key|password)\s*[:=]\s*["'][^"']{8,}` (confidence 0.6)

### Efficiency

Single `WalkDir` pass handles all four checks. Binary files are checked for size only (text checks skipped). Text files are read once with all pattern checks applied during the same scan.

## Decision

Option B accepted. Single-pass regex scanner with four high-confidence signal types.
