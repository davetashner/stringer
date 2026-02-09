#!/usr/bin/env bash
set -euo pipefail

# Stringer Evaluation Harness
# Usage: ./eval/run-eval.sh [owner/repo] [--reuse]
# Default target: httpie/cli

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Parse arguments
TARGET="${1:-httpie/cli}"
REUSE=false
for arg in "$@"; do
    if [[ "$arg" == "--reuse" ]]; then
        REUSE=true
    fi
done

# Derive repo name from owner/repo or URL
if [[ "$TARGET" == http* ]]; then
    # Extract owner/repo from URL
    TARGET=$(echo "$TARGET" | sed 's|.*github.com/||' | sed 's|\.git$||' | sed 's|/$||')
fi
OWNER=$(echo "$TARGET" | cut -d/ -f1)
REPO=$(echo "$TARGET" | cut -d/ -f2)
RESULT_DIR="$SCRIPT_DIR/results/${OWNER}-${REPO}"
REPO_DIR="$RESULT_DIR/repo"
STRINGER_BIN="$SCRIPT_DIR/results/.stringer-bin"

# Configurable timeouts
EVAL_CMD_TIMEOUT="${EVAL_CMD_TIMEOUT:-300}"
COLLECTOR_TIMEOUT="${COLLECTOR_TIMEOUT:-60s}"

echo "=== Stringer Evaluation Harness ==="
echo "Target:  $TARGET"
echo "Results: $RESULT_DIR"
echo "Cmd timeout: ${EVAL_CMD_TIMEOUT}s  Collector timeout: $COLLECTOR_TIMEOUT"
echo ""

# --- Prerequisites ---
echo "--- Checking prerequisites ---"
missing=()
for cmd in jq git go; do
    if ! command -v "$cmd" &>/dev/null; then
        missing+=("$cmd")
    fi
done
if [[ ${#missing[@]} -gt 0 ]]; then
    echo "FATAL: Missing required tools: ${missing[*]}"
    exit 1
fi

if [[ -n "${GITHUB_TOKEN:-}" ]]; then
    echo "  GITHUB_TOKEN: set (GitHub collector enabled)"
else
    echo "  GITHUB_TOKEN: not set (GitHub collector will be skipped)"
fi
echo ""

# --- Build stringer ---
echo "--- Building stringer from source ---"
mkdir -p "$SCRIPT_DIR/results"
(cd "$PROJECT_ROOT" && go build -o "$STRINGER_BIN" ./cmd/stringer)
echo "  Built: $STRINGER_BIN"
"$STRINGER_BIN" --version 2>/dev/null || echo "  (version not available)"
echo ""

# --- Clone target repo ---
if [[ -d "$REPO_DIR" ]] && [[ "$REUSE" == true ]]; then
    echo "--- Reusing existing clone: $REPO_DIR ---"
else
    echo "--- Cloning $TARGET ---"
    rm -rf "$REPO_DIR"
    mkdir -p "$RESULT_DIR"
    git clone --depth 1000 "https://github.com/$TARGET.git" "$REPO_DIR" 2>&1 | tail -3
fi
echo ""

# --- Helper: run a command with timing and wall-clock timeout ---
TIMING_FILE="$RESULT_DIR/timing.txt"
: > "$TIMING_FILE"

run_timed() {
    local label="$1"
    local outfile="$2"
    local errfile="$3"
    shift 3
    local max_seconds="${EVAL_CMD_TIMEOUT}"
    local start end elapsed
    start=$(date +%s)

    # Run command in background with watchdog kill (macOS-compatible, no timeout binary)
    "$@" > "$outfile" 2> "$errfile" &
    local pid=$!
    ( sleep "$max_seconds" && kill "$pid" 2>/dev/null ) &
    local watchdog=$!
    wait "$pid" 2>/dev/null || true
    local exit_code=$?
    kill "$watchdog" 2>/dev/null || true
    wait "$watchdog" 2>/dev/null || true

    end=$(date +%s)
    elapsed=$((end - start))
    echo "$label: ${elapsed}s" >> "$TIMING_FILE"
    if [[ "$elapsed" -ge "$max_seconds" ]]; then
        echo "  $label: ${elapsed}s (TIMEOUT after ${max_seconds}s, exit $exit_code)"
    else
        echo "  $label: ${elapsed}s (exit $exit_code)"
    fi
    return 0
}

# --- Build collector flags ---
COLLECTOR_FLAGS=""
if [[ -z "${GITHUB_TOKEN:-}" ]]; then
    COLLECTOR_FLAGS="-c todos,gitlog,patterns,lotteryrisk"
fi

# --- Run stringer scan in all formats ---
echo "--- Running stringer scan ---"

# Beads format
run_timed "scan-beads" "$RESULT_DIR/scan-beads.jsonl" "$RESULT_DIR/stderr-scan-beads.log" \
    "$STRINGER_BIN" scan $COLLECTOR_FLAGS --collector-timeout "$COLLECTOR_TIMEOUT" -f beads "$REPO_DIR"

# JSON format
run_timed "scan-json" "$RESULT_DIR/scan-json.json" "$RESULT_DIR/stderr-scan-json.log" \
    "$STRINGER_BIN" scan $COLLECTOR_FLAGS --collector-timeout "$COLLECTOR_TIMEOUT" -f json "$REPO_DIR"

# Markdown format
run_timed "scan-markdown" "$RESULT_DIR/scan-markdown.md" "$RESULT_DIR/stderr-scan-markdown.log" \
    "$STRINGER_BIN" scan $COLLECTOR_FLAGS --collector-timeout "$COLLECTOR_TIMEOUT" -f markdown "$REPO_DIR"

# Tasks format
run_timed "scan-tasks" "$RESULT_DIR/scan-tasks.txt" "$RESULT_DIR/stderr-scan-tasks.log" \
    "$STRINGER_BIN" scan $COLLECTOR_FLAGS --collector-timeout "$COLLECTOR_TIMEOUT" -f tasks "$REPO_DIR"

# Dry-run JSON
run_timed "scan-dryrun" "$RESULT_DIR/scan-dryrun.json" "$RESULT_DIR/stderr-scan-dryrun.log" \
    "$STRINGER_BIN" scan $COLLECTOR_FLAGS --collector-timeout "$COLLECTOR_TIMEOUT" --dry-run --json "$REPO_DIR"

echo ""

# --- Run stringer report ---
echo "--- Running stringer report ---"
run_timed "report" "$RESULT_DIR/report.txt" "$RESULT_DIR/stderr-report.log" \
    "$STRINGER_BIN" report $COLLECTOR_FLAGS --collector-timeout "$COLLECTOR_TIMEOUT" "$REPO_DIR"

echo ""

# --- Show timing summary ---
echo "--- Timing Summary ---"
cat "$TIMING_FILE"
echo ""

# --- Run analysis ---
echo "--- Running analysis ---"
echo ""
"$SCRIPT_DIR/analyze.sh" "$RESULT_DIR" "$REPO_DIR"
