# Branch Protection Rules

This document describes the required GitHub branch protection settings for the `main` branch of the Stringer repository.

## Required Settings

### Branch: `main`

**General:**
- Require a pull request before merging: **Enabled**
  - Required number of approvals: **1**
  - Dismiss stale pull request approvals when new commits are pushed: **Enabled**
  - Require review from Code Owners: **Disabled** (single-maintainer project)
- Require conversation resolution before merging: **Enabled**

**Status Checks:**
- Require status checks to pass before merging: **Enabled**
- Require branches to be up to date before merging: **Enabled**
- Required status checks:
  - `Test (Go 1.24)`
  - `Test (Go 1.25)`
  - `Vet`
  - `Format`
  - `Lint`
  - `Tidy`
  - `Coverage`
  - `Vulncheck`
  - `Binary Size`
  - `Go Generate`
  - `License Check`

**Restrictions:**
- Do not allow bypassing the above settings: **Enabled** (applies to admins too)
- Restrict who can push to matching branches: **Disabled** (PRs enforce the workflow)
- Allow force pushes: **Disabled**
- Allow deletions: **Disabled**

**Note:** The `Commit Lint` job runs only on pull requests. It is not listed as a required check because it does not run on direct pushes (which are blocked anyway by the PR requirement). It will still run and report on every PR.

## How to Apply

These settings are configured in the GitHub UI under **Settings > Branches > Branch protection rules**.

1. Navigate to **Settings > Branches** in the repository.
2. Click **Add rule** (or edit the existing rule for `main`).
3. Set "Branch name pattern" to `main`.
4. Enable each setting listed above.
5. Click **Save changes**.

Alternatively, use the GitHub CLI:

```bash
gh api repos/davetashner/stringer/branches/main/protection \
  --method PUT \
  --field required_status_checks='{"strict":true,"contexts":["Test (Go 1.24)","Test (Go 1.25)","Vet","Format","Lint","Tidy","Coverage","Vulncheck","Binary Size","Go Generate","License Check"]}' \
  --field enforce_admins=true \
  --field required_pull_request_reviews='{"required_approving_review_count":1,"dismiss_stale_reviews":true}' \
  --field restrictions=null \
  --field allow_force_pushes=false \
  --field allow_deletions=false
```

## Rationale

All CI checks must pass before any code reaches `main`. This prevents:
- Broken builds from reaching the default branch
- Regressions in test coverage
- Known vulnerabilities from being shipped
- Binary size bloat from going unnoticed
- License-incompatible dependencies from being introduced
- Non-conventional commit messages from cluttering the history

See [AGENTS.md](../AGENTS.md) for the full list of CI checks and what each one verifies.
