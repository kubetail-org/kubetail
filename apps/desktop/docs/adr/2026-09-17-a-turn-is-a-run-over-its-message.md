---
title: A turn is a run over its message, filed in one transaction
date: 2026-09-17
scope: cross-cutting
status: Accepted
amended_by: [A tool call is a committed row before it runs](2026-09-17-a-tool-call-is-a-committed-row-before-it-runs.md)
---

# A turn is a run over its message, filed in one transaction

## Context

The chat tables were the first draft of a wider schema (`docs/specs/appdb.sql`, since retired into
`sidecar/internal/appdb/migrations/0001_init.sql`)
in which an agent's execution — a chat answer today, a monitor pass or a spawned child agent
later — is its own record. Under the first draft an assistant message *was* the record of its
turn: `chat_message` carried `status`, `provider`, `model`, `effort`, `finish_reason` and
`completed_at`, a separate `chat_send` ledger held every accepted request id for a day, and ids
were ULIDs whose time order the transcript did not rely on but the docs half-promised.

Three things pushed the move. The monitor and child-agent work need a run that is not a message.
The ledger was a second source of truth for "was this send accepted" that outlived the chat it
described — which is why a retry into a deleted chat had to be refused rather than treated as a
fresh post. And UUIDv7 ids are monotonic only within one process, so any order a reader relied on
had to be the row's own.

## Decision

**Three tables replace `chat`, `chat_message` and `chat_send`: `conversations`, `messages` and
`agent_runs`** (`sidecar/internal/appdb/migrations/0001_init.sql`), the final definitions from the
target schema. A message is what a client posts; a run is what the server does about it. An
assistant message's `run_id` names the run that writes it, and the run's `trigger_message_id`
names the user message it answers — plain references both ways, cascaded off the conversation
alone, and one `DELETE` takes all three. The message's public status is its run's
(`queued`/`running`/`waiting_approval` → Streaming, `succeeded` → Complete, `failed` → Failed,
`cancelled` → Cancelled); what it was asked to run, its error and its finish time are the run's
columns. `messages` has no status and no model columns.

**A send is one transaction that writes the user message with the client's request key, a queued
chat run, and the empty assistant message** (`chat.writeTurnRows`), after replaying the key,
admitting the post and reserving the conversation's turn. The request key lives on the user
message (`messages.request_key`, unique), so a replay is one joined `SELECT` — message → run →
answer — and there is no ledger to prune. The key goes with its message: a retry after the chat is
deleted is a fresh send, which creates a chat when shaped like a create and is refused as chat-gone
when it names the deleted one. The key must be a UUID the client minted; anything else is
`ErrBadRequest` before the lookup.

**The turn's goroutine claims its own run** (`UPDATE agent_runs SET status = 'running' … WHERE
status = 'queued'`) and settles it with the answer's content, the legacy call rows and the
conversation's recency in one write. A cancel before the claim settles the run cancelled from
queued; a claim that fails settles it failed through the same write. Startup fails every run still
queued, running or waiting for approval; nothing resumes a queued run.

**Ids are UUIDv7 and identity alone.** The transcript's order is `messages.seq`, a per-conversation
counter the send transaction assigns from `MAX(seq)`, serialized by the single writer; the list's
is `updated_at`. `oklog/ulid` is gone.

This amends [Keep chats in app.db](2026-09-10-chats-live-in-app-db.md), whose three tables and
ULIDs this replaces, and [The answer is a record](2026-09-10-the-answer-is-a-record.md), whose
"three rows" are now the message, the run and the answer rather than the message, the answer and a
ledger entry. Everything else in both stands.

## Alternatives considered

**Keep status on the message and add runs beside it.** Two places would say whether an answer is
done, and every write would have to keep them agreeing. The run is the one that will exist for
answers with no message (a monitor pass), so it is the one that carries the state.

**A replacement request ledger, with a foreign key this time.** The key is a fact about the
message the client posted; a second table holding it is the same fact twice. Losing the
outlives-the-chat property is the price, and it buys the simpler rule: the key survives exactly as
long as its message.

**A general worker queue claiming queued runs.** Nothing but the chat turn runs a run today, and a
scheduler that resumed queued runs after a restart would re-run a turn whose client has already
been told it failed. The claim is the turn's own until there is a second kind of run.

**Order the transcript by id.** UUIDv7 orders within one process; a clock moved back between runs
would file a new message before a persisted one. The counter needs no clock.

## Consequences

A conversation posted into from two devices would collide on `seq`; the counter belongs to the
writer that owns the conversation, and multi-device conversations are the cloud mirror's to
design. `effort` is stored NULL where the model has no such knob and read as `''`. `finishReason`
is the last legacy model call's on every run status, so a failed turn that finished a tool round
now reads `tool_use` where the column read `''` — the transcript draws *Stopped:* under Complete
alone, so nothing shows. `chat_model_call` and `chat_tool_call` remain, keyed by message id and
retargeted to `messages`, until the call-recording spec replaces them.

## Revisit when

A second kind of run lands. The monitor's runs have no message and no reservation, so the
claim-by-turn shape becomes a queue then, and `waiting_approval` gets its first writer.

The child agent's run is that second kind, and it did not need a queue: the goroutine that
runs it is the one inserting it, so it is written `running` with no claim
(*A child agent is a run under its parent's*).
The monitor's is still to come.
