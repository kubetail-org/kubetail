#!/usr/bin/env bash
# Usage: cover-changed.sh cli|libs|cluster-api|cluster-agent|web|desktop
#
# Lists the lines the working tree changes in one coverage area, since the
# branch left main, that a local coverage run measured and no test reached
# (tools/coverdiff). A pre-push aid, not the gate: each coverage-*.yml's Gate
# merges every OS's runs, so a line only another OS's tests reach is listed
# here.
#
#   cli            cli/ and dashboard/, each test binary counting both
#   libs           libs/go-common/
#   cluster-api    cluster/api/
#   cluster-agent  cluster/agent/ and libs/proto/, with cargo-llvm-cov
#   web            apps/web/, the tests vitest relates to the changed files in
#                  src/, or the whole suite when the toolchain changed
#   desktop        apps/desktop/: the sidecar as the Go areas, untagged (FULL=1
#                  adds -tags debug); the webview as web, src/ and lint/; and
#                  the host's whole suite when its inputs changed, with
#                  cargo-llvm-cov
#
# A Go area runs the changed packages' tests, and -coverpkg names them alone,
# since only their lines are judged. FULL=1 runs every test of the area, as CI
# does: without it, a dashboard/ line only the CLI's tests reach is listed.
#
# Each area is measured as its coverage-*.yml measures it; a change to one
# belongs in the other. BASE is the ref the branch left, by default
# upstream/main or else origin/main.
set -euo pipefail
cd "$(dirname "$0")/.."

area=${1:-}
case "$area" in
  cli) paths=(cli dashboard) ;;
  libs) paths=(libs/go-common) ;;
  cluster-api) paths=(cluster/api) ;;
  cluster-agent) paths=(cluster/agent libs/proto) ;;
  web) paths=(apps/web) ;;
  desktop) paths=(apps/desktop) ;;
  *)
    echo "usage: $0 cli|libs|cluster-api|cluster-agent|web|desktop" >&2
    exit 2
    ;;
esac

# shellcheck source=scripts/changed.sh
. scripts/changed.sh

out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

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
  'apps/desktop/sidecar/graph/generated.go'
  'apps/desktop/sidecar/graph/model/models_gen.go'
  'apps/desktop/sidecar/grpc/authpb/**'
  'apps/desktop/sidecar/grpc/pokepb/**'
  'apps/desktop/sidecar/scripts/**'
  'apps/desktop/src/gql/**'
  'apps/desktop/src/routeTree.ts'
  'apps/desktop/src/main.tsx'
  'apps/desktop/src/test-utils.tsx'
  'apps/desktop/src-tauri/src/main.rs'
  'apps/desktop/src-tauri/src/app_menu/macos.rs'
  'apps/desktop/src-tauri/src/dock_menu/macos.rs'
  'apps/desktop/src-tauri/src/wake/linux.rs'
  'apps/desktop/src-tauri/src/wake/windows.rs'
  'apps/desktop/src-tauri/src/updater/platform.rs'
)
measured=("${paths[@]}")
for p in "${ignored[@]}"; do measured+=(":(exclude,glob)$p"); done

# The area's changed lines: tracked changes against the merge base, then each
# untracked file whole. --no-index exits 1 when the files differ.
diff_opts=(-U0 --no-color --no-ext-diff --src-prefix=a/ --dst-prefix=b/)
{
  git diff "${diff_opts[@]}" "$merge_base" -- "${measured[@]}"
  git ls-files --others --exclude-standard -- "${measured[@]}" | while read -r f; do
    git diff "${diff_opts[@]}" --no-index /dev/null "$f" || true
  done
} >"$out/changes.diff"

if [ ! -s "$out/changes.diff" ]; then
  echo "No change in ${paths[*]}; nothing to check."
  exit 0
fi

reports=()
repo=github.com/kubetail-org/kubetail

