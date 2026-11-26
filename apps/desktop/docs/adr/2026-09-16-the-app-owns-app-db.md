---
title: The app owns app.db and hands it to its services
date: 2026-09-16
scope: sidecar
status: Accepted
---

# The app owns app.db and hands it to its services

## Context

`services/chat` opened `app.db` itself: it was the one service with a table in the file, so it
called `appdb.Open`, closed the file in its own `Close`, and held the change hub its two watches
subscribed to (→ [chats live in app.db](2026-09-10-chats-live-in-app-db.md)). The schema plan
(`docs/specs/appdb.sql`, since retired into `sidecar/internal/appdb/migrations/0001_init.sql`) puts cluster records in the same file, with conversations keyed onto
them: a second service over one SQLite file, whose watchers need to hear each other's commits.

## Decision

`internal/app` opens `<data-dir>/app.db` once and hands the `*appdb.DB` to every service over
it. A service prepares its statements on the pools it is given and closes only those. The DB is
first in `App.parts`, with nothing to start and `db.Close` to close, so the reverse close order
releases the file after every service has stopped and closed. A constructor failing after the
open closes the file before `New` returns, and a failed `Start` in `main.run` reaches
`application.Close`, since `lifecycle.StartAll` unwinds what started but never closes.

The change hub moves with the file: `DB.Notify(key)` and `DB.Subscribe(keys…)` are the one bus
every writer and watcher over `app.db` uses, closed in `DB.Close` after the janitor joins and
before the pools release. The keys stay the writer's vocabulary (`chats`, `messages/<id>`,
`stream/<id>`); the DB owns the transport, not the meaning.

Two leaves land with it. `appdb.NewID` and `appdb.ValidateUUID` mint and validate the UUID ids the
target schema uses, in the package that owns the file, so every table's ids come from one
generator. `internal/lib/deltafold` is the watch fold `services/chat` had (`Snapshot`/`Diff`/`Upsert`/
`Has`), with equality a parameter so a record holding a slice can be folded too.

## Alternatives considered

**Each service opens the file.** Two `appdb.Open`s on one path is two migration runs, two
janitors and two hubs, and a cluster watcher would never hear a chat commit. The one-owner rule
that had `services/chat` open the file still holds; the owner is now the composition root.

**A repository layer over the DB.** Table-agnostic accessors would put the SQL discipline
(named statements on `onWriter`/`onReader`/`onBoth`) behind an abstraction that then has to
re-express it. Services keep their statements; only the pools and the hub are shared.

**The hub as its own leaf, injected beside the DB.** A bus with no SQLite behind it tests
without a file, and `stream/<id>` is an in-memory overlay invalidation rather than a commit. But
what that key invalidates is a read of this file with the overlay applied, and its subscriber
takes it on the same receiver as the rows key. Every consumer would carry two values that are
created together, closed in one fixed order and cleaned up together on a constructor failure —
`App.parts` would hold the order and nothing would check it. `kubestore` keeps its ping bus
inside the store for the same reason (→ [ping bus](2026-08-26-store-change-ping-bus.md)).

**ULIDs stay.** The target schema stores UUIDv7 in the canonical 36-character form, and a client
request key is a UUID it minted; one parser has to validate both. `google/uuid` serializes its v7
clock, so a process's ids increase in minted order without a wrapper of our own.

## Consequences

`services/chat` knows no path: a test builds a service over a temporary `appdb.DB` of its own, and
simulates a failed store by closing the DB, not the store. A
service's `Close` releases statements alone, so closing one service never invalidates another's
pools. The version stamp (`internal/lib/version.Version`, set with `-X` by `scripts/build-sidecar.go`
under `SIDECAR_VERSION`) lands beside this because the schema plan stores the writing build on a
run, and nothing may read it from the environment.

The invariant to keep: every service joins its own pumps in its stop, before the hub and the DB
close. The hub closing is what ends a receiver, and a pump reading past it reads closed pools.
`App.parts` order is the guarantee.
