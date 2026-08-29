# CLI Reference

Complete flag and subcommand reference for every stringer command. For a quick tour, see the [README](../README.md). Everything here is also available from the CLI itself via `stringer <command> --help`.

## `stringer scan`

```
stringer scan [path] [flags]
```

| Flag               | Short | Default | Description                                               |
| ------------------ | ----- | ------- | --------------------------------------------------------- |
| `--collectors`     | `-c`  | (all)   | Comma-separated list of collectors to run                 |
| `--format`         | `-f`  | `beads` | Output format                                             |
| `--output`         | `-o`  | stdout  | Output file path                                          |
| `--dry-run`        |       |         | Show signal count without producing output                |
| `--delta`          |       |         | Only output new signals since last scan                   |
| `--json`           |       |         | Machine-readable output for `--dry-run`                   |
| `--max-issues`     |       | `0`     | Cap output count (0 = unlimited)                          |
| `--min-confidence` |       | `0`     | Filter signals below this threshold (0.0-1.0)            |
| `--kind`           |       |         | Filter by signal kind (comma-separated)                   |
| `--strict`         |       |         | Exit non-zero on any collector failure                    |
| `--git-depth`      |       | `0`     | Max commits to examine (default 1000)                     |
| `--git-since`      |       |         | Only examine commits after this duration (e.g., 90d, 6m)  |
| `--exclude`             | `-e`  |         | Glob patterns to exclude from scanning                    |
| `--exclude-collectors`  | `-x`  |         | Comma-separated list of collectors to skip                |
| `--include-closed`      |       |         | Include closed/merged issues and PRs from GitHub          |
| `--history-depth`       |       |         | Filter closed items older than this duration (e.g., 90d)  |
| `--anonymize`           |       | `auto`  | Anonymize author names: auto, always, or never            |
| `--collector-timeout`   |       |         | Per-collector timeout (e.g. 60s, 2m); 0 = no timeout      |
| `--paths`               |       |         | Restrict scanning to specific files or directories         |
| `--include-demo-paths`  |       |         | Include demo/example/tutorial paths in noise-prone signals |
| `--infer-priority`      |       |         | Use LLM to infer priority from signal context             |
| `--infer-deps`          |       |         | Use LLM to detect dependencies between signals            |
| `--no-llm`              |       |         | Skip all LLM passes (clustering, priority, dependencies)  |
| `--workspace`           |       |         | Scan only named workspace(s) (comma-separated)            |
| `--no-workspaces`       |       |         | Disable monorepo auto-detection, scan root as single dir  |
| `--no-baseline`         |       |         | Skip baseline suppression filtering                       |
| `--sarif-baseline`      |       |         | Previous SARIF file for baseline comparison (SARIF only)  |
| `--no-snippets`         |       |         | Omit code snippets from SARIF output                      |

**Global flags:** `--quiet` (`-q`), `--verbose` (`-v`), `--no-color`, `--help` (`-h`)

**Available collectors:** `todos`, `gitlog`, `patterns`, `lotteryrisk`, `github`, `dephealth`, `vuln`, `complexity`, `deadcode`, `githygiene`, `docstale`, `configdrift`, `apidrift`, `duplication`, `coupling`

**Available formats:** `beads`, `json`, `markdown`, `sarif`, `tasks`, `html`, `html-dir`

## `stringer report`

Generates a repository health report with analysis sections for lottery risk, code churn, complexity hotspots, TODO age distribution, coverage gaps, module summaries, health trends, git hygiene, and actionable recommendations.

```bash
stringer report .              # print to stdout
stringer report . -o report.txt # write to file
stringer report . --format json     # machine-readable output
stringer report . --format html-dir # HTML dashboard export
stringer report . --sections lottery-risk,churn  # specific sections only
```

| Flag                    | Short | Default | Description                                               |
| ----------------------- | ----- | ------- | --------------------------------------------------------- |
| `--collectors`          | `-c`  | (all)   | Comma-separated list of collectors to run                 |
| `--sections`            |       | (all)   | Comma-separated report sections to include                |
| `--output`              | `-o`  | stdout  | Output file path                                          |
| `--format`              | `-f`  |         | Output format (`json` for machine-readable, `html-dir` for dashboard) |
| `--git-depth`           |       | `0`     | Max commits to examine (default 1000)                     |
| `--git-since`           |       |         | Only examine commits after this duration (e.g., 90d, 6m)  |
| `--anonymize`           |       | `auto`  | Anonymize author names: auto, always, or never            |
| `--exclude-collectors`  | `-x`  |         | Comma-separated list of collectors to skip                |
| `--collector-timeout`   |       |         | Per-collector timeout (e.g. 60s, 2m); 0 = no timeout      |
| `--paths`               |       |         | Restrict scanning to specific files or directories         |
| `--workspace`           |       |         | Report only named workspace(s) (comma-separated)          |

