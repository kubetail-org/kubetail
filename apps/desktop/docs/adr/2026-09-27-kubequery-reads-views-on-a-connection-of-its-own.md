---
title: KubeQuery reads views on a connection of its own
date: 2026-09-27
scope: sidecar
status: Accepted
---

# KubeQuery reads views on a connection of its own

## Context

KubeQuery runs a statement the model wrote over the chat's cluster's mirror, one SQLite file per
cache, without asking the user. The file is the store's, written by the cache's workers and read
by the watches. The statement is untrusted text: a model that read cluster data can be steered.
The store's tables are shaped for its writers and change as they do. modernc interrupts a
cancelled query only while it fetches the first row; `rows.Next` never looks at the context. Some
pragmas change every connection in the process, and SQLite applies them while preparing.

## Decision

**The views are the contract.** `kubestore/query_views.sql` creates `TEMP` views on each query
connection (`objects`, `labels`, `annotations`, `owners`, `ancestors`, `events`,
`status_history`, `kinds`), which the tool's prompt states. An unqualified name reads the view;
the tables below it may change. `objects.changed_at` records the last write that changed a row,
under the rule that keeps `write_seq`.

**The connection is the guard, not the text.** Each file owns a query pool
(`sqlitepool.OpenQuery`): `mode=ro` at open, through `kubestore`'s own `sqlite.Driver`, so
`body()` and the views reach query connections alone, and no idle connection, so nothing one
statement leaves reaches the next. `queryConn` sets `SQLITE_LIMIT_ATTACHED` to 0 and
`SQLITE_LIMIT_LENGTH` to 16 MiB.

**The wrap runs a query whole in its first step, and admits nothing but a query.**
`Store.Query` refuses a `;` left after a trailing one, prepares
`WITH q AS MATERIALIZED (SELECT * FROM (<sql>\n) LIMIT n+1) SELECT * FROM q` first, then the
statement alone, then runs the prepared wrap. A subquery can only be a query, so no pragma, attach
or DDL compiles. Preparing it alone keeps a stray `)` or a trailing `/*` from breaking out.
Materializing puts the whole statement in the one step modernc interrupts, so a deadline or a
file's close ends it. The tool refuses a `;` before the end as well, with a message naming
`char(59)`.

**Redaction is at the tool.** Every text cell is redacted as a command's output is. A cell holding
JSON is redacted by its structure (`safe.RedactJSON`): each string decoded and read line by line,
everything under a key naming a credential, and the value of an object named for one. Over the
compact text, a rule taking the rest of the line would take the rest of the body, and a line
inside an escaped string would be missed.

## Alternatives considered

- **The tables as the contract.** Every schema change would change the prompt, and the model would
  read compressed blobs.
- **A text check on the statement** (a leading `SELECT`, a keyword list). SQL has comments,
  quoting and CTEs a check has to model; the parser already knows what a subquery can hold.
- **Interrupting through `sqlite3_interrupt` from our side.** modernc does not export the handle.
  Materializing costs holding at most `limit + 1` rows before the first is answered.
- **Preparing the statement alone first.** SQLite applies `temp_store_directory` and
  `soft_heap_limit` while preparing them, so the wrap, which refuses any pragma, goes first.
- **Capping `temp_store` and `cache_size` on query connections.** Temp data already goes to disk,
  and `cache_size` does not bound a materialized subquery, so neither caps the temp disk a statement
  fills; `temp_store=MEMORY` moves it into RAM. A VFS that refuses to grow a temp file past a size
  would bound it, at the cost of a VFS of our own.
- **Redacting the rendered text with `safe.Redact`.** It cuts a body after the first match and
  misses escaped lines.

## Consequences

- A column the statement repeats comes back as `name:1`.
- The views are a promise: a later step adds views, and the prompt changes with them.
- A query holds a read transaction up to its deadline, which delays a checkpoint, not a writer.
- The deadline alone bounds a statement's temp disk: a few hundred MB a call before it ends. The
  row and byte limits trim the answer, not the work.
- The connection and the wrap are each enforced by tests of their own, so neither hides a
  regression in the other.

## Revisit when

- modernc interrupts every step, or exposes the handle: the wrap's materialization can go, and
  the row limit can stop the work again.
- A call's temp disk matters more than a few hundred MB: a temp-file cap in a VFS of our own.
