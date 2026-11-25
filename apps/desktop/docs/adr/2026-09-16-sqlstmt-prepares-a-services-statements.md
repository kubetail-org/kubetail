---
title: One statement set prepares and routes every store's statements
date: 2026-09-16
scope: sidecar
status: Accepted
---

# One statement set prepares and routes every store's statements

## Context

Two packages carried the same machinery. `services/chat/statements.go` prepared the chat statements on
`app.db`'s two pools and routed each call to the reader's copy, the writer's copy, or a rebinding
onto the open transaction (→ [app.db SQL discipline](2026-09-14-app-db-sql-discipline.md)).
`kubestore/statements.go` did the same for each cache file, with a two-way declaration and no
read inside a write transaction (→ [the cache store's SQL
discipline](2026-09-02-kubestore-sql-discipline.md)). The next service over `app.db`, the
clusters table ([a cluster is a row in app.db](2026-09-16-clusters-are-rows-mirrored-into-beehive.md)),
would have been a third copy. The rebinding cache and the
pool rule are the subtle part, and they were about to be written again.

## Decision

The machinery is one leaf, `internal/lib/sqlstmt`, beside `sqlitemigrate`. A store declares its table
as `[]sqlstmt.Statement`, each entry the text and its pool (`OnWriter`, `OnReader`, `OnBoth`),
indexed by the store's own id type. `sqlstmt.Prepare[ID](ctx, write, read, table)` compiles the
table on both pools at open and hands back a `Set[ID]`; `Set.Stmts()` issues on the pools,
`Set.InTx` inside one write transaction, `Set.InReadTx` inside one read-only transaction on the
reader, and each gives the caller a `Stmts[ID]` whose `Exec`/`Query`/`QueryRow` route by the table.
Inside a transaction the copy rebound is the one prepared on that transaction's pool, once per
id. `Set.Close` finalizes the statements alone; the pools stay the caller's.

`services/chat` prepares on `db.Write, db.Read` and declares its in-transaction reads `OnBoth`.
`kubestore` prepares on each file's pools, declares nothing `OnBoth`, and takes its snapshot
reads through `InReadTx`. Both keep every line of SQL and every row function. What is shared is
how a statement reaches a pool, not what it says.

## Alternatives considered

**Copy the machinery into each store.** Three copies of one routing rule, each with its own
tests, and a bug fixed in one.

**Keep it in `appdb`.** `appdb` is the app's one file, and a cache file has no `*appdb.DB` to
hand it. A leaf that takes two `*sql.DB`s serves both.

**A repository layer with table-agnostic accessors.** Rejected in [the app owns
app.db](2026-09-16-the-app-owns-app-db.md) and still rejected: the SQL stays the store's.

**Index by `int`.** `Set[ID ~int]` keeps a call site typed (`st.Exec(ctx, stmtInsertChat, …)`),
and a slice indexed by a `~int` type parameter is ordinary Go.

## Consequences

The pool sits beside the text in one table, so the separate hand-maintained pools tables and
their "a write filed as a read" trap are gone; `Prepare` refuses an id the table left without
text, and an id reached inside a transaction whose pool does not hold it panics at the first
call. `services/chat` has no `store` type: its `store.go` is the row functions over a `stmts`, which is
`sqlstmt.Stmts[stmtID]`. `kubestore`'s eight hand-opened transactions are `InTx` and `InReadTx`
closures. The tests of the routing run in `sqlstmt` against a table of their own and no store.
This amends the "each pool holds its own half" paragraph of the cache store's ADR and the
"`services/chat` carries the cache store's discipline over" paragraph of `app.db`'s.
