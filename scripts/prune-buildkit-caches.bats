#!/usr/bin/env bats

# Fakes gh. A listing applies its --jq filter to $CACHES; a DELETE appends
# its path to $DELETED, and fails for an id in $FAIL_ID.
setup() {
  bin="$BATS_TEST_TMPDIR/bin"
  mkdir -p "$bin"
  export CACHES="$BATS_TEST_TMPDIR/caches.json"
  export DELETED="$BATS_TEST_TMPDIR/deleted"
  export GITHUB_REPOSITORY=org/repo
  : >"$DELETED"
  cat >"$bin/gh" <<'EOF'
#!/bin/sh
if [ "$2" = -X ]; then
  [ "$4" = "repos/org/repo/actions/caches/${FAIL_ID:-}" ] && exit 1
  echo "$4" >>"$DELETED"
  exit 0
fi
while [ "$1" != --jq ]; do shift; done
jq -c "$2" "$CACHES"
EOF
  chmod +x "$bin/gh"
}

prune() {
  PATH="$bin:$PATH" run sh "$(dirname "$BATS_TEST_FILENAME")/prune-buildkit-caches.sh" \
    refs/heads/main 2026-11-19T22:00:00Z
}

# cache <id> <key> <created_at> <last_accessed_at>
cache() {
  printf '{"id": %s, "key": "%s", "created_at": "%s", "last_accessed_at": "%s"}\n' "$@"
}

caches() {
  jq -s '{actions_caches: .}' >"$CACHES"
}

@test "keeps the newest index of each scope" {
  caches <<EOF
$(cache 1 'index-cli-amd64-1-f921bd05#1' 2026-11-19T20:00:00.1Z 2026-11-19T23:00:00Z)
$(cache 2 'index-cli-amd64-1-f921bd05#2' 2026-11-19T22:30:00.2Z 2026-11-19T23:00:00Z)
$(cache 3 'index-cli-arm64-1-f921bd05#3' 2026-11-19T21:00:00Z 2026-11-19T21:00:00Z)
$(cache 4 'index-cluster-api-amd64-1-f921bd05#4' 2026-11-19T22:40:00Z 2026-11-19T23:00:00Z)
$(cache 5 'index-cluster-api-amd64-1-f921bd05#5' 2026-11-19T21:40:00Z 2026-11-19T23:00:00Z)
EOF
  prune
  [ "$status" -eq 0 ]
  [ "$(sort "$DELETED")" = "$(printf '%s\n' repos/org/repo/actions/caches/1 repos/org/repo/actions/caches/5)" ]
  [[ "$output" == *"deleting index-cli-amd64-1-f921bd05#1"* ]]
}

@test "deletes the blobs last accessed before the cutoff" {
  caches <<EOF
$(cache 1 buildkit-blob-1-sha256:aa 2026-11-19T09:00:00Z 2026-11-19T21:59:59.999999Z)
$(cache 2 buildkit-blob-1-sha256:bb 2026-11-19T09:00:00Z 2026-11-19T22:00:00.5Z)
$(cache 3 buildkit-blob-1-sha256:cc 2026-11-19T09:00:00Z 2026-11-19T23:00:00Z)
EOF
  prune
  [ "$status" -eq 0 ]
  [ "$(cat "$DELETED")" = repos/org/repo/actions/caches/1 ]
}

@test "leaves every other cache alone" {
  caches <<EOF
$(cache 1 go-1.26.11-cli-test-linux-amd64-run-1 2026-11-19T09:00:00Z 2026-11-19T09:00:00Z)
$(cache 2 go-1.26.11-cli-test-linux-amd64-run-2 2026-11-19T10:00:00Z 2026-11-19T10:00:00Z)
EOF
  prune
  [ "$status" -eq 0 ]
  [ ! -s "$DELETED" ]
}

@test "a failed delete warns and goes on" {
  caches <<EOF
$(cache 1 buildkit-blob-1-sha256:aa 2026-11-19T09:00:00Z 2026-11-19T09:00:00Z)
$(cache 2 buildkit-blob-1-sha256:bb 2026-11-19T09:00:00Z 2026-11-19T09:00:00Z)
EOF
  FAIL_ID=1 prune
  [ "$status" -eq 0 ]
  [[ "$output" == *"::warning::could not delete buildkit-blob-1-sha256:aa"* ]]
  [ "$(cat "$DELETED")" = repos/org/repo/actions/caches/2 ]
}
