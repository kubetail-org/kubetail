---
title: A chat can switch to any model, and each run records the dialect it ran on
date: 2026-09-25
scope: cross-cutting
status: Accepted
---

# A chat can switch to any model, and each run records the dialect it ran on

## Context

*A chat stays on the protocol it started on* pinned
each chat to the dialect of the send that created it, so that a dialect could offer native tools
with no replay obligation on the others. The cost was a chat that could not follow the user to
another vendor: a key gone, a model better at the task, a price change. The way out it named was a
fork, and no one built one.

Meanwhile the payload strip landed. A stored row keeps each provider's verbatim items as payloads
beside the app's fields, and `stripForeign` sends another provider's rows as the app's fields
alone: text, context, notices, `tool_use` and `tool_result` in the round's order. Each wire already
rendered such rows for a second provider of its own dialect. No native client tool had been built,
so nothing on any wire had a shape the others could not render. What was left was ids: a
Chat Completions vendor can spell a call id the Messages API refuses, the Chat Completions reader
mints `call-<n>` per reply so one chat can hold an id twice, and the Responses API pairs a result
to its call by `call_id` alone.

## Decision

**Any send in a chat may name any model in the catalog**, at any effort that model lists. A chat
has no dialect of its own. `agent_runs.dialect` holds each run's, written from the target at the
send, for a chat's run and a subagent's alike. It reads the run's stored content, its citations
and payloads, after that provider has left the catalog. `conversations.llm_dialect`,
`Chat.llmDialect`, `ErrChatOnAnotherDialect` and `KUBETAIL_CHAT_LLM_DIALECT_MISMATCH` are gone, and
the composer offers every chat the whole catalog.

**Every wire replays another's rows from the app's fields, under ids of its own.** `replayIDs`
(`llm/helpers.go`) is the one mint. It is shared by the three wires and runs over the conversation,
so the same history always mints the same ids. It mints every call of a foreign row, and any own
id already handed out, as `call%05d`, a spelling every API takes. It never hands out one id twice.
A result answers the id its call went under. An own call that is minted and keeps a payload has
the minted id written into it (`withReplayID`: `id` on Chat Completions, `call_id` on Responses),
so the item and its result still pair up.

Payloads still never cross providers: `stripForeign` keys on the writing provider. A subagent
still runs on its parent's provider, so the model cannot move a chat to another vendor.

## Alternatives considered

- **Keep the pin and build the fork.** A new chat seeded with a model-written summary loses
  every round, and asks the model to summarise cluster data it may have misread. The strip
  already keeps the text of every answer.
- **Keep `conversations.llm_dialect` as the first run's dialect.** A chat's rows are then in
  more than one dialect's shapes, and a column naming one of them misreads the rest. The
  citations are the case that bites: they are read from payloads by the dialect that wrote them.
- **Derive a message's dialect from its run's provider through the catalog.** A provider whose key
  is gone is no longer held, and its messages would lose their citations.
- **Mint only on the wires that refuse an id.** The Messages API refuses a spelling, and the
  Responses API cannot tell two calls apart if they share an id. Minting only where a refusal is
  documented leaves the duplicate case to whichever API does not document it.

## Consequences

A switch costs the other provider's thinking, the prefix cache (a cached prefix is per
provider) and a provider-run search's results. The text of every answer still goes. On a
Messages target with thinking on, a foreign row that holds the chat's last tool round goes
without its rounds, and goes with them again once the target runs a round of its own.

`roomFor`'s count stays per provider, so a switch to a provider with no count on the chat is not
checked ahead. The provider's own refusal and the repeat rule stop what gets past it.

The obligations: `TestEveryDialectReplaysEveryOtherDialectsTurn` walks every ordered pair of
wires, and a new wire joins it. A native client tool, when one is built, replays as a plain
function round on a target that does not offer it, and adds its row there
(*A native tool is a contract*). `make check-switch`
walks one chat across the real APIs and is run by hand, since the fixtures cannot say whether
an API takes a rebuilt round.

This supersedes *a chat stays on the protocol it started on*
and amends [the pin is an LLM dialect](2026-09-20-the-pin-is-an-llm-dialect.md), whose naming of
the dialect stands.
