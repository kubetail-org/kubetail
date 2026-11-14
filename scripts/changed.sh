# shellcheck shell=bash
# Sourced by the *-changed.sh scripts, from the repo root. Sets what the working
# tree changes since the branch left main: committed, staged, unstaged and
# untracked.
#
#   merge_base  the commit the branch left
#   untracked   the untracked files
#   changed     every changed path, one per line
#
# The functions below read them.
#
# BASE names the branch to diff against. It defaults to upstream/main when there
# is an upstream remote, since a fork's origin/main can lag, else origin/main.

base="${BASE:-}"
if [ -z "$base" ]; then
  if git rev-parse -q --verify upstream/main >/dev/null; then
    base=upstream/main
  else
    base=origin/main
  fi
fi
merge_base=$(git merge-base "$base" HEAD)

untracked=$(git ls-files --others --exclude-standard)
changed=$({
  git diff --name-only "$merge_base"
  echo "$untracked"
} | sort -u)

has() { grep -qE "$1" <<<"$changed"; }

# A deleted file has nothing to check, and vitest refuses a path that isn't there.
existing() { while read -r f; do [ -e "$f" ] && echo "$f"; done; }

# go_pkgs: the changed Go packages, as paths from modules/. A go.mod or go.sum
# change alone names none: CI checks the packages a dependency reaches.
go_pkgs() {
  grep -E '^modules/.*\.go$' <<<"$changed" | xargs -r -n1 dirname | sort -u | existing | sed 's|^modules|.|' || true
}

# vitest_scope: the vitest arguments, from dashboard-ui/, that run the changed
# files' tests: `run`, the whole suite, when the toolchain changed, else
# `related` with the changed source files, else nothing. `related` follows
# imports, but misses a test that reaches a file only through a computed
# dynamic import.
vitest_scope() {
  if has '^dashboard-ui/(package\.json|pnpm-lock\.yaml|vite\.config\.ts|vitest\.setup\.ts|tsconfig[^/]*\.json)$'; then
    echo run
    return
  fi
  local ts
  ts=$(grep -E '^dashboard-ui/src/.*\.tsx?$' <<<"$changed" | existing | sed 's|^dashboard-ui/||' | paste -sd ' ' - || true)
  [ -z "$ts" ] || echo "related --run --passWithNoTests $ts"
}
