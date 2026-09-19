# 027: Self-Scan Gate on New High-Confidence Findings

**Status:** Accepted
Accepted 2026-09-19 with implementation in the same PR (owner authorisation)
**Date:** 2026-09-19
**Context:** stringer-xb2 — the bead reports stringer finding ~746 signals in its own repository, 108 at confidence >= 0.8 (`config.Merge` cyclomatic 88, several `Collect` methods at cyclomatic 37-52). Nothing stops that number from growing. The bead asks for a committed baseline and a CI job that fails when a PR adds a finding at >= 0.8 the baseline does not cover.

## Problem

Five questions decide whether such a gate is trustworthy or just noisy:

1. **Identity.** Which key links a finding to its baseline entry across edits?
2. **Determinism.** Which collectors give the same answer for the same tree on any day and any clone depth?
3. **Test-fixture secrets.** The secret detectors fire on fixtures in `internal/collectors/*_test.go`.
4. **Behaviour.** What the job prints, what fails it, and how a contributor accepts a finding.
5. **Baseline contents.** Which findings go in the baseline.

## Options

### Identity

**Option A: exact signal ID (`str-`, today's baseline key).** `SignalID` hashes source, kind, file, **line** and **title**, and complexity titles embed metrics. Measured on `internal/config/merge.go` with the v1.11.0 binary:

| Edit | ID |
|------|----|
| none (line 14, cyclomatic 88) | `92a72b95` |
| one doc line added above `Merge` (line 15) | `cca8f59b` |
| one `if` removed from `Merge` (cyclomatic 86) | `ac6847ce` |

Both a line shift and an improvement would read as a new finding and fail the gate. Rejected.

**Option B: key computed in the gate script (jq/shell).** Keeps Go unchanged, but duplicates the hash and normalisation in shell, and the contributor needs a separate tool to compute the key they must suppress.

**Option C: stable key in the baseline machinery (chosen).** `output.StableSignalID` hashes `Source | Kind | FilePath | Title` with every standalone number (`\b\d+(\.\d+)?\b`) replaced by `#`, prefixed `sts-`. Identifier digits (`parseV2`, `sha256`) stay. `output.LookupSuppression` matches the exact ID first, then the stable key, and `scan`, SARIF suppressions and the new `baseline check` all use it, so `str-` and `sts-` entries can sit in one baseline. On the edits above the key stays `sts-301a576c`.

Costs:
- The gate does not ratchet: a baselined function that gets *worse* keeps its key and passes. The gate catches new findings and findings crossing into >= 0.8, not regressions inside ones already accepted.
- Findings of one kind in one file whose titles differ only in numbers share a key (duplicated blocks, secrets whose title embeds a line). The five private-key fixtures in `secrets_test.go` are one entry.
- Renaming a file or function produces a new key; the contributor re-accepts it.

### Determinism

Colocation boosts (`pipeline.BoostColocatedSignals`) come only from churn, vulnerable-dependency and low-lottery-risk signals, so dropping those collectors also removes the only cross-collector confidence changes.

| Collector | In the gate | Reason |
|-----------|-------------|--------|
| github, vuln, dephealth | no | Network. Go vulnerabilities are already covered by the `Vulncheck` job. |
| gitlog | no | Churn window is relative to today. |
| lotteryrisk | no | Depends on history depth and recent authors. |
| docstale | no | Drift window (`1y`) is relative to today and stale-doc dates depend on clone depth. Its maximum confidence is 0.7, so it contributes nothing at 0.8 today. |
| apidrift, complexity, configdrift, coupling, deadcode, duplication, githygiene, patterns, todos | yes | Read the working tree and `git ls-files`. |

`todos` reads blame dates for a +0.1 recency boost. In a depth-1 clone every line looks new, so FIXME comments move from 0.65 to 0.75. Only `BUG` (base 0.8) can reach 0.8, at 0.8 or 0.9 whatever its age, so the >= 0.8 set does not depend on age or depth. Checked on main `b6d2862` with the gate collectors: a `--depth 1` clone and a full clone (426 commits) gave 715 signals each, identical apart from todo confidences, and the same 86 signals at >= 0.8. The workflow keeps the default depth-1 checkout.

The collectors are listed explicitly in `scripts/self-scan.sh`, so a new collector joins the gate only by editing that line.

Known gap: `scan` drops signals whose title matches an existing bead in `.beads/` (beads-aware dedup, configurable only in `.stringer.yaml`). That can hide a finding from the gate but can never fail it; nothing is filtered today.

### Test-fixture secrets

At >= 0.8 there are six hits, all `Possible private key file` in `githygiene_test.go` (1) and `secrets_test.go` (5). The AWS, Slack and other fixture hits are 0.7 and do not reach the gate.

**Option A: repo-level `.stringer.yaml` excluding the fixture files from githygiene.** Hides every githygiene check (secrets, conflict markers, line endings) in those files for every scan of this repo, and adds a config file that changes everyone's local output.

**Option B: baseline them (chosen).** Two stable-key entries, one per file. What stays hidden is only a *private-key* finding in those two fixture files. A private key or any other >= 0.8 secret added to non-test code, or to any other test file, has a new key and fails the gate.

### Behaviour

`stringer baseline check <scan.json>` (new) reads a `--format json --no-baseline` scan, reports each signal without an unexpired entry as `NEW file:line title [sts-…]` plus the exact `stringer baseline suppress sts-… --reason acknowledged --comment '…'` command, and exits 4. Entries matching no signal print as `RESOLVED` (a `::notice` in Actions) and never fail. `--prune` drops them and `--accept` adds every new finding. Under `GITHUB_ACTIONS` new findings are also `::error` annotations on the PR diff.

`scripts/self-scan.sh` is the single source of the gate settings: build from the checkout, then `scan . --collectors <list> --min-confidence 0.8 --no-baseline --strict --format json`, then `baseline check`. `--strict` makes a failed collector fail the gate instead of silently dropping its findings. `.github/workflows/self-scan.yml` runs the script on PRs and pushes to main (read-only token, SHA-pinned actions, no expression interpolation in `run:`).

### Baseline contents

Only findings at >= 0.8 are baselined, so a finding that grows from 0.7 into the gated band fails the gate. `.stringer/baseline.json` is written one suppression per line, so accepting or dropping a finding is a one-line diff. `baseline.Save` uses this format for every baseline; it is still ordinary JSON. The baseline is also applied by a plain `stringer scan .` of this repo, which now hides the 82 acknowledged entries. That is expected for acknowledged findings.

## Recommendation

Option C for identity, the nine-collector deterministic set at 0.8, and baselining the two fixture files. Together they make the gate fail only on a new finding, never on a date or a line move. The main cost is that the gate does not ratchet on findings already accepted, which the bead does not ask for.

## Decision

Accepted as recommended. The initial baseline has 82 entries covering the 86 findings at >= 0.8 on main `b6d2862` (76 complex functions, 3 `BUG:` test comments, 1 large binary `assets/logo-whole.png`, 6 private-key fixtures). It was generated by `./scripts/self-scan.sh --accept`.
