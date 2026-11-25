---
title: Wake cluster passes from a fleet-wide kubeconn bus, not per-claim goroutines
date: 2026-08-23
scope: sidecar
status: Accepted
amended_by:
  - [A cluster is a row in app.db, mirrored into a runtime object it is deleted through](2026-09-16-clusters-are-rows-mirrored-into-beehive.md)
---

# Wake cluster passes from a fleet-wide kubeconn bus, not per-claim goroutines

## Context

[Connections are addressed by ClusterID](2026-08-22-connections-addressed-by-cluster-id.md)
put the pool behind `services/cluster` and gave every holder its news through a `Lease` —
`Conn`, `State`, `WatchState`. Its Consequences drew one implication from that: "waking a
beehive pass per cluster becomes a goroutine per claim rather than one reader over a
fleet-wide bus."

Built, that meant a bespoke `kubeconnTrigger`: a cancel func per claim, a `stopped` flag, a
fan-in channel, and a pump whose lifetime had to be tied to a claim's. `ensureLease` grew a
record-name argument and had to start the pump outside its own lock, since the controller's
lock and the trigger's were taken in opposite orders on the two paths. Four test call sites
needed a constructor that stopped pumps, because a controller built without a trigger
dereferenced nil.

The retired `kubeidentity` had solved the same problem in three lines, over the generic
`trigger[T]` every other feed uses.

## Decision

`kubeconn.Service` publishes a fleet feed — `Subscribe()`, a `gobus/conflate` bus keyed by
**context name** — and `newKubeconnTrigger` is an ordinary `trigger[T]` over it, mapping
each key to the record holding a claim on that context (`clusterLeases.clusterFor`, the cluster
controller's lease index, `clusters.go`). The cluster controller holds no trigger and knows
nothing about waking; it takes claims and reads them.

`conflate` and not `watch.WatchAcross`. A wildcard watcher collapses a burst to a single
value naming the last key to land, which for a trigger that must reach every affected
record is a dropped wake per cluster that moved in the same window. `conflate` keeps a slot
per key.

The key is the context, not the credential key the pool pools on. A claim is on a context —
`Acquire` takes one and re-resolves it, since credentials rotate under a context that never
moves — so the pool already holds a context name per claim, and publishing per context is
the direction it already has rather than a new index.

`Lease.WatchState` stays. Nothing reads it now that the trigger does not, but the boundary
is designed complete rather than pruned to current callers, and a holder watching its own
claim is the obvious next one. Both channels are fed from the same send.

## Alternatives considered

**Keep the per-claim pumps.** They have one advantage, and it is real: a pump dies with its
claim, so a claim the controller dropped can never wake a cluster record. A fleet feed
publishes for any claim on a context, so a log tail holding one after the controller
released its own wakes a record nobody is managing. The pass drops nothing and settles, so
this is wasted work rather than a wrong answer, and it does not pay for the machinery.

**Key the bus on the credential key.** What the pool actually pools on, and unusable: the
trigger must name records, and only `services/cluster` knows that the context "prod" is the record
"kubeconfig/prod". Keying on credentials would put that mapping — or an index back out to
the contexts sharing a key — inside a package whose rule is that it never learns what a
cluster is.

## Consequences

One goroutine reads the pool for the whole fleet, and the two triggers this service
registers are now the same shape. The lock-ordering hazard is gone with the machinery that
created it; `ensureLease` is back to holding claims.

The pool has two publication channels to keep consistent. They are fed from one send site,
so the risk is a future edit that feeds one and not the other.

A cluster whose context is claimed by something other than the controller — a log tail —
wakes its record on every probe. Bounded by the cadence and absorbed by beehive's no-op
suppression, which writes nothing when a pass observed nothing new.

## Revisit when

A holder needs wakes the cluster controller must not get, or the fleet feed's wasted passes
show up against a real probe cadence. The per-claim subscription is still on `Lease`, so
the reversal is a trigger change rather than a pool change.
