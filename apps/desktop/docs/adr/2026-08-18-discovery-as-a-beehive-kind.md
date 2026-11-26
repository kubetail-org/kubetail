---
title: Run cluster discovery as a beehive kind, not a loop beside one
date: 2026-08-18
scope: sidecar
status: Accepted
amended_by:
  - [A cluster is a row in app.db, mirrored into a runtime object it is deleted through](2026-09-16-clusters-are-rows-mirrored-into-beehive.md)
  - [The kubeconfig fingerprint digests the whole file](2026-09-30-the-kubeconfig-fingerprint-digests-the-whole-file.md)
---

# Run cluster discovery as a beehive kind, not a loop beside one

Replaces the importer paragraph of
[Beehive owner chain with ObjectID identity](2026-08-09-beehive-control-plane.md); the rest of that
ADR — the owner chain, and `ObjectID` as identity — still holds.

## Context

Something has to decide which `Cluster` records exist. That is not a controller's job: a controller
reconciles an object that already exists, and this has to run when there are none. So it lived
outside beehive as `kubeconfigImporter` — subscribe to the kubeconfig, create a record per
unclaimed context, and `Requeue` every record that already existed.

That last part is the crux. Each record's observation (`status.source.kubeconfig`) is folded from
the **kubeconfig service**, not from the object beehive hands the reconcile, so beehive cannot know
when it goes stale. Nothing about a file change reaches a record on its own, and a context *removed*
from the file appears in no snapshot the create loop walks — the wake was the only thing that could
ever reach it.

Two costs followed. The importer hand-rolled a doubling retry ladder, a resync channel, and its own
lifecycle, and it hung off `clusterController` purely for that lifecycle while `service` held the
controller purely to reach back through it. More seriously, the design had a hole its own comments
named: *"a lost wake is the failure nothing else re-levels."* The kubeconfig service republishes only
when contents differ, so its 30-minute backstop tick produced no pass, and a record kept a stale
observation — a departed context still marked present — until the user next edited the file.
Correctness rested on every wake being delivered or retried, which is a stronger requirement than
level-triggered reconciliation is supposed to need.

## Decision

Discovery gets an object to run against. `ClusterSource` (`clustersources.go`) is one anchor per
`ClusterSpecSource` variant — today `clustersource/kubeconfig` — created at startup by
`ensureClusterSources`, since it is the one record in the package with no parent above it. Its
controller runs the pass: the import (`importKubeconfigContexts`, `clustersources.go`) creates a
record for every unclaimed context, and the
pass then publishes what it observed.

The anchor's status carries one field, a fingerprint, and every `Cluster` declares
`AddDependency(cluster, anchor)` from its own reconcile. One status write therefore wakes all of
them through beehive's dependency waker, with the stale-dependents pass as the guarantee behind it.
`kubeconfigFingerprint` is **computed by running `observeKubeconfig`** rather than by naming the
config fields that fold happens to read, so it cannot drift from what the dependents observe.

Everything the loop hand-rolled is now beehive's: a failed pass rides its backoff ladder,
`startupPass` runs the boot import, `Requeue` is available as an out-of-band kick, and the pass
re-arms on `clusterSourceResyncInterval`, which is the periodic re-level the old design lacked.
Nothing kicks it today: `Clusters().Delete` refuses a record its source still declares
(`ErrDeclaredBySource`), so a delete cannot free a natural key the source would want back. The one piece
still outside beehive is a trigger (`newKubeconfigTrigger`, `triggers.go`), which subscribes to a source's change feed
and requeues its anchor; a source of truth is not an object, so nothing else can span that gap. It
carries no retry — a lost poke costs latency, not divergence.

`ClusterSource` binds to no GraphQL type. It is control-plane machinery, not a record family.

## Alternatives considered

