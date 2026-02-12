# Contributing to Stringer

Thank you for your interest in contributing to Stringer! This guide covers development setup, workflow, and conventions.

## Development Setup

### Prerequisites

- Go 1.24+
- Git
- [golangci-lint](https://golangci-lint.run/usage/install/) v2
- [bd](https://github.com/steveyegge/beads) CLI (optional, for task tracking)

### Getting Started

```bash
git clone https://github.com/davetashner/stringer.git
cd stringer

# Set up pre-commit hooks (gitleaks secret scanning)
git config core.hooksPath .githooks

# Build
go build -o stringer ./cmd/stringer

# Run tests
go test -race ./...

# Run linter
golangci-lint run ./...
```

## Workflow

1. **Find or create an issue.** Check `bd ready` for open work, or create a new issue.
2. **Create a branch.** Use `<type>/<short-description>` naming (e.g., `feat/new-collector`, `fix/config-loading`).
3. **Write code and tests.** Maintain 90%+ test coverage. Run `go test -race ./...` and `golangci-lint run ./...` before pushing.
4. **Open a pull request.** PRs require all CI checks to pass. Keep PRs under 500 non-test lines when possible.
5. **Address review feedback.** Push additional commits to the PR branch.

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add new collector for X
fix: handle nil config gracefully
test: add integration test for bd import
docs: update README with new commands
chore: update dependencies
```

### Branch Naming

```
feat/todo-collector
fix/config-loading
test/integration-tests
docs/update-readme
chore/update-deps
```

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Use `golangci-lint` — the CI enforces this
- Keep functions focused and short
- Prefer table-driven tests
- Use `testify/assert` and `testify/require` for assertions
- Add doc comments to all exported types and functions

## Architecture

See [AGENTS.md](./AGENTS.md) for detailed architecture documentation, including:

- Directory structure and package responsibilities
- How to add new collectors, formatters, and report sections
- Signal schema and Beads JSONL output contract
- CI checks and quality gates

## Testing

```bash
# Run all tests with race detector
go test -race ./...

# Run tests for a specific package
go test -race ./internal/config/...

# Run integration tests
go test -race ./test/integration/...

# Run a specific test
go test -race -run TestScan_BdImportRoundTrip ./test/integration/...
```

### Test Coverage

The CI enforces a 90% coverage threshold. Check coverage locally:

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tail -1
```

## PR Size Guidelines

- **Under 500 non-test lines:** Normal PR, no special attention needed
- **500-1000 lines:** CI warns; consider splitting if the changes are separable
- **Over 1000 lines:** CI fails; split into smaller PRs

Test files (`_test.go`, `.test.*`, `.spec.*`) are excluded from the line count.

## Release Process

Stringer uses [GoReleaser](https://goreleaser.com/) with SLSA L2 provenance. See [docs/release-strategy.md](docs/release-strategy.md) for full details.

Releases are automated:

```bash
git tag v0.x.0
git push origin v0.x.0
# GoReleaser builds binaries, publishes GitHub Release, updates Homebrew tap
```

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](./LICENSE).
