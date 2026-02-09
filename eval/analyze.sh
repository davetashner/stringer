#!/usr/bin/env bash
set -euo pipefail

# Stringer Evaluation Analysis
# Usage: ./eval/analyze.sh <result-dir> <repo-dir>

RESULT_DIR="${1:?Usage: analyze.sh <result-dir> <repo-dir>}"
REPO_DIR="${2:?Usage: analyze.sh <result-dir> <repo-dir>}"

# --- Counters ---
PASS_COUNT=0
WARN_COUNT=0
FAIL_COUNT=0

check() {
    local description="$1"
    local level="$2"
    local detail="${3:-}"

    case "$level" in
        PASS) PASS_COUNT=$((PASS_COUNT + 1)); printf "  %-6s %s" "[PASS]" "$description" ;;
        WARN) WARN_COUNT=$((WARN_COUNT + 1)); printf "  %-6s %s" "[WARN]" "$description" ;;
        FAIL) FAIL_COUNT=$((FAIL_COUNT + 1)); printf "  %-6s %s" "[FAIL]" "$description" ;;
    esac
    if [[ -n "$detail" ]]; then
        printf " — %s" "$detail"
    fi
    echo ""
}

ANALYSIS_OUTPUT="$RESULT_DIR/analysis.txt"

# Redirect all output to both terminal and file
exec > >(tee "$ANALYSIS_OUTPUT") 2>&1

echo "========================================"
echo "  Stringer Evaluation Analysis"
echo "========================================"
echo ""
echo "Results: $RESULT_DIR"
echo "Repo:    $REPO_DIR"
echo ""

# ============================================================
# 1. Signal Distribution
# ============================================================
echo "--- 1. Signal Distribution ---"
echo ""

JSON_FILE="$RESULT_DIR/scan-json.json"
if [[ -f "$JSON_FILE" ]] && [[ -s "$JSON_FILE" ]]; then
    TOTAL=$(jq '.metadata.total_count // 0' "$JSON_FILE")
    echo "  Total signals: $TOTAL"
    echo ""

    echo "  By collector (Source):"
    jq -r '.signals[] | .Source' "$JSON_FILE" | sort | uniq -c | sort -rn | while read -r count name; do
        printf "    %-20s %d\n" "$name" "$count"
    done
    echo ""

    echo "  By kind (Kind):"
    jq -r '.signals[] | .Kind' "$JSON_FILE" | sort | uniq -c | sort -rn | while read -r count name; do
        printf "    %-20s %d\n" "$name" "$count"
    done
    echo ""

    echo "  Confidence distribution:"
    HIGH=$(jq '[.signals[] | select(.Confidence >= 0.8)] | length' "$JSON_FILE")
    MED=$(jq '[.signals[] | select(.Confidence >= 0.5 and .Confidence < 0.8)] | length' "$JSON_FILE")
    LOW=$(jq '[.signals[] | select(.Confidence > 0 and .Confidence < 0.5)] | length' "$JSON_FILE")
    ZERO=$(jq '[.signals[] | select(.Confidence == 0)] | length' "$JSON_FILE")
    printf "    %-20s %d\n" "High (>=0.8)" "$HIGH"
    printf "    %-20s %d\n" "Medium (0.5-0.8)" "$MED"
    printf "    %-20s %d\n" "Low (>0-0.5)" "$LOW"
    printf "    %-20s %d\n" "Zero (0.0)" "$ZERO"
    echo ""

    if [[ "$TOTAL" -gt 0 ]]; then
        check "Scan produced $TOTAL signals" PASS
    else
        check "Scan produced zero signals" FAIL
    fi
else
    check "JSON output file missing or empty" FAIL
    TOTAL=0
fi
echo ""

# ============================================================
# 2. Known Bug Detection
# ============================================================
echo "--- 2. Known Bug Detection ---"
echo ""

