---
title: A shipped schema changes only by a new migration
date: 2026-11-08
scope: cross-cutting
status: Accepted
---

# A shipped schema changes only by a new migration

## Context

`desktop/v0.0.1` shipped on 2026-11-08. Until then every SQLite schema was one `0001_init.sql`,
edited in place (→ ADR: schema edit, not migration),
and the [version-stamp ADR](2026-10-29-each-persisted-format-carries-its-own-version-stamp.md)
left three things to "the first release": freezing those files, a fixture per durable store, and
a backup before the first migration past `0001`. The first is this ADR; the fixture is answered
below, and the backup is [#823](https://github.com/kstackhq/kstack/issues/823). From now on an edited `0001_init.sql` leaves
every installed `app.db`, `kube/*.db` and `logs/*.db` on a schema the binary does not match, and
nothing reports it: `sqlitemigrate` sees version 1 recorded and version 1 embedded, and skips.

The version-stamp ADR also says the chat directory layout rides `app.db`'s version, and it defers
Go steps in `sqlitemigrate` to the first migration SQL cannot express. The [directory
ADR](2026-09-27-kubetails-files-follow-the-platforms-directory-kinds.md) says "Nothing migrates:
nothing has shipped". So nothing today can move a path under `<data>` or `<cache>` on upgrade.

This ADR supersedes the 2026-08-29 ADR and replaces the version-stamp ADR's paragraph on the first
release.

## Decision

**A shipped migration is never edited or removed; a change is the next numbered file.** Each
store's test holds every file in its `migrations/` by SHA-256 (`shippedMigrations` in
`appdb_test.go`, `kubestore/schema_test.go` and `logstore/schema_test.go`, over
`sqlitemigratetest.Checksums`) and fails when a file changes, appears or goes. A new migration
adds its line in the PR that writes it. Once a desktop release carries it, every installed file
has run it, so the line never changes again.

**A migration past `0001` ships with a test that migrates the schema before it.** The test
builds a database from the frozen files up to the previous version, seeds it, applies the new
migration and reads the seed back. The frozen files are the old release's schema, so there is no
fixture file to keep: the test is Go, and the checksums are what make it honest. `sqlitemigrate`
gains a way to apply up to a given version when the first such test needs one. `beehive.db`'s
compatibility is beehive's own tests' to pin, taken by a module bump.

**There is no app-level data migration yet, so the layout under the roots only grows.** A SQL
migration changes tables. Nothing moves, renames or removes a path under `<data>` or `<cache>`,
and nothing rewrites a JSON column in Go. A new path needs nothing, since its owner creates it; an
existing path keeps its name and its root until the mechanism exists. It is in `docs/TODO.md`,
with the open question of where its position lives.

## Alternatives considered

**Make `app.db`'s sequence the app's data version now, with Go steps beside the SQL files.** One
ordered list for tables and paths, and every build back to `v0.0.1` already refuses a higher
version. Deferred: it ties the layout's position to a file the host cannot read and a later
release might move or remove, and an older build that finds no `app.db` makes a fresh one rather
than refusing. No change needs a step yet, so the choice waits for the first one, with the TODO
recording both options.

**Guard the freeze from git, against the newest `desktop/v*` tag.** Exact about which files a
release carried, where the checksum list relies on its comment. Rejected: the test would need the
repository's tags, which a shallow CI checkout or a source tarball lacks, and a test that passes
without them guards nothing.

**A binary fixture per shipped version, written by the release's build at its tag.** The file
is what the release wrote, page size and pragmas included, where a database the test builds from
the frozen SQL carries only what the SQL says. Rejected: it is a writer test and a flag per store,
a manual step per release that nothing checks, and a binary no review can read, kept forever
because a user may skip releases. No migration here depends on file-level state, and the frozen
SQL is the release's schema, so the Go test guards the same thing. If a migration ever does
depend on such state, its test can set it the way the DSN does.

## Consequences

Every schema change is a migration, so the rename trap the 2026-08-29 ADR records applies to
`kubestore`'s triggers. A migration touches each cache file at its open, so a slow one delays that
cluster, not the app.

A PR that edits a shipped SQL file fails its store's test. Its author can update the hash, so the
comment on `shippedMigrations` and review are what hold the line.

A path under `<data>` or `<cache>` cannot be renamed or moved, and a JSON rewrite SQL cannot
express cannot ship, until the app-level mechanism lands.

The backup before a migration is [#823](https://github.com/kstackhq/kstack/issues/823), and `host.json`'s accepted gap is [#824](https://github.com/kstackhq/kstack/issues/824). `app.db` migrates
in place until [#823](https://github.com/kstackhq/kstack/issues/823) lands, so its first migration past `0001` waits for it.

`appdb`'s `0001_init.sql` changed its header comment in the PR that froze it, so the checksum is
of that file, not of `v0.0.1`'s byte for byte. Its SQL is `v0.0.1`'s.

## Revisit when

The first change needs a path moved, renamed or removed, or a JSON rewrite SQL cannot express.
