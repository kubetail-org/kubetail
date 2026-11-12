#!/usr/bin/env bats

# A repo on a branch off main, a make that prints its targets, and a go and a
# pnpm that do nothing.
setup() {
  repo="$BATS_TEST_TMPDIR/repo"
  mkdir -p "$repo/scripts" "$BATS_TEST_TMPDIR/bin"
  cp "$(dirname "$BATS_TEST_FILENAME")"/{changed,check-changed}.sh "$repo/scripts/"
  printf '#!/bin/sh\necho "$@"\n' >"$BATS_TEST_TMPDIR/bin/make"
  printf '#!/bin/sh\n' >"$BATS_TEST_TMPDIR/bin/go"
  printf '#!/bin/sh\n' >"$BATS_TEST_TMPDIR/bin/pnpm"
  chmod +x "$BATS_TEST_TMPDIR/bin/"*
  git -C "$repo" init -q -b main
  git -C "$repo" -c user.name=t -c user.email=t@t commit -q --allow-empty -m base
  git -C "$repo" add scripts
  git -C "$repo" -c user.name=t -c user.email=t@t commit -q -m scripts
  git -C "$repo" checkout -q -b work
}

# check <path>... runs the script with each path changed. $jobs is the jobs
# it ran, in order, each as the make arguments it ran.
check() {
  for f in "$@"; do
    mkdir -p "$repo/$(dirname "$f")"
    echo x >"$repo/$f"
  done
  PATH="$BATS_TEST_TMPDIR/bin:$PATH" BASE=main run bash "$repo/scripts/check-changed.sh"
  jobs=$(grep -v '^── ' <<<"$output" | sed 's/^-k --no-print-directory //' | paste -sd '|' -)
}

always="lint-md|lint-toolchains|lint-digests"
tests="cover-changed test-changed"

@test "no change runs the checks that need no toolchain" {
  check
  [ "$status" -eq 0 ]
  [ "$jobs" = "$always|$tests" ]
}

@test "a dashboard-ui change adds its lint and type-check" {
  check dashboard-ui/src/app.tsx
  [ "$jobs" = "$always|lint-ts vet-ts|$tests" ]
}

@test "a modules change adds the Go lint and vet" {
  check modules/cli/main.go
  [ "$jobs" = "$always|lint-go|vet-go-changed|$tests" ]
}

@test "a crates or proto change adds the Rust lint and vet" {
  check crates/rgkl/src/lib.rs
  [ "$jobs" = "$always|lint-rust vet-rust|$tests" ]
  rm -r "$repo/crates"
  check proto/cluster_agent.proto
  [ "$jobs" = "$always|lint-rust vet-rust|$tests" ]
}

@test "an e2e change adds the Python lint" {
  check e2e/test_logs.py
  [ "$jobs" = "$always|lint-python|$tests" ]
}

@test "a shell script adds shellcheck" {
  check hack/run.sh
  [ "$jobs" = "$always|lint-sh|$tests" ]
}

@test "a YAML file or a file under .github adds the YAML lint" {
  check config/default/cli.yaml
  [ "$jobs" = "$always|lint-yaml|$tests" ]
  rm -r "$repo/config"
  check .github/CODEOWNERS
  [ "$jobs" = "$always|lint-yaml|$tests" ]
}

@test "a staged or committed change counts" {
  check modules/cli/main.go
  git -C "$repo" add modules
  check
  [[ "$jobs" == *"|lint-go|vet-go-changed|"* ]]
  git -C "$repo" -c user.name=t -c user.email=t@t commit -q -m go
  check
  [[ "$jobs" == *"|lint-go|vet-go-changed|"* ]]
}

@test "the jobs run in parallel, print in order and list the failures" {
  # lint-md fails and, by latency injected to make the order determinate,
  # finishes last; lint-digests fails.
  cat >"$BATS_TEST_TMPDIR/bin/make" <<'EOF2'
#!/bin/sh
case "$*" in
*lint-md) sleep 1; echo md; exit 2 ;;
*lint-digests) echo digests; exit 2 ;;
*) echo "$@" ;;
esac
EOF2
  check
  [ "$status" -eq 1 ]
  [ "${lines[0]}" = "── lint-md: FAILED" ]
  [ "${lines[1]}" = "md" ]
  [ "${lines[2]}" = "── lint-toolchains: ok" ]
  [ "${lines[4]}" = "── lint-digests: FAILED" ]
  [ "${lines[5]}" = "digests" ]
  [[ "$output" == *$'\nFailed: lint-md lint-digests' ]]
}

@test "test-changed skips what cover-changed measured" {
  printf '#!/bin/sh\necho "$MEASURED"\n' >"$BATS_TEST_TMPDIR/bin/make"
  check
  [[ "$output" == *$'\n1' ]]
}
