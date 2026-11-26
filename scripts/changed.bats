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

# workspace: commits to main a Go workspace of cli and libs/go-common. In libs,
# b imports a and c imports b; t's test and x's external test import a; lone
# stands alone. cli/cmd imports b.
workspace() {
  command -v go >/dev/null || skip "needs go"
  printf 'go 1.26\n\nuse (\n\t./cli\n\t./libs/go-common\n)\n' >"$repo/go.work"
  gomod libs/go-common
  gomod cli
  gofile libs/go-common/a/a.go a
  gofile libs/go-common/b/b.go b libs/go-common/a
  gofile libs/go-common/c/c.go c libs/go-common/b
  gofile libs/go-common/t/t.go t
  gofile libs/go-common/t/t_test.go t libs/go-common/a
  gofile libs/go-common/x/x.go x
  gofile libs/go-common/x/x_test.go x_test libs/go-common/a
  gofile libs/go-common/lone/lone.go lone
  gofile cli/cmd/cmd.go cmd libs/go-common/b
  commit_to_main
}

gomod() {
  mkdir -p "$repo/$1"
  echo "module github.com/kubetail-org/kubetail/$1" >"$repo/$1/go.mod"
}

# gofile <path> <package> [<dir>...]: a Go file importing the package in each dir.
gofile() {
  local f=$1 pkg=$2
  shift 2
  mkdir -p "$repo/$(dirname "$f")"
  {
    echo "package $pkg"
    for d in "$@"; do echo "import _ \"github.com/kubetail-org/kubetail/$d\""; done
  } >"$repo/$f"
}

commit_to_main() {
  git -C "$repo" add -A
  git -C "$repo" -c user.name=t -c user.email=t@t commit -q -m fixture
  git -C "$repo" branch -q -f main
}

# scope <module>... -- <path>...: runs go_scope over the modules with a comment
# appended to each path, which keeps a Go file compiling.
scope() {
  local mods=()
  while [ "$1" != -- ]; do
    mods+=("$1")
    shift
  done
  shift
  for f in "$@"; do
    mkdir -p "$repo/$(dirname "$f")"
    echo '// x' >>"$repo/$f"
  done
  cd "$repo"
  BASE=main run bash -c ". scripts/changed.sh && go_scope ${mods[*]}"
}

@test "go_scope names a changed package nothing imports" {
  workspace
  scope libs/go-common -- libs/go-common/lone/lone.go
  [ "$status" -eq 0 ]
  [ "$output" = "./libs/go-common/lone" ]
}

@test "go_scope names a changed package and the packages that import it" {
  workspace
  scope libs/go-common -- libs/go-common/b/b.go
  [ "$output" = $'./libs/go-common/b\n./libs/go-common/c' ]
}

@test "go_scope names a package whose test alone imports the changed one" {
  workspace
  scope libs/go-common -- libs/go-common/a/a.go
  [[ "$output" == *$'./libs/go-common/t\n'* ]]
  [[ "$output" == *"./libs/go-common/x" ]]
}

@test "go_scope names a package that imports the changed one through another" {
  workspace
  scope libs/go-common -- libs/go-common/a/a.go
  [ "$output" = "$(printf './libs/go-common/%s\n' a b c t x)" ]
}

@test "go_scope names the asked modules' importers of a change in another module" {
  workspace
  scope cli -- libs/go-common/b/b.go
  [ "$output" = "./cli/cmd" ]
}

@test "go_scope leaves out the packages of a module it was not asked for" {
  workspace
  scope libs/go-common -- cli/cmd/cmd.go
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
}

@test "go_scope names a module whole on a go.mod or go.sum change" {
  workspace
  scope cli libs/go-common -- cli/go.mod libs/go-common/go.sum libs/go-common/a/a.go
  [ "$output" = $'./cli/...\n./libs/go-common/...' ]
}

@test "go_scope names the package above a changed file other than Go" {
  workspace
  scope libs/go-common -- libs/go-common/lone/testdata/in/case.json libs/go-common/lone/prompt.txt
  [ "$output" = "./libs/go-common/lone" ]
}

@test "go_scope names nothing on a Markdown change" {
  workspace
  scope libs/go-common -- libs/go-common/lone/README.md libs/go-common/CLAUDE.md
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
}

@test "go_scope leaves out a deleted package" {
  workspace
  rm -r "$repo/libs/go-common/lone"
  scope libs/go-common --
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
}

@test "go_scope names the packages of a module outside go.work" {
  workspace
  gomod apps/desktop/sidecar
  printf 'go 1.26\n\nuse .\n' >"$repo/apps/desktop/sidecar/go.work"
  gofile apps/desktop/sidecar/main.go main apps/desktop/sidecar/internal/p
  gofile apps/desktop/sidecar/internal/p/p.go p
  gofile apps/desktop/sidecar/internal/q/q.go q
  commit_to_main
  scope apps/desktop/sidecar -- apps/desktop/sidecar/internal/p/p.go libs/go-common/a/a.go
  [ "$output" = $'./apps/desktop/sidecar\n./apps/desktop/sidecar/internal/p' ]
}

# scripts <file>...: commits each file under scripts/ to main, with the line
# that sources a helper when the name is <file>=<helper>.
scripts() {
  local f
  for f in "$@"; do
    if [[ "$f" == *=* ]]; then
      printf '#!/bin/sh\n%s\n' "${f#*=}" >"$repo/scripts/${f%%=*}"
    else
      echo "# $f" >"$repo/scripts/$f"
    fi
  done
  commit_to_main
}

@test "bats_scope names the bats file of a changed script" {
  scripts check-release-tag.sh check-release-tag.bats install.sh install.bats
  changed bats_scope scripts/check-release-tag.sh
  [ "$output" = "scripts/check-release-tag.bats" ]
}

@test "bats_scope names a changed bats file" {
  scripts install.sh install.bats
  changed bats_scope scripts/install.bats
  [ "$output" = "scripts/install.bats" ]
}

@test "bats_scope names the bats files of the scripts that source a changed helper" {
  scripts changed.bats cover-changed.sh='. scripts/changed.sh' cover-changed.bats \
    check-go-fix.sh='. "$root/scripts/go-builds.sh"' check-go-fix.bats go-builds.sh install.sh install.bats
  echo '# x' >>"$repo/scripts/changed.sh"
  changed bats_scope scripts/go-builds.sh
  [ "$output" = $'scripts/changed.bats\nscripts/check-go-fix.bats\nscripts/cover-changed.bats' ]
}

@test "bats_scope names nothing for a changed script without a bats file" {
  scripts check-go-fix.sh='. "$root/scripts/go-builds.sh"' go-builds.sh
  changed bats_scope scripts/go-builds.sh scripts/check-go-fix.sh
  [ "$status" -eq 0 ]
  [ "$output" = "" ]
}
