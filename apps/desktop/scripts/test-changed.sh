#!/usr/bin/env bash
# Run the tests the working tree's changes touch (scripts/changed.sh). The full
# suites and coverage run in CI.
#
#   JS    vitest's `related` on the changed files in src/ and lint/, which
#         follows imports, so a hook's change runs the tests of the components
#         using it. A change to the toolchain runs it all.
#   Go    the packages with a changed Go file, in sidecar/. A go.mod
#         or go.sum change alone runs none. tools/ runs whole.
#   Rust  the whole suite, and only when its inputs changed (rust_changed in
#         scripts/changed.sh): it builds
#         the sidecar first, and is the slowest of the three.
#
# MEASURED=1 skips the suites scripts/cover-changed.sh runs, since its coverage
# runs test them: JS, the sidecar's Go, and Rust.
# scripts/check-changed.sh runs both.
set -euo pipefail
cd "$(dirname "$0")/.."

# shellcheck source=scripts/changed.sh
. scripts/changed.sh

if [ "${MEASURED:-}" != 1 ]; then
  vitest=$(vitest_scope)
  if [ -n "$vitest" ]; then
    # shellcheck disable=SC2086 # one argument per word
    pnpm vitest $vitest
  fi

  go_pkgs=$(go_dirs sidecar)
  if [ -n "$go_pkgs" ]; then
    # shellcheck disable=SC2086 # one argument per package
    (cd sidecar && go test $go_pkgs)
  fi
fi

if has '^tools/'; then
  (cd tools && go test ./...)
fi

if rust_changed && [ "${MEASURED:-}" != 1 ]; then
  make test-rust
fi
