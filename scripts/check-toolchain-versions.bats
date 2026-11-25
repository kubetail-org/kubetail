#!/usr/bin/env bats

setup() {
  repo="$BATS_TEST_TMPDIR/repo"
  mkdir -p "$repo/scripts" "$repo/.github/actions/setup-environment" \
    "$repo/cluster/agent" "$repo/cli" "$repo/hack/tilt" \
    "$repo/apps/web" "$repo/apps/desktop/src-tauri"
  cp "$(dirname "$BATS_TEST_FILENAME")/check-toolchain-versions.sh" "$repo/scripts/"
  cat >"$repo/.github/actions/setup-environment/action.yml" <<'EOF'
inputs:
  go-version:
    description: 'Go version to install'
    required: false
    default: '1.26.10'
  node-version:
    description: 'Node.js version to install'
    required: false
    default: '26.13.0'
EOF
  printf '[toolchain]\nchannel = "1.99.1"\n' >"$repo/cluster/agent/rust-toolchain.toml"
  printf '[toolchain]\nchannel = "1.99.1"\ncomponents = ["clippy"]\n' \
    >"$repo/apps/desktop/src-tauri/rust-toolchain.toml"
  printf '{\n  "name": "web",\n  "packageManager": "pnpm@12.17.0+sha512.abc",\n  "private": true\n}\n' \
    >"$repo/apps/web/package.json"
  printf '{\n  "name": "desktop",\n  "packageManager": "pnpm@12.17.0+sha512.abc"\n}\n' \
    >"$repo/apps/desktop/package.json"
  printf 'FROM node:26.13.0-alpine3.24 AS ui\nFROM golang:1.26.10 AS builder\nFROM scratch AS final\n' \
    >"$repo/cli/Dockerfile"
  printf 'FROM rust:1.99.1-slim AS builder\nFROM alpine:3.22.0\n' \
    >"$repo/hack/tilt/Dockerfile.agent"
}

check() {
  run sh "$repo/scripts/check-toolchain-versions.sh"
}

@test "images that match CI's versions pass" {
  check
  [ "$status" -eq 0 ]
}

@test "a node image on another version fails" {
  sed -i.bak 's/node:26.13.0/node:26.12.0/' "$repo/cli/Dockerfile"
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"cli/Dockerfile:1:FROM node:26.12.0-alpine3.24, want 26.13.0"* ]]
}

@test "a golang image on another version fails" {
  sed -i.bak 's/golang:1.26.10/golang:1.26.9/' "$repo/cli/Dockerfile"
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"cli/Dockerfile:2:FROM golang:1.26.9, want 1.26.10"* ]]
}

@test "a rust image under hack/tilt on another version fails" {
  sed -i.bak 's/rust:1.99.1/rust:1.90.0/' "$repo/hack/tilt/Dockerfile.agent"
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"hack/tilt/Dockerfile.agent:1:FROM rust:1.90.0-slim, want 1.99.1"* ]]
}

@test "an image behind a --platform flag or a registry is checked" {
  printf 'FROM --platform=$BUILDPLATFORM golang:1.26.9 AS builder\nFROM docker.io/library/rust:1.90.0\n' \
    >"$repo/cli/Dockerfile"
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"cli/Dockerfile:1:FROM --platform=\$BUILDPLATFORM golang:1.26.9, want 1.26.10"* ]]
  [[ "$output" == *"cli/Dockerfile:2:FROM docker.io/library/rust:1.90.0, want 1.99.1"* ]]
}

@test "an image with no version tag fails" {
  printf 'FROM golang\nFROM node@sha256:abc\n' >"$repo/cli/Dockerfile"
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"cli/Dockerfile:1:FROM golang, want 1.26.10"* ]]
  [[ "$output" == *"cli/Dockerfile:2:FROM node@sha256:abc, want 26.13.0"* ]]
}

@test "a matching image pinned by digest passes" {
  printf 'FROM golang:1.26.10@sha256:abc AS builder\n' >"$repo/cli/Dockerfile"
  check
  [ "$status" -eq 0 ]
}

@test "a stage named for a toolchain is not an image" {
  printf 'FROM alpine:3.22.0 AS node\n' >"$repo/cli/Dockerfile"
  check
  [ "$status" -eq 0 ]
}

@test "a version that only starts with CI's fails" {
  sed -i.bak 's/golang:1.26.10/golang:1.26.100/' "$repo/cli/Dockerfile"
  check
  [ "$status" -eq 1 ]
}

@test "a channel that is not an exact version fails" {
  printf '[toolchain]\nchannel = "stable"\n' >"$repo/cluster/agent/rust-toolchain.toml"
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"rust-toolchain.toml is not an exact version: 'stable'"* ]]
}

@test "a node-version that names only a major fails" {
  sed -i.bak "s/default: '26.13.0'/default: '26'/" "$repo/.github/actions/setup-environment/action.yml"
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"node-version is not an exact version: '26'"* ]]
}

@test "a desktop rust-toolchain.toml on another version fails" {
  sed -i.bak 's/1.99.1/1.98.1/' "$repo/apps/desktop/src-tauri/rust-toolchain.toml"
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"apps/desktop/src-tauri/rust-toolchain.toml: 1.98.1, want 1.99.1"* ]]
}

@test "a desktop packageManager on another pnpm fails" {
  sed -i.bak 's/pnpm@12.17.0+sha512.abc/pnpm@12.4.1/' "$repo/apps/desktop/package.json"
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"apps/desktop/package.json: pnpm@12.4.1, want pnpm@12.17.0+sha512.abc"* ]]
}
