---
title: Keep a setting with a cluster scope as rows in app.db
date: 2026-11-20
scope: sidecar
status: Accepted
amended_by: [Keep every setting as rows in app.db](2026-11-24-settings-are-rows-in-app-db.md)
---

# Keep a setting with a cluster scope as rows in app.db

## Context

The user's variables for their commands ([#1080](https://github.com/kstackhq/kstack/issues/1080)) were first written as `sandbox.env`, a list in
`settings.json`, beside the frozen `PATH` and the registered executables, as the settings
ADR places every user setting. Nothing had
shipped.

A variable can belong to one cluster. A cluster's variables must go when the cluster goes. In
`app.db` that is a foreign key with `ON DELETE CASCADE`, as `memories` has; in a file it is a
sweep someone must remember to write and run.

`settings.json`'s protection for a hand edit is refuse-and-hold: a value Kubetail cannot read sets
its field to the most restrictive state until the user fixes the file. A variable only adds to
what a command holds, so its strictest state is the empty list, which a refusal already gives.
The file's guarantees bought nothing for this setting.

Once a desktop release carries a field in `settings.json`, nothing can move it into `app.db`:
there is no app-level data migration ([persisted data](../persisted-data.md), *What cannot change
yet*). The choice had to be made before the first release that holds the variables.

Kubetail Cloud will sync some settings. Rows with ids and timestamps merge more easily than a list
in a file. No sync is designed here.

## Decision

**A setting that has a cluster scope is rows in `app.db`, in one table per setting, where a NULL
scope means every cluster.** `settings.json` keeps what is global and what the user must be able
to edit by hand.

The user's variables are the `env_vars` table (migration `0004`), owned by
`internal/services/envvars`: an id, a nullable `cluster_id` that cascades, a name unique per
scope, a value, a target and two timestamps. The service checks a variable on every write and
again on every read: a stored row the check refuses is listed with its reason and given to no
command.

## Alternatives considered

**Every cluster's list in `settings.json`, a cluster's in rows**, as folder grants are: the
always rules in the file, a chat's grants in `chat_grants`. It keeps the hand edit for one list.
It lost because one setting would then live in two stores with two checks, two refusal paths and
two wire shapes, and the file's list could never join a sync of the rows. The rules justify their
split: an always rule restricts, so the file's refuse-and-hold protects it. A variable does not
restrict.

**Everything in `settings.json`, a scope as a key.** A deleted cluster would leave its entries
behind until a sweep removed them, and a restored backup or a crash between the delete and the
sweep could leave them for good.

## Consequences

The user cannot edit their variables by hand. The write surface is two mutations, `envVarSet` and
`envVarRemove`, from Settings; a backup of `app.db` ([#823](https://github.com/kstackhq/kstack/issues/823)) is a backup of them.

A table change is an `app.db` migration, never a `schemaVersion` bump, and the register's row is
`app.db`'s. A chat scope, when one is wanted, is a nullable `chat_id` beside `cluster_id`.

A later setting with a scope follows the same shape: its own table, a NULL scope for every
cluster, and a service that owns it.
