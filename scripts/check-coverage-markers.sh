#!/bin/sh
# A coverage marker exempts lines from coverage / Gate, so it must say why:
# `// coverage:ignore <reason>` or `// coverage:ignore-start <reason>` in Go,
# `// coverage:ignore-start <reason>` on a line of its own in Rust,
# `/* v8 ignore next -- <reason> */` in TypeScript. Prints every marker without
# a reason, and every Rust marker tools/coverignore would not read, and fails.
# A `stop` closes a `start` and carries no reason of its own.
#
#   sh scripts/check-coverage-markers.sh [dir...]   # default: every language's tree
set -eu
cd "$(dirname "$0")/.."

[ "$#" -gt 0 ] || set -- modules crates dashboard-ui/src

status=0
for dir in "$@"; do
  if git grep --untracked -nE '//[[:space:]]*coverage:ignore(-start)?[[:space:]]*$' -- "$dir/*.go"; then
    status=1
  fi
  if git grep --untracked -nE 'coverage:ignore' -- "$dir/*.rs" |
    grep -vE ':[[:space:]]*//[[:space:]]*coverage:ignore-(start[[:space:]]+[^[:space:]]|stop[[:space:]]*$)'; then
    status=1
  fi
  if git grep --untracked -nE '(istanbul|[cv]8|node:coverage)[[:space:]]+ignore[[:space:]]+(if|else|next|file|start)([^[:alnum:]]|$)' -- "$dir/*.ts" "$dir/*.tsx" |
    grep -vE -- '--[[:space:]]*[^[:space:]*]'; then
    status=1
  fi
done
if [ "$status" -ne 0 ]; then
  echo "coverage markers above need a reason; a Rust marker is ignore-start/stop on a line of its own" >&2
fi
exit "$status"
