---
title: Leaving the sandbox is the user's switch for a chat, never the model's flag
date: 2026-09-30
scope: cross-cutting
status: Accepted
amended_by: [A new chat starts with its default, and a message can leave the sandbox for its turn](2026-11-27-a-new-chat-starts-with-its-default-and-a-message-can-leave-the-sandbox-for-its-turn.md)
---

# Leaving the sandbox is the user's switch for a chat, never the model's flag

## Context

[The sandbox is the gate for a sandboxed command](2026-09-28-the-sandbox-is-the-gate-for-a-sandboxed-command.md)
let a command out of the sandbox when the model set `dangerouslyDisableSandbox` on its Bash call,
the flag Claude Code's own tool takes. That call then asked the user. The threat model in
[the agent security design note](../notes/sandbox-credentials-and-permissions.md) says the agent must not be able to
run anything outside the sandbox. A prompt injection in cluster text can make the model ask, on
any call and as persuasively as it likes. The only thing standing between that request and an
unconfined shell was one Approve.

This ADR replaces the paragraph of that ADR's decision that names the flag. Its decision that a
sandboxed command runs unasked still holds.

## Decision

**The model has no flag.** `dangerouslyDisableSandbox` leaves the Bash schema, and a call that
sends it is bad input on every machine (`bash.parse`).

**The user switches a chat.** `conversations.sandbox_disabled` is off for every chat. The composer's
button turns it on after a dialog says what it means, through `chatSandboxDisabledSet`, and turns it
off with no dialog. The mutation is refused on a machine with no sandbox.

**The turn reads the switch once.** `services/chat` reads it in the transaction that reserves the turn,
beside the context block that tells the model, into `tools.Runtime.OutsideSandbox`, and a subagent
takes its parent turn's value. `bash.sandboxerFor(rt)`
is the one test: no sandboxer for a chat switched outside. So what started under a switch keeps it,
and every command outside the sandbox still asks. A send carries the switch its sender saw, and
that transaction refuses one that differs (`KUBETAIL_CHAT_SANDBOX_CHANGED`): another window can
switch the chat between what a composer shows and when its send lands.

**The model is told.** A question's context gains a `## Sandbox` section, `{"commands":"sandboxed"}`
or `{"commands":"outside"}`, absent on a machine with no sandbox. `sandbox.md` tells the model to
say when a command needs what the sandbox lacks, and not to work around it.

**The request's heading follows the machine.** On a machine with a sandbox only a command outside
it asks, so every command's request there reads *Run this command outside the sandbox?*. The
heading never follows the chat's switch, which the user can flip while a request waits.

## Alternatives considered

**Keep the flag and make its request louder.** The request already showed the command. A request
that says "outside the sandbox" more loudly is still one click on the model's word, and a user
approving a stream of reads learns to click.

**A switch per call, set on the request.** It is the flag with the user holding the pen, and it
puts the choice at the moment an injection has prepared. A per-chat switch is made before the
command exists.

**A switch for the whole app, in the settings file.** One switch left on makes every chat
unconfined, including a monitoring agent's. The chat is the session a user is working in, so it is
the scope the choice belongs to. Step 2C folds the column into that session.

**Carry the switch on the unstarted chat's outbox entry.** A new chat could start outside. That is
a follow-on: a chat starts sandboxed, and the user switches it after the first question.

## Consequences

- An injection cannot get a command outside the sandbox. It can only ask the model to tell the user
  that one is needed.
- A stored call that carried the flag reads as bad input. The pre-release schema reset clears them.
- A subagent or background command started under the switch keeps running outside after the user
  switches back. Each of its commands still asks, and the dialog says so.
- The heading rests on one invariant, that a sandboxed command never asks (`Skip` is `sandboxed` in
  `Approval`). A step that makes a sandboxed command ask brings back a per-call field first.
- A send's runtime and its context block read the switch in one transaction, so they agree. A turn
  a background notice starts writes no context, and a subagent reads its parent's newest one, so
  either can act under a switch the model has not been told of yet. That errs toward a command that
  asks or one that is refused, never toward one that runs unasked outside.

## Revisit when

A user needs a chat to start outside the sandbox often enough that switching after the first
question is a burden, or a session (step 2C) takes the switch over.
