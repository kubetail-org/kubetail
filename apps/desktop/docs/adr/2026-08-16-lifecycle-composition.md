---
title: Compose start/stop/close as one shape, with ctx bounding startup alone
date: 2026-08-16
scope: sidecar
status: Accepted
---

# Compose start/stop/close as one shape, with ctx bounding startup alone

## Context

The sidecar is a tree of things that run: the composition root owns the poke bus, the cluster
service, and the cloud service; the cluster service owns beehive and four controllers; a
controller owns its own machinery, such as the kubeconfig watcher and importer. Each level has to
start what it owns, drain it on shutdown within a deadline the caller sets, and then release what
the drain left.

Written by hand, each owner grew its own stop closure joining its children's, and start order and
stop order were encoded separately — in one place, reverse order was a comment about
left-to-right argument evaluation inside an `errors.Join`. The shape first appeared inside
`internal/services/cluster` around early August 2026; it was hoisted to `internal/lib/lifecycle` on this
ADR's date, when the composition root and two incoming services needed the same thing.

## Decision

One interface, `lifecycle.StartCloser`, worn at every level from the leaves up:

```go
type StartCloser interface {
	Start(ctx context.Context) (func(context.Context) error, error)
	io.Closer
}
```

Two methods, three phases: the stop func is `Start`'s return value. An owner holds its children in
a slice and composes with `lifecycle.StartAll` and `lifecycle.CloseAll`; **slice order is start
order, and stop and close run in reverse**. That single ordering rule is why poke's hub outlives
its subscribers, and why beehive outlives every controller that could still touch the store.

**`ctx` bounds startup, never the run.** Background work must outlive the context passed to
`Start` and end only when the stop func is called. A stop func must also be idempotent and must
wait with `drain.WithContext`, so that a caller's deadline bounds how long *it* waits without
abandoning work that is still running.

Two adapters carry things that nearly fit: `lifecycle.StartFunc` wraps a service whose stop func
releases everything, so it has nothing left to `Close`, and `cluster.beehiveRuntime` supplies
the `Close` that belongs to beehive's store rather than to the runtime over it.

## Alternatives considered

**A `Stop` method on the interface**, symmetrical with `Start`/`Close`. Rejected: it would be
callable before `Start`, and there is nothing sensible for it to do then. Returning the stop func
from `Start` makes the ordering unrepresentable rather than merely discouraged.

**`ctx` as the run context** — the natural reading, and what `context.WithCancel(ctx)` inside a
`Start` quietly assumes. Rejected because it takes the caller's ability to time-limit startup and
turns it into the ability to kill the service: an owner that bounds startup at five seconds gets a
poke bus that dies five seconds in. `poke.Service` and `cloud.Service` were both written this way
and had to be corrected when they joined one slice with services that were not, which is the
concrete cost of leaving the question open.

**Unwinding a failed start on the startup context.** Rejected: a start usually fails *because*
that context died, and draining on a dead context returns the instant it is asked to wait. The
owner would be told its children drained while they were still running, just as it went on to
close what they use. `unwind` detaches with `context.WithoutCancel` and applies its own budget,
because there is no caller-supplied deadline to inherit — the caller never received a stop func.

**Hand-written stop closures per owner.** Rejected: start order and stop order lived in separate
statements and could disagree silently, and adding a participant meant editing a closure rather
than a list.

**A no-op `Close` on services that have nothing to release**, instead of `StartFunc`. Rejected:
it puts a method on a leaf's public API that answers a question the leaf does not have — a reader
of `poke.Service.Close` would reasonably ask what closing the bus does, when the answer is
"nothing; the stop func already closed the hub".

**One package-level registry that owns every runnable.** Rejected: ownership is the tree. A
controller's machinery must stop before the controller, which a flat registry cannot express
without re-encoding the tree inside it.

## Consequences

Adding a participant is one line in a slice. An owner's `Start` and `Close` become one call each,
and reverse-order teardown is structural rather than a fact each owner restates.

The obligations this creates are all invariants a compiler cannot check, which is the cost:

- A `Start` that derives its run context from `ctx` breaks every owner above it, silently, and
  only when that owner bounds startup.
- A stop func that is not idempotent turns a retried or doubled drain into a panic.
- A stop func that blocks on a bare `sync.WaitGroup.Wait` ignores the caller's deadline and hangs
  shutdown for everything above it.
- A `Close` safe to call before the drain invites callers to skip the stop func.

Structural typing means no implementer names `lifecycle.StartCloser`, so nothing puts the godoc in
front of the next person to write a `Start`. The mitigation is to cite the contract at the
implementation, as `kubeconfig.Watcher`, `poke.Service`, and `cloud.Service` now do.

Every participant enters the slice as a named `lifecycle.Part`, and each phase wraps that name
around a failure. Naming lives in the composition rather than in each participant because the
alternative — each service wrapping its own errors — leaves whoever forgets unattributable, and
that is exactly what happened while this package was being assembled.

## Revisit when

A participant needs a phase these two methods cannot express — a readiness gate between start and
serve, or a restart that is not "close and rebuild". Adding a third method to serve one
participant would put the ordering trap back; a second, narrower interface alongside this one
would be the thing to weigh then.
