---
title: Tool rounds live in the answer's row
date: 2026-09-14
scope: sidecar
status: Accepted
amended_by:
  - [A tool call is a committed row before it runs](2026-09-17-a-tool-call-is-a-committed-row-before-it-runs.md)
  - [The cluster is kubectl in Bash, and KubeQuery over the mirror](2026-09-27-the-cluster-is-kubectl-in-bash-and-kubequery.md)
---

# Tool rounds live in the answer's row

## Context

The model had no tools, and the cluster card was deliberately orientation only — no counts, no
names beyond namespaces ([the cluster card carries no counts](2026-09-14-the-cluster-card-carries-no-counts.md)
defers scale to "a tool's answer, when tools land"). "How many pods are in `payments`?" was a
question the model could answer only with a `kubectl` command for the user to run, though the
cache held the answer on disk.

The first tool, `list_objects` (`sidecar/internal/clustertools`), lists one kind's identities from
the mirror. Landing it needed a protocol on the wire, the sidecar running a call and answering,
a loop over the model's replies, and a record of the rounds. The question this ADR records is
where the rounds go.

[The answer is a record](2026-09-10-the-answer-is-a-record.md) made a turn one assistant message,
`Streaming` until it settles — the shape `Send`, the overlay, the checkpoint, the reservation, the
replay ledger and the webview's readers all rest on. [The record is the app's](2026-09-11-the-record-is-the-apps-not-the-providers.md)
reserved a place in the block schema for tool blocks.

## Decision

**A turn that calls tools is still one row.** The rounds live inside that row's content as blocks
— `tool_use` and `tool_result`, in the app's schema, a result for every call — and the Anthropic
encoder unfolds them into the wire's alternating messages when the row is resent
(`messagesWire.unfold` in `llm/dialect_messages.go`). The row's counts are the turn's total, its
finish reason the last round's. `agent.Run` (`internal/agent/run.go`) is the loop, and its
`runCalls` decides what each call is answered with; the budget is calls per turn (`maxToolCalls`, 8), so a reply asking
for several spends several, and the rounds are bounded by construction.

**Tools have landed, so the card's job is what the earlier ADRs said it would shrink to**: naming
the cluster the tools read. The counts belong to the tool, fresh on every call; the card keeps
its `inventory` section, since a model that has not called anything still needs to know what is
there to ask about.

## Alternatives considered

- **A row per round** — a `user` row of tool results between two assistant rows, the way the
  wire spells it. Every reader would have to know that a `user` row of results is machinery, the
  list's `updatedAt` would move per round, `Send`'s reservation would span several rows, and the
  replay ledger would need to know which of them a `requestID` names. The one-row shape keeps all
  of that untouched.
- **A table for turns** — rounds as rows of their own, joined to the message. A second table to
  migrate, watch and delete beside `chat_message`, for a record nothing draws yet.

## Consequences

Nothing outside the loop changed shape: `Send`, the overlay, the checkpoint, the reservation and
the ledger are as they were, and the webview's readers already skipped blocks they did not know.
The costs: a row can be long, since it holds every round; an interrupted turn keeps its rounds
and sends none of them, so the transcript's record is honest while the request stays well-formed
(`history` in `services/chat/turn.go` sends a row's rounds only when it settled `Complete`, and a
`Complete` row holds a result for every call);
and a switch between providers on the Anthropic protocol drops the last row's tool blocks with
its thinking block, since the API refuses a trailing `tool_use` with no thinking ahead of it — the
rounds' evidence is the price a switch already pays for the thinking.

Two invariants someone could break without noticing: every `tool_use` in a `Complete` row has its
`tool_result`, whatever the finish reason (`runner.refuse` answers every call the loop does not
run); and the live content is the settled rounds' blocks then the current round's thinking and
text (the loop's `Recorder.Progress`), so a cancel keeps the
call it interrupted.

## Revisit when

A tool round needs drawing — a *Looked at pods in payments* line under the answer — and the
webview wants the rounds as structure rather than blocks to skip. Or when a second protocol
takes tools and its unfolding wants a shape the block list cannot express.
