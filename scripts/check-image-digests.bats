#!/usr/bin/env bats

setup() {
  repo="$BATS_TEST_TMPDIR/repo"
  mkdir -p "$repo/scripts" "$repo/build/package" "$repo/hack/tilt"
  cp "$(dirname "$BATS_TEST_FILENAME")/check-image-digests.sh" "$repo/scripts/"
  cat >"$repo/build/package/Dockerfile.cli" <<'EOF'
FROM ghcr.io/org/probe:v1.0.0@sha256:abc123 AS probe
FROM golang:1.26.10@sha256:def456 AS builder
COPY --from=probe /probe /bin/probe
FROM scratch AS final
COPY --from=builder /work/bin/kubetail /usr/local/bin/kubetail
COPY --from=0 /probe /bin/probe
EOF
  printf 'FROM alpine:3.24.4@sha256:abc123\n' >"$repo/hack/tilt/Dockerfile.dashboard"
}

check() {
  run sh "$repo/scripts/check-image-digests.sh"
}

@test "images pinned by digest pass" {
  check
  [ "$status" -eq 0 ]
}

@test "a FROM with a tag alone fails" {
  printf 'FROM alpine:3.24.4\n' >"$repo/hack/tilt/Dockerfile.dashboard"
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"hack/tilt/Dockerfile.dashboard:1:FROM alpine:3.24.4"* ]]
}

@test "a FROM behind a --platform flag and with a stage name is checked" {
  printf 'FROM --platform=$BUILDPLATFORM golang:1.26.10 AS builder\n' >"$repo/build/package/Dockerfile.cli"
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"Dockerfile.cli:1:FROM --platform=\$BUILDPLATFORM golang:1.26.10 AS builder"* ]]
}

@test "a COPY --from an image with no digest fails" {
  printf 'FROM scratch\nCOPY --from=ghcr.io/org/probe:v1.0.0 /probe /bin/probe\n' \
    >"$repo/build/package/Dockerfile.cli"
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"Dockerfile.cli:2:COPY --from=ghcr.io/org/probe:v1.0.0 /probe /bin/probe"* ]]
}

@test "a FROM of an earlier stage is not an image" {
  printf 'FROM alpine:3.24.4@sha256:abc123 AS base\nFROM base AS final\n' \
    >"$repo/build/package/Dockerfile.cli"
  check
  [ "$status" -eq 0 ]
}

@test "a stage name is not carried into the next file" {
  printf 'FROM base\n' >"$repo/hack/tilt/Dockerfile.dashboard"
  printf 'FROM alpine:3.24.4@sha256:abc123 AS base\n' >"$repo/build/package/Dockerfile.cli"
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"hack/tilt/Dockerfile.dashboard:1:FROM base"* ]]
}
