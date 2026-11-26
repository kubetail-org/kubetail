---
title: Kind records mirror the catalog on disk, and Paused is the user's field
date: 2026-09-02
scope: sidecar
status: Accepted
---

# Kind records mirror the catalog on disk, and Paused is the user's field

## Context

A `ClusterCache` mirrors the kinds a cluster serves as one `ClusterCachedKind` record per kind.
The discovery sweep (`kubesync`) writes what the cluster serves into the cache's `kind_catalog`
table; the cache controller's pass turns that table into records; each record's own pass arms
that kind's sync. Two things had to be settled: where the pass reads the desired set from, and
who owns each field of the record's spec, since the sweep runs on a schedule and the user flips
a switch on the same record.

## Decision

**The desired set comes off disk, never off the sync seam.** The cache pass reads
`OpenExisting` → `KindsWithFingerprint` → `Release`: rows and fingerprint in one read transaction.
`OpenExisting` never creates a file, so a pass before any sweep prunes nothing. The fingerprint's
absence is the "never swept" bit, distinct from a cluster that serves nothing, and only the latter
may delete records. Reading rows and fingerprint together is what stops a stale fingerprint passing
its check beside a clear's empty table.

**A row with no record is a `CreateOrUpdate`**, because a kind's spec carries data beyond its name
(singular, scope) and a renamed or re-scoped kind must converge in place. **A record with no row is
a `Delete`** — marked, not collected, since the record's own pass clears its rows first. **A record
whose catalog fields all match is not written** (`sameCatalogFields`); that is what keeps a sweep
off a hundred-kind cache's write path every pass, and what makes the ownership split hold.

**The catalog owns four spec fields; the user owns `Paused`.** The desired spec is built from the
catalog row alone, so it carries `Paused`'s zero value, and a pass that wrote it whole would
un-pause every kind within one discovery interval. The one write that touches a stored record (a
catalog change converging in place) carries `Paused` forward under `deps.kindSpecMu`, which
`SetSyncEnabled` takes too, and rereads the record for it rather than trusting the pass's own list,
which ran before the lock was taken.

**The field is `Paused`, not `Enabled`.** Beehive stores a spec as JSON, so a key absent from every
existing record decodes to `false`. A positive `Enabled` would read as disabled fleet-wide on the
upgrade that shipped it, and the no-op rule above means nothing would ever rewrite the record to
repair it. The wire keeps the positive form (`syncEnabled`, one negation in the resolver).

**Pause stops the sync and keeps the rows.** The kind controller calls `ForgetKind` and never
`clearKindRows`; that call is what makes a deletion a deletion. It is level-triggered and
idempotent. A resume reconciles into the kept rows, off the cookie if the server still serves that
resourceVersion, otherwise by relisting.

**Neither controller writes a condition.** The verdict is the health gauge's; a stored one would
serve a dead process's answer until the passes caught up.

**Two triggers carry the news, one per registration.** A trigger wakes a record for every value its
feed carries, so one feed carrying both cache and kind news would wake a cache for each of its
hundreds of kinds. The cache's trigger is `WithTriggerByID` over cache ids; the kind's is
`WithTriggerByName`, mapping a `KindKey` onto `ClusterCachedKindName(cacheID, apiVersion, resource)`,
because that record's id is the store's to assign and its name is derivable, which keeps a record
id out of kubesync. `trigger[T, W]` is generic over the address for exactly this.

## Alternatives considered

- **Read the desired set from kubesync's in-memory catalog.** Rejected: a pass before the first
  sweep would see an empty set and delete every record, and a restart would do the same until the
  sweep landed.
- **`GetOrCreate` for kind records, as for caches.** Rejected: a cache's spec is its identity, a
  kind's is not. A renamed kind would leave a stale record beside a new one.
- **A positive `Enabled` field.** Rejected for the upgrade behaviour above.
- **Pause clears the rows.** Rejected: pausing is meant to stop churn while keeping what the user
  already sees; clearing is a separate, explicit mutation.
- **One trigger feed for both levels.** Rejected for the wake amplification above.

## Consequences

A sweep that changes nothing costs the cache pass one read transaction and no writes. `Paused`
survives every scheduled write by construction, but only because every writer of a kind spec goes
through the one guarded path; a new writer must take `kindSpecMu` and carry `Paused` forward.
The inversion means "syncing" is the absence of a key, which reads oddly in the store and is why
the wire hides it.
