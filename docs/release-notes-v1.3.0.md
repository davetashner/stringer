## Stringer v1.3.0

Major feature release adding 5 new collectors, expanded language support, an HTML dashboard, and new report sections.

### New Collectors

- **Complexity Hotspot Detector (C9)** — Detects complex functions using composite scoring (lines/50 + branches). Identifies functions that are candidates for refactoring. (#218)
- **Dead Code Detector (C8)** — Detects unused functions and types via regex heuristic and reference search across Go, JS/TS, Python, Ruby, Java, and Kotlin. (#220)
- **Git Hygiene Detector (C13)** — Catches large committed binaries, forgotten merge conflict markers, accidentally committed secrets, and mixed line endings. (#222)
- **Documentation Staleness Detector (C12)** — Detects stale documentation, co-change drift between docs and source, and broken internal links. (#228)
- **Configuration Drift Detector (C11)** — Detects env var drift, dead config keys, and inconsistent defaults across environment files. (#229)
- **API Contract Drift Detector (C10)** — Detects drift between OpenAPI/Swagger specs and route handler registrations in code. Supports Go, JS/TS (Express, Next.js), and Python (Flask, FastAPI, Django) frameworks.

### Language Expansion (L1)

- Added PHP, Swift, Scala, and Elixir support to the patterns collector for file detection, test identification, and test-ratio analysis. (#227)

### Report & Dashboard

- **HTML Dashboard Generator** — `stringer report --format html` now generates an interactive backlog dashboard with signal charts and filtering. (#226)
- **Health Trend Analysis Section (R3.1)** — New report section showing repository health trends over time. (#224)
- **Module Health Summary Section (A6)** — New report section with per-module health scoring and summary metrics. (#230)

### Fixes

- Fix coverage false positives for non-source directories and warn on shallow clones. (#223)
- Fix `bd init` flags in integration test. (#225)

### Infrastructure

- Migrate beads backend from SQLite to Dolt. (#219)

### Upgrade

No breaking changes. Safe to upgrade from v1.2.0 with no configuration changes. All new collectors are enabled by default — disable individual collectors in `.stringer.yaml` if needed:

```yaml
collectors:
  complexity:
    enabled: false
```
