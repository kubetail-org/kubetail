#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

git ls-files -- 'CLAUDE.md' '**/CLAUDE.md' | {
  status=0
  while IFS= read -r file; do
    budget=$(awk -F': ' -v file="$file" '$1 == file { print $2 }' .github/ci-config/claude-md-budgets.yml)
    case "$budget" in
      ''|*[!0-9]*)
        echo "$file: missing or invalid size budget in .github/ci-config/claude-md-budgets.yml" >&2
        status=1
        continue
        ;;
    esac
    size=$(wc -c < "$file" | tr -d ' ')
    if [ "$size" -gt "$budget" ]; then
      echo "$file: $size bytes exceeds $budget-byte budget" >&2
      status=1
    fi
  done
  if [ "$status" -ne 0 ]; then
    echo 'Move rationale to an ADR (docs/adr/TEMPLATE.md), delete detail the code states, and link instead of restating.' >&2
  fi
  exit "$status"
}
