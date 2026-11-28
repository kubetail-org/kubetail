---
title: Let an integration list its targets, and keep their rows in the core
date: 2026-11-28
scope: sidecar
status: Accepted
---

# Let an integration list its targets, and keep their rows in the core

## Context

The monitor reads targets: for Kubernetes, one per cluster. The user switches monitoring per
target, incidents belong to a target, and a target has a read state the screens show. Clusters
already have rows in `app.db`, owned by the cluster service; a future integration's targets would
be rows of its own. Monitoring is on by default, so a new cluster must be watched without the user
doing anything.

## Decision

**An integration lists its targets, and the core keeps a row for each in `monitor_targets`.** The
reconcile takes what `ListTargets` answers, in one transaction: it inserts a row, enabled, for each
new key, rewrites a changed label, and deletes each row whose key is listed no more, which takes its
incidents. It runs at start, on each notice from `ListenForTargetChanges`, and every 5 minutes. It
listens before its first listing, so a change between the two costs a repeated listing, never a
missed one.

**`ListTargets` lists durable records, never what answers now**, and a failed listing changes
nothing. The Kubernetes integration lists every `clusters` row, a disabled cluster's included,
keyed by the cluster's id. `monitor_targets` has no column for a cluster and no foreign key to one.

## Alternatives considered

- **A monitoring column on `clusters`**, as `clusters.monitoring_enabled` was. It gives the core
  a cluster to know, and every other integration a table to alter.
- **Targets the core keeps without asking the integration**, made when a reading first succeeds.
  An unreachable cluster would have no row, so no switch to turn off and no state to show.
- **A foreign key from a target to its cluster.** It ties the core's table to one integration's,
  and a cascade would delete the target's incidents in the cluster service's transaction.

## Consequences

The switch, the read state and the incidents follow the target, which follows the record its
integration lists. Removing a cluster removes its incidents; disabling it keeps them. The reconcile
is the one place rows are made and deleted, and it trusts only an answer. A target whose integration
this build lacks is never listed, read or deleted.

## Revisit when

An integration's targets are added by the user in the monitor itself, with a config and a test, not
listed from records another service owns.
