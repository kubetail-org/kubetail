---
title: The gate is the tool's card, and gated calls run one at a time
date: 2026-09-22
scope: sidecar
status: Accepted
amended_by:
  - [A tool call shows itself from its arguments, and an approval is only the decision](2026-09-23-a-tool-call-shows-itself-from-its-arguments.md)
  - [The sandbox is the gate for a sandboxed command](2026-09-28-the-sandbox-is-the-gate-for-a-sandboxed-command.md)
---

# The gate is the tool's card, and gated calls run one at a time

## Context

[A tool call is a committed row before it runs](2026-09-17-a-tool-call-is-a-committed-row-before-it-runs.md)
left the gate's shape open: `is_mutating`, `awaiting_approval` and `approvals` were in the schema
and written by nothing. The rewrite's loop (`agent.Run`) runs a reply's calls one after another,
while *A reply's calls run at the same time*,
written for the loop before the rewrite, has every call of a reply start at once. Bash returns
as a custom function on every dialect, the first tool that needs the user's decision.
Three layers meet at the gate: `tools` knows what a call will do, `agent` knows when a call runs,
and `services/chat` owns the rows and the live view.

## Decision

**A tool says it needs a decision through one more method.** `tools.Gated` is a `Tool` with
`ApprovalText(input) (string, error)`: the text the user decides on, exactly what the call will
do. Every call of a gated tool is asked; an error is an input the tool cannot read, refused
`bad-input` with no card. The loop finds a gated tool by type assertion at the call; `Box` does
not know.

**The loop asks, chat answers.** `agent.Run` takes an `Approver` beside the `Recorder`. In
`runCalls`, after the lookup and before `ToolCallStarted`, a gated call is put to
`Approver.Approve(ctx, call, card)`. A no is answered `denied` and the next call proceeds; a
yes checks the turn's cancel once more and then runs as today. A decision stands once made,
since chat has committed it by the time `Approve` returns. `services/chat`'s turn implements the
approver in `approval.go`: the waiter is registered first, the call's row, its approval and the
run's flip to `waiting_approval` land in one transaction, and the wait ends on the decision
(delivered by `approvalDecide` through a buffered channel of one) or the turn's cancel.

**Gated calls run one at a time, in reply order, and so does every call.** One card is before
the user at a time, and a denied first command never holds a second. The loop keeps one path
for every tool rather than a concurrent one beside a sequential one.

## Alternatives considered

**Every call of a reply at once, cards included.** What the superseded ADR decided. Several
cards up at once, each decidable in any order, means ordering what runs against what the user
approved (the old loop's ticket chain) and deciding what a denial does to the calls already
running beside it. The user reads one command at a time; the gate should ask the same way.

**Ungated calls at once, gated ones in turn.** Two shapes in `runCalls` for one rule, bought for
the only tool that would gain from it — the child agent, which is not rebuilt.

**The gate in chat, not the loop.** Chat would have to know which tool gates what and run the
tool itself. The loop already decides when a call runs; the approver keeps the rows chat's.

**A gate flag on the definition.** The definition is the offer the provider sees, and whether
the user is asked is none of the provider's business; the card, which only the tool can build,
has to come from the tool anyway.

## Consequences

A reply's wall time is the sum of its calls again. A second gated tool costs an `ApprovalText`
method and no loop change. The approval id is chat's, minted after the waiter exists, so no id
is decidable ahead of its card. A reply that asks for several commands asks the user several
times, in order.

## Revisit when

A tool whose calls are slow and ungated — the child agent — is rebuilt, and running a reply's
ungated calls together would buy back real time.
