---
title: The connection probe dials /api, builds the connection, and lets the pool retire it
date: 2026-08-25
scope: sidecar
status: Accepted
---

# The connection probe dials /api, builds the connection, and lets the pool retire it

## Context

`kubeconn`'s connection probe stopped at resolving the kubeconfig: a context that resolved
suspended with `ReasonResolved`, `Lease.Conn` always reported `ErrNoConnection`, and the four
probes behind reachability recorded `DependencyFailed` forever. Nothing wrote `status.server.uid`,
so no `ClusterCache` was ever created.

Three questions had to be answered together, because each constrains the others: what request
proves "connected", who builds a connection, and who retires one.

## Decision

**One `GET /api` is the reachability check.** `/readyz` belongs to the readiness probe and
`/version` to serverVersion, so this probe needed a request of its own. `/api` proves DNS → TCP →
TLS → authentication in one round trip, and it is the only one of the three that can answer 401 or
403 — which is what gives `ReasonUnauthorized`/`ReasonForbidden` a producer. Its
`{"versions":[...]}` body is what distinguishes a Kubernetes API server from a captive portal that
answers 200 to everything, so an empty `versions` is `ReasonMalformed`. A 401 or 403 fails the
probe rather than succeeding with a caveat: credentials that cannot read discovery cannot serve the
four probes behind this one either.

**The probe builds; the pool retires.** A run cannot retire the connection it replaces —
`Pass.Commit` is buffered and applied after the run returns, so a probe that closed `done` first
would leave holders reconnecting against a `Lease.Conn` still handing out the dead one.
`Service.publish` runs after the commit, is serialized per context, and holds the entry, so it
adopts the connection a pass carries and hands back the one nothing holds any more. Three clauses
cover every exit: publish while claimed (retire what it replaced), publish once released (retire
the connection the pass carried, which the release could not have reached), and `Release`/`Close`
(retire what the entry holds). `retire` is `sync.Once`-guarded because two of them can reach one
connection from either side of the same window.

**A connection is rebuilt on a changed fingerprint *or* no connection**, never the fingerprint
alone: a failed build commits the fingerprint it tried with nothing behind it, and a
fingerprint-only test would find it unchanged and never build again.

**`Connection.Identity` is deleted.** The three probes that read identity depend on the connection,
so a connection always exists before anything can identify the server behind it — the field would
be empty for the life of every connection. `State.Identity()` is the read surface; a holder learns
its connection went void from `Done()`.

## Alternatives considered

**`/version` or `/livez` as the reachability check.** Both are commonly readable by
`system:public-info-viewer`, so they answer for an unauthenticated caller — a revoked credential
would read as a healthy cluster. And both are another probe's endpoint, which would make one
probe's failure indistinguishable from another's.

**Retiring inside the run.** Simplest to read, and wrong: the commit lands after the run returns,
so between the two, `Lease.Conn` hands out a connection whose `Done` is already closed.

**Blocking `Lease.Conn` until a probe succeeds**, which `internal/kubeconn` did. It moves the
decision away from the only layer that can make it: the holder reads `State.Phase()` and knows
whether a failing connection is a revoked credential or a control plane mid-restart.

## Consequences

`ReasonResolved` and its `Phase()` branch are gone; an undialed context reads `PhasePending` via
the never-attempted branch instead. `Lease.Conn` hands out a connection whose last probe may have
failed. Every test that dials must aim at a literal local address or an `httptest` server — a
kubeconfig fake naming a host turns "unreachable" into a DNS lookup, which is slow and answers on a
network with a wildcard resolver.

**One leak window is tolerated.** A `Release` racing a run removes the subject before the run
returns, and the engine drops a commit against a removed subject — so that connection reaches
neither the snapshot nor the entry and no clause can name it. The `/api` request has already
finished, so what is held is idle sockets, closed at `IdleConnTimeout` (90s, client-go's inherited
default), after which nothing references the client. Closing the window means the engine reporting
a refused commit back to the run: a change to a generic engine for one caller.

## Update, 2026-08-25

The tolerated window is closed. The engine now hands a dropped value back to the probe that
committed it — `Probe` may implement `Discard(T)`, called when a commit is refused against a
removed subject, when the run concluded `Skip` or returned the zero `Result`, and when it panicked.
`connectionProbe.Discard` retires the connection, and `Service.Close` also retires the engine's
copy, which covers a commit that landed after the pass worker stopped. What made it worth doing
was not the socket — that closes at `IdleConnTimeout` either way — but that the engine had no
contract for a value it drops, which every probe committing a resource would have to work around.

## Revisit when

The identity probes land: retiring a connection because the *server* behind unchanged
credentials changed is specified but unbuilt, and it needs a rule for a part filling in from empty
(the first identification, not a new server) plus a `Wake` from `publish` to make it prompt.
