# CLAUDE.md — Stringer

## Git Workflow

- **Always use PRs.** Never push directly to `main`. Create a feature branch, commit, push, and open a PR with `gh pr create`.
- Branch naming: `<type>/<short-description>` (e.g., `feat/todo-collector`, `fix/config-loading`)
- Commit messages: conventional commits format (`feat:`, `fix:`, `chore:`, `docs:`, `test:`)
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

## Build & Test

```bash
go build -o stringer ./cmd/stringer
go test -race ./...
golangci-lint run ./...
```

## Project Structure

See `AGENTS.md` for full architecture documentation.
