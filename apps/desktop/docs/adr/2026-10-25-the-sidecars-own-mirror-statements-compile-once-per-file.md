---
title: The sidecar's own mirror statements compile once per file
date: 2026-10-25
scope: sidecar
status: Accepted
---

# The sidecar's own mirror statements compile once per file

## Context

The log service resolves a view through `kubestore.Store.ReadOwn` on every page and every follow
reconcile, several statements a resolve
(→ [a cache reading is one snapshot](2026-10-13-a-cache-reading-is-one-snapshot.md)). modernc keeps
no compiled statements, so each one was parsed again on every call, the query views expanded into
it. A profile of one resolve put 75% of its time in `sqlite3_prepare`, at about 1.2 ms a resolve.

`sqlstmt` compiles a service's statements once at open, from a fixed table
(→ [one statement set](2026-09-16-sqlstmt-prepares-a-services-statements.md)). The own reads cannot
take a fixed table: their texts are built per call, with a placeholder per node or source and a
clause per filter, and they live above `kubestore`, in `services/logs` and `tools`.

## Decision

**A file keeps a table of the statements its readings run** (`ownStmts`, `kubestore/query.go`),
keyed by the trimmed text and the row limit, each a `*sql.Stmt` on the own-query pool. `database/sql`
compiles a pool statement once on each connection it runs on and keeps that copy.

**A reading is a `database/sql` read-only transaction** on the connection `queryConn` takes, so a
pool statement can join it through `Tx.StmtContext`, rebound once a reading.

**A statement a reading meets first compiles for that reading alone.** Once the reading has let its
connection go, the same `ReadOwn` call compiles it on the pool. The table holds at most
`ownStmtsMax` statements; past that a statement compiles for each reading.

**The row limit is written into the wrap, not bound.** SQLite reads a bound `LIMIT` while
planning, so it compiles such a statement again on every run.

## Alternatives considered

- **Compile on the pool at the miss.** The reading already holds one of the pool's connections, and
  compiling on the pool waits for another. Readings holding every connection that all miss at once
  would wait on each other for good.
- **Bind the row limit, so one text serves every limit.** The issue that asked for this work
  proposed it. Measured, every run compiled again inside `sqlite3_step`, and a resolve took 0.78 ms
  instead of 0.23 ms. Callers pass a few fixed limits, so a key per limit costs little.
- **Cache compiled statements per driver connection, through `Conn.Raw`.** It works without a
  transaction, but it bypasses `database/sql`'s argument conversion and scanning, and it would have
  to notice a connection the pool closed.

## Consequences

A resolve takes about 0.23 ms instead of 1.2 ms. A statement's first reading compiles it twice,
once for itself and once for the pool, and its first run on each other connection once more.

Never compile on the own-query pool from inside a reading. KubeQuery's `Store.Query` keeps nothing:
a model's statement is new text each call, and its connections are not kept.
