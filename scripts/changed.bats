#!/usr/bin/env bats

# A repo on a branch off main.
setup() {
  repo="$BATS_TEST_TMPDIR/repo"
  mkdir -p "$repo/scripts"
  cp "$(dirname "$BATS_TEST_FILENAME")/changed.sh" "$repo/scripts/"
  git -C "$repo" init -q -b main
  git -C "$repo" -c user.name=t -c user.email=t@t commit -q --allow-empty -m base
  git -C "$repo" checkout -q -b work
}

# changed <fn> <path>...: runs the function with each path changed.
changed() {
  local fn=$1
  shift
  for f in "$@"; do
    mkdir -p "$repo/$(dirname "$f")"
    echo x >"$repo/$f"
  done
  cd "$repo"
  BASE=main run bash -c ". scripts/changed.sh && $fn"
}

@test "go_pkgs lists each changed package once" {
  changed go_pkgs modules/cli/cmd/logs.go modules/cli/cmd/serve_test.go modules/shared/util/util.go dashboard-ui/src/app.tsx
  [ "$status" -eq 0 ]
  [ "$output" = $'./cli/cmd\n./shared/util' ]
}

@test "go_pkgs leaves out a deleted package" {
  mkdir -p "$repo/modules/cli/gone"
  echo x >"$repo/modules/cli/gone/gone.go"
  git -C "$repo" add modules
  git -C "$repo" -c user.name=t -c user.email=t@t commit -q -m gone
  git -C "$repo" branch -q -f main
  rm -r "$repo/modules/cli/gone"
  changed go_pkgs
  [ "$output" = "" ]
}

@test "go_pkgs adds no package on a go.mod, go.sum or go.work change" {
  changed go_pkgs modules/cli/cmd/logs.go modules/cli/go.mod modules/cli/go.sum modules/go.work
  [ "$output" = "./cli/cmd" ]
}

@test "go_pkgs prints nothing without a Go change" {
  changed go_pkgs modules/cli/README.md
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
}

@test "vitest_scope relates the changed source files" {
  changed vitest_scope dashboard-ui/src/app.tsx dashboard-ui/src/lib/util.ts dashboard-ui/index.html
  [ "$output" = "related --run --passWithNoTests src/app.tsx src/lib/util.ts" ]
}

@test "vitest_scope runs the whole suite on a toolchain change" {
  changed vitest_scope dashboard-ui/src/app.tsx dashboard-ui/pnpm-lock.yaml
  [ "$output" = "run" ]
}

@test "vitest_scope prints nothing without a source change" {
  changed vitest_scope dashboard-ui/index.html modules/cli/main.go
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
}
