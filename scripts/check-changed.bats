#!/usr/bin/env bats

# A repo on a branch off main, and a make that prints its targets.
setup() {
  repo="$BATS_TEST_TMPDIR/repo"
  mkdir -p "$repo/scripts" "$BATS_TEST_TMPDIR/bin"
  cp "$(dirname "$BATS_TEST_FILENAME")"/{changed,check-changed}.sh "$repo/scripts/"
  printf '#!/bin/sh\necho "$@"\n' >"$BATS_TEST_TMPDIR/bin/make"
  chmod +x "$BATS_TEST_TMPDIR/bin/make"
  git -C "$repo" init -q -b main
  git -C "$repo" -c user.name=t -c user.email=t@t commit -q --allow-empty -m base
  git -C "$repo" add scripts
  git -C "$repo" -c user.name=t -c user.email=t@t commit -q -m scripts
  git -C "$repo" checkout -q -b work
}

# check <path>... runs the script with each path changed.
check() {
  for f in "$@"; do
    mkdir -p "$repo/$(dirname "$f")"
    echo x >"$repo/$f"
  done
  PATH="$BATS_TEST_TMPDIR/bin:$PATH" BASE=main run bash "$repo/scripts/check-changed.sh"
}

@test "no change runs the checks that need no toolchain" {
  check
  [ "$status" -eq 0 ]
  [ "$output" = "lint-md lint-toolchains lint-digests test-changed cover-changed" ]
}

@test "a dashboard-ui change adds its lint and type-check" {
  check dashboard-ui/src/app.tsx
  [ "$output" = "lint-md lint-toolchains lint-digests lint-ts vet-ts test-changed cover-changed" ]
}

@test "a modules change adds the Go lint and vet" {
  check modules/cli/main.go
  [ "$output" = "lint-md lint-toolchains lint-digests lint-go vet-go test-changed cover-changed" ]
}

@test "a crates or proto change adds the Rust lint and vet" {
  check crates/rgkl/src/lib.rs
  [ "$output" = "lint-md lint-toolchains lint-digests lint-rust vet-rust test-changed cover-changed" ]
  rm -r "$repo/crates"
  check proto/cluster_agent.proto
  [ "$output" = "lint-md lint-toolchains lint-digests lint-rust vet-rust test-changed cover-changed" ]
}

@test "an e2e change adds the Python lint" {
  check e2e/test_logs.py
  [ "$output" = "lint-md lint-toolchains lint-digests lint-python test-changed cover-changed" ]
}

@test "a shell script adds shellcheck" {
  check hack/run.sh
  [ "$output" = "lint-md lint-toolchains lint-digests lint-sh test-changed cover-changed" ]
}

@test "a YAML file or a file under .github adds the YAML lint" {
  check config/default/cli.yaml
  [ "$output" = "lint-md lint-toolchains lint-digests lint-yaml test-changed cover-changed" ]
  rm -r "$repo/config"
  check .github/CODEOWNERS
  [ "$output" = "lint-md lint-toolchains lint-digests lint-yaml test-changed cover-changed" ]
}

@test "a staged or committed change counts" {
  check modules/cli/main.go
  git -C "$repo" add modules
  check
  [[ "$output" == *"lint-go vet-go"* ]]
  git -C "$repo" -c user.name=t -c user.email=t@t commit -q -m go
  check
  [[ "$output" == *"lint-go vet-go"* ]]
}

@test "a failing check fails the run" {
  printf '#!/bin/sh\nexit 2\n' >"$BATS_TEST_TMPDIR/bin/make"
  check
  [ "$status" -eq 2 ]
}
