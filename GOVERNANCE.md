# Governance

Stringer uses a **Benevolent Dictator For Life (BDFL)** governance model, which is common for single-maintainer open source projects.

## Roles

### Maintainer (BDFL)

**Dave Tashner** ([@davetashner](https://github.com/davetashner))

The maintainer has final authority on all project decisions, including:

- Feature direction and roadmap priorities
- Accepting or rejecting pull requests
- Release timing and versioning
- Changes to project governance

### Contributors

Anyone who submits a pull request, files an issue, or participates in discussions. Contributors are expected to follow the [Contributing Guide](./CONTRIBUTING.md) and the project's conventions.

## Decision Process

1. **Minor changes** (bug fixes, small improvements): Submit a PR. The maintainer reviews and merges if it meets project standards.
2. **Significant changes** (new features, architectural changes): Open an issue or discussion first to align on the approach before investing in implementation. Decision records in `docs/decisions/` document non-trivial technical choices.
3. **Breaking changes**: Require explicit maintainer approval and a major version bump per [Semantic Versioning](https://semver.org/). The CI includes a breaking change guard that flags these automatically.

## Code Review

All changes go through pull requests. Direct pushes to `main` are blocked by branch protection. The maintainer reviews all PRs before merge.

## Security

Vulnerability reports follow the process in [SECURITY.md](./SECURITY.md). The maintainer commits to a 48-hour acknowledgment SLA for security issues.

## Continuity Plan

Stringer is MIT-licensed and hosted on GitHub, so the source code is always publicly available and forkable. To ensure the project can continue with minimal interruption if the maintainer becomes unavailable:

- **Source code**: Publicly available on GitHub under MIT license. Anyone may fork and continue development.
- **Release infrastructure**: GitHub Actions handles automated builds, signing, and publishing. Workflows are committed to the repository and can be run by anyone with repository write access.
- **Homebrew tap**: Hosted at [davetashner/homebrew-tap](https://github.com/davetashner/homebrew-tap). A successor maintainer with repository access can continue publishing formula updates.
- **Package registry**: Published via `go install`, which requires no special credentials — any fork can be installed directly.
- **Domain/DNS**: No custom domain. All project infrastructure is on GitHub.

If you are interested in serving as a backup maintainer, please open an issue to discuss.

## Evolution

If the project grows to have regular contributors, this governance model may evolve to include additional maintainers or a contributor ladder. Any governance changes will be documented here.
