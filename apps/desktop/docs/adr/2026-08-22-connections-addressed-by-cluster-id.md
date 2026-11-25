---
title: Address every Kubernetes connection by ClusterID
date: 2026-08-22
scope: sidecar
status: Accepted
amended_by:
  - [One connection per kube-context, not per credential fingerprint](2026-08-23-one-connection-per-context.md)
  - [Wake cluster passes from a fleet-wide kubeconn bus, not per-claim goroutines](2026-08-23-kubeconn-wakes-ride-a-fleet-bus.md)
  - [A connection carries the identity confirmed over it](2026-08-25-connection-carried-identity.md)
---

# Address every Kubernetes connection by ClusterID

## Context

`internal/kubeconn` is a worked-out connection pool: one entry per credential *key*, built on
first use, kept validated by a probe, handed out only through a `Lease`. It reads no kubeconfig
and knows nothing about a cluster — the caller computes the key.

It sat in `internal/` as a peer of the cluster service, on the reading that it is a transport
leaf several subsystems would draw from directly: log tail, exec, port-forward, the kube-context
picker's metadata, the `SelfSubjectRulesReview` behind `ClusterPermissions`. The composition root
built it and drove its lifecycle; nothing acquired a lease.

That reading did not survive being checked. Every one of those consumers knows *which cluster* it
means, and none of them can turn that into credentials on its own: the `ClusterID` → source →
context name → `rest.Config` chain lives entirely inside `services/cluster`. Worse, dialing needs policy
that only `services/cluster` holds — whether the cluster is enabled, whether its record is tombstoned,
and whether the server answering is still the one `ClusterActiveUID` identifies. A tail streaming
from a server the cluster record no longer claims is a bug the pool cannot even describe.

We could not construct a case for connecting to a cluster without addressing it by `ClusterID`.
The nearest was "validate credentials before committing them", which the record shape already
answers: `importKubeconfigContexts` derives a record per kubeconfig context, so the record exists
before the dial does, and a manual-add flow can create a disabled one first. In-cluster /
service-account mode would be a real exception, but that is kubetail's cluster-api, not this
desktop app.

## Decision

Connections are obtained by `ClusterID`, from `services/cluster`. The pool lives behind that boundary at
`internal/services/cluster/internal/kubeconn`, alongside `kubeidentity`, and is unimportable from
anywhere else. `cluster.New` constructs it and carries it in the service's own `lifecycle.Part`
slice, ahead of beehive, so a connection outlives every reconcile pass that could still be dialing
on it. The composition root no longer knows the pool exists.

The pool still keys on credentials, not on clusters: two kube-contexts aimed at one server as one
user are one socket and one probe. Only the *address a caller uses* is a cluster.

`Service.AcquireConnection(ctx, id)` returns a `Lease`; `Lease.Conn` a `*Connection`. Both are the
leaf's types, re-exported as **aliases** — a connection is native vocabulary all the way down
(`rest.Config`, `dynamic.Interface`, `http.Client`), so a boundary struct would be a field-for-field
copy that drifts, and a plain `internal/` return type could not be *named* by the resolver tests'
fake implementing `Service`.

A connection is scoped to one **identity** — `{ServerUID, ServerVersion, Username}`, comparable by
value — so a probe reading a different one retires it and builds another, exactly as moved
credentials do under a new key. All three retire: a version change invalidates what was discovered
against the old one, and a username change means a different principal. That matches the rest of
the system, where `ClusterCache` is named `{ClusterID}/{serverUID}` and a UID change is a cache
migration. Why a part of it is missing stays on the `Observation` that could not read it — that reports
what the probe could read rather than who answered, it comes and goes as a grant does, and keeping
errors off `Identity` is what keeps the comparison a `==`. Comparing is the caller's, since the pool keys on credentials, which
do not move when a cluster is rebuilt behind them; and a long-lived stream cannot see a field it is
not re-reading, so a retired connection closes `Connection.Done()`.

Everything a holder learns comes through its `Lease` — `Conn`, `State`, `WatchState` — which is
what removes the index from a credential key back out to the contexts resolving to it: signalling
every claim on an entry is the whole fan-out. `WatchState` returns a `gobus/watch` receiver of
`State`, over the hub `Conn` already parks on — one hub rather than a value hub beside a ping bus,
so a watcher and a parked caller cannot disagree. Its values are levels: the hub keeps the latest,
so a reader that falls behind skips what came between.

*Corrected 2026-08-23: this section first claimed the receiver is current on attach. It is not —
gobus's baseline is the value later ones are compared against, never delivered — so a watcher
reads `State` for what is known now and watches for what follows.*

`internal/kubeconn` stays in the tree, built by nothing, as the worked-out implementation the new
package draws from as it fills in.

## Alternatives considered

**Leave it in `internal/` and let subsystems dial directly.** This is what we had. It requires a
credential key or a context name as the address, and no consumer has one — they would each
re-derive the `ClusterID` → credentials chain, and each would have to remember the enablement,
tombstone, and server-UID checks that make a dial legitimate. Every place that forgot one would be
a bug the types could not catch.

**Leave it in `internal/`, but make `services/cluster` the only caller.** Consistent, and it preserves
the composition root's explicit start ordering. Rejected because the ordering guarantee it
preserves is a comment nobody may reorder, whereas ownership makes it structural — and because
`internal/` placement would then describe nothing true about the package except that one importer
happens to be allowed to bypass the boundary.

**Bury it and drop the credential key, one entry per cluster.** Simplest to explain, and wrong: it
would dial and probe a server once per kube-context pointing at it, which is exactly the waste the
key exists to remove.

## Consequences

The composition root gets smaller and the pool's lifecycle is enforced by position in
`services/cluster`'s slice rather than by a comment in `app.go`.

`kubeconn` and `kubeidentity` become siblings, which exposes how much they overlap: the two
`Identity` types are now the same three comparable fields, both pair a `State` read with a feed of
it, and `kubeidentity`'s central mechanism — storing the credential key beside each answer so a
stored identity is refutable — is what credential-keying gives `kubeconn` for free. Its probe is
unwritten and `kubeconn`'s is not, so the two are likely to collapse into one package with two
views rather than staying separate.

Two limits this leaves. Permission changes are **not** connection events: an ordinary RBAC edit
leaves `Username` identical, so surfacing them needs the `SelfSubjectRulesReview` already planned
for `ClusterPermissions` and a digest to diff. And nothing counts transitions off `WatchState` —
flaps and history come from the record's conditions and event timeline.

Waking a beehive pass per cluster becomes a goroutine per claim rather than one reader over a
fleet-wide bus.

The obligation the new location creates: the pool's unit is credentials, shared *across* clusters,
and nothing in the vocabulary of a cluster record says so. The package doc has to lead with it, or
a reader will assume one entry per cluster and reason wrongly about probe counts and socket reuse.

Anything genuinely needing a connection without a `ClusterID` now has to go through `services/cluster`
anyway, or get a record first.

## Revisit when

The sidecar grows an in-cluster / service-account mode, or any dial whose subject is not a tracked
cluster — a bare credential check in an add-cluster flow that we decide should not create a record
first.
