# Security record — the request reads the arguments, 23 September 2026

**Subject:** the approval request's command, description and background flag stop being a
copy stored on the `approvals` row. They are read again from `tool_calls.arguments` by the
tool's own `ActionOf` on every read, and the directory is stored on `tool_calls.cwd`. This
builds on [the bash tool](2026-09-18-bash-tool.md),
[the command description](2026-09-23-the-command-description.md) and
[the command names its directory](2026-09-23-the-command-names-its-directory.md). The decision
is [a tool call shows itself from its arguments](../adr/2026-09-23-a-tool-call-shows-itself-from-its-arguments.md).
The living model is [security-model.md](../security-model.md).

No capability is added. What moves is where the text the user approves comes from.

## What is shown is still what runs

**One reading of one input.** `arguments` is `call.Input` byte for byte, written with the call's
first row. `ActionOf` and `Run` read it through the same `parse`, and `ActionOf` applies the
same `checkWorkdir`, so every input `Run` refuses as bad input, `ActionOf` refuses
(`TestTheApprovalAndRunReadTheInputTheSameWay`, `TestARefusedWorkdirIsNotShown`), and the text
it serves is the command `Run` hands the shell (`TestActionOfReadsTheCommand`, bidirectional
override included).

**The directory is the one `Run` starts in.** The gate writes `Approval`'s resolved `cwd` onto
the row, and `Run` resolves again from the same home with the same `resolveWorkdir`. The two
agree because `resolveWorkdir` is string work and a tool's home is fixed for the life of the
process (`TestTheToolCallRowHoldsTheCwd`, `TestTheApprovalShowsTheDirectoryItRunsIn`).

**Nothing outside the tool parses its input.** The webview reads `action` and never
`arguments`; `chats.tsx` no longer selects them.

## The request fails closed

A gated tool's action is a `Command`. A request whose call has no `action.command` — a gated
tool the sidecar has no `ActionOf` for, or arguments it now refuses — draws one line saying it
can't be shown, offers no Approve, and keeps Deny (`draws no Approve for a request it cannot
show`). `TestEveryGatedToolCanBeShown` pins that every gated tool in chat's box has an entry in
the actions chat is given, so no user meets this today.

## The transcript names commands nobody was asked about

A Bash call that never reached the gate — refused, cancelled before its turn, answered not-run
after a failed write, or unknown on a machine with no shell — now reads as its command, where it
read `Bash` before. It is drawn through `VisibleText` like any other command, beside a tag that
says it did not run, with no directory line, and it has no approval, so nothing can approve it
(`TestAnUngatedBashCallShowsItsCommand`, `summarises a command that never reached the gate`).

## Residual

**Any later change to how a tool reads its input rewrites what old calls show, approved ones
included.** The command is recomputed by whatever build reads it, where the stored copy held
what the user saw. A `parse` that refuses more hides an old call's command; one that reads the
same bytes differently would show an approved command as something other than what was
approved. The rule is that changing what an input means is a new tool name, never an edit to
`parse`, and `TestActionOfReadsOldRowsTheSame` in `tools/bash` and `tools/read` pins a fixed set
of stored arguments against their actions. Before release a dev database is reset instead.
