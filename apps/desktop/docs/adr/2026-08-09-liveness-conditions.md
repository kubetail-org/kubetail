---
title: Every condition is a liveness condition, written transactionally with status
date: 2026-08-09
scope: sidecar
status: Accepted
amended_by:
  - [The Cluster pass folds its claim into two conditions and never writes timing](2026-09-02-cluster-conditions-two-subjects.md)
  - [Kind records mirror the catalog on disk, and Paused is the user's field](2026-09-02-kind-records-mirror-the-catalog.md)
---

# Every condition is a liveness condition, written transactionally with status

## Context

The cluster subsystem's conditions — `Connected`, `Healthy`, `Synced`, `Discovered` — all
describe process-scoped state: a live connection, a live API-server observation, a running
sync worker. None survives a restart. Serving a previous process's `Connected=True` as
current truth after a restart would show green for a cluster nobody has probed. beehive
distinguishes store-truth conditions from liveness conditions, which it serves downgraded to
`Unknown` until the owning process re-confirms them.

## Decision

Every condition the subsystem writes is a liveness condition (`LiveCondition` in `shared.go`
is the only constructor). The downgrade is a read-time transform: nothing is written at
startup, and `Reason` plus the stamps survive — which is why `syncWasRunning` (since removed) could key on a
pre-restart reason. That we have no store-truth conditions is a fact about the domain, not an
oversight: everything reported here is an observation of live state.

**A downgraded condition is flagged `Unconfirmed`, and the flag is load-bearing on the
wire.** Because reason and stamps survive the downgrade, they describe the *last known*
status, not the `Unknown` being served — a consumer reading them uncritically asserts state
nobody has observed. The sync panel must not render a pre-restart `SyncFailed` as a red
error, nor a pre-restart `transitionedAt` as uptime; both fall back to a pending reading
until re-confirmed. Controllers themselves ignore the flag — they overwrite unconditionally,
and `syncWasRunning` (since removed) *wanted* the pre-restart reason.

**A pass writes its conditions once, transactionally with its status.** The cluster
pass writes each condition with `SetCondition` inside one `client.Within`, alongside
`UpdateStatus` (`clusters.go`). The transaction means a watcher never sees `Connected=True`
beside a stale `Server`. The pass then returns `beehive.Settled()`, and that explicit
handshake matters because a condition write bumps `resource_version` but does **not** settle
a generation — a pass whose only output is a condition would otherwise stay owed and be
re-enqueued forever.

Conditions are beehive's type served as-is (`cluster.Condition` aliases `beehive.Condition`;
gqlgen binds the GraphQL `Condition` straight to it), living beside `status` on the wire, not
inside it. There is no per-condition `ObservedGeneration`: the object-level one is the
handshake, and each object's conditions are written by one controller in one pass.

## Alternatives considered

**Store-truth conditions with explicit startup resets.** Rejected: a write-at-startup sweep
races the first reconciles, costs a write per object per boot, and loses the last-known
reason the read-time downgrade preserves.

**Hiding stale reasons/stamps from the wire instead of flagging.** Rejected: `syncWasRunning`
and the pause-event guard legitimately need the pre-restart reason; the `Unconfirmed` flag
lets each consumer decide.

**A projection layer over beehive's condition type.** Rejected: the alias plus direct gqlgen
binding means the `liveness` flag and both stamps reach the client verbatim with no mapping
code to drift.

## Consequences

After a restart the UI honestly shows "pending" until re-confirmation. Consumers must check
`unconfirmed` before treating a reason or stamp as current — this is the invariant an
obvious-looking frontend simplification would break. New conditions use `LiveCondition`;
writes go in the pass's one transaction with its status, never ad hoc.