# stringer-xd5: Empty timestamps (zero time)
if [[ -f "$JSON_FILE" ]] && [[ -s "$JSON_FILE" ]]; then
    ZERO_TS=$(jq '[.signals[] | select(.Timestamp == "0001-01-01T00:00:00Z")] | length' "$JSON_FILE")
    TOTAL_SIGNALS=$(jq '.signals | length' "$JSON_FILE")
    if [[ "$ZERO_TS" -gt 0 ]]; then
        PCT=0
        if [[ "$TOTAL_SIGNALS" -gt 0 ]]; then
            PCT=$(( (ZERO_TS * 100) / TOTAL_SIGNALS ))
        fi
        check "stringer-xd5: Empty timestamps" FAIL "$ZERO_TS/$TOTAL_SIGNALS signals (${PCT}%) have zero timestamp"

        # Break down by collector
        echo "    By collector:"
        jq -r '.signals[] | select(.Timestamp == "0001-01-01T00:00:00Z") | .Source' "$JSON_FILE" \
            | sort | uniq -c | sort -rn | while read -r count name; do
            printf "      %-20s %d\n" "$name" "$count"
        done
    else
        check "stringer-xd5: No empty timestamps" PASS
    fi
fi
echo ""

# stringer-y1q: Duplicate "stringer-generated" label
BEADS_FILE="$RESULT_DIR/scan-beads.jsonl"
if [[ -f "$BEADS_FILE" ]] && [[ -s "$BEADS_FILE" ]]; then
    DUP_LABEL=$(jq -c 'select(.labels) | .labels' "$BEADS_FILE" \
        | jq -s '[.[] | [.[] | select(. == "stringer-generated")] | select(length > 1)] | length')
    if [[ "$DUP_LABEL" -gt 0 ]]; then
        BEADS_TOTAL=$(wc -l < "$BEADS_FILE" | tr -d ' ')
        check "stringer-y1q: Duplicate stringer-generated label" FAIL "$DUP_LABEL/$BEADS_TOTAL records affected"
    else
        check "stringer-y1q: No duplicate labels" PASS
    fi
fi
echo ""

# ============================================================
# 3. Signal Quality
# ============================================================
echo "--- 3. Signal Quality ---"
echo ""

