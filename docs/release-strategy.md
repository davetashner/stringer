# Release Strategy

## Versioning

Stringer follows [Semantic Versioning](https://semver.org/):

- **MAJOR** (1.0.0) — Breaking changes to CLI flags, output format, or collector interface
- **MINOR** (0.x.0) — New collectors, output formats, or features
- **PATCH** (0.0.x) — Bug fixes, test improvements, documentation, CI changes

Stringer follows strict semver at all versions — breaking changes always require a major version bump, even pre-1.0.

## Release Process

### 1. Prepare

- Ensure `main` is clean and CI is green
- Review merged PRs since the last release for changelog accuracy
- Update README.md:
  - Status line version and feature summary
  - Usage reference table (new flags)
  - Other Commands section (new commands)
  - Current Limitations (remove shipped items, add new ones)
  - Roadmap (update planned features)
- Verify `go build ./cmd/stringer && go test -race ./... && golangci-lint run ./...` passes

### 2. Tag

```bash
git tag v0.x.0
git push origin v0.x.0
```

### 3. Automated Release

Pushing a `v*` tag triggers `.github/workflows/release.yml`, which:

1. Runs GoReleaser (`.goreleaser.yml`)
2. Builds cross-platform binaries (linux/darwin/windows, amd64/arm64)
3. Generates SHA-256 checksums
4. Creates a GitHub Release with a filtered changelog (excludes docs/test/chore/ci commits)
5. Publishes a Homebrew formula to `davetashner/homebrew-tap` (requires `HOMEBREW_TAP_TOKEN` secret)

### 4. Verify

- Check the [Releases page](https://github.com/davetashner/stringer/releases) for the new release
- Verify binaries are attached and checksums are correct
- Test installation: `brew install davetashner/tap/stringer` (if Homebrew tap is set up)
- Test binary: `stringer version` should show the new version

## Version Injection

The version string is injected at build time via `-ldflags`:

```
-X main.Version={{.Version}}
```

- During development: `Version = "dev"` (default in `cmd/stringer/main.go`)
- In releases: GoReleaser sets it to the tag version (e.g., `0.2.0`)

## What Triggers a Release

| Change Type | Version Bump | Examples |
|-------------|-------------|---------|
| New collector | Minor | Adding GitHub collector, lottery risk analyzer |
| New output format | Minor | Adding JSON, markdown formatters |
| New CLI command or flag | Minor | Adding `stringer docs`, `--min-confidence` |
| Pipeline enhancement | Minor | Parallel execution, deduplication |
| Bug fix | Patch | Fixing incorrect confidence scoring |
| Test/CI/docs only | Patch (or skip) | Coverage improvements, CI hardening |
| Breaking change | Major | Renaming flags, changing output schema, removing collectors, altering signal hash algorithm |

## Homebrew Tap Setup

The `HOMEBREW_TAP_TOKEN` repository secret is required for Homebrew publishing:

1. Create a GitHub PAT (classic) at https://github.com/settings/tokens/new
   - Note: `HOMEBREW_TAP_TOKEN`
   - Scope: `repo` (full control of private repos)
2. Create the `davetashner/homebrew-tap` repository (can be empty)
3. Add the PAT as a repository secret at https://github.com/davetashner/stringer/settings/secrets/actions
   - Name: `HOMEBREW_TAP_TOKEN`
   - Value: the PAT

After the first release, users can install via:

```bash
brew install davetashner/tap/stringer
```

## Release History

See [GitHub Releases](https://github.com/davetashner/stringer/releases) for the full changelog.
