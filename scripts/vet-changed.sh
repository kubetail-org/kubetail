#!/usr/bin/env bash
# Usage: vet-changed.sh cli|libs|cluster-api|cluster-agent|web|desktop
#
# Vets what the working tree's change reaches in one area, since the branch
# left main, for this platform and the default build tags. It prints each
# command it runs and each pass it skips. A development aid: it trades
# correctness for speed, so make vet-<area> and CI remain the check.
#
#   cli, libs,     go vet and the go fix check on the changed Go packages and
#   cluster-api    every package of the area whose build or tests import one
#   cluster-agent  clippy, when cluster/agent/ or libs/proto/ changed
#   web            tsc -b, when a TypeScript file or its config changed
#   desktop        the sidecar as the Go areas, the webview as web, and the
#                  host's clippy when its inputs changed
#
# BASE is the ref the branch left, by default upstream/main or else origin/main.
set -euo pipefail
cd "$(dirname "$0")/.."

area=${1:-}
case "$area" in
  cli) mods=(cli dashboard) ;;
  libs) mods=(libs/go-common) ;;
  cluster-api) mods=(cluster/api) ;;
  cluster-agent | web | desktop) ;;
  *)
    echo "usage: $0 cli|libs|cluster-api|cluster-agent|web|desktop" >&2
    exit 2
    ;;
esac

# shellcheck source=scripts/changed.sh
. scripts/changed.sh

# run_diff <command>...: as run, failing when the command prints a diff.
run_diff() {
  local out
  echo "→ $*"
  out=$("$@" 2>&1) || true
  if [ -n "$out" ]; then
    echo "$out"
    echo "Run it without -diff and review the result."
    return 1
  fi
}

# As scripts/check-go-fix.sh runs go fix; keep the two in step.
fix_flags=(-plusbuild=false -stringsbuilder=false)

# tsc <app>: type-checks the app when its TypeScript changed. tsc -b is
# incremental, so it checks the app whole.
tsc() {
  if has_code "^$1/.*\.tsx?$|^$1/(package\.json|tsconfig[^/]*\.json)$"; then
    run pnpm -C "$1" exec tsc -b
  else
    echo "Skipped tsc: no change to $1/'s TypeScript, tsconfigs or package.json."
  fi
}

case "$area" in
  cli | libs | cluster-api)
    pkgs=$(go_scope "${mods[@]}")
    if [ -n "$pkgs" ]; then
      # shellcheck disable=SC2086 # one argument per package
      run go vet $pkgs
      # shellcheck disable=SC2086 # one argument per package
      run_diff go fix "${fix_flags[@]}" -diff $pkgs
    else
      echo "Skipped go vet and go fix: no change reaches a package in ${mods[*]}."
    fi
    echo "Skipped GOOS=windows go vet and staticcheck: make vet-$area runs them."
    ;;
  cluster-agent)
    if has_code '^(cluster/agent|libs/proto)/'; then
      run_in cluster/agent cargo clippy --all --all-targets -- -D warnings
    else
      echo "Skipped clippy: no change in cluster/agent/ or libs/proto/."
    fi
    ;;
  web) tsc apps/web ;;
  desktop)
    sidecar=apps/desktop/sidecar
    pkgs=$(go_scope "$sidecar" | sed "s|^\./$sidecar|.|")
    if [ -n "$pkgs" ]; then
      # shellcheck disable=SC2086 # one argument per package
      run go -C "$sidecar" vet $pkgs
      # shellcheck disable=SC2086 # one argument per package
      run_diff go -C "$sidecar" fix "${fix_flags[@]}" -diff $pkgs
    else
      echo "Skipped the sidecar's go vet and go fix: no change reaches a package in $sidecar."
    fi
    tsc apps/desktop
    # The layout fixture sits in the sidecar's module, and the host's tests read it.
    if has_code '^apps/desktop/(src-tauri|proto)/|^apps/desktop/sidecar/internal/app/layout/testdata/'; then
      # tauri-build fails without the bundled sidecar binary.
      run go -C apps/desktop run scripts/build-sidecar.go
      run_in apps/desktop/src-tauri cargo clippy --all-targets -- -D warnings
    else
      echo "Skipped clippy: no change in apps/desktop/src-tauri/, apps/desktop/proto/ or the layout fixture."
    fi
    echo "Skipped the sidecar's debug, livecache and liveswitch tags, staticcheck and check-sidecar-deps.sh: make vet-desktop runs them."
    echo "Skipped the other platforms: CI vets them."
    ;;
esac
