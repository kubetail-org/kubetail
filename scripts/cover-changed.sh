#!/usr/bin/env bash
# List the lines the working tree changes since the branch left main that a
# local coverage run measured and no test reached (tools/coverdiff). A pre-push
# aid, not the gate: each coverage-*.yml's Gate merges every OS's runs, so a
# line only another OS's tests reach is listed here.
#
#   Go    the changed packages' tests, or every test with FULL=1, in one run
#         per area as CI measures them: cli/ with dashboard/, libs/go-common/,
#         and cluster/api/. Each run measures its own area alone, so a
#         dashboard/ line that only the CLI's tests reach is listed unless
#         FULL=1.
#   TS    the tests vitest relates to the changed files in apps/web/src/,
#         or the whole suite when the toolchain changed.
#   Rust  when cluster/agent/ or libs/proto/ changed, and only with RUST=1: it is the
#         slowest of the three. Needs cargo-llvm-cov on PATH.
#
# The Go and TypeScript runs go side by side; Rust follows them.
#
# Each suite is measured as the coverage-*.yml workflows measure it; a change
# to one belongs in the other. BASE is the ref the branch left, by
# default upstream/main or else origin/main.
set -euo pipefail
cd "$(dirname "$0")/.."

# shellcheck source=scripts/changed.sh
. scripts/changed.sh

out=$(mktemp -d)
pids=()
cleanup() {
  [ ${#pids[@]} -eq 0 ] || kill "${pids[@]}" 2>/dev/null || true
  rm -rf "$out"
}
trap cleanup EXIT

# The paths .github/codecov.yml ignores, which the gate never judges. Keep the
# two lists in step.
ignored=(
  'dashboard/graph/generated.go'
  'dashboard/graph/model/models_gen.go'
  'cluster/api/graph/generated.go'
  'cluster/api/graph/model/models_gen.go'
  'libs/go-common/clusteragentpb/**'
  'libs/go-common/testpb/**'
  'apps/web/src/lib/graphql/*/__generated__/**'
  'dashboard/**/mock/**'
  'libs/go-common/**/mock/**'
  'libs/go-common/testutils/**'
  'apps/web/src/mocks/**'
  'apps/web/src/test-utils.tsx'
)
measured=(.)
for p in "${ignored[@]}"; do measured+=(":(exclude,glob)$p"); done

# The changed lines: tracked changes against the merge base, then each
# untracked file whole. --no-index exits 1 when the files differ.
diff_opts=(-U0 --no-color --no-ext-diff --src-prefix=a/ --dst-prefix=b/)
{
  git diff "${diff_opts[@]}" "$merge_base" -- "${measured[@]}"
  git ls-files --others --exclude-standard -- "${measured[@]}" | while read -r f; do
    git diff "${diff_opts[@]}" --no-index /dev/null "$f" || true
  done
} >"$out/changes.diff"

reports=()
names=()

# run <name> <command...>: starts a suite beside the others. The wait below
# prints its output.
run() {
  local name=$1
  shift
  "$@" >"$out/$name.log" 2>&1 &
  pids+=($!)
  names+=("$name")
}

# go_area <name> <-coverpkg> <package pattern>: measures the area's changed
# packages, or all of them with FULL=1.
go_area() {
  local name=$1 cover=$2 pattern=$3 pkgs
  if [ "${FULL:-}" = 1 ] && has "$go_modules"; then
    pkgs=$(tr '|' '\n' <<<"$pattern" | sed 's|^|./|; s|$|/...|')
  else
    pkgs=$(grep -E "^\./($pattern)(/|$)" <<<"$(go_pkgs)" || true)
  fi
  [ -n "$pkgs" ] || return 0
  # shellcheck disable=SC2086 # one argument per package
  run "go-$name" go test -covermode=atomic -coverpkg="$cover" -coverprofile="$out/go-$name.out" $pkgs
}
repo=github.com/kubetail-org/kubetail
go_area cli "$repo/cli/...,$repo/dashboard/..." 'cli|dashboard'
go_area libs "$repo/libs/go-common/..." 'libs/go-common'
go_area cluster "$repo/cluster/api/..." 'cluster/api'

vitest=$(vitest_scope)
if [ -n "$vitest" ]; then
  # shellcheck disable=SC2086 # one argument per word
  run vitest pnpm -C apps/web exec vitest $vitest --coverage
fi

failed=()
for i in "${!pids[@]}"; do
  wait "${pids[$i]}" || failed+=("${names[$i]}")
  cat "$out/${names[$i]}.log"
done
if [ ${#failed[@]} -gt 0 ]; then
  echo "Failed: ${failed[*]}"
  exit 1
fi

for area in cli libs cluster; do
  [ -f "$out/go-$area.out" ] || continue
  (cd tools && go run ./coverignore -dir .. -o "$out/go-$area-filtered.out" "$out/go-$area.out")
  reports+=(-go ".=$out/go-$area-filtered.out")
done
if [ -n "$vitest" ]; then
  reports+=(-lcov "$PWD/apps/web/coverage/lcov.info")
fi

if [ "${RUST:-}" = 1 ] && has '^(cluster/agent|libs/proto)/'; then
  if ! (cd cluster/agent && cargo llvm-cov --version >/dev/null 2>&1); then
    echo "cargo-llvm-cov is not installed: cargo install cargo-llvm-cov --locked" >&2
    exit 1
  fi
  (cd cluster/agent && cargo llvm-cov --lcov --output-path "$out/rust.lcov")
  (cd tools && go run ./coverignore -lcov -o "$out/rust-filtered.lcov" "$out/rust.lcov")
  reports+=(-lcov "$out/rust-filtered.lcov")
fi

if [ ${#reports[@]} -eq 0 ]; then
  echo "No measured suite changed; nothing to check."
  exit 0
fi
(cd tools && go run ./coverdiff -root .. -diff "$out/changes.diff" "${reports[@]}")
echo "Every changed line a test measured is covered."
