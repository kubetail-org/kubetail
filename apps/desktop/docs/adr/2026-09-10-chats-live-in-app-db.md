---
title: Keep chats in app.db
date: 2026-09-10
scope: sidecar
status: Accepted
amended_by:
  - [The app owns app.db and hands it to its services](2026-09-16-the-app-owns-app-db.md)
  - [A turn is a run over its message, filed in one transaction](2026-09-17-a-turn-is-a-run-over-its-message.md)
---

# Keep chats in app.db

## Context

The app needed somewhere to keep conversations with the assistant: the chats, their messages, and
a small ledger of accepted sends. Four places could hold them, and each was already in use for
something.

The requirement that decides it is that **a chat belongs to the app, not to a window**. Two
windows can be looking at one chat, a window can be closed mid-answer, and an answer that was
being written when the app quit has to still be there at the next launch. The sidecar is the only
process that outlives every window, and `internal/appdb` already owned a durable, migrated SQLite
file — `<data-dir>/app.db` — that nothing had yet put a table in.

## Decision

Three tables in `app.db`: `chat`, `chat_message`, `chat_send`. They go into the existing
`0001_init.sql` rather than a new `0002` — nothing has shipped, so the initial schema is still the
whole schema (→ schema edit, not migration).

`internal/services/chat` owns the file: `chat.New(dataDir, provider)` opens it itself, the way
`cluster.New` opens its own store, so there is one owner and no handoff. Ids are ULIDs
(`github.com/oklog/ulid/v2`) and timestamps are unix millis, which is what would let chats ride
the existing `cloud/mutationqueue` and `cloud/syncstore` machinery later without a rewrite.

## Alternatives considered

**The webview's `localStorage`.** It is per-origin and never synchronises between windows, and it
is unreachable from the sidecar — so the process that writes an answer could not write it where
the answer is kept. Two windows on one chat is the ordinary case, not the exotic one.

**`host.json`.** That file is the settings source of truth (→ [host.json
settings](2026-08-09-host-json-settings.md)), and it is rewritten whole on every change. A
transcript that grows by a chunk every few hundred milliseconds would rewrite the user's settings
file thousands of times per answer.

**A per-cluster cache file.** `clusterCacheClear` deletes those, and a chat is not about one
cluster. The user pressing "clear cache" must not take their conversations with it.

**A new `0002` migration.** It would exist only to add what the first file can say correctly the
first time, and would leave a migration in the sequence that never ran anywhere.

## Consequences

`app.db` now holds cluster-derived text in the clear — a user can paste anything from a cluster
into a chat, and the fake provider is enough to make that worth doing. The file's mode is the
protection (owner-only, like every other SQLite file we open), and cloud sign-out does not clear
it. [`docs/security-model.md`](../security-model.md) carries the row.

The pool is one connection (`appdb.Open` calls `OpenPool(path, 1)`), so nothing may hold a
transaction open across a provider call, and the turns mutex is never held across a database call.
Both are correctness rules rather than latency ones: a holder that then needed the connection
would deadlock the service.

Nothing bounds the tables yet — no automatic deletion of old chats.

## Revisit when

Chats need to sync between machines. The ULIDs and millis are there for it, but the sync path
itself would decide whether `app.db` is still the right owner.
