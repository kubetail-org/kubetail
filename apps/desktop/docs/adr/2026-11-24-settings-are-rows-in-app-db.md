---
title: Keep every setting as rows in app.db
date: 2026-11-24
scope: sidecar
status: Accepted
---

# Keep every setting as rows in app.db

## Context

The settings-file ADR kept the sidecar's
settings in `<data>/settings.json`, a file the user could edit by hand. Nobody did: every value in
it was written by Kubetail, from a Settings mutation or the `PATH` sync. Most of the code around it,
`internal/lib/jsonsettings`, existed only to survive a hand edit: held fields, the raw JSON kept
for a refused value, unknown keys written back, and `strictest`, which read a refused restriction
as its most restrictive state.

Other settings were already rows in `app.db`
([a setting with a cluster scope](2026-11-20-a-setting-with-scopes-is-rows.md),
[tool settings](2026-11-21-tool-settings-are-rows-in-app-db.md)), each in a typed table of its
own. Both of those ADRs say a field that shipped in `settings.json` cannot move into `app.db`,
because a release must keep reading what it wrote.

A typed table fits a setting with structure. For a single value it costs more with each one
added: a migration and its test per setting, a prepared statement per column, a fallback from a
cluster to the default written again by each setting with scopes, and a default written into the
row, so a later build cannot change it and a value never touched reads the same as one the user
chose. Most of the settings still to come are single values.

`desktop/v0.0.1` is the one release, and Kubetail has no users who are owed its data. The
maintainer decided to move the file's settings without an import ([#1127](https://github.com/kstackhq/kstack/issues/1127)).

## Decision

**Every setting the sidecar keeps is a row in `app.db`** (migration `0006`). Nothing reads or
writes `settings.json`, and `internal/lib/jsonsettings` is gone.

**A single-value setting is a row of `settings`**: a dotted `key`, a scope (`cluster_id` NULL for
every cluster) and a `value` of `ANY` kind, an integer, text or NULL. The cloud platform switches,
the `PATH` setting, the approval modes and the tool settings are kept there. A setting belongs in
`settings` when all three hold: it is one value, read and written whole; a value Kubetail cannot use
can cost a permission, never grant one, because its entry names a strictest value that such a
value reads as, or its reader stops on it; and the code names it, not the user.

- **The code holds each setting's entry** (`services/settings/scalar.go`, `Entry[T]`): its key,
  default, strictest value, whether a cluster may have its own, and check. No row, or a NULL, is
  the default, so a later build can change it. A reset writes NULL; a row goes only with its
  cluster.
- **A value Kubetail cannot use reads as its strictest value, else its default**, logged by key and
  reason, never by value, and is not listed. Choosing in Settings writes over it. The `PATH` is
  the exception: its reader lists the refusal and starts no run.
- **A JSON value is opaque to SQL** and decodes into the entry's Go type, refusing an unknown
  field.

**A setting with structure is a typed row of its own table**: the Kubernetes rules, the folder
grants ([folder grants are rows](2026-11-24-a-folder-grant-is-its-own-row-and-a-mode-and-a-rule-belong-to-a-cluster.md))
and the registered executables. Each value has its own typed column, with a `CHECK` for a fixed
set of values, and no JSON column. A row Kubetail cannot use is refused: it stays in its table,
Settings lists it with its reason, and the user removes it there. A remove always works on a
refused row.

In either kind, a value Kubetail cannot read may cost a permission, never a restriction, and **a
failed read is an error, never an empty result**. The operation that needed it stops. A decision
that must still be answered gets the strictest answer. A read that fails partway is a failed read.

**Nothing is imported.** `settings.json`, written by `desktop/v0.0.1`, is dropped by the
maintainer's decision, as are `chat_grants` and `tool_settings`, which migration `0006` drops
without copying their rows. No build deletes `settings.json`; it stays on disk, unread.

An install of `desktop/v0.0.1` loses, on upgrade, its approval modes, its always rules (deny and
ask rules included), its folder grants, its registered executables, its `PATH` decisions and
every chat's grants. Each returns to its default.

This replaces paragraphs of four earlier ADRs:

- [A setting with a cluster scope is rows](2026-11-20-a-setting-with-scopes-is-rows.md) and
  [tool settings are rows in app.db](2026-11-21-tool-settings-are-rows-in-app-db.md): "nothing
  can move it into `app.db`" and the user's need to edit a setting by hand no longer apply, and
  the tables they name for the approval modes and the tool settings are rows of `settings`.
- [Every cloud platform is on until the user turns it off](2026-11-18-every-cloud-platform-is-on-until-the-user-turns-it-off.md):
  *A refused value reads off* described `strictest`. Now a switch Kubetail cannot use, or whose read
  fails, reads off: a run whose switches cannot be read reaches no cloud platform.
- [Each persisted format carries its own version stamp](2026-10-29-each-persisted-format-carries-its-own-version-stamp.md):
  `settings.json`'s `schemaVersion` no longer applies, and `host.json` is the one preference file.

## Alternatives considered

**Import the file once, then delete the reader.** It keeps an install's settings, but every
format it reads must still be parsed and tested, and no user is owed the data.

**Keep the file for global switches only.** Two stores for one kind of thing, and the hand-edit
machinery stays for a few booleans.

**A typed table per single-value setting.** Every new single value repays the migration, the
statements and the scope fallback, and keeps its default in the row.

**One `settings` table with a column per setting.** A setting retired after a release still
leaves a column, and a new one is still a migration.

**List a single value Kubetail cannot use, as a typed table's row is.** Each setting would need its
own refusal on the wire and a control to show it. Reading it as its strictest value is already
safe, and the next write repairs the row.

## Consequences

The user cannot edit a setting by hand; Settings is the only write path. Adding a single-value
setting is an entry, its methods, its wire field and its control: no migration and no statement.
A setting with structure is an ordinary numbered `app.db` migration. The persisted-data register's
rule that every `desktop/v0.0.1` format stays readable holds for everything but the two formats
dropped here.

A single value Kubetail cannot use is refused at the read, as its strictest value, never at the
write. A missing row is the default, not a failed read: every switch on, `$PATH`. A JSON value
written by a newer build, with a field an older build does not know, reads there as the default
and is lost at that build's next write. `created_at` and `updated_at` are not a sync design.

## Revisit when

A single-value setting needs its parts read, written or queried apart, or a value Kubetail cannot
use must be shown to the user rather than read as its strictest value.
