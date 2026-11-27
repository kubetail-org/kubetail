---
title: A new chat starts with its default, and a message can leave the sandbox for its turn
date: 2026-11-27
scope: cross-cutting
status: Accepted
---

# A new chat starts with its default, and a message can leave the sandbox for its turn

## Context

[Leaving the sandbox is the user's switch for a chat](2026-09-30-leaving-the-sandbox-is-the-users-switch-for-a-chat.md)
started every chat sandboxed and let the user switch one outside from its composer. Nothing in the
app drew that switch, so every chat ran sandboxed. A user who works outside the sandbox on one
cluster had to switch each new chat after its first question. A user who needs one command
outside had to switch the whole chat and remember to switch it back.

[Network is the user's switch](2026-10-04-network-is-the-users-switch.md) gave the same two scopes
to the network: the chat's switch and a toggle for one message.

This ADR replaces the earlier ADR's rule that a chat starts sandboxed, and its alternative
*Carry the switch on the unstarted chat's outbox entry*. The rest of it holds.

## Decision

**A new chat starts with a default.** The default is the `sandbox.disabled` row of `settings`
(`services/settings/sandboxdefault.go`): a cluster's own row, else every cluster's, else `false`.
Its strictest value is `false`, so a value Kubetail cannot read starts a chat sandboxed. The
transaction in `chatSend` that creates a chat reads the default and writes it to
`chats.sandbox_disabled`. Nothing reads a default for a chat that exists, so a change to either
default leaves every existing chat as it was. On a machine with no sandbox a stored default is
ignored and a write is refused.

**A create names the default it saw.** `chatSend`'s `sandboxDisabled` is, for a create, the default
the new-chat composer showed from `sandboxDefaultStateWatch`. The transaction refuses one that
differs from the default it resolves with `KUBETAIL_CHAT_SANDBOX_CHANGED`, as it refuses a stale
switch for an existing chat. The composer holds Send while any default's change is in flight.

**A message can leave the sandbox for its turn alone.** `chatSend`'s `sandboxDisabledThisTurn` runs
that turn's commands outside the sandbox, its subagents' included. It is resolved once, in the
transaction that reserves the turn, into `tools.Runtime.OutsideSandbox` (the override, else the
chat's switch), and the `## Sandbox` section reports it. It is never stored on the chat, so the
next turn runs where the chat does. It goes only outside: a chat that is outside comes back in
through its own switch.

**Network is a setting of the sandbox.** Outside the sandbox a command has the machine's network,
and Kubetail cannot take it away. So a send that sets both `sandboxDisabledThisTurn` and
`networkThisTurn` is refused, and the composer turns one toggle off when the other goes on. Leaving
the sandbox still clears the chat's network switch, and a chat created outside starts with it off,
so a chat that comes back in has no network until the user turns it on.

**Every move toward outside confirms.** The chat's switch, the message's toggle and each default in
Settings › Sandbox (every cluster's on the status card, a cluster's under New chats on a cluster)
open a dialog before they move outside, a reset that would take a cluster outside included. Moving back inside is immediate. Only the dialog's confirm calls the
mutation or sets the toggle.

## Alternatives considered

**A default that applies to existing chats.** One change would move every chat at once, including
chats the user is reading in another window, and a chat's switch would no longer be the user's
choice for that chat.

**An override that goes both ways.** Running one turn back inside a chat that is outside needs a
second meaning for the toggle and buys little: the chat's switch moves back inside at once, with no
dialog.

**Network off for a turn outside.** Nothing outside the sandbox can enforce it. Saying it would
promise what the machine does not keep.

## Consequences

- A user can start every chat on a cluster outside the sandbox. Each command there still asks,
  with its exact text, as before.
- The model still has no flag. A default, a chat's switch and a message's toggle are the user's
  clicks and a confirm.
- A turn already running keeps its value. A default changed meanwhile reaches the next chat
  created, and a switch flipped meanwhile the next turn.
- `chatSend` gains a required argument, and the new-chat composer opens one more watch.

## Revisit when

A host allowlist gives network a meaning outside the sandbox, or the defaults sync to Kubetail
Cloud.
