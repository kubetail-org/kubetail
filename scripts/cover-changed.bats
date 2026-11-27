#!/usr/bin/env bats

# Each case adds an uncovered function to one area and checks that the area's
# measurement lists it. The cases run the real suites, so they pin how each
# area maps a report's paths back to its files. A case skips when its
# toolchain is missing.

# A worktree of HEAD with this checkout's uncommitted changes committed on
# top, so the cases check the scripts and configs as they stand.
setup_file() {
  src=$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)
  repo="$BATS_FILE_TMPDIR/repo"
  export src repo
  git -C "$src" worktree add -q --detach "$repo" HEAD
  git -C "$src" diff HEAD --binary >"$BATS_FILE_TMPDIR/changes.patch"
  if [ -s "$BATS_FILE_TMPDIR/changes.patch" ]; then
    git -C "$repo" apply "$BATS_FILE_TMPDIR/changes.patch"
  fi
  (cd "$src" && git ls-files --others --exclude-standard -z | tar -cf - --null -T -) | tar -xf - -C "$repo"
  git -C "$repo" add -A
  git -C "$repo" -c user.name=t -c user.email=t@t commit -q --no-verify --allow-empty -m base
  for app in web desktop; do
    if [ -d "$src/apps/$app/node_modules" ]; then
      ln -s "$src/apps/$app/node_modules" "$repo/apps/$app/node_modules"
    fi
  done
  # Reuses this checkout's build of the dependencies.
  export CARGO_TARGET_DIR="${CARGO_TARGET_DIR:-$src/cluster/agent/target}"
}

teardown_file() {
  git -C "$src" worktree remove --force "$repo"
}

setup() {
  if [[ "$OSTYPE" == msys* || "$OSTYPE" == cygwin* ]]; then
    skip "cover-changed.sh runs on Linux and macOS"
  fi
  command -v go >/dev/null || skip "needs go"
}

teardown() {
  git -C "$repo" reset -q --hard
}

# uncovered <area> <file> <code>: appends the code, whose third line is
# uncovered, to the file, then checks that the area lists that line.
uncovered() {
  local area=$1 file=$2 code=$3
  local line=$(($(wc -l <"$repo/$file") + 3))
  printf '%s' "$code" >>"$repo/$file"
  BASE=HEAD run bash "$repo/scripts/cover-changed.sh" "$area"
  [ "$status" -eq 1 ]
  [[ "$output" == *"$file:$line "* ]]
}

go_code=$'\nfunc batsUncovered() int {\n\treturn 1\n}\n'

@test "cli lists an uncovered line in dashboard/" {
  uncovered cli dashboard/pkg/config/config.go "$go_code"
}

@test "libs lists an uncovered line in libs/go-common/" {
  uncovered libs libs/go-common/util/syncmap.go "$go_code"
}

@test "cluster-api lists an uncovered line in cluster/api/" {
  uncovered cluster-api cluster/api/pkg/config/config.go "$go_code"
}

@test "cluster-agent lists an uncovered line in cluster/agent/" {
  (cd "$repo/cluster/agent" && cargo llvm-cov --version >/dev/null 2>&1) || skip "needs cargo-llvm-cov"
  command -v protoc >/dev/null || skip "needs protoc"
  uncovered cluster-agent cluster/agent/rgkl/src/lib.rs $'\npub fn bats_uncovered() -> i32 {\n    1\n}\n'
}

@test "web lists an uncovered line in apps/web/" {
  command -v pnpm >/dev/null || skip "needs pnpm"
  [ -d "$repo/apps/web/node_modules" ] || skip "needs pnpm install in apps/web"
  uncovered web apps/web/src/lib/timestamp-format.ts $'\nexport function batsUncovered() {\n  return 1;\n}\n'
}

@test "desktop lists an uncovered line in apps/desktop/sidecar/" {
  uncovered desktop apps/desktop/sidecar/internal/lib/realpath/realpath.go "$go_code"
}

@test "desktop lists an uncovered line in apps/desktop/src/" {
  command -v pnpm >/dev/null || skip "needs pnpm"
  [ -d "$repo/apps/desktop/node_modules" ] || skip "needs pnpm install in apps/desktop"
  uncovered desktop apps/desktop/src/lib/held-still.ts $'\nexport function batsUncovered() {\n  return 1;\n}\n'
}

# go test writes a profile with no blocks for a package with no test file and
# no statements.
@test "a changed Go package with no test file passes" {
  printf '\nconst batsUnmeasured = 1\n' >>"$repo/libs/go-common/config/config.go"
  BASE=HEAD run bash "$repo/scripts/cover-changed.sh" libs
  [ "$status" -eq 0 ]
  [[ "$output" == *"libs/go-common/config"*"[no test files]"* ]]
  [[ "$output" == *"Every changed line a test measured is covered." ]]
}

@test "an area leaves out another area's lines" {
  printf '%s' "$go_code" >>"$repo/cluster/api/pkg/config/config.go"
  BASE=HEAD run bash "$repo/scripts/cover-changed.sh" libs
  [ "$status" -eq 0 ]
  [[ "$output" == "No change in libs/go-common; nothing to check." ]]
}

@test "an unknown area fails" {
  run bash "$repo/scripts/cover-changed.sh" go
  [ "$status" -eq 2 ]
  [[ "$output" == usage:* ]]
}

@test "FULL=1 notes the run to make before a push" {
  FULL=1 BASE=HEAD run bash "$repo/scripts/cover-changed.sh" libs
  [ "$status" -eq 0 ]
  [[ "$output" == "note: CI measures the whole area; before a push, run make cover-libs-changed without FULL=1"* ]]
}
