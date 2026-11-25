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
  changed go_pkgs cli/cmd/logs.go cli/cmd/serve_test.go cluster/api/cmd/main.go libs/go-common/util/util.go apps/web/src/app.tsx
  [ "$status" -eq 0 ]
  [ "$output" = $'./cli/cmd\n./cluster/api/cmd\n./libs/go-common/util' ]
}

@test "go_pkgs leaves out Go files outside the workspace" {
  changed go_pkgs tools/coverdiff/main.go scripts/stress.go
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
}

@test "go_pkgs leaves out a deleted package" {
  mkdir -p "$repo/cli/gone"
  echo x >"$repo/cli/gone/gone.go"
  git -C "$repo" add cli
  git -C "$repo" -c user.name=t -c user.email=t@t commit -q -m gone
  git -C "$repo" branch -q -f main
  rm -r "$repo/cli/gone"
  changed go_pkgs
  [ "$output" = "" ]
}

@test "go_pkgs adds no package on a go.mod, go.sum or go.work change" {
  changed go_pkgs cli/cmd/logs.go cli/go.mod cli/go.sum go.work
  [ "$output" = "./cli/cmd" ]
}

@test "go_pkgs prints nothing without a Go change" {
  changed go_pkgs cli/README.md
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
}

@test "vitest_scope relates the changed source files" {
  changed 'vitest_scope apps/web' apps/web/src/app.tsx apps/web/src/lib/util.ts apps/web/index.html
  [ "$output" = "related --run --passWithNoTests src/app.tsx src/lib/util.ts" ]
}

@test "vitest_scope runs the whole suite on a toolchain change" {
  changed 'vitest_scope apps/web' apps/web/src/app.tsx apps/web/pnpm-lock.yaml
  [ "$output" = "run" ]
}

@test "vitest_scope prints nothing without a source change" {
  changed 'vitest_scope apps/web' apps/web/index.html cli/main.go
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
}

@test "vitest_scope relates the changed files of the app it names" {
  changed 'vitest_scope apps/desktop' apps/desktop/src/app.tsx apps/desktop/lint/rule.js apps/web/src/b.ts
  [ "$output" = "related --run --passWithNoTests lint/rule.js src/app.tsx" ]
}

@test "go_dirs lists a module's changed packages from its directory" {
  changed 'go_dirs apps/desktop/sidecar' apps/desktop/sidecar/internal/a/a.go apps/desktop/sidecar/internal/a/a_test.go \
    apps/desktop/sidecar/main.go cli/cmd/logs.go apps/desktop/sidecar/go.mod
  [ "$output" = "$(printf '.\n./internal/a')" ]
}

@test "go_dirs leaves out a go:build ignore script" {
  mkdir -p "$repo/apps/desktop/sidecar/scripts"
  printf '// Copyright 2024 The Kubetail Authors\n\n//go:build ignore\n\npackage main\n' >"$repo/apps/desktop/sidecar/scripts/clean.go"
  changed 'go_dirs apps/desktop/sidecar'
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
}
