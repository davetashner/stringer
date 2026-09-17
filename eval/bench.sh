#!/usr/bin/env bash
# README real-world benchmark runner.
#
# Clones each target with a shallow history, runs `stringer scan` (JSON) and
# `stringer report` sequentially, and records wall-clock time, peak RSS of the
# stringer process, per-collector signal counts, confidence distribution and the
# pinned commit SHA so the run can be reproduced later.
#
# Usage:
#   eval/bench.sh [--bin PATH] [--out DIR] [--depth N] [--reuse] [--exclude LIST] owner/repo [owner/repo ...]
#
# Defaults: --bin stringer (from PATH), --out eval/results/bench-<YYYY-MM>,
#           --depth 100, --exclude github (needs a token; not part of the README run).
#
# Output per repo (in $OUT/<owner>-<repo>/):
#   scan.json, scan.stderr, report.txt, report.stderr, summary.json
# Aggregate with: python3 eval/bench-table.py $OUT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="stringer"
OUT="$SCRIPT_DIR/results/bench-$(date +%Y-%m)"
DEPTH=100
REUSE=false
EXCLUDE="github"
TARGETS=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --bin) BIN="$2"; shift 2 ;;
        --out) OUT="$2"; shift 2 ;;
        --depth) DEPTH="$2"; shift 2 ;;
        --exclude) EXCLUDE="$2"; shift 2 ;;
        --reuse) REUSE=true; shift ;;
        -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
        *) TARGETS+=("$1"); shift ;;
    esac
done

