#!/usr/bin/env bats

check() {
  run bash "$(dirname "$BATS_TEST_FILENAME")/check-release-tag.sh" "$@"
}

@test "a release tag of each component passes" {
  for tag in dashboard/v1.2.3 cluster-api/v0.1.0 cluster-agent/v10.20.30 cli/v1.2.3; do
    check "$tag"
    [ "$status" -eq 0 ]
  done
}

@test "a pre-release tag passes" {
  check cli/v1.2.3-rc.1
  [ "$status" -eq 0 ]
}

@test "a branch name fails" {
  check main
  [ "$status" -eq 1 ]
  [[ "$output" == *"not a release tag: main"* ]]
}

@test "an unknown component fails" {
  check server/v1.2.3
  [ "$status" -eq 1 ]
}

@test "a partial version fails" {
  check dashboard/v1.2
  [ "$status" -eq 1 ]
}

@test "text after the version fails" {
  check 'dashboard/v1.0.0,extra'
  [ "$status" -eq 1 ]
}

@test "a release tag followed by a second line fails" {
  check $'dashboard/v1.0.0\nmain'
  [ "$status" -eq 1 ]
}

@test "a full ref fails" {
  check refs/tags/dashboard/v1.0.0
  [ "$status" -eq 1 ]
}

@test "no tag fails" {
  check
  [ "$status" -eq 1 ]
}
