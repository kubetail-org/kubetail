---
title: The cache store prepares every statement once and binds collections as JSON
date: 2026-09-02
scope: sidecar
status: Accepted
amended_by: [One statement set prepares and routes every store's statements](2026-09-16-sqlstmt-prepares-a-services-statements.md)
---

# The cache store prepares every statement once and binds collections as JSON

## Context

`internal/kubestore` is one SQLite file per cache (→ [one store per
cache](2026-08-26-cache-store-per-cache.md)) driven through `modernc.org/sqlite`, which compiles
and finalizes a statement on every `database/sql` call and caches nothing. A kind sync writes one
object per delta and a relist rewrites a whole collection, so the write path's per-call cost is
the cache's throughput. The read side re-reads on every change ping, so it must never queue
behind the single writer.

## Decision

**Every statement is named in `statements.go` and never written at a call site.** `stmtID`
indexes `stmtText`; a call is `st.exec(ctx, stmtUpsertObject, …)`. `openFile` prepares the set on
both pools, and `TestNoSQLTextLivesOutsideTheTable` keeps text from creeping back. The one
exemption is the stats read, which also serves a closed cache through a per-call read-only open
that has no prepared set. PRAGMAs are outside the scheme: they take no bound parameter, so they
are never prepared.

**Each pool holds its own half of the set.** The writer prepares the writes, the reader the reads.
`stmts` carries the file and an optional transaction (`f.stmts()` on the pool, `f.tx(tx)` inside
one) and rebinds through `Tx.StmtContext`. The helper does not pick the pool: `exec`/`query`/
`queryRow` say what shape the call has, `stmtWrites` says where it runs, so the prune's
`DELETE … RETURNING` goes through `query` and still runs on the writer. `stmtWrites` drives both
halves of its own enforcement, so a wrong entry is invisible until SQLite answers "attempt to
write a readonly database"; `TestEveryStatementDeclaresWhatItDoes` cross-checks each id against
its text. A read that must run inside a write transaction has no home here, and none exists.

**A collection is bound as one JSON argument.** The edge tables (`owner_refs`, `labels`) take one
`INSERT … SELECT … FROM json_each(?) WHERE true` per object, so the text is the same whatever the
object carries; text that varied with an argument count would be a fresh `sqlite3_prepare_v2` per
distinct count. The `SELECT` list follows the JSON's shape (array via `value ->> n`, object via
`json_each`'s `key`/`value`), `WHERE true` stops SQLite parsing the `ON CONFLICT` as a join
constraint, and the `len(…) > 0` guards are load-bearing because a nil marshals to `null` and
`json_each('null')` yields one all-NULL row. Floor is SQLite 3.38, for `->>`.

**The relist prune deletes `objects` first with `RETURNING uid`**, and the cascades take those
uids as one bound list, so the predicate is evaluated once. Parent-first is safe because nothing
references `objects`. The `RETURNING` cursor is drained to the end and its `Err()` checked: the
delete runs whether or not its rows are read, so a short read orphans every side-table row it
never saw, with no error from anywhere. `ClearKind` keeps its own statements and clears
`owner_refs` by `child_uid` only, since an edge is what the child says.

**A relist reconciles by mark and sweep on `updated_at`**, never on `generation` (the object's own
`metadata.generation`). Every write takes a strictly increasing stamp (`Store.stamp`): the clock
has millisecond resolution, so a relist in the same tick as the rows it supersedes would otherwise
keep every one of them.

**Reads ride their own pool.** Each `file` holds a reader pool beside the one-connection writer.
It opens through `sqlitepool.OpenReader`, whose DSN carries `query_only` and no `_txlock`:
the enforcement is the pragma, not the caller's `sql.TxOptions`, because a read transaction that
forgot `ReadOnly` would take the WAL write lock and contend as latency rather than fail as an
error. `sqlitepool` is the one home for the open contract: both pools keep every connection
until the idle timeout, and `OpenWriter`'s DSN sets `auto_vacuum=INCREMENTAL`, which a migration
cannot because SQLite ignores the pragma once any table exists.

## Alternatives considered

- **SQL text at call sites, relying on the driver's statement cache.** modernc has none. Measured
  as a recompile per call.
- **A run of placeholders per collection element.** One prepared statement per distinct element
  count, which for labels is unbounded.
- **Cascade deletes by re-evaluating the prune predicate per side table.** Four evaluations of a
  predicate the parent delete already answered.
- **Reconcile a relist on `generation`.** It is the object's field, not ours, and does not move
  on every write.
- **Enforce read-only via `sql.TxOptions{ReadOnly: true}`.** A forgotten flag contends silently.

## Consequences

Adding a statement is an entry in the table plus a `stmtWrites` flag, and two tests refuse the
common mistakes. Any statement that needs a read inside a write transaction is a design change,
not a flag flip. The JSON-binding shape is unusual enough that its three traps are documented
beside it and must be kept when a new edge table is added.
