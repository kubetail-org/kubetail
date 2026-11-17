#!/usr/bin/env bats

setup() {
  repo="$BATS_TEST_TMPDIR/repo"
  mkdir -p "$repo/scripts"
  cp "$(dirname "$BATS_TEST_FILENAME")/check-coverage-markers.sh" "$repo/scripts/"
  git -C "$repo" init -q
}

# write <path> <line>: a file in the repo holding one line.
write() {
  mkdir -p "$(dirname "$repo/$1")"
  printf '%s\n' "$2" >"$repo/$1"
}

check() {
  run sh "$repo/scripts/check-coverage-markers.sh" "$@"
}

@test "markers that give a reason pass" {
  write cli/a/a.go 'if err != nil { // coverage:ignore only a full disk fails here'
  write cli/a/b.go '// coverage:ignore-start only the child runs this'
  write cli/a/c.go '// coverage:ignore-stop'
  write cluster/agent/a/src/a.rs '    // coverage:ignore-start the entry point'
  write cluster/agent/a/src/b.rs '    // coverage:ignore-stop'
  write apps/web/src/a.ts '/* v8 ignore next -- jsdom has no layout */'
  check
  [ "$status" -eq 0 ]
}

@test "a Go marker without a reason fails" {
  write cli/a/a.go 'if err != nil { // coverage:ignore'
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"cli/a/a.go:1:"* ]]
}

@test "a Rust marker after code fails" {
  write cluster/agent/a/src/a.rs 'let x = 1; // coverage:ignore-start the entry point'
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"cluster/agent/a/src/a.rs:1:"* ]]
}

@test "a Rust start without a reason fails" {
  write cluster/agent/a/src/a.rs '// coverage:ignore-start'
  check
  [ "$status" -eq 1 ]
}

@test "a TypeScript marker without a reason fails" {
  write apps/web/src/a.tsx '/* v8 ignore next */'
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"apps/web/src/a.tsx:1:"* ]]
}

@test "a directory argument checks that directory alone" {
  write cli/a/a.go 'if err != nil { // coverage:ignore'
  write apps/web/src/a.ts '/* v8 ignore next -- jsdom has no layout */'
  check apps/web/src
  [ "$status" -eq 0 ]
  check cli/a
  [ "$status" -eq 1 ]
}
