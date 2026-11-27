#!/usr/bin/env bats

# A repo on a branch off main, holding the scripts the area runs, with cargo
# and pnpm stubbed to print their arguments.
setup() {
  repo="$BATS_TEST_TMPDIR/repo"
  mkdir -p "$repo/scripts"
  cp "$(dirname "$BATS_TEST_FILENAME")"/{changed,vet-changed}.sh "$repo/scripts/"
  git -C "$repo" init -q -b main
  git -C "$repo" config core.autocrlf false
  git -C "$repo" -c user.name=t -c user.email=t@t commit -q --allow-empty -m base
  git -C "$repo" checkout -q -b work
  bin="$BATS_TEST_TMPDIR/bin"
  mkdir -p "$bin"
  for tool in cargo pnpm; do
    printf '#!/bin/sh\necho "ran %s $* in ${PWD#%s/}"\n' "$tool" "$repo" >"$bin/$tool"
    chmod +x "$bin/$tool"
  done
}

# workspace [<file> <code>]...: commits to main a Go workspace of cli and
# libs/go-common, in which b imports a, then each file with its code.
workspace() {
  command -v go >/dev/null || skip "needs go"
  mkdir -p "$repo/libs/go-common/a" "$repo/libs/go-common/b"
  printf 'go 1.26\n\nuse ./libs/go-common\n' >"$repo/go.work"
  printf 'module github.com/kubetail-org/kubetail/libs/go-common\n\ngo 1.26\n' >"$repo/libs/go-common/go.mod"
  echo 'package a' >"$repo/libs/go-common/a/a.go"
  printf 'package b\n\nimport _ "github.com/kubetail-org/kubetail/libs/go-common/a"\n' >"$repo/libs/go-common/b/b.go"
  while [ $# -gt 0 ]; do
    printf '%s' "$2" >"$repo/$1"
    shift 2
  done
  commit_to_main
}

# sidecar: commits to main the desktop's sidecar, a module outside go.work,
# whose root package imports internal/p. scripts/build-sidecar.go prints that
# it ran.
sidecar() {
  command -v go >/dev/null || skip "needs go"
  local dir="$repo/apps/desktop/sidecar"
  mkdir -p "$dir/internal/p" "$repo/apps/desktop/scripts" "$repo/apps/desktop/src-tauri"
  printf 'module github.com/kubetail-org/kubetail/apps/desktop/sidecar\n\ngo 1.26\n' >"$dir/go.mod"
  printf 'go 1.26\n\nuse .\n' >"$dir/go.work"
  printf 'package sidecar\n\nimport _ "github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/p"\n' >"$dir/sidecar.go"
  echo 'package p' >"$dir/internal/p/p.go"
  printf '//go:build ignore\n\npackage main\n\nfunc main() { println("built the sidecar") }\n' >"$repo/apps/desktop/scripts/build-sidecar.go"
  commit_to_main
}

commit_to_main() {
  git -C "$repo" add -A
  git -C "$repo" -c user.name=t -c user.email=t@t commit -q -m fixture
  git -C "$repo" branch -q -f main
}

# vet_changed <area> [<path>...]: runs the area with a comment appended to
# each path, which keeps a Go file compiling.
vet_changed() {
  local area=$1
  shift
  for f in "$@"; do
    mkdir -p "$repo/$(dirname "$f")"
    echo '// x' >>"$repo/$f"
  done
  cd "$repo"
  BASE=main PATH="$bin:$PATH" run bash scripts/vet-changed.sh "$area"
}

@test "a vet error in a package that imports the changed one fails" {
  workspace libs/go-common/b/printf.go $'package b\n\nimport "fmt"\n\nfunc P() { fmt.Printf("%d\\n", "s") }\n'
  vet_changed libs libs/go-common/a/a.go
  [ "$status" -ne 0 ]
  [[ "$output" == *"→ go vet ./libs/go-common/a ./libs/go-common/b"$'\n'* ]]
  [[ "$output" == *"printf.go"*"%d"* ]]
}

@test "a go fix diff in a changed package fails" {
  workspace libs/go-common/a/any.go $'package a\n\nvar X interface{}\n'
  vet_changed libs libs/go-common/a/a.go
  [ "$status" -ne 0 ]
  [[ "$output" == *"→ go fix -plusbuild=false -stringsbuilder=false -diff ./libs/go-common/a ./libs/go-common/b"$'\n'* ]]
  [[ "$output" == *"+var X any"* ]]
}

@test "a clean change passes and prints each command" {
  workspace
  vet_changed libs libs/go-common/b/b.go
  [ "$status" -eq 0 ]
  [[ "$output" == *"→ go vet ./libs/go-common/b"$'\n'"→ go fix -plusbuild=false -stringsbuilder=false -diff ./libs/go-common/b"* ]]
}

@test "a Markdown change vets nothing" {
  workspace
  vet_changed libs libs/go-common/a/README.md
  [ "$status" -eq 0 ]
  [[ "$output" != *"→"* ]]
  [[ "$output" == *"Skipped go vet and go fix: no change reaches a package in libs/go-common."* ]]
}

@test "web type-checks on a TypeScript or tsconfig change" {
  for f in apps/web/src/app.tsx apps/web/tsconfig.app.json; do
    rm -rf "$repo/apps"
    vet_changed web "$f"
    [ "$status" -eq 0 ]
    [[ "$output" == *"→ pnpm -C apps/web exec tsc -b"$'\n'"ran pnpm -C apps/web exec tsc -b"* ]]
  done
}

@test "web type-checks nothing on a change other than TypeScript" {
  vet_changed web apps/web/index.html apps/web/src/index.css
  [ "$status" -eq 0 ]
  [[ "$output" != *"→"* ]]
  [[ "$output" == *"Skipped tsc: no change to apps/web/'s TypeScript, tsconfigs or package.json."* ]]
}

@test "cluster-agent runs clippy on a change to the crate" {
  vet_changed cluster-agent cluster/agent/rgkl/src/lib.rs
  [ "$status" -eq 0 ]
  [[ "$output" == *"→ cd cluster/agent && cargo clippy --all --all-targets -- -D warnings"$'\n'"ran cargo clippy --all --all-targets -- -D warnings in cluster/agent"* ]]
}

@test "cluster-agent runs no clippy on a change elsewhere" {
  vet_changed cluster-agent cluster/agent/README.md apps/desktop/src-tauri/src/lib.rs
  [ "$status" -eq 0 ]
  [[ "$output" != *"→"* ]]
  [[ "$output" == *"Skipped clippy: no change in cluster/agent/ or libs/proto/."* ]]
}

@test "desktop builds the sidecar and runs clippy, and no go vet, on a host change" {
  sidecar
  vet_changed desktop apps/desktop/src-tauri/src/lib.rs
  [ "$status" -eq 0 ]
  [[ "$output" == *"→ go -C apps/desktop run scripts/build-sidecar.go"$'\n'"built the sidecar"$'\n'* ]]
  [[ "$output" == *"→ cd apps/desktop/src-tauri && cargo clippy --all-targets -- -D warnings"$'\n'"ran cargo clippy --all-targets -- -D warnings in apps/desktop/src-tauri"* ]]
  [[ "$output" != *"→ go -C apps/desktop/sidecar vet"* ]]
}

@test "desktop vets a changed sidecar package and its importers once" {
  sidecar
  vet_changed desktop apps/desktop/sidecar/internal/p/p.go
  [ "$status" -eq 0 ]
  [[ "$output" == *"→ go -C apps/desktop/sidecar vet . ./internal/p"$'\n'* ]]
  [[ "$output" == *"→ go -C apps/desktop/sidecar fix -plusbuild=false -stringsbuilder=false -diff . ./internal/p"$'\n'* ]]
}

@test "a Go area names the passes it leaves to CI" {
  workspace
  vet_changed libs libs/go-common/b/b.go
  [[ "$output" == *"Skipped staticcheck and the other platforms: CI runs them, as make vet-libs does."* ]]
}

@test "desktop names the passes it leaves to CI" {
  sidecar
  vet_changed desktop apps/desktop/sidecar/internal/p/p.go
  [[ "$output" == *"Skipped the sidecar's debug, livecache and liveswitch tags, staticcheck, check-sidecar-deps.sh and the other platforms: CI runs them, as make vet-desktop does."* ]]
}
