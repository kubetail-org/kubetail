#!/usr/bin/env bats

# go stubbed to report darwin as the host's GOOS.
setup() {
  real_go=$(command -v go || true)
  bin="$BATS_TEST_TMPDIR/bin"
  mkdir -p "$bin"
  printf '#!/bin/sh\necho darwin\n' >"$bin/go"
  chmod +x "$bin/go"
  PATH="$bin:$PATH"
  . "$(dirname "$BATS_TEST_FILENAME")/go-builds.sh"
}

# gofile <path> <package> [build constraint] writes a Go file to the fixture
# module.
gofile() {
  mkdir -p "$(dirname "$mod/$1")"
  {
    [ -z "${3:-}" ] || printf '//go:build %s\n\n' "$3"
    printf 'package %s\n' "$2"
  } >"$mod/$1"
}

@test "go_builds lists every platform by default" {
  run go_builds cli
  [ "$output" = $'linux - - ./...\nwindows - - ./...' ]
}

@test "go_builds with GO_BUILDS=host lists a workspace module once for this platform" {
  GO_BUILDS=host run go_builds cli
  [ "$output" = "darwin - - ./..." ]
}

@test "go_builds with GO_BUILDS=host keeps each set of the sidecar's tags" {
  tagged_packages() { printf '%s\n' example.com/a example.com/b; }
  GO_BUILDS=host run go_builds apps/desktop/sidecar
  [ "$output" = "$(printf 'darwin - %s\n' '- ./...' 'debug example.com/a example.com/b' 'livecache,liveswitch ./internal/agent/catalog')" ]
}

@test "go_builds fails when the sidecar's packages cannot be listed" {
  tagged_packages() { return 1; }
  run go_builds apps/desktop/sidecar
  [ "$status" -ne 0 ]
}

@test "tagged_packages lists every package with a file the tag changes, tests included" {
  [ -n "$real_go" ] || skip "needs go"
  PATH="${PATH#"$bin:"}"
  mod="$BATS_TEST_TMPDIR/mod"
  mkdir -p "$mod"
  printf 'module example.com/m\n\ngo 1.21\n' >"$mod/go.mod"
  gofile m.go m
  gofile debug.go m debug
  gofile release.go m '!debug'
  gofile test/test.go test
  gofile test/debug_test.go test debug
  gofile xtest/xtest.go xtest
  gofile xtest/debug_test.go xtest_test debug
  gofile linux/linux.go linux
  gofile linux/debug_windows.go linux debug
  gofile plain/plain.go plain
  GOWORK=off run tagged_packages "$mod" debug
  [ "$status" -eq 0 ]
  [ "$output" = $'example.com/m\nexample.com/m/test\nexample.com/m/xtest' ]
}
