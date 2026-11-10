#!/usr/bin/env bash
# List the lines the working tree changes since the branch left main that a
# local coverage run measured and no test reached (tools/coverdiff). A pre-push
# aid, not the gate: CI's coverage / Gate merges every OS's runs, so a line only
# another OS's tests reach is listed here.
#
#   Go    when modules/ changed: the changed packages' tests, or every test
#         with FULL=1 or a change to a go.mod or go.sum. Either way the whole
#         workspace is measured, so a line in shared/ that only another
#         module's tests reach is listed unless FULL=1.
#   TS    when dashboard-ui/ changed: the whole suite.
#   Rust  when crates/ or proto/ changed, and only with RUST=1: it is the
#         slowest of the three. Needs cargo-llvm-cov on PATH.
#
# Each suite is measured as .github/workflows/coverage.yml's jobs measure it; a
# change to one belongs in the other. BASE is the ref the branch left, by
# default upstream/main or else origin/main.
set -euo pipefail
cd "$(dirname "$0")/.."

# shellcheck source=scripts/changed.sh
. scripts/changed.sh

out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

# The paths .github/codecov.yml ignores, which the gate never judges. Keep the
# two lists in step.
ignored=(
  'modules/*/graph/generated.go'
  'modules/*/graph/model/models_gen.go'
  'modules/shared/clusteragentpb/**'
  'dashboard-ui/src/lib/graphql/*/__generated__/**'
  'modules/**/mock/**'
  'modules/shared/testutils/**'
  'dashboard-ui/src/mocks/**'
  'dashboard-ui/src/test-utils.tsx'
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

workspace=github.com/kubetail-org/kubetail/modules/...
if has '^modules/'; then
  if [ "${FULL:-}" = 1 ] || has '^modules/.*go\.(mod|sum)$'; then
    go_pkgs=$workspace
  else
    go_pkgs=$(grep -E '^modules/.*\.go$' <<<"$changed" | xargs -r -n1 dirname | sort -u | existing | sed 's|^modules|.|' || true)
  fi
  if [ -n "$go_pkgs" ]; then
    # shellcheck disable=SC2086 # one argument per package
    (cd modules && go test -covermode=atomic -coverpkg="$workspace" -coverprofile="$out/go.out" $go_pkgs)
    (cd tools && go run ./coverignore -dir ../modules -o "$out/go-filtered.out" "$out/go.out")
    reports+=(-go "modules=$out/go-filtered.out")
  fi
fi

if has '^dashboard-ui/'; then
  (cd dashboard-ui && pnpm vitest run --coverage)
  reports+=(-lcov "$PWD/dashboard-ui/coverage/lcov.info")
fi

if [ "${RUST:-}" = 1 ] && has '^(crates|proto)/'; then
  if ! (cd crates && cargo llvm-cov --version >/dev/null 2>&1); then
    echo "cargo-llvm-cov is not installed: cargo install cargo-llvm-cov --locked" >&2
    exit 1
  fi
  (cd crates && cargo llvm-cov --lcov --output-path "$out/rust.lcov")
  (cd tools && go run ./coverignore -lcov -o "$out/rust-filtered.lcov" "$out/rust.lcov")
  reports+=(-lcov "$out/rust-filtered.lcov")
fi

if [ ${#reports[@]} -eq 0 ]; then
  echo "No measured suite changed; nothing to check."
  exit 0
fi
(cd tools && go run ./coverdiff -root .. -diff "$out/changes.diff" "${reports[@]}")
echo "Every changed line a test measured is covered."
