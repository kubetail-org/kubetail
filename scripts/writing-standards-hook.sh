#!/usr/bin/env bash
# UserPromptSubmit hook: restates the repo's writing standards at the start of every turn,
# before any code, comment or doc is composed. PreToolUse would be too late: the content
# is already written by then.
#
# This is the short form of the "Writing standards" section of the root CLAUDE.md.
# Keep the two in sync.
set -euo pipefail

read -r -d '' REMINDER <<'EOF' || true
Writing standards for this repo (the root CLAUDE.md has the full statement):
1. Code — simple, idiomatic, easy for a human to follow. Prefer the boring construction.
   Match the idiom of the file you are in.
2. Comments — terse, necessary, easy for a human to read. Say what the code cannot: the
   why, the invariant, the trap. Do not restate the code, do not argue against the
   alternatives you rejected (that goes in docs/adr/), and do not address a reviewer
   or point at a PR.
3. Documentation — simple, concise, easy for a human to read. Lead with what is true now.
Applies to hand-written files, not generated output (graph/generated.go,
graph/model/models_gen.go, *.pb.go, dashboard-ui/src/lib/graphql/*/__generated__/) or lockfiles.
EOF

jq -n --arg ctx "$REMINDER" \
  '{hookSpecificOutput: {hookEventName: "UserPromptSubmit", additionalContext: $ctx}}'
