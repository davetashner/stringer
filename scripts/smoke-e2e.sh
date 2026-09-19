#!/usr/bin/env bash
# End-to-end smoke test: run the built stringer binary against a git
# repository (by default the stringer checkout itself) and check that scan
# and report succeed and produce sane output.
#
# Used by the cross-platform CI job on macOS and Windows (Git Bash), so keep
# it to portable bash + jq.
#
# Usage: scripts/smoke-e2e.sh <stringer-binary> [repo-path]
# The repo needs full history (fetch-depth: 0) for the gitlog assertions.
set -euo pipefail

bin=${1:?usage: smoke-e2e.sh <stringer-binary> [repo-path]}
repo=${2:-.}
exclude=github,vuln,dephealth
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

fail() {
  echo "::error::smoke-e2e: $*" >&2
  exit 1
}

echo "== stringer version"
"$bin" version

echo "== stringer scan (json)"
"$bin" scan "$repo" -f json -x "$exclude" -o "$out/scan.json" ||
  fail "stringer scan exited non-zero"
jq -e '.signals | type == "array"' "$out/scan.json" >/dev/null ||
  fail "scan output is not valid JSON with a signals array"

echo "Signals per collector:"
jq -r '.signals | group_by(.Source) | map("  \(.[0].Source): \(length)") | .[]' "$out/scan.json"

# Collectors that must find something in the stringer repo: todos and
# complexity walk files; gitlog walks history via go-git (churn in the last
# 90 days); lotteryrisk shells out to git blame/log.
for src in todos complexity gitlog lotteryrisk; do
  n=$(jq --arg s "$src" '[.signals[] | select(.Source == $s)] | length' "$out/scan.json")
  [ "$n" -gt 0 ] || fail "expected signals from $src, got 0"
done

# Output paths are repo-relative and slash-separated on every OS.
bad=$(jq -r '.signals[] | select(.FilePath | test("\\\\") or startswith("/") or test("^[A-Za-z]:")) | .FilePath' "$out/scan.json" | head -5)
[ -z "$bad" ] || fail "non-portable FilePath values in scan output: $bad"

echo "== stringer report (text)"
"$bin" report "$repo" -x "$exclude" --no-color -o "$out/report.txt" ||
  fail "stringer report exited non-zero"
[ -s "$out/report.txt" ] || fail "report output is empty"
head -20 "$out/report.txt"

echo "== stringer report (json)"
"$bin" report "$repo" -x "$exclude" -f json -o "$out/report.json" ||
  fail "stringer report -f json exited non-zero"
jq -e '(.sections | length) > 0' "$out/report.json" >/dev/null ||
  fail "report JSON has no sections"

echo "smoke-e2e: OK"
