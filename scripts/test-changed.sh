#!/usr/bin/env bash
# Runs the tests the working tree's changes touch (scripts/changed.sh). CI runs
# every suite.
#
#   Go          the changed packages in modules/.
#   TypeScript  vitest's `related`, which follows imports. A change to the
#               toolchain runs the whole suite.
#   Rust        the whole workspace, when crates/ or proto/ changed.
#   Shell       bats, when scripts/ changed.
#
# MEASURED=1 skips the suites scripts/cover-changed.sh runs, since its coverage
# runs test them: Go, TypeScript, and Rust under RUST=1.
# scripts/check-changed.sh runs both.
set -euo pipefail
cd "$(dirname "$0")/.."

# shellcheck source=scripts/changed.sh
. scripts/changed.sh

if [ "${MEASURED:-}" != 1 ]; then
  go_pkgs=$(go_pkgs)
  if [ -n "$go_pkgs" ]; then
    # shellcheck disable=SC2086 # one argument per package
    (cd modules && go test $go_pkgs)
  fi

  vitest=$(vitest_scope)
  if [ -n "$vitest" ]; then
    # shellcheck disable=SC2086 # one argument per word
    (cd dashboard-ui && pnpm exec vitest $vitest)
  fi
fi

if has '^(crates|proto)/' && ! { [ "${MEASURED:-}" = 1 ] && [ "${RUST:-}" = 1 ]; }; then
  (cd crates && cargo test)
fi

if has '^scripts/'; then
  bats scripts/
fi
