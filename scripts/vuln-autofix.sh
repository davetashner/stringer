#!/usr/bin/env bash
# Apply the fixes for every vulnerability govulncheck says stringer's code
# reaches, in the working tree only. See DR-028.
#
# The Go toolchain and module fixes are applied together because Vulncheck is
# required on every PR: if both are vulnerable, a PR fixing only one of them
# fails on the other and neither could ever merge.
#
# Exit codes:
#   0  nothing reachable, or fixes applied and verified by a clean re-scan
#      (a Markdown summary is printed to stdout; logs go to stderr)
#   3  a person is needed; the working tree may be partly changed
set -euo pipefail

GOVULNCHECK="golang.org/x/vuln/cmd/govulncheck@v1.9.0"
GO_RELEASES_URL="https://go.dev/dl/?mode=json&include=all"

log() { echo "vuln-autofix: $*" >&2; }
need_person() {
  log "needs a person: $*"
  exit 3
}

# Prints one JSON object per reachable finding. Findings without a function in
# the trace are only imported or required, not called, so Vulncheck passes them.
reachable_findings() {
  go run "$GOVULNCHECK" -format json ./... |
    jq -c 'select(.finding and .finding.trace[0].function)
      | {id: .finding.osv, fixed: (.finding.fixed_version // ""), module: .finding.trace[0].module}' |
    sort -u
}

highest_version() { sed 's/^v//' | sort -V | tail -1; }

GOTOOLCHAIN="go$(cat .go-version)"
export GOTOOLCHAIN

findings=$(reachable_findings)
if [ -z "$findings" ]; then
  log "no reachable vulnerabilities"
  exit 0
fi

ids=$(jq -r .id <<<"$findings" | sort -u)
log "reachable: $(tr '\n' ' ' <<<"$ids")"

if jq -e 'select(.fixed == "")' <<<"$findings" >/dev/null; then
  need_person "a reachable vulnerability has no fixed version"
fi

summary=""

# The standard library is fixed by a newer Go patch on the same minor line.
go_needed=$(jq -r 'select(.module == "stdlib") | .fixed' <<<"$findings" | highest_version)
if [ -n "$go_needed" ]; then
  current=$(cat .go-version)
  minor=${current%.*}
  [ "${go_needed%.*}" = "$minor" ] || need_person "the Go fix is $go_needed, outside the $minor line"
  latest=$(curl -fsSL "$GO_RELEASES_URL" |
    jq -r --arg prefix "go$minor." '.[] | select(.stable) | .version | select(startswith($prefix)) | ltrimstr("go")' |
    sort -V | tail -1)
  [ -n "$latest" ] || need_person "could not find a Go $minor release"
  if [ "$(printf '%s\n%s\n' "$go_needed" "$latest" | sort -V | tail -1)" != "$latest" ]; then
    need_person "Go $go_needed is needed but the latest release is $latest"
  fi
  echo "$latest" >.go-version
  export GOTOOLCHAIN="go$latest"
  summary+="- Go $current -> $latest"$'\n'
fi

# Modules are fixed by the lowest version that clears all of their findings.
while read -r module; do
  [ -n "$module" ] || continue
  fixed=$(jq -r --arg m "$module" 'select(.module == $m) | .fixed' <<<"$findings" | highest_version)
  log "go get $module@v$fixed"
  go get "$module@v$fixed" >&2
  summary+="- \`$module\` -> v$fixed"$'\n'
done < <(jq -r 'select(.module != "stdlib") | .module' <<<"$findings" | sort -u)

go mod tidy >&2

# Only a person should raise the language version or change anything else.
if git diff -U0 -- go.mod | grep -qE '^\+(go|toolchain) '; then
  need_person "a fix raises the go or toolchain directive in go.mod"
fi
other=$(git status --porcelain | awk '{print $2}' | grep -vxE '\.go-version|go\.mod|go\.sum' || true)
[ -z "$other" ] || need_person "unexpected changes: $other"

go build -o /dev/null ./... >&2

remaining=$(reachable_findings)
if [ -n "$remaining" ]; then
  need_person "still reachable after fixes: $(jq -r .id <<<"$remaining" | sort -u | tr '\n' ' ')"
fi

printf "Fixes vulnerabilities that stringer's code reaches, found by \`govulncheck\`:\n\n"
while read -r id; do
  printf -- '- [%s](https://pkg.go.dev/vuln/%s)\n' "$id" "$id"
done <<<"$ids"
printf '\nChanges:\n\n%s\n' "$summary"
printf 'Released binaries stay affected until the next tag.\n'
