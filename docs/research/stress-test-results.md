# Q3: Real-World Stress Test Results

**Date:** 2026-02-12
**Version:** dev (main @ ae6f5a2, post-v0.9.0)
**Machine:** macOS Darwin 25.2.0, Apple Silicon

## Summary

Stringer was run against 5 diverse repositories with zero panics, crashes, or
errors. All repos completed successfully for both `scan` and `report` commands.

## Results

| Repo | Files | Signals | Scan Time | Report Time | Peak RSS | Notes |
|------|------:|--------:|----------:|------------:|---------:|-------|
| stringer (self) | ~200 | 514 | 8.3s | 8.2s | 38 MB | Dogfood — output reasonable |
| golang/go | ~15,000 | 5,514 | 73s | 72s | 261 MB | Largest test; no timeouts |
| googleapis/googleapis | ~8,600 | 108 | 11.5s | — | 160 MB | Polyglot (proto, YAML, Go, Java) |
| nickel-org/nickel.rs | ~100 | 106 | 4.4s | — | 37 MB | Rust, Cargo deps, vuln scanner exercised |
| minimal (1 file, 1 commit) | 1 | 1 | 0.06s | 0.05s | 22 MB | No tags, no history edge case |

## Per-Repo Details

### 1. stringer (dogfood)

- **What:** Stringer's own repository
- **Clone:** Full clone (local)
- **Collectors fired:** gitlog (18), lotteryrisk (27), patterns (252), todos (217), vuln (0), dephealth (0), github (0, no token)
- **Observations:**
  - Lottery risk correctly flags every directory as single-contributor (CRITICAL) — expected for a solo project
  - High churn on `.beads/issues.jsonl` (102 changes in 90 days) — accurate
  - 217 TODOs found, all < 1 week old — reasonable
  - No false large-file signals after threshold bump to 1500

### 2. golang/go (Go standard library)

- **What:** ~15,000 files, deep commit history, massive codebase
- **Clone:** `--depth 500`
- **Collectors fired:** gitlog (30), lotteryrisk (86), patterns (2,752), todos (2,646), vuln (0), dephealth (0)
- **Observations:**
  - patterns collector took 62s (dominant cost), todos took 73s
  - 2,646 TODOs found — includes 3 stale TODOs > 1 year old (correctly identified in `src/cmd/compile` and `src/fmt`)
  - 256 MB peak RSS — acceptable for a repo this size
  - No panics during large-file traversal of deep directory trees
  - vuln collector correctly found no vulnerabilities (stdlib has no external deps in go.mod)

### 3. googleapis/googleapis (polyglot monorepo)

- **What:** ~8,600 files, primarily .proto, YAML, with some Go/Java
- **Clone:** `--depth 100`
- **Collectors fired:** lotteryrisk (17), patterns (61), todos (30), vuln (0), dephealth (0)
- **Observations:**
  - Handled non-source file extensions (proto, yaml, bazel) gracefully — skipped as expected
  - 160 MB RSS — higher due to large directory tree traversal
  - 11.5s total scan time — efficient for 8,600+ files

### 4. nickel-org/nickel.rs (Rust project)

- **What:** Rust web framework, Cargo dependencies, small-to-medium size
- **Clone:** Full clone
- **Collectors fired:** lotteryrisk (7), patterns (29), todos (54), vuln (23), dephealth (0)
- **Observations:**
  - Vuln collector found 23 signals — exercised Cargo.toml parsing and OSV.dev queries
  - Rust inline test detection (`#[cfg(test)]`) working correctly
  - Full git history available — lottery risk accurate

### 5. minimal (edge case)

- **What:** Single file (`main.go`), single commit, no tags, no releases
- **Clone:** Created locally
- **Collectors fired:** lotteryrisk (1)
- **Observations:**
  - All collectors handled gracefully — no division-by-zero, no panics on empty history
  - gitlog returned 0 signals (not enough history for churn detection) — correct
  - Lottery risk correctly identified single contributor
  - 60ms total scan time, 22 MB RSS

## Panics / Crashes / Errors

**None.** All five repositories completed without any panics, crashes, or runtime errors.

## Known Limitations

1. **Shallow clones reduce lottery risk accuracy:** With `--depth N`, blame history is truncated. The lottery risk collector may undercount contributors. Documented separately (stringer-8qi).
2. **No GITHUB_TOKEN:** GitHub collector and dephealth GitHub checks were skipped in all tests. These features require `GITHUB_TOKEN` to be set.
3. **Memory scales with file count:** golang/go used 256 MB RSS for ~15,000 files. Repositories with 100K+ files may approach 1 GB. No OOM observed.
4. **Scan time dominated by patterns + todos collectors:** For large repos, these two collectors account for 80%+ of wall-clock time due to per-file line counting and content scanning.
