---
title: A connection carries the identity confirmed over it
date: 2026-08-25
scope: sidecar
status: Accepted
amended_by:
  - [A recorded identity conflict rebuilds the connection, woken by an edge](2026-08-27-identity-driven-retirement.md)
---

# A connection carries the identity confirmed over it

## Context

`kubecatalog` sweeps a cluster's served kinds into the cache named for that cluster's
`kube-system` UID. The cache is per-identity; the connection it sweeps over is per **context**,
and a kube-context is a name in a file that can be re-pointed at another cluster. Mirroring one
cluster's kinds into another's cache corrupts the cache with no signal, so the sweep has to
refuse a connection that is not the server its subject was armed for.

Two designs shipped and were not enough:

1. **Compare the cache's UID against `Lease.State().ServerUID`.** `Conn` and `State` are separate
   reads of the pool's engine, so a connection replaced between them pairs with the identity from
   the other side of the replacement.
2. **Read both from one `Lease.Snapshot`.** Atomic, and still wrong. `serverUID` is its own probe,
   registered `WithWatches(nameConnection)`: a committed connection *queues* it rather than
   applying it, and the engine deliberately retains a probe's last value until its next commit. So
   the engine holds a perfectly consistent `{conn: B, serverUID: "uid-A"}` for a dispatch plus a
   round-trip. A subject armed for `uid-A` matches, and sweeps B.

The observation records no connection, so no amount of atomicity makes the pairing mean anything.

## Decision

**`kubeconn.Connection` carries a set-once `serverUID`**, written by `serverUIDProbe` when it
reads one *through that connection*. `Lease.ConnFor(ctx, serverUID)` hands back the connection
only when it answers as that cluster, refusing with `ErrIdentityMismatch` otherwise. Its callers — the kubesync session
(`kubesync/session.go`) and the bash tool's upstream (`tools/bash/upstream.go`) — call it and do
not reason about identity at all. `Lease.Snapshot` was deleted with the design
it existed for.

The stamp is unconditional and the commit stays change-gated, because they answer different
questions: the commit says the *context's* identity moved (driving the fleet signal), the stamp
says *this connection* has been identified. A stamp gated on the commit leaves every rebuilt
connection to an unchanged cluster unstamped, which is the common case.

A connection nobody has identified yet answers `("", false)` and is refused. That is the window
this ADR closes: after a re-point the pool holds connection B **unidentified** rather than B
beside A's UID, so the sweep parks until the probe reads it, and the pool's news is the wake.

## Alternatives considered

**Record the identity on `connInfo`**, the connection probe's committed value, where it would sit
beside `conn` in one comparable struct with no mutable field. Rejected, and it is the design the
parked retirement item sketched. `connInfo` is written by the *connection* probe, which never
reads a UID — it would have to take one off its own snapshot, which has the same stale pairing. A
30s re-dial landing before the UID probe re-runs would commit `connInfo{conn: B, identity:
"uid-A"}`, turning a transient bad pairing into a **durable record** later readers trust. Moving
the correlation does not remove it.

**Keep the comparison in `kubecatalog` over an atomically-read `State`.** This is what shipped
first. It fixes a real torn read and leaves the stale-by-design pairing untouched, which was the
actual defect.

**Invalidate the dependent probes' observations when a connection commits.** Would make
`State.ServerUID` unknown until re-read, which is the right semantics for this caller — but the
engine's retention through failure is deliberate and load-bearing elsewhere (a cluster whose UID
probe starts failing must keep reporting its last known identity, not flap). Changing a general
engine rule for one caller's benefit is the wrong trade.

## Consequences

The identity question has one answer with one writer: the probe that made the request over that
object. Nothing correlates two facts, so there is no interleaving to reason about and no atomicity
to preserve — which is why `Snapshot` could go rather than being kept "just in case".

`Connection`'s "never changes after it is built" contract now has an explicit carve-out: the
clients and the config are immutable, the identity is written once. That is stated on the type,
beside the `done`/`once` fields that were already mutable.

**The stamp is never overwritten, and a second, different UID makes the connection vouch for
nobody.** A server replaced behind an endpoint and credentials that never moved rebuilds no
connection, so the UID probe eventually reads a different uid over the one already stamped.
Keeping the old stamp would go on authorizing the old cluster's subjects against the replacement;
adopting the new one would let a connection that has already answered as something else vouch for
what answers now. So `setServerUID` records the conflict, the connection stops
vouching, and `ConnFor` refuses every caller. Identity is one `atomic.Pointer[connIdentity]`, not
a uid beside a conflict flag: two words a reader loads separately let a caller through between
them, holding the old uid against the replacement server.

**The pool's news carries what the current connection vouches for** (`news.vouchedFor`), which is
what makes the claim "the pool's news is the wake" true. A credential rotation for an *unchanged*
cluster moves nothing else — same phase, same identity, same per-probe verdicts — so the rebuilt
connection would be refused until re-stamped, and the re-stamp commits nothing because the uid it
read equals the one already recorded. Without that field no signal ever fires and every
identity-scoped holder stalls for good.

The cost is a **visible stall rather than silent corruption**: until something rebuilds that
connection, no identity-scoped work runs over it, and nothing rebuilds a connection whose
credentials never changed. In practice the common causes of a swapped server (a `minikube
delete`/`start`, a re-issued cluster) also rewrite the kubeconfig, so the fingerprint moves and
the connection is replaced. The residual case is an endpoint re-pointed at a different cluster
with the same credentials, which stalls until [identity-driven retirement](2026-08-27-identity-driven-retirement.md) acts on the
recorded conflict.

Retiring the connection is deliberately *not* done here. It belongs to the pool, and alone it
would not help: `Conn` hands out a retired connection too — `Done` is how a holder hears about one
— so refusing is the load-bearing half either way.

## Revisit when

Retirement lands. Once a recorded conflict rebuilds the connection, the stall goes away and the
conflict flag becomes a short-lived state rather than a terminal one — at which point it is worth
checking whether `ConnFor`'s conflict branch is still reachable long enough to be worth its own
message.
