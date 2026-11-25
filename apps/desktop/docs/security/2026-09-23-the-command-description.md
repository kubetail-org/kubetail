# Security record — the command description, 23 September 2026

**Subject:** the model's `description` of a bash command is drawn beside the command: above it on
the approval request, and under it in the settled transcript. It is stored on the approval row.
This builds on [the bash tool](2026-09-18-bash-tool.md) and
[bash offered to every turn](2026-09-22-bash-offered-to-every-turn.md). The living model is
[security-model.md](../security-model.md).

## What is new

**Model-written text sits beside the command the user approves.** The model has read the
cluster, so the description is cluster data at one remove. An injected instruction can make it
lie: "List files" over an `rm`, or a verdict such as "Read-only. Checked by Kubetail."

**The model is told the user sees the command.** The schema's `description` property keeps the
reference's text but says the user reads the description *above the command*
(`TestTheDefinitionIsTheReferences`).

## The bound is unchanged

**Approve decides on the command.** `tools.Approval` carries the command as `Text` and the
description as `Description`; `Approval` and `Run` read the input through one `parse`, and the
description never reaches `Text` (`TestTheApprovalCarriesTheDescription`,
`TestTheApprovalAndRunReadTheInputTheSameWay`). The command is drawn exactly as before: every
invisible character spelled out, folded past 2,000 characters or 24 lines, Approve waiting on the
rest (`folds a long command under a description as it does without one`).

**The description cannot push the command out of view.** `descriptionLine` takes one line of at
most 200 characters, and it is drawn `truncate`, so it is one line on screen at any width. It goes
through `VisibleText`, so a reordering character in it is spelled and cannot reach past it
(`spells an invisible character in the description out`, the `descriptionLine` cases).

**The description is the model's voice, never the app's.** On the request it follows *The model
says:*, and it is italic, while the app's own words are upright and muted. The label and the
italic are the marker, not the quotes around it: a description can hold `”` and close them early.
It carries no `title`, since a native tooltip draws text unspelled (`draws the model's description
above the command, labelled and quoted`).

**The description never outshines the command.** It takes the color of where it sits: muted in
the transcript, like the command above it.

## The transcript still names the command

A settled call's summary is the command that ran, and the description sits under it. A lying
description cannot rename a call after the fact (`draws a settled command's description under
it`).

**New: the settled summary is spelled.** Until now it drew the raw command with a `title`, so a
right-to-left override in the command could reorder the tag beside it. It now goes through
`VisibleText` and carries no `title` (`gives the summary no title`, and `spells an override in the
command, keeping the tag in place` and its twin for the description).

## The residual

A user who reads the description and not the command approves what the command does. This is the
reference's residual too: Claude Code shows the same description in its permission prompt.

## The record

The description shown is stored on the approval, beside the text (`approvals.description`,
`TestACommandWaitsOnTheUser`), and served as `ToolCallApproval.description` on both the live list
and the stored read (`TestToolCallsMatchTheStoredRead`). The input in `tool_calls.arguments` also
holds it; the column is the record of what was shown.
