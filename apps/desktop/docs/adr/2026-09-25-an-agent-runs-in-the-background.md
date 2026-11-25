---
title: An agent runs in the background, as a task of the chat that owns its rows
date: 2026-09-25
scope: cross-cutting
status: Accepted
---

# An agent runs in the background, as a task of the chat that owns its rows

## Context

[An agent is a tool in the box](2026-09-25-an-agent-is-a-tool-in-the-box.md) ran the subagent
inside the `Agent` call, on the parent's goroutine. The parent waited for it, subagents ran one
after another, and the user could ask nothing while they ran: a question that splits four ways took
four subagents' time. The subagent shared the parent's turn: its calls rode the parent's live list,
its gated call flipped both runs to `waiting_approval`, a write that failed inside it ended the
parent's turn, and the parent's settle wrote its rows again.

Background commands had already built what an agent needs to outlive its call: a task row, a slot
counted per chat and per app, a watcher that writes how it ended, stops by the model, the user,
the chat's delete and the app, a notice on the next question, and a turn of its own for an exit.
Claude Code's own `Agent` tool answers at once and reports through a notification.

## Decision

**An `Agent` call answers at once with the agent's id, and the subagent is a background task of
the chat.** `tools.Spawner.Start` writes the subagent's run, its `background_tasks` row and the
`Agent` row's `spawned_run_id` in one transaction, and the subagent runs on a goroutine of its own
(`services/chat`'s `agentTask`), under a context only a stop cancels. A start that fails is the call's
refusal and the turn goes on; a Cancel during the start starts nothing, and the start's rows are
taken back. `agent.Tool` is no longer `Bounded`. A limit refuses in the words bash's does, naming
both, since an agent takes one of the chat's four slots.

**The subagent owns its rows.** Its recorder names the chat and the service, never the parent's
turn, and publishes by notifying the chat's watchers. `watchTask` is the one writer of its end:
after `Wait`, it names the task `completed`, `failed` or `stopped` off the run's outcome, and
`finishRow` writes the run, every row of it whole again and the task's end in one transaction. The
report goes into the task's file and on the run.

**The report is a notice.** It rides the chat's next question, inline to `tools.InlineLimit` in a
`<result>` element, and previewed from the task's file past it; a failed agent's notice carries its
error. An agent's end starts a turn of its own, as an exit does — **but only for an agent a user's
send launched** (the `Agent` call's turn has a trigger with a request key). The transcript reads
the report off the call's task (`BackgroundTask.report`).

**A subagent's request waits on the user whatever the parent is doing.** Only the subagent's own run
flips; `awaitingApproval` on the message and the chat says a request can be answered. Every waiting
request is drawn, in the order asked, and Approve arms only once its place on screen has held for
500ms. A request unanswered for 30 minutes stops its agent, `stopped_by` `unanswered`. The composer
points to a request in an earlier message, and the chat list marks a chat with one waiting.

**A subagent's `TaskStop` reaches only what its own calls started**; the parent's reaches every task
of the chat.

This amends *an agent is a tool in the box*: its paragraph that the subagent runs inside the call,
and that a write that stops the subagent stops the parent and the parent's settle writes its rows,
is replaced here. The rest of it stands.

## Alternatives considered

**Keep the call waiting, and run a reply's `Agent` calls in parallel.** Four subagents would take
one subagent's time, but the user could still ask nothing until the last reported, and a subagent
waiting on the user would hold the parent's turn with it. The notice path already existed.

**Start a turn on every agent's end.** `Agent` is ungated, so a notice turn could launch an agent
whose end starts another notice turn, and so on, with no one approving anything. A command's chain
is bounded by the approval each command needs; an agent's is not. Keying on the launching turn's
request key stops the chain one link past the user's send: an agent a sidecar-started turn launched
rides the next question.

**Let a Cancel stop the agents the answer started.** Cancel is the stop on an answer, and a
background command already outlives one. Stop on the call is the stop on an agent. The cost, which we
accept: an agent that ends after a Cancel starts a turn, so the model can speak again after the user
pressed Cancel. One that ended before the Cancel rides the next question, since a cancelled turn
kicks nothing.

**Leave an agent's request waiting for as long as it takes.** A request in a chat the user is not
looking at would hold its slot for good, and 16 forgotten agents across chats would refuse every
background command in Kubetail. The parent's own request is unchanged: it holds the turn, which the
user sees and can cancel.

**Re-arm Approve when the list of requests above it changes.** A request no longer stops the turn,
so the answer above it grows, the transcript follows a growing answer and blocks open above it; a
click aimed elsewhere could land on an Approve that slid under the pointer. Only the button's own
position answers every one of those.

**Let a subagent's `TaskStop` reach every task, as the parent's does.** A stop by the model is
written notified, because its result told it; only the model that called it read that result, so a
subagent stopping a sibling or itself would leave the parent waiting for a notice that never comes.

## Consequences

- A report now reaches the model in a user message rather than a tool result. It is escaped by
  `noticeEscaper` and the prompt calls it data, but it is cluster data one step removed, in the
  message a model weighs most.
- Four agents fill a chat's slots, so a subagent may find none left for a background command; its
  prompt says to run it in the foreground.
- The fake's replies are routed by the prompt that opens a request (`llm.Fake.Route`), since a
  subagent's requests and its parent's now interleave.
- Two users of `background_tasks.status` exist; a new status is a change to the check, the wire enum
  and the transcript's tag together.

## Revisit when

A subagent needs to be continued after it reports (`SendMessage` in the reference), or an agent needs
to run without holding a slot.