**Available sections:** `lottery-risk`, `churn`, `todo-age`, `coverage`, `recommendations`, `trends`, `hotspots`, `git-hygiene`, `complexity`, `module-summary`

## `stringer docs`

Auto-generates an `AGENTS.md` scaffold from your repository structure, documenting modules, entry points, and conventions for AI agents.

```bash
stringer docs .              # print to stdout
stringer docs . -o AGENTS.md # write to file
stringer docs . --update     # update existing AGENTS.md, preserving manual sections
```

| Flag       | Short | Default | Description                                              |
| ---------- | ----- | ------- | -------------------------------------------------------- |
| `--output` | `-o`  | stdout  | Output file path                                         |
| `--update` |       |         | Update existing AGENTS.md, preserving manual sections    |

## `stringer context`

Generates a compact context summary of the repository for use in AI prompts. Includes project structure, recent git activity, and open work items.

```bash
stringer context .
stringer context . --format json  # machine-readable output
stringer context . --weeks 8      # include 8 weeks of history
```

| Flag       | Short | Default | Description                                              |
| ---------- | ----- | ------- | -------------------------------------------------------- |
| `--output` | `-o`  | stdout  | Output file path                                         |
| `--format` | `-f`  |         | Output format: `json` or `markdown`                      |
| `--weeks`  |       | `4`     | Weeks of git history to include                          |

## `stringer init`

Bootstraps stringer in a repository. Detects project characteristics and generates starter configuration. Non-destructive by default, so files that already exist are skipped.

```bash
stringer init .          # bootstrap stringer config
stringer init . --force  # overwrite existing .stringer.yaml
```

When run, `stringer init`:
- Creates `.stringer.yaml` with sensible defaults based on project detection
- Appends a stringer integration section to `AGENTS.md`
- Generates `.mcp.json` when a `.claude/` directory is detected (for MCP server integration)

| Flag      | Short | Default | Description                          |
| --------- | ----- | ------- | ------------------------------------ |
| `--force` |       |         | Overwrite existing `.stringer.yaml`  |

## `stringer config`

View and modify stringer configuration from the CLI. Supports dot-notation key paths and both repo-level and global config.

```bash
stringer config list                          # show all settings with source
stringer config get output_format             # get a single value
stringer config set output_format json        # set a value in .stringer.yaml
stringer config set collectors.todos.min_confidence 0.8
stringer config set --global no_llm true      # set in global config
```

| Subcommand | Description |
|------------|-------------|
| `get <key>` | Get a config value by dot-notation key path |
| `set <key> <value>` | Set a config value (auto-detects type) |
| `list` | List all values with source annotations (repo/global) |

Use `--global` on `get`/`set` to target `~/.config/stringer/config.yaml` instead of the repo-level `.stringer.yaml`.

## `stringer baseline`

Manage signal suppressions. Create a baseline from the current scan, suppress known findings, and track them across scans. Suppressed signals are filtered from future scan output.

```bash
stringer baseline create .                              # snapshot current signals as baseline
stringer baseline suppress str-0e4098f9 --reason acknowledged  # suppress a signal
stringer baseline suppress str-11e6af70 --reason false-positive --note "Test fixture"
stringer baseline suppress str-3afa7732 --reason won't-fix --expires 90d
stringer baseline list                                  # show active suppressions
stringer baseline remove str-0e4098f9                   # un-suppress a signal
stringer baseline status                                # summary counts
```

| Subcommand | Description |
|------------|-------------|
| `create <path>` | Create baseline from current scan |
| `suppress <id>` | Suppress a signal by ID |
| `list` | List active suppressions |
| `remove <id>` | Remove a suppression |
| `status` | Show suppression summary |

**Suppression reasons:** `acknowledged`, `won't-fix`, `false-positive`

## `stringer collectors`

List and inspect registered collectors.

```bash
stringer collectors list         # table of all collectors with status
stringer collectors info todos   # detailed info, signal types, config options
stringer collectors info duplication --json  # machine-readable with thresholds
```

| Subcommand | Description |
|------------|-------------|
| `list` | Show all collectors with name, status, and description |
| `info <name>` | Show detailed info including signal types, config options, and tunable thresholds |

## Exit codes

| Code | Name              | Meaning                                          |
| ---- | ----------------- | ------------------------------------------------ |
| `0`  | OK                | All collectors succeeded                         |
| `1`  | Invalid Args      | Invalid arguments or bad path                    |
| `2`  | Partial Failure   | Some collectors failed, partial output written   |
| `3`  | Total Failure     | No output produced                               |
