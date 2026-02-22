# Large Repository Guidance

Stringer works out of the box on most repositories, but repos with 10k+ files
benefit from a few configuration tweaks.

## Collector Timeouts

By default collectors run to completion with no timeout. On very large repos
the duplication, todos, deadcode, and patterns collectors can take several
minutes. Set a per-collector timeout to keep scans predictable:

```bash
stringer scan . --collector-timeout=60s
```

Or configure it in `.stringer.yaml`:

```yaml
collector_timeout: 60s
```

When a collector exceeds the timeout it is cancelled and its partial results
are discarded. The scan continues with the remaining collectors.

## Duplication Collector

The duplication collector caps file input at 10,000 files and output at 200
signals (configurable via `max_issues`). On a stress-test scan of a mixed
monorepo the uncapped collector produced 7,554 signals — the cap keeps output
actionable.

```yaml
collectors:
  duplication:
    max_issues: 100   # tighter cap if needed
```

## Recommended Excludes

Stringer ships with default excludes for `vendor/`, `node_modules/`,
`testdata/`, `third_party/`, `eval/`, and similar paths. Add project-specific
excludes for generated code or large vendored trees:

```yaml
exclude_patterns:
  - "generated/**"
  - "assets/**"
```

## Stress-Test Reference (Q3 2026)

| Metric | Value |
|--------|-------|
| Repo scanned | stringer (self-scan including eval fixtures) |
| Total signals (uncapped) | ~9,268 |
| Duplication signals (uncapped) | ~7,554 |
| After eval/ exclude + 200 cap | ~1,700 |