case "$area" in
  cli | libs | cluster-api)
    if [ "${FULL:-}" = 1 ]; then
      pkgs=$(printf './%s/...\n' "${paths[@]}")
      cover=$(printf "$repo/%s/...\n" "${paths[@]}" | paste -sd, -)
    else
      pattern=$(IFS='|' && echo "${paths[*]}")
      pkgs=$(grep -E "^\./($pattern)(/|$)" <<<"$(go_pkgs)" || true)
      cover=$(paste -sd, - <<<"${pkgs//.\//$repo/}")
    fi
    if [ -n "$pkgs" ]; then
      # shellcheck disable=SC2086 # one argument per package
      go test -covermode=atomic -coverpkg="$cover" -coverprofile="$out/go.out" $pkgs
      (cd tools && go run ./coverignore -dir .. -o "$out/go-filtered.out" "$out/go.out")
      reports+=(-go ".=$out/go-filtered.out")
    fi
    ;;
  cluster-agent)
    if ! (cd cluster/agent && cargo llvm-cov --version >/dev/null 2>&1); then
      echo "cargo-llvm-cov is not installed: cargo install cargo-llvm-cov --locked" >&2
      exit 1
    fi
    (cd cluster/agent && cargo llvm-cov --lcov --output-path "$out/rust.lcov")
    (cd tools && go run ./coverignore -lcov -o "$out/rust-filtered.lcov" "$out/rust.lcov")
    reports+=(-lcov "$out/rust-filtered.lcov")
    ;;
  web)
    vitest=$(vitest_scope apps/web)
    if [ -n "$vitest" ]; then
      # shellcheck disable=SC2086 # one argument per word
      pnpm -C apps/web exec vitest $vitest --coverage
      reports+=(-lcov "$PWD/apps/web/coverage/lcov.info")
    fi
    ;;
  desktop)
    sidecar=apps/desktop/sidecar
    if [ "${FULL:-}" = 1 ]; then pkgs=./...; else pkgs=$(go_dirs "$sidecar"); fi
    if [ -n "$pkgs" ]; then
      # The login shell's tests read SSH_AUTH_SOCK, as in CI's run.
      profiles=("$out/sidecar.out")
      # shellcheck disable=SC2086 # one argument per package
      SSH_AUTH_SOCK="" go -C "$sidecar" test -covermode=atomic -coverpkg=./... -coverprofile="$out/sidecar.out" $pkgs
      if [ "${FULL:-}" = 1 ]; then
        SSH_AUTH_SOCK="" go -C "$sidecar" test -tags debug -covermode=atomic -coverpkg=./... -coverprofile="$out/sidecar-debug.out" ./...
        profiles+=("$out/sidecar-debug.out")
      fi
      (cd tools && go run ./coverignore -dir "../$sidecar" -o "$out/sidecar-filtered.out" "${profiles[@]}")
      reports+=(-go "$sidecar=$out/sidecar-filtered.out")
    fi
    vitest=$(vitest_scope apps/desktop)
    if [ -n "$vitest" ]; then
      # --mode coverage measures the source without the React compiler.
      # shellcheck disable=SC2086 # one argument per word
      pnpm -C apps/desktop exec vitest $vitest --mode coverage
      reports+=(-lcov "$PWD/apps/desktop/coverage/lcov.info")
    fi
    # The layout fixture sits in the sidecar's module, and the host's tests read it.
    if has '^apps/desktop/(src-tauri|proto)/|^apps/desktop/sidecar/internal/app/layout/testdata/'; then
      if ! (cd apps/desktop/src-tauri && cargo llvm-cov --version >/dev/null 2>&1); then
        echo "cargo-llvm-cov is not installed: cargo install cargo-llvm-cov --locked" >&2
        exit 1
      fi
      # tauri-build fails without the bundled sidecar binary.
      (cd apps/desktop && go run scripts/build-sidecar.go)
      (cd apps/desktop/src-tauri && cargo llvm-cov --lcov --output-path "$out/rust.lcov")
      (cd tools && go run ./coverignore -lcov -o "$out/rust-filtered.lcov" "$out/rust.lcov")
      reports+=(-lcov "$out/rust-filtered.lcov")
    fi
    ;;
esac

if [ ${#reports[@]} -eq 0 ]; then
  echo "No changed file in ${paths[*]} has tests to run; nothing to check."
  exit 0
fi
(cd tools && go run ./coverdiff -root .. -diff "$out/changes.diff" "${reports[@]}")
echo "Every changed line a test measured is covered."
