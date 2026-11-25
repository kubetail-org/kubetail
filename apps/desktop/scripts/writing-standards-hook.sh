#!/usr/bin/env bash
# UserPromptSubmit hook: injects the repo's writing standards at the start of every turn,
# before any code, comment or doc is composed. PreToolUse would be too late — the content
# is already written by then.
#
# The canonical statement, with rationale, is the "Writing standards" section of CLAUDE.md.
# Keep the two in sync; this is the short form.
set -euo pipefail

read -r -d '' REMINDER <<'EOF' || true
Writing standards for this repo (CLAUDE.md has the full statement):
1. Code — simple, idiomatic, easy for a human to follow. Prefer the boring construction.
2. Names — descriptive, idiomatic, consistent with the rest of the codebase,
   signatures and schemas included. One concept, one name.
3. Comments — terse, necessary, easy for a human to read. Say what the code cannot: the
   why, the invariant, the trap. Do not restate the code, and do not argue against
   alternatives the code does not contain.
4. Documentation — simple, concise, easy for a human to read.
Applies to hand-written source and docs, not generated output (src/gql/, lockfiles).
EOF

jq -n --arg ctx "$REMINDER" \
  '{hookSpecificOutput: {hookEventName: "UserPromptSubmit", additionalContext: $ctx}}'
