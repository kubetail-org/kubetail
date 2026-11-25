#!/usr/bin/env bash
# List the lines the working tree changes since the branch left main that a
# local coverage run measured and no test reached (tools/coverdiff). A pre-push
# aid, not the gate: CI's coverage gate merges every OS's runs, so a line only
# another OS's tests reach is listed here.
#
#   Go    sidecar/, when it changed: the tests of the
#         packages with a changed Go file, or every test with FULL=1.
#         The sidecar's -tags debug run takes only the packages with a changed
#         file only the tag builds, or every changed package with
#         ALL_PLATFORMS=1.
#   JS    the tests vitest relates to the changed files in src/ and lint/, or
#         the whole suite when the toolchain changed. A changed file no test
#         imports is listed whole.
#   Rust  the whole suite, when its inputs changed (rust_changed). Uses the
#         cargo-llvm-cov on PATH when it is the pinned version; otherwise
#         downloads the release's prebuilt binary into the user cache, checked
#         against its SHA-256.
#
# The Go and JS runs go side by side; Rust follows them.
#
# Each suite is measured as .github/workflows/coverage.yml's jobs measure
# it; a change to one belongs in the other. BASE is as in scripts/changed.sh.
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

# The changed lines: tracked changes against the merge base, then each
# untracked file whole. --no-index exits 1 when the files differ.
diff_opts=(-U0 --no-color --no-ext-diff --src-prefix=a/ --dst-prefix=b/)
{
  git diff "${diff_opts[@]}" "$merge_base"
  while read -r f; do
    [ -n "$f" ] && { git diff "${diff_opts[@]}" --no-index /dev/null "$f" || true; }
  done <<<"$untracked"
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

# The sidecar as CI measures it, with scripts/changed.sh's CGO_ENABLED=0 and
# an empty SSH_AUTH_SOCK, once untagged and once under -tags debug.
cover=(-covermode=atomic -coverpkg=./...)
go_pkgs=
if has '^sidecar/'; then
  if [ "${FULL:-}" = 1 ]; then go_pkgs=./...; else go_pkgs=$(go_dirs sidecar); fi
fi
# shellcheck disable=SC2086 # one argument per package
if [ -n "$go_pkgs" ]; then
  run sidecar-untagged env SSH_AUTH_SOCK="" \
    go -C sidecar test -timeout 30m "${cover[@]}" -coverprofile="$out/sidecar-untagged.out" $go_pkgs
  if [ "$go_pkgs" != ./... ] && [ "${ALL_PLATFORMS:-}" != 1 ]; then
    # The packages with a changed file only the tag builds.
    go_pkgs=$(go_changed_only "$(go_files sidecar "" $go_pkgs)" "$(go_files sidecar GOFLAGS=-tags=debug $go_pkgs)")
  fi
  if [ -n "$go_pkgs" ]; then
    run sidecar-debug env SSH_AUTH_SOCK="" \
      go -C sidecar test -tags debug -timeout 30m "${cover[@]}" -coverprofile="$out/sidecar-debug.out" $go_pkgs
  fi
fi

vitest=$(vitest_scope)
if [ -n "$vitest" ]; then
  # lcov alone: the per-file table would bury the lines listed below.
  # shellcheck disable=SC2086 # one argument per word
  run vitest pnpm vitest $vitest --mode coverage --coverage.reporter=lcovonly
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

profiles=("$out"/sidecar-*.out)
if [ -e "${profiles[0]}" ]; then
  (cd tools && go run ./coverignore -dir ../sidecar -o "$out/sidecar.out" "${profiles[@]}")
  reports+=(-go "sidecar=$out/sidecar.out")
fi
if [ -n "$vitest" ]; then
  reports+=(-lcov "$PWD/coverage/lcov.info")
fi

# Keep in step with cargo-llvm-cov's version in .github/workflows/coverage.yml.
llvm_cov_version=0.9.1

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  else
    shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

# Puts a verified cargo-llvm-cov $llvm_cov_version first on PATH, downloading
# it on first use.
fetch_llvm_cov() {
  local target digest
  case "$(uname -s)/$(uname -m)" in
    Darwin/arm64)
      target=aarch64-apple-darwin
      digest=7e821a6c90c0884f79f759f5d9be50799ade67bb82515186d922f26032ef43b8
      ;;
    Darwin/x86_64)
      target=x86_64-apple-darwin
      digest=ddf949b82a63017441ea76aa068bdd7e2dde8055f47cd3ef736fdbbd633bc33e
      ;;
    Linux/x86_64)
      target=x86_64-unknown-linux-musl
      digest=3fca950394a3c49457657c158b1619cec8dfd2647ae5b48746734c0ab969a522
      ;;
    Linux/aarch64 | Linux/arm64)
      target=aarch64-unknown-linux-musl
      digest=49c0085628392a8d727c4464d91ec417c613c5015f5197cd16d5c05aa3fb5ad1
      ;;
    MINGW*/x86_64 | MSYS*/x86_64)
      target=x86_64-pc-windows-msvc
      digest=15eefd5b90fcdd4006b727c6e710d70a61aff509fcd7bb37f9c2a35c4f024948
      ;;
    MINGW*/aarch64 | MSYS*/aarch64)
      target=aarch64-pc-windows-msvc
      digest=a827e57b93feab3d81ceb24c0602a1b778d6be72231ca83adaec909c1af0f7ef
      ;;
    *)
      echo "cargo-llvm-cov $llvm_cov_version: no prebuilt binary for $(uname -s)/$(uname -m); install it yourself" >&2
      exit 1
      ;;
  esac

  local dir=${XDG_CACHE_HOME:-$HOME/.cache}/kubetail/cargo-llvm-cov-$llvm_cov_version
  local tarball=$dir/cargo-llvm-cov-$target.tar.gz
  mkdir -p "$dir"

  if [ ! -f "$tarball" ]; then
    echo "→ downloading cargo-llvm-cov $llvm_cov_version ($target)" >&2
    curl -fsSL --retry 3 --retry-all-errors -o "$tarball.part" \
      "https://github.com/taiki-e/cargo-llvm-cov/releases/download/v$llvm_cov_version/cargo-llvm-cov-$target.tar.gz"
    mv "$tarball.part" "$tarball"
  fi

  # Checked on every run, so a corrupted cache fails rather than runs.
  local actual
  actual=$(sha256 "$tarball")
  if [ "$actual" != "$digest" ]; then
    echo "cargo-llvm-cov $llvm_cov_version: $tarball has SHA-256 $actual; expected $digest" >&2
    echo "delete $dir and run again" >&2
    exit 1
  fi

  # The tarball holds the binary alone; cargo finds it as a subcommand on PATH.
  local bin=$dir/bin
  if [ ! -x "$bin/cargo-llvm-cov" ] && [ ! -x "$bin/cargo-llvm-cov.exe" ]; then
    mkdir -p "$bin"
    tar -xzf "$tarball" -C "$bin"
  fi
  PATH=$bin:$PATH
}

if rust_changed; then
  if [ "$(cd src-tauri && cargo llvm-cov --version 2>/dev/null || true)" != "cargo-llvm-cov $llvm_cov_version" ]; then
    fetch_llvm_cov
  fi
  # tauri-build fails without the bundled sidecar binary.
  make build-sidecar
  (cd src-tauri && cargo llvm-cov --lcov --output-path "$out/rust.lcov")
  (cd tools && go run ./coverignore -lcov -o "$out/rust-filtered.lcov" "$out/rust.lcov")
  reports+=(-lcov "$out/rust-filtered.lcov")
fi

if [ ${#reports[@]} -eq 0 ]; then
  echo "No measured suite changed; nothing to check."
  exit 0
fi
(cd tools && go run ./coverdiff -root .. -diff "$out/changes.diff" "${reports[@]}")
echo "Every changed line a test measured is covered."
