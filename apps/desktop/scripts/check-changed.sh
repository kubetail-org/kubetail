#!/usr/bin/env bash
# Runs the checks for the areas the working tree's changes touch
# (scripts/changed.sh), so a change to one area needs only that area's
# toolchain. `make check` runs every area; CI runs what
# .github/ci-config/path-filters.yml selects.
#
#   always                     lint-md, lint-claude-md, lint-markers, lint-security-model,
#                              cover-changed, then test-changed for what cover-changed did
#                              not test
#   sidecar/, tools/           lint-go, vet-go-changed
#   src-tauri/, proto/, the layout fixture  lint-rust, vet-rust
#   the webview and its config lint-ts, lint-tailwind
#   design/, tools/designsync/, lint-design
#   src/components/, layouts/,
#   routes/
#   *.sh                       lint-sh
#   YAML, .github/             lint-yaml
#
# The targets run in parallel, each one's output printed whole once it and the
# ones listed before it finish. vet-rust runs just before the tests, not beside
# them: each builds the sidecar binary to the same path.
#
# It checks this machine's platform, and CI the rest. CI alone runs, on every
# PR:
#
#   - go vet and go fix on the packages the branch did not change, and on other
#     platforms and tags
#   - staticcheck
#   - the sidecar's -tags debug tests, unless a changed file only the tag
#     builds (scripts/cover-changed.sh)
#   - every test on another OS
#
# ALL_PLATFORMS=1 runs the first two for the changed packages
# (scripts/vet-go-changed.sh).
set -euo pipefail
cd "$(dirname "$0")/.."

# shellcheck source=scripts/changed.sh
. scripts/changed.sh

jobs=(lint-md lint-claude-md lint-markers lint-security-model)
tests="cover-changed test-changed"
if has '^(sidecar|tools)/'; then jobs+=(lint-go vet-go-changed); fi
if rust_changed; then
  jobs+=(lint-rust)
  tests="vet-rust $tests"
fi
if has '^(src|lint)/|^\.ox[a-z]+rc[^/]*\.json$|^package\.json$|^pnpm-[^/]*\.yaml$|^tsconfig[^/]*\.json$|^[^/]*\.ts$'; then
  jobs+=(lint-ts lint-tailwind)
fi
if has '^(design|src/(components|layouts|routes)|tools/designsync)/'; then jobs+=(lint-design); fi
if has '\.sh$'; then jobs+=(lint-sh); fi
if has '\.ya?ml$|^\.github/|^\.yamllint'; then jobs+=(lint-yaml); fi
jobs+=("$tests")

# Compiles the changed Go packages' dependencies before the jobs that need
# them start: Go shares no work between running processes, so each would
# compile them itself. A failure is vet-go-changed's to report.
for mod in sidecar tools; do
  dirs=$(go_dirs "$mod")
  # shellcheck disable=SC2086 # one argument per package
  [ -z "$dirs" ] || go -C "$mod" build $dirs >/dev/null 2>&1 || true
done

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
