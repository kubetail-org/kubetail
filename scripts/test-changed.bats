#!/usr/bin/env bats

# A repo on a branch off main, holding the scripts the area runs, with bats,
# cargo and pnpm stubbed to print their arguments.
setup() {
  repo="$BATS_TEST_TMPDIR/repo"
  mkdir -p "$repo/scripts"
  cp "$(dirname "$BATS_TEST_FILENAME")"/{changed,test-changed}.sh "$repo/scripts/"
  git -C "$repo" init -q -b main
  git -C "$repo" config core.autocrlf false
  git -C "$repo" -c user.name=t -c user.email=t@t commit -q --allow-empty -m base
  git -C "$repo" checkout -q -b work
  bin="$BATS_TEST_TMPDIR/bin"
  mkdir -p "$bin"
  for tool in bats cargo pnpm; do
    printf '#!/bin/sh\necho "ran %s $* in ${PWD#%s/}"\n' "$tool" "$repo" >"$bin/$tool"
    chmod +x "$bin/$tool"
  done
}

# workspace: commits to main a Go workspace of cli and libs/go-common, each
# package with a passing test. In libs, b imports a; cli/cmd imports b.
workspace() {
  command -v go >/dev/null || skip "needs go"
  mkdir -p "$repo/libs/go-common" "$repo/cli"
  printf 'go 1.26\n\nuse (\n\t./cli\n\t./libs/go-common\n)\n' >"$repo/go.work"
  echo 'module github.com/kubetail-org/kubetail/libs/go-common' >"$repo/libs/go-common/go.mod"
  echo 'module github.com/kubetail-org/kubetail/cli' >"$repo/cli/go.mod"
  gopkg libs/go-common/a
  gopkg libs/go-common/b libs/go-common/a
  gopkg cli/cmd libs/go-common/b
  commit_to_main
}

# sidecar: commits to main the desktop's sidecar, a module outside go.work. Its
# root package imports internal/p; internal/q stands alone.
# scripts/build-sidecar.go prints that it ran.
sidecar() {
  command -v go >/dev/null || skip "needs go"
  mkdir -p "$repo/apps/desktop/sidecar"
  echo 'module github.com/kubetail-org/kubetail/apps/desktop/sidecar' >"$repo/apps/desktop/sidecar/go.mod"
  printf 'go 1.26\n\nuse .\n' >"$repo/apps/desktop/sidecar/go.work"
  gopkg apps/desktop/sidecar apps/desktop/sidecar/internal/p
  gopkg apps/desktop/sidecar/internal/p
  gopkg apps/desktop/sidecar/internal/q
  mkdir -p "$repo/apps/desktop/scripts" "$repo/apps/desktop/src-tauri"
  printf '//go:build ignore\n\npackage main\n\nfunc main() { println("built the sidecar") }\n' >"$repo/apps/desktop/scripts/build-sidecar.go"
  commit_to_main
}

# gopkg <dir> [<dir>...]: a package importing the package in each further dir,
# with a passing test.
gopkg() {
  local dir=$1 name
  name=$(basename "$1")
  shift
  mkdir -p "$repo/$dir"
  {
    echo "package $name"
    for d in "$@"; do echo "import _ \"github.com/kubetail-org/kubetail/$d\""; done
  } >"$repo/$dir/$name.go"
  printf 'package %s\n\nimport "testing"\n\nfunc TestOK(t *testing.T) {}\n' "$name" >"$repo/$dir/${name}_test.go"
}

commit_to_main() {
  git -C "$repo" add -A
  git -C "$repo" -c user.name=t -c user.email=t@t commit -q -m fixture
  git -C "$repo" branch -q -f main
}

# test_changed <area> [<path>...]: runs the area with a comment appended to
# each path, which keeps a Go file compiling.
test_changed() {
  local area=$1
  shift
  for f in "$@"; do
    mkdir -p "$repo/$(dirname "$f")"
    echo '// x' >>"$repo/$f"
  done
  cd "$repo"
  BASE=main PATH="$bin:$PATH" run bash scripts/test-changed.sh "$area"
}

@test "an unknown area fails" {
  test_changed go
  [ "$status" -eq 2 ]
  [[ "$output" == usage:* ]]
}

@test "a change no Go package of the area holds runs nothing" {
  workspace
  test_changed libs cli/cmd/cmd.go apps/web/src/app.tsx
  [ "$status" -eq 0 ]
  [[ "$output" != *"→"* ]]
  [[ "$output" == *"Skipped go test: no change reaches a package in libs/go-common."* ]]
}

@test "a Go area runs a changed leaf package alone, with the race detector" {
  workspace
  test_changed cli cli/cmd/cmd.go
  [ "$status" -eq 0 ]
  [[ "$output" == *"→ env GORACE=atexit_sleep_ms=0 go test -race ./cli/cmd"$'\n'* ]]
  [[ "$output" == *"ok  "*"/cli/cmd"* ]]
}

@test "a Go area runs the packages that import a changed one" {
  workspace
  test_changed libs libs/go-common/a/a.go
  [ "$status" -eq 0 ]
  [[ "$output" == *"go test -race ./libs/go-common/a ./libs/go-common/b"$'\n'* ]]
}

