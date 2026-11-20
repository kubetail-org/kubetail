#!/usr/bin/env bats

# Fakes protoc, go and pnpm. Each fake records its call, and the directory it
# ran in, in $calls.
setup() {
  repo="$BATS_TEST_TMPDIR/repo"
  bin="$BATS_TEST_TMPDIR/bin"
  calls="$BATS_TEST_TMPDIR/calls"
  export repo calls
  mkdir -p "$repo/scripts" "$repo/.github/actions/setup-environment" "$repo/tools" \
    "$repo/libs/go-common" "$repo/dashboard" "$repo/cluster/api" "$repo/cli" \
    "$repo/apps/web" "$bin"
  cp "$(dirname "$BATS_TEST_FILENAME")/generate.sh" "$repo/scripts/"
  cat >"$repo/.github/actions/setup-environment/action.yml" <<'YAML'
inputs:
  protoc-version:
    description: 'Protoc version to install'
    required: false
    default: '33.1'
  setup-go:
    description: 'Whether to set up Go'
    default: 'false'
YAML

  printf '#!/bin/sh\necho "libprotoc ${PROTOC_VERSION:-33.1}"\n' >"$bin/protoc"
  # Drops `go build`'s -o, which names a temporary directory.
  cat >"$bin/go" <<'SH'
#!/bin/sh
[ "$1" = build ] && shift 3 && set -- build "$@"
echo "go $* in ${PWD#"$repo"/}" >>"$calls"
SH
  printf '#!/bin/sh\necho "pnpm $* in ${PWD#"$repo"/}" >>"$calls"\n' >"$bin/pnpm"
  chmod +x "$bin"/*
}

generate() {
  PATH="$bin:$PATH" run bash "$repo/scripts/generate.sh"
}

@test "builds the pinned plugins, then runs every generator in order" {
  generate
  [ "$status" -eq 0 ]
  [ "$(cat "$calls")" = "$(cat <<'OUT'
go build google.golang.org/protobuf/cmd/protoc-gen-go google.golang.org/grpc/cmd/protoc-gen-go-grpc in tools
go generate ./... in libs/go-common
go generate ./... in dashboard
go generate ./... in cluster/api
go generate ./... in cli
pnpm install --frozen-lockfile in apps/web
pnpm graphql-codegen in apps/web
OUT
)" ]
}

@test "another protoc version fails before anything runs" {
  PROTOC_VERSION=3.21.12 generate
  [ "$status" -eq 1 ]
  [[ "$output" == *"protoc 3.21.12, want 33.1"* ]]
  [ ! -e "$calls" ]
}

@test "a protoc that does not run fails before anything runs" {
  printf '#!/bin/sh\nexit 127\n' >"$bin/protoc"
  generate
  [ "$status" -eq 1 ]
  [[ "$output" == *"protoc not found, want 33.1"* ]]
  [ ! -e "$calls" ]
}
