#!/usr/bin/env bash
# Usage: test-changed.sh cli|libs|cluster-api|cluster-agent|web|desktop
#
# Runs the tests of one area that the working tree's change reaches, since the
# branch left main, with the flags make test-<area> uses. It prints each
# command it runs and each suite it skips. A development aid: it trades
# correctness for speed, so make test-<area> and CI remain the check.
#
#   cli, libs,     the changed Go packages and every package of the area
#   cluster-api    whose build or tests import one (go_scope)
#   cluster-agent  cargo test, when cluster/agent/ or libs/proto/ changed
#   web            the tests vitest relates to the changed files in src/ and
#                  lint/, or the whole suite when the toolchain changed
#   desktop        the sidecar as the Go areas, the webview as web, and the
#                  host's cargo test when its inputs changed
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

# vitest <app>: runs vitest on the tests the app's change reaches.
vitest() {
  local scope
  scope=$(vitest_scope "$1")
  if [ -n "$scope" ]; then
    # shellcheck disable=SC2086 # one argument per word
    run pnpm -C "$1" exec vitest $scope
  else
    echo "Skipped vitest: no change in $1/src/, $1/lint/ or its toolchain."
  fi
}

case "$area" in
  cli | libs | cluster-api)
    pkgs=$(go_scope "${mods[@]}")
    if [ -n "$pkgs" ]; then
      # GORACE drops the race detector's one-second sleep as each test binary exits.
      # shellcheck disable=SC2086 # one argument per package
      run env GORACE=atexit_sleep_ms=0 go test -race $pkgs
    else
      echo "Skipped go test: no change reaches a package in ${mods[*]}."
    fi
    ;;
  cluster-agent)
    if has_code '^(cluster/agent|libs/proto)/'; then
      run_in cluster/agent cargo test
    else
      echo "Skipped cargo test: no change in cluster/agent/ or libs/proto/."
    fi
    ;;
  web) vitest apps/web ;;
  desktop)
    sidecar=apps/desktop/sidecar
    pkgs=$(go_scope "$sidecar" | sed "s|^\./$sidecar|.|")
    if [ -n "$pkgs" ]; then
      # As test-desktop runs them.
      # shellcheck disable=SC2086 # one argument per package
      run env GORACE=atexit_sleep_ms=0 SSH_AUTH_SOCK= go -C "$sidecar" test -race -short -gcflags='modernc.org/...=-d=checkptr=0' $pkgs
    else
      echo "Skipped the sidecar's go test: no change reaches a package in $sidecar."
    fi
    vitest apps/desktop
    # The layout fixture sits in the sidecar's module, and the host's tests read it.
    if has_code '^apps/desktop/(src-tauri|proto)/|^apps/desktop/sidecar/internal/app/layout/testdata/'; then
      # tauri-build fails without the bundled sidecar binary.
      run go -C apps/desktop run scripts/build-sidecar.go
      run_in apps/desktop/src-tauri cargo test
    else
      echo "Skipped the host's cargo test: no change in apps/desktop/src-tauri/, apps/desktop/proto/ or the layout fixture."
    fi
    ;;
esac
