#!/usr/bin/env bats

# A repo with two tracked guides, each within its budget.
setup() {
  repo="$BATS_TEST_TMPDIR/repo"
  mkdir -p "$repo/scripts" "$repo/.github/ci-config" "$repo/cli"
  cp "$(dirname "$BATS_TEST_FILENAME")/check-claude-md.sh" "$repo/scripts/"
  printf '0123456789' >"$repo/CLAUDE.md"
  printf '01234' >"$repo/cli/CLAUDE.md"
  cat >"$repo/.github/ci-config/claude-md-budgets.yml" <<'EOF'
# Budgets.
CLAUDE.md: 10
cli/CLAUDE.md: 512
EOF
  git -C "$repo" init -q
  git -C "$repo" add .
}

check() {
  run sh "$repo/scripts/check-claude-md.sh"
}

@test "guides within their budgets pass" {
  check
  [ "$status" -eq 0 ]
  [ -z "$output" ]
}

@test "a guide over its budget fails" {
  printf '0123456789x' >"$repo/CLAUDE.md"
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"CLAUDE.md: 11 bytes exceeds 10-byte budget"* ]]
  [[ "$output" == *"Move rationale to an ADR"* ]]
}

@test "a tracked guide with no budget fails" {
  mkdir -p "$repo/e2e"
  printf 'x' >"$repo/e2e/CLAUDE.md"
  git -C "$repo" add e2e
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"e2e/CLAUDE.md: missing or invalid size budget"* ]]
}

@test "an untracked guide is not checked" {
  mkdir -p "$repo/e2e"
  printf 'x' >"$repo/e2e/CLAUDE.md"
  check
  [ "$status" -eq 0 ]
}

@test "a budget that is not a number fails" {
  printf 'CLAUDE.md: 10KB\ncli/CLAUDE.md: 512\n' >"$repo/.github/ci-config/claude-md-budgets.yml"
  check
  [ "$status" -eq 1 ]
  [[ "$output" == *"CLAUDE.md: missing or invalid size budget"* ]]
}

@test "a budget for another path does not match by suffix" {
  printf 'cli/CLAUDE.md: 512\n' >"$repo/.github/ci-config/claude-md-budgets.yml"
  check
  [ "$status" -eq 1 ]
  [ "${lines[0]}" = "CLAUDE.md: missing or invalid size budget in .github/ci-config/claude-md-budgets.yml" ]
}
