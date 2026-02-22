# CLAUDE.md — Stringer

## Git Workflow

- **Always use PRs.** Never push directly to `main`. Create a feature branch, commit, push, and open a PR with `gh pr create`.
- Branch naming: `<type>/<short-description>` (e.g., `feat/todo-collector`, `fix/config-loading`)
- Commit messages: conventional commits format (`feat:`, `fix:`, `chore:`, `docs:`, `test:`)
- **DCO sign-off required.** Always use `git commit -s` (or `--signoff`) so every commit includes a `Signed-off-by` line. CI will reject commits without it.
- Check for existing open PRs with `gh pr list` before creating new ones to avoid duplicates
- Prefer adding commits to an existing open PR for related work

## Secret Safety

- **Never commit secrets.** The pre-commit hook runs `gitleaks` automatically.
- If you get a gitleaks error, fix the issue — do not bypass with `--no-verify`.
- New clones must run: `git config core.hooksPath .githooks`

## Beads Backlog

- Find work: `bd ready --json`
- Before starting: claim an issue or create one with `bd create`
- Reference issue ID in commits: `feat: add scanner [stringer-bsf]`
- After PR merge: `bd close <id> --reason "Completed in PR #N"`

## Decision Records

- When a task involves choosing between approaches (libraries, patterns, formats, trade-offs), **write a decision record before implementing**.
- Create `docs/decisions/NNN-short-title.md` using the template in AGENTS.md.
- Set status to `Proposed` — do not implement until a developer accepts the decision.
- Reference the beads issue ID in the record's Context field.

## Build & Test

```bash
go build -o stringer ./cmd/stringer
go test -race ./...
golangci-lint run ./...
```

## Current Focus
1. **Active epic**: L1 Language Support Expansion (`stringer-043`) — PHP, Swift, Scala, Elixir
2. **Quick wins**: P3/P4 unblocked tasks — run `bd ready`

## Post-Release Checklist
After tagging a release:
1. Close completed beads: `bd close <id> --reason "Completed in PR #N"`
2. Close parent epics if all children done: check `bd children <epic-id>`
3. Catch missed closures: `bd orphans`
4. Fix stale blockers: `bd blocked` — if all blockers show ✓, remove deps
5. Update MEMORY.md "What's Next" section

## Task Scoping
When requesting work, include the full outcome:
- Release intent: "implement X, then cut vN.M.0"
- Parallel opportunities: "also fix Y while CI runs on X"
- Beads IDs for traceability

## Doc Staleness Guard

CI warns when internal Go files change without an `AGENTS.md` update. To avoid noisy warnings:
- **Always update `AGENTS.md`** when changing architecture, interfaces, public contracts, or adding new collectors/formatters/report sections
- For routine changes (adding language patterns, fixing bugs, refactoring internals), include `AGENTS.md` in the PR with a no-op touch or a genuine doc update to suppress the warning
- The guard **hard-fails** if `Collector`, `Formatter`, or `Section` interface signatures drift between source and `AGENTS.md`

## Terminology

- **Always use "lottery risk"**, never "bus factor". The collector is `lotteryrisk`, the signals are `low-lottery-risk`, and all docs/UI should say "lottery risk". This applies to README, AGENTS.md, decision records, commit messages, and PR descriptions.

## Project Structure

See `AGENTS.md` for full architecture documentation.