@test "a Go area fails on a failing test" {
  workspace
  printf 'package b\n\nimport "testing"\n\nfunc TestFails(t *testing.T) { t.Fatal("no") }\n' >"$repo/libs/go-common/b/fail_test.go"
  test_changed libs libs/go-common/a/a.go
  [ "$status" -ne 0 ]
  [[ "$output" == *"FAIL"*"/libs/go-common/b"* ]]
}

@test "a Markdown change runs nothing" {
  workspace
  test_changed libs libs/go-common/a/README.md libs/go-common/CLAUDE.md
  [ "$status" -eq 0 ]
  [[ "$output" != *"→"* ]]
}

@test "web runs vitest on the tests related to the changed files" {
  test_changed web apps/web/src/app.tsx apps/web/README.md
  [ "$status" -eq 0 ]
  [[ "$output" == *"→ pnpm -C apps/web exec vitest related --run --passWithNoTests src/app.tsx"$'\n'* ]]
  [[ "$output" == *"ran pnpm -C apps/web exec vitest related --run --passWithNoTests src/app.tsx in "* ]]
}

@test "web runs nothing without a source change" {
  test_changed web apps/web/README.md
  [ "$status" -eq 0 ]
  [[ "$output" != *"→"* ]]
  [[ "$output" == *"Skipped vitest: no change in apps/web/src/, apps/web/lint/ or its toolchain."* ]]
}

@test "cluster-agent runs cargo test on a change to the crate" {
  test_changed cluster-agent cluster/agent/rgkl/src/lib.rs
  [ "$status" -eq 0 ]
  [[ "$output" == *"→ cd cluster/agent && cargo test"$'\n'"ran cargo test in cluster/agent"* ]]
}

@test "cluster-agent runs cargo test on a change to the proto" {
  mkdir -p "$repo/cluster/agent"
  test_changed cluster-agent libs/proto/cluster_agent.proto
  [ "$status" -eq 0 ]
  [[ "$output" == *"ran cargo test in cluster/agent"* ]]
}

@test "cluster-agent runs nothing on a change elsewhere or to Markdown" {
  test_changed cluster-agent cluster/agent/README.md cli/main.go
  [ "$status" -eq 0 ]
  [[ "$output" != *"→"* ]]
  [[ "$output" == *"Skipped cargo test: no change in cluster/agent/ or libs/proto/."* ]]
}

@test "desktop runs a changed sidecar package and its importers, as test-desktop does" {
  sidecar
  test_changed desktop apps/desktop/sidecar/internal/p/p.go
  [ "$status" -eq 0 ]
  [[ "$output" == *"→ env GORACE=atexit_sleep_ms=0 SSH_AUTH_SOCK= go -C apps/desktop/sidecar test -race -short -gcflags=modernc.org/...=-d=checkptr=0 -gcflags=modernc.org/sqlite/lib=-race=false . ./internal/p"$'\n'* ]]
  [[ "$output" == *"ok  "*"/internal/p"* ]]
}

@test "desktop builds the sidecar and runs cargo test, and no Go test, on a host change" {
  sidecar
  test_changed desktop apps/desktop/src-tauri/src/lib.rs
  [ "$status" -eq 0 ]
  [[ "$output" == *"→ go -C apps/desktop run scripts/build-sidecar.go"$'\n'"built the sidecar"$'\n'* ]]
  [[ "$output" == *"→ cd apps/desktop/src-tauri && cargo test"$'\n'"ran cargo test in apps/desktop/src-tauri"* ]]
  [[ "$output" != *"go -C apps/desktop/sidecar test"* ]]
}

@test "desktop runs vitest on the webview's change and says which suites it skipped" {
  sidecar
  test_changed desktop apps/desktop/src/app.tsx
  [ "$status" -eq 0 ]
  [[ "$output" == *"ran pnpm -C apps/desktop exec vitest related --run --passWithNoTests src/app.tsx"* ]]
  [[ "$output" == *"Skipped the sidecar's go test: no change reaches a package in apps/desktop/sidecar."* ]]
  [[ "$output" == *"Skipped the host's cargo test: no change in apps/desktop/src-tauri/, apps/desktop/proto/ or the layout fixture."* ]]
}

@test "repo runs the bats file of a changed script" {
  touch "$repo/scripts/"{check-release-tag.sh,check-release-tag.bats,install.sh,install.bats}
  commit_to_main
  test_changed repo scripts/check-release-tag.sh
  [ "$status" -eq 0 ]
  [[ "$output" == "→ bats scripts/check-release-tag.bats"$'\n'"ran bats scripts/check-release-tag.bats in "* ]]
}

@test "repo says when no changed script has a bats file" {
  touch "$repo/scripts/check-go-fix.sh"
  commit_to_main
  test_changed repo scripts/check-go-fix.sh
  [ "$status" -eq 0 ]
  [[ "$output" != *"→"* ]]
  [[ "$output" == *"Skipped bats: no changed script has a bats file."* ]]
}

@test "repo runs the stress tool's test on a change to it" {
  command -v go >/dev/null || skip "needs go"
  printf 'package main\n\nfunc main() {}\n' >"$repo/scripts/stress.go"
  printf 'package main\n\nimport "testing"\n\nfunc TestOK(t *testing.T) {}\n' >"$repo/scripts/stress_test.go"
  commit_to_main
  test_changed repo scripts/stress.go
  [ "$status" -eq 0 ]
  [[ "$output" == *"→ go test scripts/stress.go scripts/stress_test.go"$'\n'"ok  "* ]]
}
