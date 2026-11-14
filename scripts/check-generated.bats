#!/usr/bin/env bats

# Fakes protoc, go and pnpm. The fake `go generate` writes $GENERATE into the
# CLI's config copy when it is set, as a stale output would be rewritten.
setup() {
  repo="$BATS_TEST_TMPDIR/repo"
  bin="$BATS_TEST_TMPDIR/bin"
  mkdir -p "$repo/scripts" "$repo/.github/actions/setup-environment" "$repo/tools" \
    "$repo/dashboard-ui" "$bin"
  cp "$(dirname "$BATS_TEST_FILENAME")/check-generated.sh" "$repo/scripts/"
  cat >"$repo/.github/actions/setup-environment/action.yml" <<'EOF'
inputs:
  protoc-version:
    description: 'Protoc version to install'
    required: false
    default: '33.1'
  setup-go:
    description: 'Whether to set up Go'
    default: 'false'
EOF
  for dir in modules/shared/clusteragentpb modules/shared/testpb \
    modules/dashboard/graph modules/cluster-api/graph modules/cli/assets \
    dashboard-ui/src/lib/graphql/dashboard/__generated__ \
    dashboard-ui/src/lib/graphql/cluster-api/__generated__; do
    mkdir -p "$repo/$dir"
    echo committed >"$repo/$dir/file"
  done

  printf '#!/bin/sh\necho "libprotoc ${PROTOC_VERSION:-33.1}"\n' >"$bin/protoc"
  cat >"$bin/go" <<'EOF'
#!/bin/sh
if [ "$1" = generate ] && [ -n "${GENERATE:-}" ] && [ "${PWD##*/}" = cli ]; then
  echo "$GENERATE" >assets/file
fi
EOF
  printf '#!/bin/sh\n' >"$bin/pnpm"
  chmod +x "$bin"/*
}

check() {
  PATH="$bin:$PATH" run bash "$repo/scripts/check-generated.sh"
}

@test "outputs that regenerate unchanged pass" {
  check
  [ "$status" -eq 0 ]
  [[ "$output" == *"Generated code is current."* ]]
}

@test "an output that regenerates differently fails and is named" {
  GENERATE=stale check
  [ "$status" -eq 1 ]
  [[ "$output" == *"generated code is out of date"* ]]
  [[ "$output" == *"modules/cli/assets/file"* ]]
  [ "$(cat "$repo/modules/cli/assets/file")" = stale ]
}

@test "a change the tree already had is not counted" {
  echo edited >"$repo/modules/cli/assets/file"
  GENERATE=edited check
  [ "$status" -eq 0 ]
}

@test "another protoc version fails before anything runs" {
  PROTOC_VERSION=3.21.12 GENERATE=stale check
  [ "$status" -eq 1 ]
  [[ "$output" == *"protoc 3.21.12, want 33.1"* ]]
  [ "$(cat "$repo/modules/cli/assets/file")" = committed ]
}
