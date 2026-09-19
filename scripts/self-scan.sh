#!/usr/bin/env bash
#
# self-scan.sh — run stringer on this repository and compare the findings at
# confidence >= 0.8 with .stringer/baseline.json (DR-027). CI runs this exact
# script (.github/workflows/self-scan.yml), so a local run matches the gate.
#
# Usage:
#   ./scripts/self-scan.sh            # check; exits 4 on new findings
#   ./scripts/self-scan.sh --prune    # also drop baseline entries no longer found
#   ./scripts/self-scan.sh --accept   # accept every new finding (review the diff!)
#
# Extra arguments are passed to `stringer baseline check`. To accept a single
# finding, run the `stringer baseline suppress sts-…` command the check prints.
set -euo pipefail

# Deterministic collectors only: no network (github, vuln, dephealth) and no
# history window relative to today or dependent on clone depth (gitlog,
# lotteryrisk, docstale). Listed explicitly so a new collector joins the gate
# only by editing this line.
COLLECTORS="apidrift,complexity,configdrift,coupling,deadcode,duplication,githygiene,patterns,todos"
MIN_CONFIDENCE="0.8"

cd "$(git rev-parse --show-toplevel)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# Build from this checkout so the gate measures the code under review.
go build -o "$tmp/stringer" ./cmd/stringer

# --no-baseline: the check needs every finding to tell new from resolved.
# --strict: a failed collector fails the gate instead of hiding findings.
"$tmp/stringer" scan . --quiet \
  --collectors "$COLLECTORS" \
  --min-confidence "$MIN_CONFIDENCE" \
  --no-baseline --strict \
  --format json --output "$tmp/scan.json"

"$tmp/stringer" baseline check "$tmp/scan.json" "$@"
