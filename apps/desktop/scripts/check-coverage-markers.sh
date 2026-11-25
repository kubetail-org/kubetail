#!/bin/sh
# A coverage marker exempts lines from the coverage gate, so it must say why:
# `// coverage:ignore <reason>` or `// coverage:ignore-start <reason>` in Go,
# `// coverage:ignore-start <reason>` on a line of its own in Rust,
# `/* v8 ignore next -- <reason> */` in TypeScript. Prints every marker without
# a reason, and every Rust marker tools/coverignore would not read, and fails.
# A `stop` closes a `start` and carries no reason of its own.
set -eu
cd "$(dirname "$0")/.."

status=0
if git grep -nE '//[[:space:]]*coverage:ignore(-start)?[[:space:]]*$' -- 'sidecar/*.go'; then
  status=1
fi
if git grep -nE 'coverage:ignore' -- 'src-tauri/*.rs' |
  grep -vE ':[[:space:]]*//[[:space:]]*coverage:ignore-(start[[:space:]]+[^[:space:]]|stop[[:space:]]*$)'; then
  status=1
fi
if git grep -nE '(istanbul|[cv]8|node:coverage)[[:space:]]+ignore[[:space:]]+(if|else|next|file|start)([^[:alnum:]]|$)' -- 'src/*.ts' 'src/*.tsx' |
  grep -vE -- '--[[:space:]]*[^[:space:]*]'; then
  status=1
fi
if [ "$status" -ne 0 ]; then
  echo "coverage markers above need a reason; a Rust marker is ignore-start/stop on a line of its own" >&2
fi
exit "$status"
