---
title: The cluster card carries no counts
date: 2026-09-14
scope: sidecar
status: Accepted
---

# The cluster card carries no counts

## Context

The card a question carries is resent only when its text changes
([the cluster card rides the user message](2026-09-13-the-cluster-card-rides-the-user-message.md)),
so every field on it is either near-constant or wears a status saying how current it is. The first
card also carried one number — the node count — and the cache's census of kinds. A count is the
one kind of fact the card cannot carry honestly: it moves on its own, under an autoscaler, a Job,
a CI run. Either the card is resent on every turn it moved, spending the 4 KiB on nothing the
model asked about, or the model holds a stale number in context with nothing on the card saying
so, and answers with it.

The card's shape mirrored the sidecar's subsystems — a `cache` object with the health reason and
two tallies, lists beside it — where the model asks three things: which cluster is this, can I
trust what follows, and what is in it.

## Decision

The card is organized by those questions (`cluster`, `connection`, `freshness`, `inventory`;
`sidecar/internal/clustercard/render.go`) and **carries no counts** of anything. Scale is a tool's
answer, when tools land; the catalog already says whether `v1/nodes` is served.
`TestRenderCarriesNoCounts` allows a number only under a cut list's `more` marker.

Two facts join the card. `connection.tls` — `verified`, `unverified`, `none` — is the one
kubeconfig fact that changes an answer, read off the entry the status mirrors and never its server
URL. `freshness.since` is when the app last saw the cluster live: the health reading's
`LastLiveAt`, the oldest proof across every kind, carried only under `last-known`.

## Alternatives considered

- **Keep the node count, accept the resends.** A node count is the number the model most wants,
  and on a fixed-size cluster it is stable. But the card cannot tell a fixed-size cluster from an
  autoscaled one, and the failure — a stale number answered as fact — is silent.
- **Carry the count with a stamp saying when it was read.** Truthful, but the stamp moves with the
  count and the card resends anyway; and a model told "3 nodes as of 10:41" still answers "3".
- **No timestamp at all**, as the earlier ADR rejected. That rejection was of a wall-clock stamp,
  which defeats the dedupe on its own. `since` is not one: `kubesync` moves a kind's stamp only on
  the path that reports it `Watching`, so with every kind behind the verdict every stamp is
  frozen, and the card is byte-stable until something recovers — a real change, and a card worth
  sending.
- **Decide `last-known` from the rollup's reason.** The rollup reports its first offender's reason,
  so one stale CRD beside a lost connection reads as `SyncFailed` when the cache as a whole is
  `NoConnection`. The arm is decided by count instead: every unpaused kind behind is `last-known`,
  fewer is `partial`.

## Consequences

The card is byte-stable while nothing the model would ask about has changed, which is what the
dedupe was built for. The model is told, in the system prompt, that how many of anything there are
is unseen, so a scale question gets a `kubectl` command rather than a number.

`since` is absent while any unpaused kind has never proved live, since the reading's stamp is nil
then: a cluster with one kind forbidden from birth loses the timestamp on the day its connection
drops, which is the day the model wants it. Accepted — the rollup's rule is the honest one, and a
looser reading on the card would be a second freshness rule to keep in step with the gauge.

The `freshness` cache-wide arms read the rollup's reason where the count rule does not trust it.
They can: `SizeLimit` and `StoreFailed` are the cache's own verdicts, set above the per-kind fold,
and `IdentityMismatch` is the store's, so no kind carries it alone. A new cache-level reason has to
be placed in one arm or the other on purpose.

## Revisit when

Tools land and the card's job shrinks to naming the cluster the tools read — the counts belong to
them then, fresh on every call, and the `inventory` section may follow.
