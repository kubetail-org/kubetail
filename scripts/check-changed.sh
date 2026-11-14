#!/usr/bin/env bash
# Runs the checks for the areas the working tree's changes touch
# (scripts/changed.sh), so a change to one area needs only that area's
# toolchain. `make check` runs every area; CI runs what
# .github/ci-config/path-filters.yml selects.
#
#   always           lint-md, lint-toolchains and lint-digests, which need no
#                    toolchain; cover-changed, then test-changed for what
#                    cover-changed did not test
#   modules/         lint-go, vet-go-changed
#   crates/, proto/  lint-rust, vet-rust
#   dashboard-ui/    lint-ts, vet-ts
#   e2e/             lint-python
#   *.sh             lint-sh
#   YAML, .github/   lint-yaml
#
# The jobs run in parallel, each one's output printed whole, in the order
# listed, once it and the ones before it finish. The targets within a job run
# in sequence: lint-ts and vet-ts each run pnpm install, lint-rust and vet-rust
# each run rustup, and test-changed runs what cover-changed did not.
#
# It checks this machine's platform, and CI the rest. CI alone runs, on every
# PR:
#
#   - go vet and go fix on the packages the branch did not change, and on
#     Windows
#   - staticcheck
#   - every test on another OS, and the Go tests under the race detector
#
# ALL_PLATFORMS=1 runs the first two for the changed packages
# (scripts/vet-go-changed.sh).
set -euo pipefail
cd "$(dirname "$0")/.."

# shellcheck source=scripts/changed.sh
. scripts/changed.sh

jobs=(lint-md lint-toolchains lint-digests)
if has '^modules/'; then jobs+=(lint-go vet-go-changed); fi
if has '^(crates|proto)/'; then jobs+=("lint-rust vet-rust"); fi
if has '^dashboard-ui/'; then jobs+=("lint-ts vet-ts"); fi
if has '^e2e/'; then jobs+=(lint-python); fi
if has '\.sh$'; then jobs+=(lint-sh); fi
if has '\.ya?ml$|^\.github/|^\.yamllint'; then jobs+=(lint-yaml); fi
jobs+=("cover-changed test-changed")

# Compiles the changed Go packages' dependencies before the jobs that need
# them start: Go shares no work between running processes, so each would
# compile them itself. A failure is vet-go-changed's to report.
go_pkgs=$(go_pkgs)
# shellcheck disable=SC2086 # one argument per package
[ -z "$go_pkgs" ] || go -C modules build $go_pkgs >/dev/null 2>&1 || true

# cover-changed's vitest runs beside lint-ts and vet-ts, so it must not meet
# their install changing node_modules.
if has '^dashboard-ui/'; then pnpm -C dashboard-ui install; fi

# test-changed leaves out what cover-changed's coverage runs tested.
export MEASURED=1

logs=$(mktemp -d)
pids=()
cleanup() {
  [ ${#pids[@]} -eq 0 ] || kill "${pids[@]}" 2>/dev/null || true
  rm -rf "$logs"
}
trap cleanup EXIT

for i in "${!jobs[@]}"; do
  # shellcheck disable=SC2086 # one argument per target
  make -k --no-print-directory ${jobs[$i]} >"$logs/$i" 2>&1 &
  pids+=($!)
done

failed=()
for i in "${!jobs[@]}"; do
  if wait "${pids[$i]}"; then result=ok; else result=FAILED; failed+=("${jobs[$i]}"); fi
  echo "── ${jobs[$i]}: $result"
  cat "$logs/$i"
done

if [ ${#failed[@]} -gt 0 ]; then
  echo "Failed: ${failed[*]}"
  exit 1
fi
