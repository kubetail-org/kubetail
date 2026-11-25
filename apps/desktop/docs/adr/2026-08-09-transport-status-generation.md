---
title: Publish subscription connection status out of band, keyed to the host's open frame
date: 2026-08-09
scope: frontend
status: Accepted
---

# Publish subscription connection status out of band, keyed to the host's open frame

## Context

`subscribe-exchange.ts` reconnects dropped subscription transports invisibly to urql. But
consumers need to know two things urql can't tell them: whether the transport is up (to render
"connecting" vs "connected, empty"), and when a *new* connection has started (to discard
accumulated state — an object deleted during an outage is simply absent from the reconnect's
snapshot replay, so carried-over state would show ghosts). urql retains a `useSubscription`
accumulator across a variables change and a same-key pause/re-execute, which makes "is this
frame from the connection I accumulated under?" a real question.

## Decision

A per-operation transport-status registry (`src/lib/graphql/transport-status.ts`), keyed by
urql's `operation.key`, carries `connected` plus a `generation` — a **process-wide monotonic
serial** stamped at each successful connection. Process-wide rather than per-key so every open
across all operations is unique: a carried-over accumulator can never alias a new connection's
tag. The exchange calls `markConnected` on the host's **`open` frame** and `markDisconnected`
on every drop; the data channel carries only real GraphQL results — no synthetic frames.

Consumers use `useWatchSubscription` (`src/lib/graphql/use-watch-subscription.ts`), never raw
`useSubscription`. It is generation-gated two ways off the same counter, order-independent
between the side-channel notify and the sink frame: the reducer folds onto `undefined` when
the live generation differs from the accumulator's tag, and exposed `data` is masked to
`undefined` while the tag is stale. Reducers hold only domain logic.

**The generation bumps on the host's `open` frame, not the `graphql_subscribe` ack.** The
Rust bridge (`src-tauri/src/services/sidecar/graphql/subscribe.rs`) emits `{type:"open"}`
once per connection, ahead of the snapshot, *only after the SSE dial succeeds* — the host acks
the subscribe call **before** it dials, so an ack-driven reset would wrongly clear last-known
data on every failed retry during an outage (a failed dial emits `error`, never `open`). This
is the load-bearing detail: keying off the ack looks equivalent and is not.

## Alternatives considered

**A synthetic reset frame in the data channel.** Rejected: it forces every reducer to
special-case a non-domain frame, and ordering between reset and first real frame becomes each
consumer's problem. The out-of-band registry solves ordering once.

**Per-key generation counters.** Rejected: urql's retained accumulator across a key reuse
means two distinct connections could share generation 1 under the same key and alias.

**Keying reconnect-reset off the subscribe ack.** Rejected for the outage behavior above.

**Deriving "connecting" from `data === undefined` alone.** Rejected: it can't distinguish
"connecting" from "connected, empty snapshot" — a spinner needs `connected` separately.

## Consequences

Transport status is a transport-level signal decoupled from operation results, so a future web
deployment ports by feeding the same registry from a `graphql-ws`/`graphql-sse` client's
`connecting`/`connected`/`closed` events. The obligation: any new subscription consumer goes
through `useWatchSubscription`, and nobody re-keys the reset to the ack.