**Keep the loop, move it off the controller onto the service.** This was the first attempt, and it
fixed the ownership smell — `service` no longer held a controller to reach through — while changing
nothing else. Rejected once it was clear the loop's *contents* were the problem: it still hand-rolled
the ladder and the wake, and it left the lost-wake hole exactly where it was.

**Wake only the records a change affects.** The obvious efficiency, and it costs more than it saves.
Deciding which records are affected means comparing each one's stored observation against the
snapshot, which is precisely the fold `clusterController.Reconcile` already runs — so the anchor
would duplicate the controller's logic in order to decide whether to ask the controller to run it,
in two places that can disagree. The cheaper-looking variant, diffing consecutive snapshots and
waking only the touched contexts, is correct only if every record's stored observation equals the
previous snapshot; a failed status write, a record left by a previous process, or a missed wake all
break that. Not assuming stored state matches the last event is the point of level-triggered
reconciliation. An unaffected wake is a map lookup, a struct compare, and a no-op settle.

**Give discovery its own per-context kind whose spec the anchor writes** — the shape
`ClusterCachedCatalog → ClusterCachedResource` already uses, where identity fields are "written
wholly from above" and an unchanged child is not written and therefore not woken. That would deliver
the targeted wake for free. It does not transfer, because spec ownership is inverted: `ClusterSpec`
is **user-owned** (`Name`, `Enabled`, `SyncEnabled`), beehive's `Update` takes a whole spec with no
compare-and-swap — `service.clusterSpecMu` existed for exactly that reason — and a discovery writer
would race the API setters over one struct. A separate per-context kind sidesteps the race but adds
a kind between the file and the API's root noun, plus a server-side join, buying precision we do not
need while a pass is a map lookup. Kept as the upgrade path.

**Put the observation table in the anchor's status** instead of a hash. Rejected: it duplicates
state each record already holds and invites a second source of truth for `isPresent`. A fingerprint
is deliberately meaningless, so nothing can come to depend on it.

**Declare the dependency from the anchor's pass** rather than from each Cluster's reconcile. It
would save a `GetByName` per pass, since the anchor already holds the record list. Rejected to keep
one idiom: `clusterCacheController` declares its own edge onto its cluster, and an edge declared
next to the read it pays for is the one a later change is least likely to invalidate.

## Consequences

The framework supplies retry, startup passes, an observed generation, an events timeline, and the
dependency wake — all of which the loop either hand-rolled or lacked. The subsystem is uniform:
everything in it is now register-a-controller.

The costs are real. There is a fifth beehive kind that is not one of the five GraphQL families, so
the package's "one file per family, and the families are the API" symmetry now has an exception a
reader has to be told about. The anchor must be bootstrapped by the service, which is the only
creation here with no parent. Each cluster reconcile does a `GetByName` to resolve the anchor id,
which scales with the fan-out on a single-connection store. And the wake stays broad.

Four obligations someone could break without noticing:

- **The fingerprint must stay derived from `observeKubeconfig`.** List the config fields by hand and
  it silently stops tracking the day the fold reads one more — no record is woken, and no unit test
  catches it, because they all stub the write.
- **The create pass must run ahead of the fingerprint gate.** A create that failed is retried
  against the snapshot that failed, so a pass returning early on an unchanged fingerprint would
  skip the retry and never import that context.
- **`ClusterSourceStatus` must gain no field that moves every pass.** A timestamp there would wake
  every record on every tick.
- **One live-beehive test exercises the edge.** Every other test stubs `ControllerClient`, so
  `AddDependency` is recorded rather than followed; reverse its arguments and only
  `TestSourceWakesADepartedContextsRecord` fails.

## Revisit when

`clusterController.Reconcile` becomes expensive — the connection probe is the one on the way. Today
a wake is a map lookup and fan-out is free; once a pass can dial a cluster, waking every record
because someone saved their kubeconfig is not. The first answer is to gate the expense inside
`Reconcile` on an observed change, which the existing early return already establishes. If that is
not enough, adopt the per-context discovery kind described above.
