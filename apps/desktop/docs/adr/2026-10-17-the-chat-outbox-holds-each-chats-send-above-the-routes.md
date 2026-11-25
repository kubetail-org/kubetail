---
title: Hold each chat's draft and send in an outbox above the routes
date: 2026-10-17
scope: frontend
status: Accepted
---

# Hold each chat's draft and send in an outbox above the routes

## Context

This decision predates this file; it was recorded in the root `CLAUDE.md` while the chat composer
was built (September 2026) and moved here when that file was compacted.

The composer unmounts under a send in flight: a first send moves chat mode to the new chat's
route, and a chat deleted in another window takes its pane down. A send must still settle
somewhere. `chatSend` is idempotent on its `requestID` (→
[the answer is a record](2026-09-10-the-answer-is-a-record.md)), and a dropped response cannot be
told apart from a send that never arrived.

## Decision

`src/lib/chat-outbox.tsx` holds one entry per chat, keyed by chat id or `new:<mode>`: the draft,
the pick (model and effort), the one-message network toggle, and one `Send` value
(`idle | sending | held | awaiting`). Its provider is mounted at `AppLayout`, above the routes.
`useChatOutbox` is the whole API.

- A send settles into the key it left from. Nothing moves between keys but `moveDraft`.
- A GraphQL error is a refusal: the entry returns to `idle`, the draft is kept, and the next
  submit mints a new `requestID`.
- A `networkError` holds the send — id, content, cluster, pick, switches, toggle — behind
  Retry/Discard. Retry resends exactly that, under the same id, and the sidecar answers a repeat
  by its key.
- Ask again, on a failed answer, is a new send under a new id carrying the failed row's question
  and pick. It does not touch the draft.
- `submit`/`retry` resolve to the created chat and its cluster; the composer follows only while
  mounted and still on that cluster.

## Alternatives considered

**State in the composer.** Lost when the composer unmounts, which is exactly when a first send
lands.

**Retry as a fresh send.** A committed-but-lost send would then run twice. Retrying under the
window's current cluster or pick would send something other than what the user sent.

**Following a created chat whenever the composer is mounted.** The `new:` composer outlives a
cluster switch, so it would open the chat under a cluster it does not belong to.

## Consequences

One `Send` value means a held send cannot also be settled. A press while anything is in flight
sends nothing, Ask again included. The composer must call `settle(seq)` when the messages watch
delivers the row an `awaiting` send names, or the entry never returns to idle.
