# AGENTS.md — Stringer

## What is Stringer?

Stringer is a codebase archaeology tool that mines existing repositories to produce [Beads](https://github.com/steveyegge/beads)-formatted issues. It solves the cold-start problem: when you adopt Beads on a mature codebase, agents wake up with zero context. Stringer gives them instant situational awareness by extracting actionable work items from signals already present in the repo.

## Architecture

```
stringer/
├── cmd/stringer/           # CLI entrypoint
│   ├── main.go                 # cobra root setup
│   ├── root.go                 # root command, global flags
│   ├── scan.go                 # scan subcommand and flags
│   ├── report.go               # report subcommand
│   ├── context.go              # context subcommand
│   ├── docs.go                 # docs subcommand
│   ├── init.go                 # init subcommand (bootstrap stringer in a repo)
│   ├── config.go               # config get/set/list subcommands
│   ├── collectors.go           # collectors list/info subcommands (info shows thresholds, supports --json)
│   ├── baseline.go             # baseline create/suppress/list/remove/status subcommands
│   ├── mcp.go                  # mcp serve subcommand (MCP server)
│   ├── validate.go             # validate subcommand (JSONL validation)
│   ├── version.go              # version subcommand
│   ├── configwiring.go         # shared flag-to-config wiring
│   ├── exitcodes.go            # exit code constants
│   └── fs.go                   # filesystem helpers
├── internal/
│   ├── beads/              # Beads integration
│   │   ├── conventions.go      # Beads naming and format conventions
│   │   ├── dedup.go            # Beads-aware signal deduplication
│   │   └── reader.go           # Read existing beads from .beads/ directory
│   ├── bootstrap/          # stringer init bootstrapping
│   │   ├── bootstrap.go        # Bootstrap orchestration
│   │   ├── detect.go           # Project detection (language, framework, CI)
│   │   ├── config.go           # Generate .stringer.yaml defaults
│   │   ├── agentsmd.go         # Append stringer section to AGENTS.md
│   │   └── mcpjson.go          # Generate .mcp.json for Claude Code
│   ├── collector/          # Collector registry and interface
│   │   └── collector.go        # Register(), List(), Get(), Collector interface
│   ├── collectors/         # Signal extraction modules (one file per collector)
│   │   ├── todos.go            # TODO/FIXME/HACK/XXX/BUG/OPTIMIZE scanner
│   │   ├── gitlog.go           # Reverts, high-churn files, stale branches
│   │   ├── patterns.go         # Large files, missing tests, low test coverage ratios (Go, JS/TS, Python, Ruby, Java, Kotlin, Rust, C#, PHP, Swift)
│   │   ├── lotteryrisk*.go     # Lottery risk: core, ownership math, review analysis
│   │   ├── github.go           # GitHub issues, PRs, and review comments
│   │   ├── dephealth*.go       # Dependency health: 10 ecosystems (Go, npm, Cargo, Maven, NuGet, PyPI, Packagist, SwiftPM, sbt, Hex)
│   │   ├── vuln*.go            # Vuln scanner: 11 ecosystems via OSV.dev (+ PHP, Swift, Scala, Elixir parsers)
│   │   ├── configdrift.go       # Config drift: env var drift, dead keys, inconsistent defaults
│   │   ├── apidrift.go         # API drift: undocumented routes, unimplemented spec paths, stale versions
│   │   ├── docstale.go         # Doc staleness: stale docs, co-change drift, broken links
│   │   ├── duplication*.go     # Code duplication: exact clones (Type 1) and near-clones (Type 2) via FNV-64a sliding window
│   │   ├── coupling*.go        # Coupling: circular dependencies (Tarjan's SCC) and high fan-out modules via import graph
│   │   ├── complexity.go       # Complexity: AST-based for Go (cyclomatic/cognitive/nesting), regex-based for other languages
│   │   ├── complexity_go.go    # Go AST analysis: cyclomatic, cognitive, nesting depth via go/parser
│   │   ├── githygiene.go       # Git hygiene: large binaries, merge conflicts, committed secrets, mixed line endings
│   │   ├── secrets.go          # Secret detection: 24+ built-in patterns, custom patterns, allowlist, entropy detection
│   │   └── duration.go         # Duration parsing helpers
│   ├── analysis/           # LLM-powered analysis
│   │   ├── cluster.go          # Signal clustering via LLM
│   │   ├── priority.go         # Priority inference via LLM
│   │   └── dependency.go       # Dependency detection via LLM
│   ├── config/             # .stringer.yaml config file support
│   │   ├── config.go           # Config and CollectorConfig structs
│   │   ├── yaml.go             # Load(), Write(), LoadRaw(), WriteFile()
│   │   ├── validate.go         # Validate() — multi-error validation
│   │   ├── merge.go            # Merge() — file config + CLI merge
│   │   ├── keypath.go          # Dot-notation key path navigation
│   │   └── global.go           # Global config (~/.config/stringer/)
│   ├── context/            # Context generation (stringer context)
│   │   ├── generator.go        # Context generation orchestration
│   │   ├── githistory.go       # Git history analysis for context
│   │   └── render_json.go      # JSON output for context
│   ├── docs/               # Docs generation (stringer docs)
│   │   ├── analyzer.go         # Repository analysis for docs
│   │   ├── detector.go         # Language/framework detection
│   │   ├── generator.go        # AGENTS.md generation
│   │   └── updater.go          # Update existing AGENTS.md preserving manual sections
│   ├── gitcli/             # Native git CLI wrapper (DR-011)
│   │   └── gitcli.go           # Shell out to git for blame and ownership
│   ├── llm/                # LLM provider abstraction
│   │   ├── provider.go         # Provider interface and registry
│   │   ├── anthropic.go        # Anthropic Claude provider
│   │   └── openai.go           # OpenAI-compatible provider
│   ├── log/                # Structured logging
│   │   └── log.go              # slog-based logging helpers
│   ├── mcpserver/          # MCP server for AI agent integration
│   │   ├── server.go           # Server creation and lifecycle
│   │   ├── tools.go            # Tool handlers: scan, report, context, docs
│   │   └── resolve.go          # Path resolution and input parsing
│   ├── output/             # Output formatters
│   │   ├── formatter.go        # Formatter interface and registry
│   │   ├── beads.go            # Beads JSONL writer (primary)
│   │   ├── json.go             # JSON with metadata envelope
│   │   ├── markdown.go         # Human-readable markdown summary
│   │   ├── sarif.go            # SARIF v2.1.0 output with suppressions + baseline comparison
│   │   ├── tasks.go            # Claude Code task format
│   │   └── signalid.go         # Shared deterministic signal ID generation
│   ├── pipeline/           # Scan orchestration
│   │   ├── pipeline.go         # New(), Run() — parallel execution via errgroup
│   │   ├── dedup.go            # Content-based signal deduplication
│   │   ├── enrich.go           # Cross-signal confidence boosting (co-location)
│   │   ├── baseline.go         # FilterSuppressed() — baseline suppression filtering
│   │   └── validate.go         # ScanConfig validation
│   ├── redact/             # Secret redaction
│   │   └── redact.go           # Scrub sensitive patterns from signal content
│   ├── report/             # Report generation (stringer report)
│   │   ├── section.go          # Section registry and interface
│   │   ├── render.go           # Report rendering orchestration
│   │   ├── color.go            # Color-coded terminal output
│   │   ├── table.go            # Table formatting helpers
│   │   ├── lotteryrisk.go      # Lottery risk analysis section
│   │   ├── churn.go            # Code churn hotspots section
│   │   ├── todoage.go          # TODO age distribution section
│   │   ├── coverage.go         # Test coverage gaps section
│   │   ├── recommendations.go  # Actionable recommendations section
│   │   └── modulesummary.go    # Module health summary section
│   ├── baseline/           # Signal suppression state (baseline.json)
│   │   ├── baseline.go         # Load/Save/Lookup/AddOrUpdate/Remove for .stringer/baseline.json
│   │   └── rename.go           # Atomic rename helper (overridable for tests)
│   ├── signal/             # Domain types
│   │   └── signal.go           # RawSignal, ScanConfig, ScanResult, CollectorOpts
│   ├── state/              # Delta scan state persistence
│   │   └── state.go            # Load/Save/FilterNew/Build for .stringer/last-scan.json
│   ├── validate/           # JSONL validation for beads compatibility
│   │   └── validate.go         # Validate() — field-level JSONL validation
│   └── testable/           # Interfaces for test mock injection
│       ├── exec.go             # CommandExecutor interface
│       ├── exec_mock.go        # Mock command executor
│       ├── fs.go               # FileSystem interface
│       ├── mock_fs.go          # Mock filesystem
│       ├── git.go              # GitOpener interface
│       └── git_mock.go         # Mock git opener
├── test/
│   └── integration/        # End-to-end integration tests
├── eval/                   # Evaluation harness for stress-testing
├── testdata/
│   ├── fixtures/           # Test fixture repos
│   └── golden/             # Golden file outputs
├── docs/
│   ├── decisions/          # Decision records (see docs/decisions/)
│   ├── agent-integration.md    # MCP setup and tool reference
│   ├── branch-protection.md    # Branch protection rules
│   ├── competitive-analysis.md # Competitive landscape
│   └── release-strategy.md     # Versioning and release process
├── go.mod
├── go.sum
├── AGENTS.md               # You are here
├── README.md
├── LICENSE
└── CLAUDE.md
```

**Note:** The collector architecture is extensible (see [Adding a new collector](#adding-a-new-collector)). Collectors self-register via `init()` — run `stringer scan --help` for the current list. Output formatters follow the same pattern — run `stringer scan --format=help` or see `internal/output/`. See [docs/release-strategy.md](docs/release-strategy.md) for versioning and release process.

## Tech Stack

- **Language:** Go 1.24+ (matches Beads ecosystem)
- **CLI framework:** `spf13/cobra` for command/flag parsing
- **Git interaction:** `go-git` for commit iteration and diffs; native `git` CLI for blame and ownership analysis ([DR-011](docs/decisions/011-native-git-blame.md))
- **Testing:** `stretchr/testify` for assertions
- **Linting:** `golangci-lint` v2 with gosec
- **Output:** Beads JSONL, JSON, Markdown, and Tasks formatters
- **MCP:** `modelcontextprotocol/go-sdk` for Model Context Protocol server
- **Release:** GoReleaser for cross-platform binaries and Homebrew tap

## MCP Server

Stringer exposes an MCP server for direct AI agent integration. The architecture:

```
cmd/stringer/mcp.go          # CLI wiring: "stringer mcp serve"
  └─ internal/mcpserver/
       ├── server.go          # Server creation and lifecycle
       └── tools.go           # Tool handlers: scan, report, context, docs
```

### Tools

| Tool | Handler | Description |
|------|---------|-------------|
| `scan` | `handleScan` | Run collectors and return structured signals |
| `report` | `handleReport` | Generate health report with metrics |
| `context` | `handleContext` | Generate CONTEXT.md for agent onboarding |
| `docs` | `handleDocs` | Generate or update AGENTS.md scaffold |

### Registration

```bash
claude mcp add stringer -- stringer mcp serve
```

Or use `stringer init .` which auto-generates `.mcp.json` when a `.claude/` directory is detected.

## Build & Test

```bash
# Build
go build -o stringer ./cmd/stringer

# Run tests
go test -race ./...

# Run linter
golangci-lint run ./...

# Run on a target repo
./stringer scan /path/to/repo

# Run specific collectors
./stringer scan /path/to/repo --collectors=todos,gitlog

# Dry run (preview signal count without writing JSONL)
./stringer scan /path/to/repo --dry-run

# Dry run with machine-readable JSON output
./stringer scan /path/to/repo --dry-run --json

# Skip baseline suppression filtering
./stringer scan /path/to/repo --no-baseline

# SARIF with baseline comparison (marks results as new/unchanged/absent)
./stringer scan /path/to/repo --format sarif --sarif-baseline previous.sarif -o current.sarif
```

## Key Design Decisions

1. **Collectors are independent and composable.** Each collector produces a stream of `RawSignal` structs. They can run in parallel. Adding a new collector means implementing one interface.

2. **The LLM pass is optional.** `--no-llm` mode skips clustering and produces one bead per signal. Useful for CI, air-gapped environments, or when you just want the raw TODO scan.

3. **Output is always valid beads JSONL.** The beads JSONL writer is the critical path. Every output must produce valid JSONL compatible with `bd create`. Test this in CI.

4. **Stringer never modifies the target repo.** It is read-only. It writes output to stdout or a specified file. The user decides when and how to import signals into their backlog.

5. **Idempotency matters.** Running stringer twice on the same repo should produce the same output (modulo LLM non-determinism in clustering mode). Use deterministic hashing for signal deduplication.

## Semver & Breaking Changes

Stringer follows **strict semver at all versions** — breaking changes always require a major version bump, even pre-1.0. The `Breaking Change Guard` CI job enforces this by failing PRs that contain conventional commit breaking markers (`feat!:` or `BREAKING CHANGE:` in commit body).

**Breaking change surfaces:**

- **CLI:** Flag names, flag defaults, exit codes, subcommand names
- **Output formats:** Beads JSONL schema, JSON envelope schema, markdown structure
- **Interfaces:** `collector.Collector`, `output.Formatter` — method signatures and behavior contracts
- **Domain types:** `signal.RawSignal` struct fields, `signal.ScanConfig` fields, `signal.CollectorOpts` fields
- **Algorithms:** Signal dedup hash (SHA-256 of source+kind+filepath+line+title), confidence scoring formula, priority mapping thresholds
- **Beads output:** ID format (`str-` prefix + 8 hex chars), field mapping, label conventions

If you need to make a breaking change, bump the major version. Use the `!` marker in commit messages (e.g., `feat!: rename --format to --output-format`) and document the migration path in the release notes.

## Decision Records

When you encounter a design decision with multiple valid approaches, **create a decision record before implementing**. Decision records ensure developers can review trade-offs and make informed choices rather than discovering baked-in assumptions after the fact.

### When to create a decision record

- Choosing between libraries or dependencies (e.g., `go-git` vs. shelling out to `git`)
- Architectural patterns (e.g., streaming vs. batch signal processing)
- API/CLI surface design (e.g., flag naming, output format defaults)
- Data format choices (e.g., how to hash signals for dedup)
- Trade-offs between simplicity and flexibility (e.g., hardcoded defaults vs. config)
- Anything where a reasonable person could argue for a different approach

### Decision record format

Create a markdown file in `docs/decisions/` named `NNN-short-title.md`:

```markdown
# NNN: Short Decision Title

**Status:** Proposed | Accepted | Superseded by NNN
**Date:** YYYY-MM-DD
**Context:** What beads issue or work prompted this decision?

## Problem

What question needs answering? What constraint or trade-off exists?

## Options

### Option A: [Name]
**Pros:**
- ...

**Cons:**
- ...

### Option B: [Name]
**Pros:**
- ...

**Cons:**
- ...

### Option C: [Name] (if applicable)
...

## Recommendation

Which option do you recommend and why? What's the key differentiator?

## Decision

[Filled in by developer after review. State the chosen option and any
conditions or caveats.]
```

### Rules

- **Do NOT implement a decision before it's recorded.** Write the record, set status to `Proposed`, and let a developer accept it.
- Number sequentially: `001`, `002`, etc.
- Reference the relevant beads issue ID in the Context field.
- Keep options concrete — include code snippets, interface sketches, or config examples where they clarify trade-offs.
- If a decision is later reversed, set status to `Superseded by NNN` and create a new record explaining why.

### Lifecycle

Every DR moves through these states:

- **Proposed** — authored but not reviewed. Do not implement.
- **Accepted** — reviewed and approved. Implementation may proceed (or has).
- **Superseded by NNN** — the decision has been reversed or replaced. Keep the original file; add a pointer line at the top to the replacement.
- **Archived** — the decision no longer applies (feature removed, approach abandoned without replacement). Leave the file in place; prefix the title with `[Archived]` and add a one-line note under Status.

When you open a PR that implements an Accepted DR, flip its status in the same PR. When you supersede a DR, do it in the PR that introduces the replacement. A DR should never linger in `Proposed` once the corresponding code ships — treat a mismatched status as a correctness bug on par with stale docs.

## Working on Stringer

### Adding a new collector

1. Create `internal/collectors/yourname.go`
2. Implement the `collector.Collector` interface:
   ```go
   type Collector interface {
       Name() string
       Collect(ctx context.Context, repoPath string, opts signal.CollectorOpts) ([]signal.RawSignal, error)
   }
   ```
3. Self-register in an `init()` function: `collector.Register(&YourCollector{})`
4. Add a blank import in `cmd/stringer/scan.go`: `_ "github.com/davetashner/stringer/internal/collectors"`
   (already present — this ensures all collector `init()` functions run)
5. Add tests in `internal/collectors/yourname_test.go`
6. Update `README.md` collector list

### Logging conventions

Stringer uses the stdlib `log/slog` everywhere. Follow these rules so agents and CI consumers can parse logs reliably:

- **Level:** `Info` for user-visible status (caps reached, collectors disabled). `Warn` for recoverable degradation (parse failure, skipping a file). `Debug` for per-item tracing. `Error` only for abort-worthy conditions that stop the scan.
- **Message:** lowercase, no terminal punctuation, prefixed with the collector/component name: `"complexity: Go AST parse failed, skipping file"`.
- **Error field:** name it exactly `"error"` and pass the raw error value (slog formats it). Always include at least one context field alongside (e.g. `"file"`, `"path"`, `"package"`).
- **Never concatenate runtime values into the message.** Do not write `slog.Warn("vuln: reading "+name, …)`; use `slog.Warn("vuln: reading manifest", "file", name, …)` so structured consumers can index on `file`.
- **Do not log-and-return the same error.** Either log it or wrap-and-return (with `fmt.Errorf("…: %w", err)`), not both — pick based on whether the caller can do anything with it.
- **Field names:** `snake_case`, stable across releases. Common keys: `file`, `path`, `package`, `version`, `url`, `status`, `cap`, `attempt`.

### Adding a new formatter

1. Create `internal/output/yourformat.go`
2. Implement the `output.Formatter` interface:
   ```go
   type Formatter interface {
       Name() string
       Format(signals []signal.RawSignal, w io.Writer) error
   }
   ```
3. Self-register in an `init()` function: `output.RegisterFormatter(&YourFormatter{})`
4. Add tests in `internal/output/yourformat_test.go`
5. Update `README.md` format list

### Adding a new report section

1. Create `internal/report/yoursection.go`
2. Implement the `report.Section` interface:
   ```go
   type Section interface {
       Name() string
       Description() string
       Analyze(result *signal.ScanResult) error
       Render(w io.Writer) error
   }
   ```
3. Self-register in an `init()` function: `report.Register(&YourSection{})`
4. Add a blank import in `cmd/stringer/report.go`: `_ "github.com/davetashner/stringer/internal/report"`
   (already present — this ensures all section `init()` functions run)
5. Add tests in `internal/report/yoursection_test.go`

### Signal schema

```go
type RawSignal struct {
    Source      string    // Collector name: "todos", "gitlog", etc.
    Kind        string    // "todo", "fixme", "revert", "churn", "stale_branch", etc.
    FilePath    string    // Where in the repo this was found
    Line        int       // Line number (0 if not applicable)
    Title       string    // Short description (used as bead title)
    Description string    // Longer context (used as bead description)
    Author      string    // Git blame author or commit author
    Timestamp   time.Time // When this signal was created
    Confidence  float64   // 0.0-1.0, how certain we are this is real work
    Tags        []string  // Free-form tags for clustering hints
    ClosedAt    time.Time // When this signal was closed/resolved (zero if open)
    Priority    *int      // LLM-inferred priority (1-4). Nil = use confidence mapping.
    Blocks      []string  // Bead IDs this signal blocks
    DependsOn   []string  // Bead IDs this signal depends on
}
```

### Beads JSONL output contract

Each line must be a valid JSON object compatible with beads. Required fields:
- `id`: deterministic hash with `str-` prefix (e.g., `str-0e4098f9`) — SHA-256 of source+kind+filepath+line+title, truncated to 8 hex chars
- `title`: string
- `type`: one of `bug`, `task`, `chore` (mapped from signal kind)
- `priority`: 1-4 (mapped from confidence: >=0.8→P1, >=0.6→P2, >=0.4→P3, <0.4→P4)
- `status`: `open` or `closed` (closed for pre-closed beads from resolved TODOs, merged PRs, and closed GitHub issues)
- `created_at`: ISO 8601 timestamp (from git blame, empty if unavailable)
- `created_by`: git blame author or `stringer` as fallback

Optional but valuable:
- `description`: file location context (e.g., `Location: main.go:42`)
- `labels`: kind tag + `stringer-generated` + collector name

### Before submitting changes

- `go test -race ./...` — all tests pass
- `golangci-lint run ./...` — no new warnings
- Test output against `bd create` on a real repo
- Update AGENTS.md if you changed the architecture or interfaces

### Main branch integrity

`main` must never contain code that fails to build, test, or lint. All changes require a pull request with passing CI — no direct pushes to `main`.

**Required CI status checks** (all must pass before merge):

| Check | What it verifies |
|-------|-----------------|
| `Test (Go 1.24)` | Build + tests on minimum supported Go version |
| `Test (Go 1.25)` | Build + tests on latest Go version |
| `Vet` | `go vet` static analysis |
| `Format` | `gofmt` formatting compliance |
| `Lint` | `golangci-lint` (includes gosec SAST) |
| `Tidy` | `go.mod` / `go.sum` are tidy; `go mod verify` checksums match |
| `Coverage` | Test coverage above 90% threshold |
| `Vulncheck` | No known vulnerabilities in dependencies |
| `Binary Size` | Binary does not exceed 2x baseline (`.github/binary-size-baseline`) |
| `Commit Lint` | PR commits follow conventional commits format (PRs only) |
| `Breaking Change Guard` | No breaking changes without major version bump (PRs only) |
| `Go Generate` | Generated files are up to date |
| `License Check` | All dependency licenses are OSS-compatible |
| `Archived Deps Check` | Warns if any GitHub-hosted dependencies are archived |
| `PR Size Guard` | Warns at 500 lines, fails at 1000 non-test lines (PRs only) |
| `Doc Staleness` | AGENTS.md interface code blocks match source; warns on internal Go changes without doc update (PRs only) |
| `Fuzz` | Fuzz testing for input parsing (mcpserver, config, beads) |
| `Backlog Health` | Beads backlog consistency checks |
| `Analyze` / `CodeQL` | Static analysis and security scanning |

A separate [OpenSSF Scorecard](https://securityscorecards.dev/viewer/?uri=github.com/davetashner/stringer) workflow runs on the default branch to track supply chain security posture.

**No exceptions.** Branch protection enforces these checks for all users including admins. If CI is broken, fix the checks — do not bypass them.

## Releasing

See [docs/release-strategy.md](docs/release-strategy.md) for the full release strategy, versioning policy, and Homebrew tap setup.

**Quick release checklist:**

1. Ensure `main` is clean, CI is green, README is up to date
2. Tag: `git tag v0.x.0 && git push origin v0.x.0`
3. GoReleaser runs automatically via `.github/workflows/release.yml`
4. Verify the [GitHub Release](https://github.com/davetashner/stringer/releases) has binaries and checksums

**Version is injected at build time** — never hardcode it. GoReleaser sets `-X main.Version={{.Version}}` via ldflags. During development, `stringer version` shows `dev`.

## Use Beads for task tracking

This project dogfoods Beads. Use `bd` for all task tracking:

```bash
bd ready --json          # Find next work
bd create "Title" -t task -p 2 --json
bd close bd-xxx --reason "Done" --json
bd sync                  # Before ending session
```

Do not use markdown TODOs or external trackers.

## Landing the Plane (Session Completion)

**When ending a work session**, you MUST complete ALL steps below. Work is NOT complete until `git push` succeeds.

**MANDATORY WORKFLOW:**

1. **File issues for remaining work** - Create issues for anything that needs follow-up
2. **Run quality gates** (if code changed) - Tests, linters, builds
3. **Update issue status** - Close finished work, update in-progress items
4. **PUSH TO REMOTE** - This is MANDATORY:
   ```bash
   git pull --rebase
   bd sync
   git push
   git status  # MUST show "up to date with origin"
   ```
5. **Clean up** - Clear stashes, prune remote branches
6. **Verify** - All changes committed AND pushed
7. **Hand off** - Provide context for next session

**CRITICAL RULES:**
- Work is NOT complete until `git push` succeeds
- NEVER stop before pushing - that leaves work stranded locally
- NEVER say "ready to push when you are" - YOU must push
- If push fails, resolve and retry until it succeeds
