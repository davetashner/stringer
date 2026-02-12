#!/usr/bin/env bash
# scan-and-import.sh — End-to-end stringer workflow
#
# Usage: ./scan-and-import.sh [repo-path]
#
# This script demonstrates the typical stringer workflow:
# 1. Preview signal count with --dry-run
# 2. Scan and save output to a file
# 3. Validate the output
# 4. Import into beads

set -euo pipefail

REPO="${1:-.}"
OUTPUT="signals.jsonl"

echo "=== Step 1: Preview signal count ==="
stringer scan "$REPO" --dry-run
echo

read -r -p "Continue with full scan? [y/N] " confirm
if [[ "$confirm" != [yY] ]]; then
    echo "Aborted."
    exit 0
fi

echo
echo "=== Step 2: Scan and save output ==="
stringer scan "$REPO" -o "$OUTPUT"
echo "Saved to $OUTPUT"
echo

echo "=== Step 3: Validate output ==="
stringer validate "$OUTPUT"
echo

echo "=== Step 4: Import into beads ==="
echo "Run the following command to import:"
echo
echo "  bd import -i $OUTPUT"
echo
echo "Or preview first with:"
echo
echo "  bd import -i $OUTPUT --dry-run"
