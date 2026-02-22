# Release Notes — v1.4.0

## Highlights

**New collectors:** Code duplication detector (C11) and coupling & circular
dependency detector (C12) join the lineup, giving Stringer structural analysis
capabilities alongside its existing signal mining.

**SARIF output:** Stringer now emits SARIF v2.1.0, with auto-detection when the
output file ends in `.sarif` or `.sarif.json`. Drop results straight into GitHub
Code Scanning or any SARIF-compatible viewer.

**Cross-collector confidence boosting (UX4.2):** Signals that appear in multiple
collectors now receive a confidence boost, surfacing truly important findings.

**Noise reduction:** Default excludes now skip `eval/` directories, the
duplication collector caps output at 200 signals, and a new large-repo guide
documents timeout behavior.

## What's Changed

### Features
- feat: add SARIF v2.1.0 output formatter (#235)
- feat: add SARIF format auto-detection and usage docs (#236)
- feat: add code duplication detector C11 (#238)
- feat: add coupling and circular dependency detector C12 (#239)
- feat: add cross-collector confidence boosting UX4.2 (#240)

### Fixes
- fix: increase fuzz test timeout to 2m in CI (#232)
- fix: sort signals by priority before applying --max-issues cap (#234)
- fix: flip age-boost to recency-boost and widen keyword severity gaps (#237)
- fix: deduplicate collector source in beads labels (#241)

### Docs & Maintenance
- docs: add real-world benchmark results, standardize lottery risk terminology (#233)
- docs: add large-repo timeout and noise-reduction guidance
- chore: add eval/ to default excludes, cap duplication output at 200

## Upgrading

```bash
brew upgrade stringer
# or
go install github.com/davetashner/stringer/cmd/stringer@v1.4.0
```

No configuration changes required. Existing `.stringer.yaml` files continue to
work as-is. The new `eval/` exclude and duplication cap apply automatically.
