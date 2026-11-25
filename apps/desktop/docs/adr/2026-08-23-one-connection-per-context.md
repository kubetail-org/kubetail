---
title: One connection per kube-context, not per credential fingerprint
date: 2026-08-23
scope: sidecar
status: Accepted
---

# One connection per kube-context, not per credential fingerprint

## Context

[Connections are addressed by ClusterID](2026-08-22-connections-addressed-by-cluster-id.md) kept
the pool keyed on a credential fingerprint: two kube-contexts resolving to the same server, TLS
and auth shared one socket and one probe. It rejected one-entry-per-cluster on the grounds that
it "would dial and probe a server once per kube-context pointing at it, which is exactly the
waste the key exists to remove."

Built, that sharing costs a pool held three ways — `byFingerprint`, `byContext`, and
`entry.contexts` — three views of one relation that only agree because two functions are the
only writers. Every publish fans an entry's result out across its contexts, and both hubs are
keyed by context anyway, so the fan-out exists purely to undo the merge.

Two facts checked while weighing it:

**The expensive duplication is not ours.** client-go caches exec authenticators process-wide,
keyed by `(ExecConfig, Cluster)` (`plugin/pkg/client/auth/exec`). Two `rest.Config`s with
identical exec blocks share one authenticator and one cached token, so per-context connections do
not run the credential helper twice — which was the cost worth avoiding.

**Sharing is uncommon.** The fingerprint requires an identical server, CA and auth, so it fires
only for context aliases or namespace-scoped context sets against one cluster. In a kubeconfig
where contexts map one-to-one to clusters — the usual shape — the merging machinery never merges
anything.

## Decision

One entry per claimed context. `Service.pool` is a single map keyed by context name, holding the
holder count, the fingerprint the connection was built from, and the last `State`. Contexts that
resolve alike get a connection each.

The fingerprint moves from the key into the entry. It is what `rekey` compares to tell a
credential rotation from a kubeconfig write that changed nothing: unchanged keeps the connection
and its backoff ladder, changed builds a new one and forgets the old `State`, since the server
behind new credentials has yet to say anything. Empty means the context does not resolve.

## Alternatives considered

**Keep fingerprint keying.** Fewer sockets and fewer probes where contexts share a server. It
buys that with three indexes, a fan-out on every publish, and an attribution problem: bytes
measured at a shared socket cannot be split back across the clusters using it, so a throughput
gauge has to explain why two clusters report one number.

**Key on the pair, `(context, fingerprint)`.** The same outcome as this decision, spelled as a
composite key. Rejected as a spelling: the fingerprint is not an address — nothing looks a
connection up by it — so it belongs in the entry as the thing a rotation is detected against.

## Consequences

The pool is one map, `attachLocked`/`detachLocked` become build/drop, and `entry.contexts` and
the agreement invariant behind it are gone. Publishing is one entry to one key.

Attribution is exact. Anything measured at a socket — bytes, latency, error rate — belongs to one
context and so to one cluster record, with no apportioning. Planned work on per-connection
throughput loses its shared-socket caveat.

N contexts against one server means N handshakes and N probe cycles. Each cycle is a handful of
small requests, and every kubeconfig context is enabled on arrival, so a large namespace-scoped
kubeconfig is where this is worst — twenty namespace contexts against one cluster become twenty
sockets. The lever if that bites is cadence, slowing probes for clusters nobody is looking at,
which is wanted regardless and does not need sharing to deliver.

A failure belongs to one caller: one context's backoff ladder is its own, rather than shared with
whatever else resolved alike.

## Revisit when

A real kubeconfig makes the duplicate probe load visible on an API server, and cadence has
already been tuned. Sharing would then come back as a transport-level concern — one `http.Client`
behind several entries — rather than as a merged pool, so that attribution and failure isolation
survive it.