if [[ -f "$JSON_FILE" ]] && [[ -s "$JSON_FILE" ]] && [[ "$TOTAL" -gt 0 ]]; then
    # File paths exist in repo (skip github pseudo-paths and historical paths)
    MISSING_FILES=0
    CHECKED_FILES=0
    MISSING_EXAMPLES=""
    while IFS= read -r filepath; do
        # Skip github pseudo-paths
        if [[ "$filepath" == github/* ]]; then
            continue
        fi
        # Skip empty paths
        if [[ -z "$filepath" ]]; then
            continue
        fi
        CHECKED_FILES=$((CHECKED_FILES + 1))
        # Use -e (exists) not -f (is file) — lotteryrisk and patterns emit directory paths
        if [[ ! -e "$REPO_DIR/$filepath" ]]; then
            MISSING_FILES=$((MISSING_FILES + 1))
            if [[ "$MISSING_FILES" -le 5 ]]; then
                MISSING_EXAMPLES="$MISSING_EXAMPLES $filepath"
            fi
        fi
    done < <(jq -r '.signals[] | select(.Tags | map(. == "historical-path") | any | not) | .FilePath' "$JSON_FILE" | sort -u)

    if [[ "$MISSING_FILES" -gt 0 ]]; then
        check "File paths: $MISSING_FILES/$CHECKED_FILES unique paths don't exist" WARN "e.g.${MISSING_EXAMPLES}"
    else
        check "File paths: all $CHECKED_FILES unique paths exist in repo" PASS
    fi

    # Line numbers valid (spot check: non-zero lines should be within file length)
    BAD_LINES=0
    CHECKED_LINES=0
    while IFS=$'\t' read -r filepath line; do
        if [[ "$filepath" == github/* ]] || [[ -z "$filepath" ]] || [[ "$line" -eq 0 ]]; then
            continue
        fi
        if [[ ! -f "$REPO_DIR/$filepath" ]]; then
            continue
        fi
        CHECKED_LINES=$((CHECKED_LINES + 1))
        FILE_LEN=$(wc -l < "$REPO_DIR/$filepath" | tr -d ' ')
        if [[ "$line" -gt "$FILE_LEN" ]]; then
            BAD_LINES=$((BAD_LINES + 1))
        fi
        # Only spot-check first 50
        if [[ "$CHECKED_LINES" -ge 50 ]]; then
            break
        fi
    done < <(jq -r '.signals[] | [.FilePath, (.Line | tostring)] | @tsv' "$JSON_FILE" | sort -u | head -60)

    if [[ "$BAD_LINES" -gt 0 ]]; then
        check "Line numbers: $BAD_LINES/$CHECKED_LINES checked exceed file length" WARN
    else
        check "Line numbers: $CHECKED_LINES spot-checked, all valid" PASS
    fi

    # Title quality: not empty, not just whitespace
    EMPTY_TITLES=$(jq '[.signals[] | select(.Title == "" or .Title == null)] | length' "$JSON_FILE")
    if [[ "$EMPTY_TITLES" -gt 0 ]]; then
        check "Titles: $EMPTY_TITLES signals have empty titles" WARN
    else
        check "Titles: all signals have non-empty titles" PASS
    fi

    # Short titles (< 5 chars) — might be bare keywords
    SHORT_TITLES=$(jq '[.signals[] | select((.Title | length) < 5 and .Title != "")] | length' "$JSON_FILE")
    if [[ "$SHORT_TITLES" -gt 0 ]]; then
        EXAMPLES=$(jq -r '[.signals[] | select((.Title | length) < 5 and .Title != "") | .Title] | unique[:5] | join(", ")' "$JSON_FILE")
        check "Titles: $SHORT_TITLES signals have very short titles (<5 chars)" WARN "e.g. $EXAMPLES"
    else
        check "Titles: no suspiciously short titles" PASS
    fi
fi
echo ""

# ============================================================
# 4. Collector Coverage
# ============================================================
echo "--- 4. Collector Coverage ---"
echo ""

DRYRUN_FILE="$RESULT_DIR/scan-dryrun.json"
if [[ -f "$DRYRUN_FILE" ]] && [[ -s "$DRYRUN_FILE" ]]; then
    COLLECTOR_COUNT=$(jq '.collectors | length' "$DRYRUN_FILE")
    echo "  Collectors run: $COLLECTOR_COUNT"
    echo ""

    ZERO_COLLECTORS=""
    ERROR_COLLECTORS=""
    while IFS=$'\t' read -r name signals error; do
        if [[ "$signals" -eq 0 ]]; then
            ZERO_COLLECTORS="$ZERO_COLLECTORS $name"
        fi
        if [[ -n "$error" ]] && [[ "$error" != "null" ]] && [[ "$error" != "" ]]; then
            ERROR_COLLECTORS="$ERROR_COLLECTORS $name($error)"
        fi
        printf "    %-20s %d signals" "$name" "$signals"
        if [[ -n "$error" ]] && [[ "$error" != "null" ]] && [[ "$error" != "" ]]; then
            printf "  ERROR: %s" "$error"
        fi
        echo ""
    done < <(jq -r '.collectors[] | [.name, (.signals | tostring), (.error // "")] | @tsv' "$DRYRUN_FILE")

    echo ""
    if [[ -n "$ZERO_COLLECTORS" ]]; then
        check "Collectors with zero signals:$ZERO_COLLECTORS" WARN
    else
        check "All collectors produced signals" PASS
    fi

    if [[ -n "$ERROR_COLLECTORS" ]]; then
        check "Collectors with errors:$ERROR_COLLECTORS" FAIL
    else
        check "No collector errors" PASS
    fi
else
    check "Dry-run JSON missing or empty" FAIL
fi
echo ""

# ============================================================
# 5. Report Quality
# ============================================================
echo "--- 5. Report Quality ---"
echo ""

REPORT_FILE="$RESULT_DIR/report.txt"
if [[ -f "$REPORT_FILE" ]] && [[ -s "$REPORT_FILE" ]]; then
    REPORT_LINES=$(wc -l < "$REPORT_FILE" | tr -d ' ')
    check "Report generated ($REPORT_LINES lines)" PASS

    # Check for known section headers
    for section in "Lottery Risk" "Churn" "TODO Age" "Coverage"; do
        if grep -qi "$section" "$REPORT_FILE" 2>/dev/null; then
            # Check if section says "skipped" or "no data"
            if grep -qi "${section}.*skip" "$REPORT_FILE" 2>/dev/null; then
                check "Report section '$section': skipped" WARN
            else
                check "Report section '$section': present" PASS
            fi
        else
            check "Report section '$section': missing" WARN
        fi
    done
else
    check "Report output missing or empty" FAIL
fi
echo ""

# ============================================================
# 6. Performance
# ============================================================
echo "--- 6. Performance ---"
echo ""

if [[ -f "$DRYRUN_FILE" ]] && [[ -s "$DRYRUN_FILE" ]]; then
    TOTAL_DURATION=$(jq -r '.duration // "unknown"' "$DRYRUN_FILE")
    echo "  Total scan duration: $TOTAL_DURATION"
    echo ""

    # Per-collector duration
    echo "  Per-collector:"
    jq -r '.collectors[] | "    \(.name): \(.duration)"' "$DRYRUN_FILE"
    echo ""

    # Check for suspiciously slow collectors (> 30s)
    # Parse durations properly: handles "45.123s", "1m2.5s", etc.
    SLOW=$(jq -r '[.collectors[] |
        {name, dur: .duration} |
        {name, sec: ((.dur | capture("^(?<n>[0-9.]+)s$").n // null) | tonumber? // 0),
         has_min: ((.dur | test("^[0-9.]+m[0-9]")) // false)} |
        {name, sec: (if .has_min then 999 elif .sec > 0 then .sec else 0 end)} |
        select(.sec > 30) | .name] | join(", ")' "$DRYRUN_FILE" 2>/dev/null || true)
    if [[ -n "$SLOW" ]]; then
        check "Slow collectors (>30s): $SLOW" WARN
    else
        check "All collectors completed in reasonable time" PASS
    fi
fi

TIMING_FILE="$RESULT_DIR/timing.txt"
if [[ -f "$TIMING_FILE" ]]; then
    echo "  Wall-clock times:"
    while IFS= read -r line; do
        echo "    $line"
    done < "$TIMING_FILE"
    echo ""
fi
echo ""

# ============================================================
# 7. False Positives
# ============================================================
echo "--- 7. False Positives ---"
echo ""

if [[ -f "$JSON_FILE" ]] && [[ -s "$JSON_FILE" ]] && [[ "$TOTAL" -gt 0 ]]; then
    # Signals in directories that should be excluded
    for dir in vendor/ node_modules/ .git/ __pycache__/ .tox/ .eggs/ dist/ build/; do
        count=$(jq --arg dir "$dir" '[.signals[] | select(.FilePath | startswith($dir))] | length' "$JSON_FILE")
        if [[ "$count" -gt 0 ]]; then
            check "Signals in $dir: $count found" WARN
            # Show first 3 examples
            jq -r --arg dir "$dir" '[.signals[] | select(.FilePath | startswith($dir)) | .FilePath] | unique[:3] | .[]' "$JSON_FILE" \
                | while read -r fp; do echo "      $fp"; done
        fi
    done

    # Check for signals in test directories (informational, not necessarily bad)
    TEST_SIGNALS=$(jq '[.signals[] | select(.FilePath | test("test|spec|_test"; "i"))] | length' "$JSON_FILE")
    echo "  Signals in test files: $TEST_SIGNALS (informational)"

    # Overall false-positive verdict
    VENDOR_SIGNALS=$(jq '[.signals[] | select(.FilePath | test("^vendor/|^node_modules/|^\\.git/|^__pycache__/|^\\.tox/|^\\.eggs/|^dist/|^build/"))] | length' "$JSON_FILE")
    if [[ "$VENDOR_SIGNALS" -gt 0 ]]; then
        check "False positives in excluded dirs: $VENDOR_SIGNALS total" FAIL
    else
        check "No signals in vendor/build directories" PASS
    fi
fi
echo ""

# ============================================================
# 8. Cross-Format Consistency
# ============================================================
echo "--- 8. Cross-Format Consistency ---"
echo ""

# Count signals in each format
BEADS_COUNT=0
JSON_COUNT=0
DRYRUN_COUNT=0

if [[ -f "$BEADS_FILE" ]] && [[ -s "$BEADS_FILE" ]]; then
    BEADS_COUNT=$(wc -l < "$BEADS_FILE" | tr -d ' ')
fi
if [[ -f "$JSON_FILE" ]] && [[ -s "$JSON_FILE" ]]; then
    JSON_COUNT=$(jq '.signals | length' "$JSON_FILE")
fi
if [[ -f "$DRYRUN_FILE" ]] && [[ -s "$DRYRUN_FILE" ]]; then
    DRYRUN_COUNT=$(jq '.total_signals // 0' "$DRYRUN_FILE")
fi

echo "  Signal counts:"
printf "    %-15s %d\n" "beads (lines)" "$BEADS_COUNT"
printf "    %-15s %d\n" "json" "$JSON_COUNT"
printf "    %-15s %d\n" "dry-run" "$DRYRUN_COUNT"
echo ""

# Beads may differ from JSON due to dedup — check JSON vs dry-run (should match)
if [[ "$JSON_COUNT" -eq "$DRYRUN_COUNT" ]] && [[ "$JSON_COUNT" -gt 0 ]]; then
    check "JSON count matches dry-run count ($JSON_COUNT)" PASS
elif [[ "$JSON_COUNT" -gt 0 ]] && [[ "$DRYRUN_COUNT" -gt 0 ]]; then
    check "JSON ($JSON_COUNT) vs dry-run ($DRYRUN_COUNT) count mismatch" WARN
else
    check "Cannot compare formats (missing data)" WARN
fi

# Beads dedup may reduce count — just report the diff
if [[ "$BEADS_COUNT" -gt 0 ]] && [[ "$JSON_COUNT" -gt 0 ]]; then
    DIFF=$((JSON_COUNT - BEADS_COUNT))
    if [[ "$DIFF" -gt 0 ]]; then
        echo "  Note: Beads has $DIFF fewer records than JSON (expected from dedup)"
        check "Beads dedup removed $DIFF signals" PASS
    elif [[ "$DIFF" -lt 0 ]]; then
        check "Beads has more records than JSON (unexpected)" FAIL
    else
        check "Beads count equals JSON count (no dedup)" PASS
    fi
fi
echo ""

# ============================================================
# Stderr Review
# ============================================================
echo "--- Stderr Review ---"
echo ""

for logfile in "$RESULT_DIR"/stderr-*.log; do
    if [[ ! -f "$logfile" ]]; then
        continue
    fi
    basename=$(basename "$logfile")
    lines=$(wc -l < "$logfile" | tr -d ' ')
    if [[ "$lines" -gt 0 ]]; then
        echo "  $basename ($lines lines):"
        head -5 "$logfile" | while IFS= read -r line; do
            echo "    $line"
        done
        if [[ "$lines" -gt 5 ]]; then
            echo "    ... ($((lines - 5)) more lines)"
        fi
    fi
done
echo ""

# ============================================================
# Summary
# ============================================================
echo "========================================"
TOTAL_CHECKS=$((PASS_COUNT + WARN_COUNT + FAIL_COUNT))
echo "  Summary: $PASS_COUNT PASS, $WARN_COUNT WARN, $FAIL_COUNT FAIL ($TOTAL_CHECKS checks)"
echo "========================================"
