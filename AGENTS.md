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
│   ├── version.go              # version subcommand
│   └── exitcodes.go            # exit code constants
├── internal/
│   ├── collector/          # Collector registry and interface
│   │   └── collector.go        # Register(), List(), Get(), Collector interface
│   ├── collectors/         # Signal extraction modules
│   │   └── todos.go            # TODO/FIXME/HACK/XXX/BUG/OPTIMIZE scanner
│   ├── output/             # Output formatters
│   │   ├── formatter.go        # Formatter interface and registry
│   │   └── beads.go            # Beads JSONL writer (primary)
│   ├── pipeline/           # Scan orchestration
│   │   ├── pipeline.go         # New(), Run() — runs collectors, caps output
│   │   └── validate.go         # ScanConfig validation
│   ├── redact/             # Secret redaction
│   │   └── redact.go           # Scrub sensitive patterns from signal content
│   └── signal/             # Domain types
│       └── signal.go           # RawSignal, ScanConfig, ScanResult, CollectorOpts
├── test/
│   └── integration/        # Integration tests
├── testdata/
│   └── fixtures/           # Test fixture repos
├── docs/
│   ├── decisions/          # Decision records (see "Decision Records" section)
│   └── competitive-analysis.md
├── go.mod
├── go.sum
├── AGENTS.md               # You are here
├── README.md
├── LICENSE
└── CLAUDE.md
```

**Note:** The collector architecture is extensible (see [Adding a new collector](#adding-a-new-collector)) but v0.1.0 ships with one implementation: the TODO collector. Planned collectors (gitlog, github, patterns) and additional formatters (markdown, json) are on the roadmap.

## Tech Stack

- **Language:** Go 1.24+ (matches Beads ecosystem)
- **CLI framework:** `spf13/cobra` for command/flag parsing
- **Git interaction:** `go-git` for blame lookups
- **Testing:** `stretchr/testify` for assertions
- **Linting:** `golangci-lint` v2 with gosec
- **Output:** Beads-compatible JSONL adhering to `bd import` expectations

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

# Only the TODO collector (currently the only one)
./stringer scan /path/to/repo --collectors=todos

# Dry run (preview signal count without writing JSONL)
./stringer scan /path/to/repo --dry-run

# Dry run with machine-readable JSON output
./stringer scan /path/to/repo --dry-run --json
```

## Key Design Decisions

1. **Collectors are independent and composable.** Each collector produces a stream of `RawSignal` structs. They can run in parallel. Adding a new collector means implementing one interface.

2. **The LLM pass is optional.** `--no-llm` mode skips clustering and produces one bead per signal. Useful for CI, air-gapped environments, or when you just want the raw TODO scan.

3. **Output is always valid `bd import` input.** The beads JSONL writer is the critical path. Every output must round-trip through `bd import` cleanly. Test this in CI.

4. **Stringer never modifies the target repo.** It is read-only. It writes output to stdout or a specified file. The user decides when and how to `bd import`.

5. **Idempotency matters.** Running stringer twice on the same repo should produce the same output (modulo LLM non-determinism in clustering mode). Use deterministic hashing for signal deduplication.

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
}
```

### Beads JSONL output contract

Each line must be a valid JSON object that `bd import` accepts. Required fields:
- `id`: deterministic hash with `str-` prefix (e.g., `str-0e4098f9`) — SHA-256 of source+kind+filepath+line+title, truncated to 8 hex chars
- `title`: string
- `type`: one of `bug`, `task`, `chore` (mapped from signal kind)
- `priority`: 1-4 (mapped from confidence: >=0.8→P1, >=0.6→P2, >=0.4→P3, <0.4→P4)
- `status`: `open` (always, since these are discovered work)
- `created_at`: ISO 8601 timestamp (from git blame, empty if unavailable)
- `created_by`: git blame author or `stringer` as fallback

Optional but valuable:
- `description`: file location context (e.g., `Location: main.go:42`)
- `labels`: kind tag + `stringer-generated` + collector name

### Before submitting changes

- `go test -race ./...` — all tests pass
- `golangci-lint run ./...` — no new warnings
- Test output against `bd import` on a real repo
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
| `Tidy` | `go.mod` / `go.sum` are tidy |
| `Coverage` | Test coverage above 55% threshold |
| `Vulncheck` | No known vulnerabilities in dependencies |
| `Binary Size` | Binary does not exceed 2x baseline (`.github/binary-size-baseline`) |
| `Commit Lint` | PR commits follow conventional commits format (PRs only) |
| `Go Generate` | Generated files are up to date |
| `License Check` | All dependency licenses are OSS-compatible |

**No exceptions.** Branch protection enforces these checks for all users including admins. If CI is broken, fix the checks — do not bypass them.

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
