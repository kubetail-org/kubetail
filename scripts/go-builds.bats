#!/usr/bin/env bats

# go stubbed to report darwin as the host's GOOS.
setup() {
  bin="$BATS_TEST_TMPDIR/bin"
  mkdir -p "$bin"
  printf '#!/bin/sh\necho darwin\n' >"$bin/go"
  chmod +x "$bin/go"
  PATH="$bin:$PATH"
  . "$(dirname "$BATS_TEST_FILENAME")/go-builds.sh"
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
  GO_BUILDS=host run go_builds apps/desktop/sidecar
  [ "$output" = "$(printf 'darwin - %s\n' '- ./...' 'debug ./...' 'livecache ./internal/agent/catalog' 'liveswitch ./internal/agent/catalog')" ]
}
