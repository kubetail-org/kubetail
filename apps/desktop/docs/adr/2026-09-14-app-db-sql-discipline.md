---
title: app.db prepares every statement once and reads on its own pool
date: 2026-09-14
scope: sidecar
status: Accepted
amended_by: [One statement set prepares and routes every store's statements](2026-09-16-sqlstmt-prepares-a-services-statements.md)
---

# app.db prepares every statement once and reads on its own pool

## Context

`app.db` opened as one `*sql.DB` capped at a single connection, and `services/chat/store.go` handed
SQL text to it on every call. Two costs followed, both invisible with one window open and both
real past it. Every read waited for the writer: `WatchList` and `WatchMessages` re-read on every
ping, each window holds one of each, and with one connection those re-reads serialized with each
other and with every write — a checkpoint every three seconds, a settle, a delete. And every
call compiled its statement: `modernc.org/sqlite` caches nothing, so a send's eight statements
and every watch's re-read each paid a full `sqlite3_prepare_v2`.

The cache files already had the answer (→ [the cache store prepares every statement
once](2026-09-02-kubestore-sql-discipline.md)), with one gap: that ADR says a read that must run
inside a write transaction "has no home here, and none exists", and reserves it as a design
change. `services/chat`'s send does exactly that, on purpose — the ledger is re-read inside the
inserting transaction, the chat's existence is checked there, `nextSeq` allocates under the
write lock, and `newestCard` compares the card beside the rows.

## Decision

**`appdb.Open` returns two pools.** `appdb.DB{Write, Read}`: the writer is
`sqlitemigrate.OpenPool(path, 1)`, migrated, and the reader `OpenReadPool(path, 4)`,
`query_only` and without `_txlock`, opened after the migration. The reader's size lives in
`appdb`, since `appdb` opens the file.

**`services/chat` carries the cache store's discipline over**, in `statements.go`: `stmtID` indexes
`stmtText`, the set is prepared at open on a `store` (`store.go`) that owns both pools, and
`TestNoSQLTextLivesOutsideTheTable` keeps text out of the helpers. The scan matches
case-sensitively: every statement is upper-case and the helpers' error wraps open with the same
verbs in lower case.

**The declaration is three-way, not a bool.** `stmtPools` says `onWriter`, `onReader`, or
`onBoth` — a read some caller runs inside a write transaction, prepared on both pools.
`stmts.stmt` routes by it: inside a transaction the writer's copy, rebound through
`Tx.StmtContext` once per id; on the pools the reader's copy when there is one. `inTx` hands out
a `stmts`, never a `*sql.Tx`, so nothing can pass a transaction to a read prepared elsewhere.
Every read on the pools — the watches, `Get`, `List`, `Replay`, a send's pre-transaction ledger
read — lands on the reader; `checkpoint` is the one pool-level write and lands on the writer.

## Alternatives considered

- **Keep the bool and move the in-transaction reads to the reader.** They would read a snapshot
  the transaction's own writes are not in: `nextSeq` would hand out a taken seq, and the ledger
  re-read would miss the retry the wait let in.
- **Keep the bool and prepare every read on the writer.** Every watch's re-read then queues
  behind the single write connection — the queuing the reader pool exists to prevent.
- **Route by the call's shape (`queryRow` on the writer, `query` on the reader).** The shape and
  the pool are separate facts: `stmtRenameChat` returns rows and writes.

## Consequences

A watcher's re-read no longer waits for the checkpoint a turn is writing, and no statement is
compiled twice on one connection. Adding a statement is an entry in the table plus a pool
declaration; the tests refuse a write misfiled as a read, and an `onReader` id reached inside a
transaction fails its first call with `statement from different database used`, which the
service tests reach by driving every in-transaction path. The other direction — a read declared
`onBoth` that nothing runs in a transaction — costs one prepared statement on the reader and is
not guarded. `Send`'s pre-transaction reads are a reader's snapshot now, which its own design
already tolerated: the inserting transaction reads the ledger again.

The turns-mutex rule is unchanged — never held across a database call — because the writer
still has one connection. [Keep chats in app.db](2026-09-10-chats-live-in-app-db.md) says "the
pool is one connection"; it stays as written, and `sidecar/CLAUDE.md` states the present.
