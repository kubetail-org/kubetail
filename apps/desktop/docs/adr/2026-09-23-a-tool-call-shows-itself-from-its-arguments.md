---
title: A tool call shows itself from its arguments, and an approval is only the decision
date: 2026-09-23
scope: cross-cutting
status: Accepted
amended_by:
  - [Every tool the model can call is in one box, and what it is follows from what it implements](2026-09-24-every-tool-is-in-the-box.md)
  - [The sandbox is the gate for a sandboxed command](2026-09-28-the-sandbox-is-the-gate-for-a-sandboxed-command.md)
  - [A sandboxed command asks for each cluster write](2026-09-29-a-sandboxed-command-asks-for-each-cluster-write.md)
  - [A tool whose action is what its run showed records it at the run](2026-10-08-a-shown-tool-records-its-action-at-the-run.md)
  - [A tool call records its tool's contract beside its stored JSON](2026-10-25-a-tool-call-records-its-tools-contract.md)
---

# A tool call shows itself from its arguments, and an approval is only the decision

## Context

[The gate is the tool's card](2026-09-22-the-gate-is-the-tools-card.md) had a gated tool
answer `Approval(input)` with what the user decides on, and chat stored that answer on the
`approvals` row: `text`, `dir`, `description` and `background` beside `status`, `created_at`
and `decided_at`. Two things lived on one row. The first four say what the call does, and they
are bash's; the last three are the decision.

An ungated call has no approval row, so it had nowhere to keep what it does. The webview's
`summaryOf` therefore read Read's `file_path` out of `arguments` itself, a second parse of a
tool's input outside the tool. A Bash call that never reached the gate (refused, cancelled
before its turn, or unknown on a machine with no shell) read as `Bash`, since its command lived
on a row it never got.

## Decision

**`approvals` holds the decision alone**: `id`, `tool_call_id`, `status`, `created_at`,
`decided_at`.

**What a call does is read from its arguments by its own tool, in the sidecar.** Each tool
that can show a call exports `ActionOf(arguments, cwd) (tools.Action, error)`, a package-level
function over the same `parse` its `Run` uses, so a call shows whether or not its tool is in
this run's box. A `tools.Action` is the model's description and exactly one kind: a
`CommandAction` (`text`, `cwd`, `background`) or a `ReadAction` (`path`). Chat is given a
`tools.Actions` map by tool name and serves the result as `ToolCall.action`, nil when the tool
has no `ActionOf` or refuses the arguments. The webview parses no tool's arguments.

**`tool_calls.cwd` is stored; everything else is read again.** The directory a command starts
in depends on the home directory the tool had when it resolved it, which can change between
runs, so the gate writes it on the call's first row. The command, the description and
`background` are in the arguments, so storing them again would be a second copy that could
disagree with the first.

**Bash's `ActionOf` refuses every input `Run` refuses as bad input.** `resolveWorkdir`'s
refusals that no home can fix (`~user`; on Windows `C:x`, `\x` and a Git Bash path it cannot
map) move into `checkWorkdir`, which both call. A missing directory and a background command outside a
chat are refused for where the call runs, not for its input, so `ActionOf` shows them.

**Every call whose arguments its tool accepts shows what it asked for.** A Bash call that never
reached the gate reads as its command, with the model's description and no directory line,
beside a tag that says it did not run. Only arguments the tool refuses read as the tool's name.

**The approval request draws only a command it has.** A gated tool's action is a `Command`
(the rule is on `tools.Gated`), and a request with no `action.command` offers no Approve: one
line says the request can't be shown, and Deny stays.

This amends the paragraph *A tool says it needs a decision through one more method* of the gate
ADR: `Approval(input)` now returns only what the gate records (`Cwd`), and what the user decides
on is the call's action.

## Consequences

- **Old calls are re-read by the current build.** The command a transcript shows is recomputed
  on every read. A `parse` that refuses more makes an old call show only its name; one that
  reads the same bytes differently would show an approved command as something it was not.
  So how a tool reads its input never changes for rows it has written: changing what an input
  means is a new tool name. `TestActionOfReadsOldRowsTheSame` in each tool pins a fixed set of
  stored arguments. Before release a dev database is reset instead.
- Every publish of a turn's live list parses every call's arguments. That is cheap at one
  publish per status change; per-chunk publishing would keep the actions on the turn's entries.
- `chat.New` takes the actions beside the box, and `app` passes them whether or not a shell
  was found, so a stored Bash call still shows on a machine that offers none.