if [[ ${#TARGETS[@]} -eq 0 ]]; then
    echo "usage: $0 [options] owner/repo [...]" >&2
    exit 1
fi

for cmd in git python3 perl; do
    command -v "$cmd" >/dev/null || { echo "FATAL: missing $cmd" >&2; exit 1; }
done

mkdir -p "$OUT"
BIN_VERSION="$("$BIN" version 2>/dev/null | head -1 || echo unknown)"
{
    echo "stringer: $BIN_VERSION"
    echo "binary:   $(command -v "$BIN")"
    echo "depth:    $DEPTH"
    echo "exclude:  $EXCLUDE"
    echo "host:     $(uname -m) $(sysctl -n hw.ncpu 2>/dev/null || nproc) cores, $(( $(sysctl -n hw.memsize 2>/dev/null || echo 0) / 1024 / 1024 / 1024 )) GB RAM, $(uname -s) $(uname -r)"
    echo "started:  $(date -u +%Y-%m-%dT%H:%M:%SZ)"
} > "$OUT/environment.txt"
cat "$OUT/environment.txt"
echo

now() { perl -MTime::HiRes=time -e 'printf "%.2f\n", time'; }

# run_measured <label> <stdout-file> <stderr-file> <cmd...>
# Records "<label>_seconds", "<label>_max_rss_mb" and "<label>_exit" into $MEASURE_FILE.
run_measured() {
    local label="$1" outfile="$2" errfile="$3"; shift 3
    local start end max_rss=0 rss pid exit_code
    start=$(now)
    "$@" > "$outfile" 2> "$errfile" &
    pid=$!
    while kill -0 "$pid" 2>/dev/null; do
        rss=$(ps -o rss= -p "$pid" 2>/dev/null | tr -d ' ' || true)
        if [[ -n "$rss" && "$rss" -gt "$max_rss" ]]; then max_rss=$rss; fi
        sleep 1
    done
    wait "$pid" && exit_code=0 || exit_code=$?
    end=$(now)
    {
        echo "${label}_seconds=$(perl -e "printf '%.1f', $end - $start")"
        echo "${label}_max_rss_mb=$(( max_rss / 1024 ))"
        echo "${label}_exit=$exit_code"
    } >> "$MEASURE_FILE"
    printf "  %-7s %6.1fs  peak %5d MB  exit %d\n" "$label" "$(perl -e "print $end - $start")" "$(( max_rss / 1024 ))" "$exit_code"
}

for target in "${TARGETS[@]}"; do
    target="${target#https://github.com/}"; target="${target%.git}"; target="${target%/}"
    owner="${target%%/*}"; repo="${target##*/}"
    dir="$OUT/${owner}-${repo}"
    repo_dir="$dir/repo"
    MEASURE_FILE="$dir/measure.txt"
    mkdir -p "$dir"

    echo "=== $target ==="
    if [[ -d "$repo_dir/.git" && "$REUSE" == true ]]; then
        echo "  reusing clone"
    else
        rm -rf "$repo_dir"
        echo "  cloning --depth $DEPTH"
        git clone --quiet --depth "$DEPTH" "https://github.com/$target.git" "$repo_dir"
    fi
    sha=$(git -C "$repo_dir" rev-parse HEAD)
    sha_date=$(git -C "$repo_dir" log -1 --format=%cs)
    files=$(git -C "$repo_dir" ls-files | wc -l | tr -d ' ')
    echo "  HEAD $sha ($sha_date), $files tracked files"

    : > "$MEASURE_FILE"
    run_measured scan "$dir/scan.json" "$dir/scan.stderr" \
        "$BIN" scan "$repo_dir" -f json -x "$EXCLUDE" --no-color
    run_measured report "$dir/report.txt" "$dir/report.stderr" \
        "$BIN" report "$repo_dir" -x "$EXCLUDE" --no-color

    python3 - "$dir" "$target" "$sha" "$sha_date" "$files" "$BIN_VERSION" "$DEPTH" <<'PY'
import json, re, sys, collections, os
d, target, sha, sha_date, files, version, depth = sys.argv[1:8]
m = {}
for line in open(os.path.join(d, "measure.txt")):
    k, v = line.strip().split("=", 1)
    m[k] = float(v) if "." in v else int(v)
summary = {
    "repo": target, "sha": sha, "sha_date": sha_date, "files": int(files),
    "stringer": version, "clone_depth": int(depth), **m,
}
try:
    data = json.load(open(os.path.join(d, "scan.json")))
    sigs = data.get("signals") or []
    summary["total_signals"] = len(sigs)
    summary["by_source"] = dict(collections.Counter(s["Source"] for s in sigs).most_common())
    summary["by_kind"] = dict(collections.Counter(s["Kind"] for s in sigs).most_common())
    conf = [s["Confidence"] for s in sigs]
    summary["confidence"] = {
        "high_ge_0.8": sum(c >= 0.8 for c in conf),
        "med_0.5_0.8": sum(0.5 <= c < 0.8 for c in conf),
        "low_lt_0.5": sum(c < 0.5 for c in conf),
    }
    summary["collectors_ran"] = data.get("metadata", {}).get("collectors", [])
except Exception as e:  # noqa: BLE001
    summary["scan_parse_error"] = str(e)
# Per-collector durations and counts from the report header.
per = {}
try:
    for line in open(os.path.join(d, "report.txt"), errors="replace"):
        mm = re.match(r"\s+(\w+)\s+(\d+) signals \(([\d.]+)(ms|s|m)", line)
        if mm:
            name, n, t, unit = mm.groups()
            secs = float(t) / 1000 if unit == "ms" else float(t) * (60 if unit == "m" else 1)
            per[name] = {"signals": int(n), "seconds": round(secs, 2)}
except FileNotFoundError:
    pass
summary["report_collectors"] = per
# Stderr diagnostics.
for label in ("scan", "report"):
    try:
        txt = open(os.path.join(d, f"{label}.stderr"), errors="replace").read()
    except FileNotFoundError:
        txt = ""
    summary[f"{label}_stderr"] = {
        "lines": txt.count("\n"),
        "errors": len(re.findall(r"(?i)\berror\b", txt)),
        "warnings": len(re.findall(r"(?i)\bwarn", txt)),
        "panics": len(re.findall(r"(?i)panic", txt)),
        "timeouts": len(re.findall(r"(?i)timeout|deadline", txt)),
    }
json.dump(summary, open(os.path.join(d, "summary.json"), "w"), indent=2)
print(f"  {summary.get('total_signals', '?')} signals; summary.json written")
PY
    echo
done

echo "finished: $(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$OUT/environment.txt"
echo "Done. Aggregate with: python3 $SCRIPT_DIR/bench-table.py $OUT"
