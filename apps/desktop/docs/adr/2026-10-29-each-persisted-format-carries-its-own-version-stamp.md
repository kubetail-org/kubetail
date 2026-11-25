---
title: Each persisted format carries its own version stamp, and a downgrade is refused only where it would break
date: 2026-10-29
scope: cross-cutting
status: Accepted
amended_by:
  - [A shipped schema changes only by a new migration](2026-11-08-a-shipped-schema-changes-only-by-a-new-migration.md)
  - [Keep every setting as rows in app.db](2026-11-24-settings-are-rows-in-app-db.md)
---

# Each persisted format carries its own version stamp, and a downgrade is refused only where it would break

## Context

Kubetail keeps user data in several formats: three SQLite databases of its own (`app.db`, the
cluster caches, the log caches), beehive's `beehive.db`, two JSON files (`settings.json`,
`host.json`), JSON inside database columns, a directory layout under `chats/` and `monitor/`, a
keyring entry and the webview's `localStorage`. Once a release ships, the next release has to read
all of it. There was no single list of what is persisted, no stated class for each format, and no
rule for what a build does with a file a newer build wrote.

What already existed: `sqlitemigrate` runs numbered SQL files, records the version in the file and
refuses a file recorded newer than the binary. beehive's runner does the same for `beehive.db`.
`jsonsettings` stamps `schemaVersion` and keeps the keys and the version of a file a newer build
wrote. `host.json` stamps `schemaVersion` and reads a bad file as defaults. Nothing has shipped, so
every `0001_init.sql` is still edited in place
(→ ADR).

## Decision

**Every format has a class, recorded in [`docs/persisted-data.md`](../persisted-data.md).** A
*durable* format is user data: it carries a version, migrates forward from every shipped version,
and a file a newer Kubetail wrote is refused with a message naming the file and both versions. A
*preference* is cheap to lose: a newer file is read, and a write keeps what this build does not
know. A *disposable* store is one Kubetail rebuilds; a bad value reads as the default.

**The caches are durable.** They hold history the cluster no longer has: events past their TTL,
status transitions, rotated log lines. A cache is refused the way `app.db` is, but the refusal is
scoped to that cache and surfaces through its health gauge, so the app and every other cluster
keep running.

**Each format's own stamp is the enforcement.** `sqlitemigrate` for `app.db`, `kube/*.db` and
`logs/*.db`; beehive's runner for `beehive.db`; `schemaVersion` for `settings.json` and
`host.json`. The JSON columns and the chat directory layout ride `app.db`'s version: a change to a
block shape or to the layout ships as an `app.db` migration. There is no app-level version marker
and no app-level migration step; every format migrates when it opens.

**A downgrade is unsupported, and is refused only where it would break.** A build whose formats
match the files on disk opens them, whatever its version.

**The chat directory layout only grows, and the data directory never moves.** Transcripts name
absolute paths in free text that no migration can rewrite.

**`0001_init.sql` freezes at the first release.** Until then a schema change is an edit in place,
and no stamp moves, so no mismatch is detectable. The freeze, a v1 fixture per durable store, and a
backup before the first migration past `0001` belong to that release.

## Alternatives considered

**A version marker in the data directory, recording the highest Kubetail that opened it.** One
check for every format, with a dev-build exemption and a host dialog. Rejected: it refuses every
downgrade, including a reinstall of the previous build after a bad release, which is the one
downgrade a user needs and which the stamps allow when no format moved. It cannot reach `host.json`,
the keyring or `localStorage`, which live outside the data directory. And every durable format
still needs its own stamp to migrate, so the marker is a second refusal in front of a first one.

**Disposable caches, discarded on any mismatch.** One `os.Remove` and a reopen, and no cache
migration ever. Rejected because the caches hold history the cluster cannot give back.

**Go migration steps in `sqlitemigrate`, now.** A numbered step that can be Go as well as SQL,
for the JSON columns and the chat directories. Deferred: nothing has shipped, so no migration
exists, and SQLite's JSON functions cover many column rewrites in SQL. The steps land in the PR
that writes the first migration SQL cannot express.

**Refusing a newer `settings.json`.** The policy for a durable format. Rejected: `jsonsettings`
already keeps the unknown keys and the higher version through a write, which is the preference
behaviour, and stopping the sidecar over a settings file would lose more than it protects.

**Fixing `host.json`'s overwrite.** An older build rewrites a newer file with only the fields it
knows. Accepted as a gap: the file holds the colour scheme, and the fix is small if that changes.

## Consequences

Every schema change after the first release is a numbered migration, for the caches as much as for
`app.db`. The rename trap the 2026-08-29 ADR records applies to `kubestore`'s triggers from then
on. A migration past `0001` touches every cache file at its open, one per cluster cache and one per
log cache, so a slow migration delays that cluster, not the app.

A change to a persisted format updates its row in the register in the same commit; the root
`CLAUDE.md` requires it.

A refusal today fails the sidecar's start with a migrate error and no path to the user. Carrying
the message to the start screen, and pinning each store's refusal with a test, is the open work on
[#512](https://github.com/kstackhq/kstack/issues/512).

## Revisit when

A downgrade corrupts data the stamps did not refuse. That is the case the marker exists for, and
the moment to add it, with a concrete file to test against.
