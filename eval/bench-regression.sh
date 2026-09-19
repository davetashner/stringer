#!/usr/bin/env bash
# Benchmark regression check: scan the pinned repos listed in eval/baseline/ and
# compare per-collector signal counts and durations with the committed baseline.
#
# Usage: eval/bench-regression.sh [--bin PATH] [--out DIR] [--update]
#   --bin     stringer binary to test (default: build ./cmd/stringer into $OUT/stringer)
#   --out     results directory (default: eval/results/bench-regression)
#   --update  rewrite eval/baseline/*.json from this run instead of checking
# Extra environment: BENCH_CHECK_MARKDOWN=FILE also writes the report as Markdown.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="$SCRIPT_DIR/results/bench-regression"
BIN=""
UPDATE=false
while [[ $# -gt 0 ]]; do
    case "$1" in
        --bin) BIN="$2"; shift 2 ;;
        --out) OUT="$2"; shift 2 ;;
        --update) UPDATE=true; shift ;;
        -h|--help) sed -n '2,9p' "$0"; exit 0 ;;
        *) echo "unknown option: $1" >&2; exit 1 ;;
    esac
done

mkdir -p "$OUT"
if [[ -z "$BIN" ]]; then
    BIN="$OUT/stringer"
    (cd "$SCRIPT_DIR/.." && go build -o "$BIN" ./cmd/stringer)
fi

# GITHUB_TOKEN would switch on lottery-risk review analysis (GitHub API): keep the scan offline.
# shellcheck disable=SC2046 # word-splitting the target list is intended
env -u GITHUB_TOKEN "$SCRIPT_DIR/bench.sh" --bin "$BIN" --out "$OUT" --depth 100 --no-report \
    --exclude github,vuln,dephealth $(python3 "$SCRIPT_DIR/bench-check.py" --targets)

if [[ "$UPDATE" == true ]]; then
    python3 "$SCRIPT_DIR/bench-check.py" --update "$OUT"
else
    python3 "$SCRIPT_DIR/bench-check.py" "$OUT" ${BENCH_CHECK_MARKDOWN:+--markdown "$BENCH_CHECK_MARKDOWN"}
fi
