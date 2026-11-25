#!/usr/bin/env bash
# Runs go vet and go fix over the Go packages the working tree's changes touch
# (scripts/changed.sh), in sidecar/ and tools/, on this machine's
# platform, untagged. CI's Vet · Go and Staticcheck run make vet-go's
# checks over every package, platform and tag.
#
#   go vet         the changed packages and the packages importing them, so a
#                  changed API fails where it is used
#   go fix -diff   the changed packages
#
# ALL_PLATFORMS=1 adds staticcheck, and checks a changed package again on each
# other GOOS, linux/arm64 and tag that builds it from different files, as
# make vet-go does for every package. Importers run here alone. A change to a
# module's go.mod or go.sum alone checks no package; CI checks them all.
# scripts/check-sidecar-deps.sh runs when the sidecar's code or go.mod changed.
set -euo pipefail
cd "$(dirname "$0")/.."

# shellcheck source=scripts/changed.sh
. scripts/changed.sh

all=${ALL_PLATFORMS:-}

# configs <mod>: the platforms and tags make vet-go checks the module on, as
# environments, one per line.
configs() {
  echo GOOS=linux GOARCH=amd64
  [ "$1" != sidecar ] || echo GOOS=linux GOARCH=arm64
  echo GOOS=darwin GOARCH=amd64
  echo GOOS=windows GOARCH=amd64
  if [ "$1" = sidecar ]; then
    echo GOFLAGS=-tags=debug
    echo GOFLAGS=-tags=livecache
    echo GOFLAGS=-tags=liveswitch
  fi
}

# importers <mod> <pkg...>: the module's packages importing one of them, from
# their tests too.
importers() {
  local mod=$1
  shift
  go -C "$mod" list -f '{{.ImportPath}}{{range .Imports}} {{.}}{{end}}{{range .TestImports}} {{.}}{{end}}{{range .XTestImports}} {{.}}{{end}}' ./... |
    awk -v pkgs="$*" 'BEGIN { n = split(pkgs, p, " "); for (i = 1; i <= n; i++) changed[p[i]] = 1 }
      { for (i = 2; i <= NF; i++) if ($i in changed) { print $1; next } }'
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
if [ "$all" = 1 ]; then
  # Built for the host: under GOOS=windows, `go tool` would build a Windows binary.
  go build -C tools -o "$work/staticcheck" honnef.co/go/tools/cmd/staticcheck
fi

status=0

# check <mod> <env> <pkg...>: go vet and go fix under one environment, and
# staticcheck with ALL_PLATFORMS=1, its run kept for the module's -merge.
check() {
  local mod=$1 config=$2 name out
  shift 2
  name=${config:-host}
  # shellcheck disable=SC2086 # one word per variable
  if ! (cd "$mod" && env $config go vet "$@"); then
    echo "go vet failed ($mod, $name)"
    status=1
  fi
  # shellcheck disable=SC2086
  out=$(cd "$mod" && env $config go fix -stringsbuilder=false -diff "$@" 2>&1) || true
  if [ -n "$out" ]; then
    echo "$out"
    echo "Run 'go fix -stringsbuilder=false' ($mod, $name) on these packages and review the result."
    status=1
  fi
  [ "$all" = 1 ] || return 0
  # shellcheck disable=SC2086
  (cd "$mod" && env $config "$work/staticcheck" -f binary "$@") >"$work/$mod-$(tr -c 'a-z0-9\n' - <<<"$name").bin" || status=1
}

# shellcheck disable=SC2086 # one argument per package
for mod in sidecar tools; do
  dirs=$(go_dirs "$mod")
  [ -n "$dirs" ] || continue
  echo "→ $mod"
  pkgs=$(go -C "$mod" list $dirs)

  check "$mod" "" $pkgs
  importing=$(importers "$mod" $pkgs)
  if [ -n "$importing" ]; then
    (cd "$mod" && go vet $importing) || status=1
  fi
  [ "$all" = 1 ] || continue

  here=$(go_files "$mod" "" $pkgs)
  while read -r config; do
    there=$(go_files "$mod" "$config" $pkgs)
    # The packages whose files differ under this config.
    split=$(comm -3 <(echo "$here") <(echo "$there") | tr -d '\t' | cut -d' ' -f1 | sort -u)
    if [ -n "$split" ]; then
      check "$mod" "$config" $split
    fi
  done < <(configs "$mod")
  # Code only another platform or tag uses reads as unused here: -merge
  # reports U1000 only where every run that checked the file agrees.
  (cd "$mod" && "$work/staticcheck" -merge "$work/$mod"-*.bin) || status=1
done

if grep -E '^sidecar/.*\.go$|^sidecar/go\.(mod|sum)$' <<<"$changed" | grep -qv '_test\.go$'; then
  sh scripts/check-sidecar-deps.sh || status=1
fi

exit $status
