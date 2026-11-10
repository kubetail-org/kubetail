#!/usr/bin/env bash
# Runs the tests the working tree's changes touch (scripts/changed.sh). CI runs
# every suite.
#
#   Go          the changed packages of each module in modules/. A change to a
#               module's go.mod or go.sum runs that module's whole suite.
#   TypeScript  vitest's `related`, which follows imports. A change to the
#               toolchain runs the whole suite.
#   Rust        the whole workspace, when crates/ or proto/ changed.
#   Shell       bats, when scripts/ changed.
set -euo pipefail
cd "$(dirname "$0")/.."

# shellcheck source=scripts/changed.sh
. scripts/changed.sh

for mod in cli cluster-api dashboard shared; do
  dir=modules/$mod
  if has "^$dir/go\.(mod|sum)$"; then
    (cd "$dir" && go test ./...)
  else
    pkgs=$(grep -E "^$dir/.*\.go$" <<<"$changed" | xargs -r -n1 dirname | sort -u | existing | sed "s|^$dir|.|" || true)
    if [ -n "$pkgs" ]; then
      # shellcheck disable=SC2086 # one argument per package
      (cd "$dir" && go test $pkgs)
    fi
  fi
done

if has '^dashboard-ui/(package\.json|pnpm-lock\.yaml|vite\.config\.ts|vitest\.setup\.ts|tsconfig[^/]*\.json)$'; then
  (cd dashboard-ui && pnpm test run)
else
  ts=$(grep -E '^dashboard-ui/src/.*\.tsx?$' <<<"$changed" | existing | sed 's|^dashboard-ui/||' || true)
  if [ -n "$ts" ]; then
    # shellcheck disable=SC2086 # one argument per file
    (cd dashboard-ui && pnpm exec vitest related --run $ts)
  fi
fi

if has '^(crates|proto)/'; then
  (cd crates && cargo test)
fi

if has '^scripts/'; then
  bats scripts/
fi
