# shellcheck shell=bash
# Sourced by scripts/cover-changed.sh, from the repo root. Sets what the
# working tree changes since the branch left main: committed, staged, unstaged
# and untracked.
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

# The Go workspace's modules, as go.work lists them.
go_modules='^(cli|cluster/api|dashboard|libs/go-common)/'

# go_pkgs: the changed Go packages, as paths from the repo root. A go.mod or
# go.sum change alone names none: CI checks the packages a dependency reaches.
go_pkgs() {
  grep -E "$go_modules.*\.go$" <<<"$changed" | xargs -r -n1 dirname | sort -u | existing | sed 's|^|./|' || true
}

# go_dirs <module>: the changed Go packages of a module outside the workspace,
# as paths from the module's directory. A `go:build ignore` script, which only
# `go run` builds, is no package's file.
go_dirs() {
  grep -E "^$1/.*\.go$" <<<"$changed" |
    while read -r f; do sed '/^package /q' "$f" 2>/dev/null | grep -qx '//go:build ignore' || echo "$f"; done |
    xargs -r -n1 dirname | sort -u | existing | sed "s|^$1|.|" || true
}

# vitest_scope <app>: the vitest arguments, from the app's directory, that run
# the changed files' tests: `run`, the whole suite, when the toolchain changed,
# else `related` with the changed files in src/ and lint/, else nothing.
# `related` follows imports, but misses a test that reaches a file only through
# a computed dynamic import.
vitest_scope() {
  if has "^$1/(package\.json|pnpm-lock\.yaml|vite\.config\.ts|vitest\.setup\.ts|tsconfig[^/]*\.json)$"; then
    echo run
    return
  fi
  local ts
  ts=$(grep -E "^$1/(src/.*\.tsx?|lint/.*\.[jt]s)$" <<<"$changed" | existing | sed "s|^$1/||" | paste -sd ' ' - || true)
  [ -z "$ts" ] || echo "related --run --passWithNoTests $ts"
}
