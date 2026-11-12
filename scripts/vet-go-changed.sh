#!/usr/bin/env bash
# Runs go vet and go fix over the Go packages the working tree's changes touch
# (scripts/changed.sh), on this machine's platform. CI's Go · Vet and
# Go · Staticcheck run make vet-go's checks over every package, on Linux and
# Windows.
#
#   go vet         the changed packages and the workspace's packages importing
#                  them, so a changed API fails where it is used
#   go fix -diff   the changed packages
#
# ALL_PLATFORMS=1 adds staticcheck, and checks a changed package again under
# each GOOS make vet-go checks that builds it from different files. Importers
# run on this platform alone.
#
# shellcheck disable=SC2086 # package lists and GOOS settings split into words
set -euo pipefail
cd "$(dirname "$0")/.."

# shellcheck source=scripts/changed.sh
. scripts/changed.sh

go_pkgs=$(go_pkgs)
[ -n "$go_pkgs" ] || exit 0

all=${ALL_PLATFORMS:-}
workspace=github.com/kubetail-org/kubetail/modules/...
status=0

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
if [ "$all" = 1 ]; then
  # Built for the host: under GOOS=windows, `go tool` would build a Windows binary.
  go build -C tools -o "$work/staticcheck" honnef.co/go/tools/cmd/staticcheck
fi

# go_files <env> <pkg...>: each Go file the packages build under the
# environment, tests included, as "<package> <file>", sorted.
go_files() {
  local config=$1
  shift
  # shellcheck disable=SC2016 # a Go template
  env $config go -C modules list -e -f '{{$p := .ImportPath}}{{range .GoFiles}}{{$p}} {{.}}
{{end}}{{range .CgoFiles}}{{$p}} {{.}}
{{end}}{{range .TestGoFiles}}{{$p}} {{.}}
{{end}}{{range .XTestGoFiles}}{{$p}} {{.}}
{{end}}' "$@" | sort
}

# check <env> <pkg...>: go fix under one environment, and staticcheck with
# ALL_PLATFORMS=1, its run kept for the -merge below. plusbuild is off, as in
# scripts/check-go-fix.sh.
check() {
  local config=$1 out
  shift
  out=$(env $config go -C modules fix -plusbuild=false -diff "$@" 2>&1) || true
  if [ -n "$out" ]; then
    echo "$out"
    echo "Run '${config:+$config }go fix -plusbuild=false' in modules/ on these packages and review the result."
    status=1
  fi
  [ "$all" = 1 ] || return 0
  (cd modules && env $config "$work/staticcheck" -f binary "$@") >"$work/${config:-host}.bin" || status=1
}

# Import paths, as go list names an import. It has printed why it listed none.
pkgs=$(go -C modules list $go_pkgs) || true
[ -n "$pkgs" ] || exit 1

# The workspace's packages importing a changed one, from their tests too.
importers=$(go -C modules list -f '{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}' "$workspace" |
  awk 'NR == FNR { changed[$1]; next }
    { for (i = 2; i <= NF; i++) if ($i in changed) { print $1; next } }' <(echo "$pkgs") -)

go -C modules vet $pkgs $importers || status=1
check "" $pkgs

if [ "$all" = 1 ]; then
  here=$(go_files "" $pkgs)
  for config in GOOS=linux GOOS=windows; do
    # The packages this GOOS builds from other files.
    split=$(comm -3 <(echo "$here") <(go_files "$config" $pkgs) | tr -d '\t' | cut -d' ' -f1 | sort -u)
    [ -n "$split" ] || continue
    env $config go -C modules vet $split || status=1
    check "$config" $split
  done
  # Code only another platform uses reads as unused here: -merge reports U1000
  # only where every run that checked the file agrees.
  if ! (cd modules && "$work/staticcheck" -merge "$work"/*.bin) >"$work/merged.out"; then
    sed "s|^|modules/|" "$work/merged.out"
    status=1
  fi
fi

exit $status
