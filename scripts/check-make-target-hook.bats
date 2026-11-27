#!/usr/bin/env bats

setup() {
  command -v jq >/dev/null || skip "needs jq"
  hook="$(dirname "$BATS_TEST_FILENAME")/check-make-target-hook.sh"
}

# check <command>: runs the hook on a Bash tool call of <command>.
check() {
  run bash -c 'jq -n --arg c "$1" "{tool_name: \"Bash\", tool_input: {command: \$c}}" | bash "$2"' _ "$1" "$hook"
}

# refused <reason>: the last check denied the call with <reason>.
refused() {
  [ "$status" -eq 0 ]
  [ "$(jq -r .hookSpecificOutput.permissionDecision <<<"$output")" = deny ]
  jq -r .hookSpecificOutput.permissionDecisionReason <<<"$output" | grep -qF -- "$1"
}

# passed: the last check let the call through.
passed() {
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "refuses each full desktop target, naming its -changed counterpart" {
  for verb in vet test; do
    for target in "$verb-desktop" "$verb-desktop-sidecar" "$verb-desktop-host" "$verb-desktop-webview"; do
      check "make $target"
      refused "CI runs $target. Before a push, run make $verb-desktop-changed; to reproduce a CI failure, rerun with CHECKS=full."
    done
  done
}

@test "refuses test-e2e" {
  check "make test-e2e"
  refused "CI runs test-e2e. Before a push, run make lint-e2e and the tests you changed"
}

@test "refuses a cover-<area>-changed run with FULL=1, before or after make" {
  check "FULL=1 make cover-cli-changed"
  refused "Before a push, run make cover-cli-changed without FULL=1"
  check "make cover-desktop-changed FULL=1"
  refused "Before a push, run make cover-desktop-changed without FULL=1"
}

@test "passes CHECKS=full" {
  check "CHECKS=full make test-desktop"
  passed
  check "make test-desktop CHECKS=full"
  passed
  check "export CHECKS=full && FULL=1 make cover-libs-changed"
  passed
}

@test "refuses a target inside a longer command" {
  check "make lint-desktop vet-desktop && make test-desktop-changed"
  refused "CI runs vet-desktop."
  check "cd apps/desktop && make -C ../.. -j 4 test-desktop-host 2>&1 | tail -20"
  refused "CI runs test-desktop-host."
  check "bash -lc 'make test-e2e'"
  refused "CI runs test-e2e."
}

@test "names each refused target once" {
  check "make test-desktop; make test-desktop vet-desktop"
  refused "CI runs vet-desktop."
  [ "$(jq -r .hookSpecificOutput.permissionDecisionReason <<<"$output" | wc -l)" -eq 2 ]
}

@test "passes a command that names a target without running it" {
  check "grep -n 'test-desktop:' Makefile"
  passed
  check "echo make test-desktop"
  passed
  check 'git commit -m "fix: run make test-desktop; then vet-desktop"'
  passed
  check "make -n test-desktop"
  passed
  check $'cat >notes.md <<\'EOF\'\nmake test-desktop\nEOF\nmake lint-web'
  passed
  check $'gh issue create --body "$(cat <<\'EOF\'\nmake test-e2e\nEOF\n)"'
  passed
}

@test "passes every other target" {
  check "make lint-desktop vet-desktop-changed test-desktop-changed cover-desktop-changed"
  passed
  check "make vet-cli test-cli test-libs test-web lint-e2e test-repo build-desktop"
  passed
  check "make cover-cli-changed"
  passed
}

@test "passes a call with no command" {
  run bash -c 'echo "{}" | bash "$1"' _ "$hook"
  passed
}
