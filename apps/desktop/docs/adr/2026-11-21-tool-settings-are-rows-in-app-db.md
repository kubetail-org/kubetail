---
title: Keep the tool settings as rows in app.db
date: 2026-11-21
scope: sidecar
status: Accepted
amended_by: [Keep every setting as rows in app.db](2026-11-24-settings-are-rows-in-app-db.md)
---

# Keep the tool settings as rows in app.db

## Context

Settings › Tools lets the user turn each of the model's tools off ([#1082](https://github.com/kstackhq/kstack/issues/1082)). The setting is global
today. The settings ADR puts global
settings in `settings.json`, and the cloud platform switches, also global on/off restrictions,
live there (`sandbox.aws` and the rest,
[every cloud platform is on until the user turns it off](2026-11-18-every-cloud-platform-is-on-until-the-user-turns-it-off.md)).
[The rows ADR](2026-11-20-a-setting-with-scopes-is-rows.md) moves only scoped settings to `app.db`.

Three things pull the other way:

- The choice is one-way. Once a release carries a field in `settings.json`, nothing can move it
  into `app.db` ([persisted data](../persisted-data.md), *What cannot change yet*). Rows change by
  an ordinary numbered migration.
- Kubetail Cloud will sync these settings. Rows with keys and timestamps merge; a list in a file
  does not, and a hand edit meeting a synced value is an open problem ([TODO](../TODO.md)).
- A cluster scope ("no Bash on prod") is a likely next request, and a scoped setting is rows with
  a cascading `cluster_id`.

## Decision

**The tool settings are the `tool_settings` table in `app.db`** (migration `0005`): a row per
tool per scope, keyed on the tool's `Name()` and `ifnull(cluster_id, '')`, with `enabled` and
`updated_at`. `cluster_id` NULL is every cluster; only that scope is written today.

- **A row is an override.** No row means on. Turning a tool back on writes `enabled = 1` rather
  than deleting the row, so a sync carries no delete and `updated_at` can order the writes.
- **The key is the tool's name**, not a random id, so one setting made on two machines offline
  is one row.
- **A row naming a tool this build or machine lacks is kept and ignored.** The box differs per
  machine (no shell means no `Bash`) and per build.
- **No sync columns yet.** The sync design adds what it needs by migration.
- **A tool's options beyond on/off** get a typed column, or a table of their own for a list,
  each by its own migration; never a generic JSON column.

`internal/services/toolsettings` owns the table. The chat service reads the disabled tools with
its own statement in the transaction that reserves a turn, as it reads `clusters`.

## Alternatives considered

**A `tools.off` list in `settings.json`**, beside the cloud platform switches. It would be one
store for the global restrictions, but it is the one-way choice above, it does not merge, and a
turn would read a second store beside the transaction it already holds.

## Consequences

The tool settings and the cloud platform switches live in different stores. The user cannot edit
the tool settings by hand; Settings is the only write path. A per-cluster scope needs no table
change.
